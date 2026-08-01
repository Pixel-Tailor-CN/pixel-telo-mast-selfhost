// Package runtime 实现 Self-host 的可写 SQLite 查询缓存。
package runtime

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
	_ "github.com/glebarez/go-sqlite"
)

//go:embed migrations/000001_init.sql
var initialMigration string

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
	dsn := "file:" + filepath.ToSlash(absPath) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open runtime database: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping runtime database: %w", err)
	}
	if _, err := db.Exec(initialMigration); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate runtime database: %w", err)
	}
	if err := os.Chmod(absPath, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("restrict runtime database permissions: %w", err)
	}
	return &Repository{db: db}, nil
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
