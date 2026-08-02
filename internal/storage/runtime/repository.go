// Package runtime 实现 Self-host 的可写 SQLite 查询缓存。
package runtime

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
	_ "github.com/glebarez/go-sqlite"
	"github.com/google/uuid"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version int
	sql     string
}

// Repository 使用 SQLite WAL 持久化 Provider 骚扰结果。
type Repository struct {
	db *sql.DB
}

var _ port.QueryRepository = (*Repository)(nil)

// Open 打开或创建 Runtime 数据库并执行显式 migration。
func Open(path string) (*Repository, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("runtime database path is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		return nil, fmt.Errorf("create runtime database directory: %w", err)
	}
	dsn := sqliteFileURI(absPath, url.Values{
		"_pragma": {"busy_timeout(5000)", "journal_mode(WAL)"},
	})
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open runtime database: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	if err := pingWithRetry(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping runtime database: %w", err)
	}
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("load runtime migrations: %w", err)
	}
	if err := applyMigrations(context.Background(), db, migrations); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate runtime database: %w", err)
	}
	if err := os.Chmod(absPath, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("restrict runtime database permissions: %w", err)
	}
	return &Repository{db: db}, nil
}

func pingWithRetry(db *sql.DB) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := db.Ping()
		if err == nil || !isSQLiteBusy(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func isSQLiteBusy(err error) bool {
	message := err.Error()
	return strings.Contains(message, "SQLITE_BUSY") || strings.Contains(message, "database is locked")
}

func loadMigrations(files fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(files, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migration directory: %w", err)
	}
	migrations := make([]migration, 0, len(entries))
	versions := make(map[int]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, found := strings.Cut(entry.Name(), "_")
		if !found {
			return nil, fmt.Errorf("migration %q must start with a version", entry.Name())
		}
		version, err := strconv.Atoi(prefix)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid migration version in %q", entry.Name())
		}
		if _, exists := versions[version]; exists {
			return nil, fmt.Errorf("duplicate migration version %d", version)
		}
		contents, err := fs.ReadFile(files, "migrations/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		versions[version] = struct{}{}
		migrations = append(migrations, migration{version: version, sql: string(contents)})
	}
	sort.Slice(migrations, func(left, right int) bool {
		return migrations[left].version < migrations[right].version
	})
	return migrations, nil
}

func applyMigrations(ctx context.Context, db *sql.DB, migrations []migration) error {
	connection, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer connection.Close()
	if err := withImmediateTransaction(ctx, connection, func() error {
		_, err := connection.ExecContext(ctx, `
            CREATE TABLE IF NOT EXISTS schema_migrations (
                version INTEGER NOT NULL PRIMARY KEY,
                applied_at TEXT NOT NULL
            )`)
		return err
	}); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	for _, migration := range migrations {
		if err := withImmediateTransaction(ctx, connection, func() error {
			var applied bool
			if err := connection.QueryRowContext(ctx,
				`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)`, migration.version,
			).Scan(&applied); err != nil {
				return fmt.Errorf("check migration %d: %w", migration.version, err)
			}
			if applied {
				return nil
			}
			if _, err := connection.ExecContext(ctx, migration.sql); err != nil {
				return fmt.Errorf("apply migration %d: %w", migration.version, err)
			}
			if _, err := connection.ExecContext(ctx,
				`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
				migration.version, time.Now().UTC().Format(time.RFC3339Nano),
			); err != nil {
				return fmt.Errorf("record migration %d: %w", migration.version, err)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func withImmediateTransaction(ctx context.Context, connection *sql.Conn, operation func() error) (err error) {
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer func() {
		if err == nil {
			return
		}
		if _, rollbackErr := connection.ExecContext(ctx, "ROLLBACK"); rollbackErr != nil {
			err = fmt.Errorf("%w; rollback migration transaction: %v", err, rollbackErr)
		}
	}()
	if err := operation(); err != nil {
		return err
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit migration transaction: %w", err)
	}
	return nil
}

func sqliteFileURI(path string, query url.Values) string {
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := &url.URL{Scheme: "file", Path: uriPath}
	uri.RawQuery = query.Encode()
	return uri.String()
}

// Close 关闭 SQLite 连接池。
func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) ListByPhone(ctx context.Context, phone string) ([]*domain.Record, error) {
	return r.list(ctx, `
        SELECT phone_number, source, tag, confidence, hit_count, fetched_at
        FROM query_records
        WHERE phone_number = ?
        ORDER BY source`, []any{phone})
}

func (r *Repository) ListByPhoneAndSources(ctx context.Context, phone string, sources []string) ([]*domain.Record, error) {
	if len(sources) == 0 {
		return nil, domain.ErrNotFound
	}
	placeholders := make([]string, len(sources))
	arguments := make([]any, 0, len(sources)+1)
	arguments = append(arguments, phone)
	for index, source := range sources {
		placeholders[index] = "?"
		arguments = append(arguments, source)
	}
	query := `
        SELECT phone_number, source, tag, confidence, hit_count, fetched_at
        FROM query_records
        WHERE phone_number = ? AND source IN (` + strings.Join(placeholders, ",") + `)
        ORDER BY source`
	return r.list(ctx, query, arguments)
}

func (r *Repository) list(ctx context.Context, query string, arguments []any) ([]*domain.Record, error) {
	rows, err := r.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query runtime records: %w", err)
	}
	defer rows.Close()

	records := make([]*domain.Record, 0)
	for rows.Next() {
		var record domain.Record
		var fetchedAt string
		if err := rows.Scan(&record.PhoneNumber, &record.Source, &record.Tag, &record.Confidence, &record.HitCount, &fetchedAt); err != nil {
			return nil, fmt.Errorf("scan runtime record: %w", err)
		}
		record.FetchedAt, err = time.Parse(time.RFC3339Nano, fetchedAt)
		if err != nil {
			return nil, fmt.Errorf("parse runtime record timestamp: %w", err)
		}
		records = append(records, &record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime records: %w", err)
	}
	if len(records) == 0 {
		return nil, domain.ErrNotFound
	}
	return records, nil
}

// SaveBatch 只保存骚扰结果，并按 phone_number + source 执行 upsert。
func (r *Repository) SaveBatch(ctx context.Context, records []*domain.Record) error {
	spamRecords := make([]*domain.Record, 0, len(records))
	for _, record := range records {
		if record != nil && record.IsSpam() {
			spamRecords = append(spamRecords, record)
		}
	}
	if len(spamRecords) == 0 {
		return nil
	}

	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin runtime write: %w", err)
	}
	defer transaction.Rollback()
	statement, err := transaction.PrepareContext(ctx, `
        INSERT INTO query_records (
            phone_number, source, tag, confidence, hit_count, fetched_at
        ) VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT(phone_number, source) DO UPDATE SET
            tag = excluded.tag,
            confidence = excluded.confidence,
            hit_count = excluded.hit_count,
            fetched_at = excluded.fetched_at`)
	if err != nil {
		return fmt.Errorf("prepare runtime upsert: %w", err)
	}
	defer statement.Close()
	for _, record := range spamRecords {
		fetchedAt := record.FetchedAt
		if fetchedAt.IsZero() {
			fetchedAt = time.Now().UTC()
		}
		if _, err := statement.ExecContext(ctx, record.PhoneNumber, record.Source, record.Tag,
			record.Confidence, record.HitCount, fetchedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("upsert runtime record: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit runtime write: %w", err)
	}
	return nil
}

// SetMetadata 保存实例和 baseline 生命周期元数据。
func (r *Repository) SetMetadata(ctx context.Context, key, value string) error {
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO runtime_metadata (key, value, updated_at)
        VALUES (?, ?, ?)
        ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("set runtime metadata: %w", err)
	}
	return nil
}

// GetMetadata 读取 Runtime 元数据。
func (r *Repository) GetMetadata(ctx context.Context, key string) (string, error) {
	var value string
	if err := r.db.QueryRowContext(ctx, `SELECT value FROM runtime_metadata WHERE key = ?`, key).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", domain.ErrNotFound
		}
		return "", fmt.Errorf("get runtime metadata: %w", err)
	}
	return value, nil
}

// EnsureInstanceID 返回并持久化该实例唯一身份。
func (r *Repository) EnsureInstanceID(ctx context.Context) (string, error) {
	value, err := r.GetMetadata(ctx, "instance_id")
	if err == nil && strings.TrimSpace(value) != "" {
		return value, nil
	}
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return "", err
	}
	value = uuid.NewString()
	if err := r.SetMetadata(ctx, "instance_id", value); err != nil {
		return "", err
	}
	return value, nil
}
