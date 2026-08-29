// Package postgres 实现 Self-host 的 PostgreSQL 查询缓存。
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const (
	maxOpenConns    = 2
	maxIdleConns    = 1
	connMaxLifetime = 5 * time.Minute
	connMaxIdleTime = 30 * time.Second
	pingTimeout     = 5 * time.Second
)

// Repository 使用 PostgreSQL 持久化 Provider 骚扰结果和实例元数据。
type Repository struct {
	db *sql.DB
}

var _ port.QueryRepository = (*Repository)(nil)

// Open 打开 PostgreSQL 连接池并执行显式 migration。
func Open(ctx context.Context, databaseURL string) (*Repository, error) {
	databaseURL = strings.TrimSpace(databaseURL)
	if err := validateDatabaseURL(databaseURL); err != nil {
		return nil, err
	}
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		// 不 wrap pgx 解析错误，避免无效参数把完整 DSN 或密码带进日志。
		return nil, errors.New("postgres database url is invalid")
	}
	db := stdlib.OpenDB(*config)
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(connMaxLifetime)
	db.SetConnMaxIdleTime(connMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("load postgres migrations: %w", err)
	}
	if err := applyMigrations(ctx, db, migrations); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate postgres database: %w", err)
	}
	return &Repository{db: db}, nil
}

func validateDatabaseURL(databaseURL string) error {
	if databaseURL == "" {
		return errors.New("postgres database url is required")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		// 不 wrap net/url 错误，避免畸形转义把完整 DSN 或密码带进日志。
		return errors.New("postgres database url is invalid")
	}
	switch parsed.Scheme {
	case "postgres", "postgresql":
		return nil
	default:
		return errors.New("postgres database url is required")
	}
}

// Close 关闭 PostgreSQL 连接池。
func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) ListByPhone(ctx context.Context, phone string) ([]*domain.Record, error) {
	return r.list(ctx, `
        SELECT phone_number, source, tag, confidence, hit_count, fetched_at
        FROM query_records
        WHERE phone_number = $1
        ORDER BY source`, phone)
}

func (r *Repository) ListByPhoneAndSources(ctx context.Context, phone string, sources []string) ([]*domain.Record, error) {
	if len(sources) == 0 {
		return nil, domain.ErrNotFound
	}
	return r.list(ctx, `
        SELECT phone_number, source, tag, confidence, hit_count, fetched_at
        FROM query_records
        WHERE phone_number = $1 AND source = ANY($2)
        ORDER BY source`, phone, sources)
}

func (r *Repository) list(ctx context.Context, query string, arguments ...any) ([]*domain.Record, error) {
	rows, err := r.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query postgres records: %w", err)
	}
	defer rows.Close()

	records := make([]*domain.Record, 0)
	for rows.Next() {
		var record domain.Record
		if err := rows.Scan(&record.PhoneNumber, &record.Source, &record.Tag, &record.Confidence, &record.HitCount, &record.FetchedAt); err != nil {
			return nil, fmt.Errorf("scan postgres record: %w", err)
		}
		records = append(records, &record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgres records: %w", err)
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
		return fmt.Errorf("begin postgres write: %w", err)
	}
	defer transaction.Rollback()
	statement, err := transaction.PrepareContext(ctx, `
        INSERT INTO query_records (
            phone_number, source, tag, confidence, hit_count, fetched_at
        ) VALUES ($1, $2, $3, $4, $5, $6)
        ON CONFLICT (phone_number, source) DO UPDATE SET
            tag = EXCLUDED.tag,
            confidence = EXCLUDED.confidence,
            hit_count = EXCLUDED.hit_count,
            fetched_at = EXCLUDED.fetched_at`)
	if err != nil {
		return fmt.Errorf("prepare postgres upsert: %w", err)
	}
	defer statement.Close()
	for _, record := range spamRecords {
		fetchedAt := record.FetchedAt
		if fetchedAt.IsZero() {
			fetchedAt = time.Now().UTC()
		} else {
			fetchedAt = fetchedAt.UTC()
		}
		if _, err := statement.ExecContext(ctx, record.PhoneNumber, record.Source, record.Tag,
			record.Confidence, record.HitCount, fetchedAt); err != nil {
			return fmt.Errorf("upsert postgres record: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit postgres write: %w", err)
	}
	return nil
}

// SetMetadata 保存实例生命周期元数据。
func (r *Repository) SetMetadata(ctx context.Context, key, value string) error {
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO runtime_metadata (key, value, updated_at)
        VALUES ($1, $2, $3)
        ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`,
		key, value, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("set postgres metadata: %w", err)
	}
	return nil
}

// GetMetadata 读取 Runtime 元数据。
func (r *Repository) GetMetadata(ctx context.Context, key string) (string, error) {
	var value string
	if err := r.db.QueryRowContext(ctx, `SELECT value FROM runtime_metadata WHERE key = $1`, key).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", domain.ErrNotFound
		}
		return "", fmt.Errorf("get postgres metadata: %w", err)
	}
	return value, nil
}

// DeleteMetadata 删除运行时元数据。
func (r *Repository) DeleteMetadata(ctx context.Context, key string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM runtime_metadata WHERE key = $1`, key); err != nil {
		return fmt.Errorf("delete postgres metadata: %w", err)
	}
	return nil
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
	candidate := uuid.NewString()
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin instance identity write: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `
        INSERT INTO runtime_metadata (key, value, updated_at)
        VALUES ($1, $2, $3)
        ON CONFLICT (key) DO UPDATE SET
            value = EXCLUDED.value,
            updated_at = EXCLUDED.updated_at
        WHERE runtime_metadata.value ~ '^[[:space:]]*$'`,
		"instance_id", candidate, time.Now().UTC()); err != nil {
		return "", fmt.Errorf("persist instance identity: %w", err)
	}
	if err := transaction.QueryRowContext(ctx, `SELECT value FROM runtime_metadata WHERE key = $1`, "instance_id").Scan(&value); err != nil {
		return "", fmt.Errorf("read persisted instance identity: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return "", fmt.Errorf("commit instance identity: %w", err)
	}
	return value, nil
}
