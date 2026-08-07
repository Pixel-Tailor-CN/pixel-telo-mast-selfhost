// Package baseline 实现官方离线库的本地只读查询和句柄切换。
package baseline

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	_ "github.com/glebarez/go-sqlite"
)

type snapshot struct {
	db      *sql.DB
	path    string
	version string
}

// Store 持有当前活动 baseline 的只读 SQLite 句柄。
type Store struct {
	mu           sync.RWMutex
	active       *snapshot
	closed       bool
	openSnapshot func(path string) (*snapshot, error)
}

var errStoreClosed = errors.New("baseline store is closed")

// NewStore 创建尚未加载 baseline 的 Store。
func NewStore() *Store {
	return &Store{openSnapshot: openSnapshot}
}

// Replace 打开新 baseline 并原子替换活动查询句柄。
func (s *Store) Replace(path string) error {
	s.mu.RLock()
	closed := s.closed
	opener := s.openSnapshot
	s.mu.RUnlock()
	if closed {
		return errStoreClosed
	}
	if opener == nil {
		opener = openSnapshot
	}
	next, err := opener(path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		if err := next.db.Close(); err != nil {
			return fmt.Errorf("close unopened baseline snapshot: %w", err)
		}
		return errStoreClosed
	}
	previous := s.active
	s.active = next
	s.mu.Unlock()
	if previous != nil {
		return previous.db.Close()
	}
	return nil
}

// Clear 清除当前活动 baseline。
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errStoreClosed
	}
	previous := s.active
	s.active = nil
	if previous != nil {
		return previous.db.Close()
	}
	return nil
}

// ActiveVersion 返回当前 baseline metadata 中的版本。
func (s *Store) ActiveVersion() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.active == nil {
		return ""
	}
	return s.active.version
}

// ActivePath 返回当前活动文件路径。
func (s *Store) ActivePath() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.active == nil {
		return ""
	}
	return s.active.path
}

func (s *Store) ListByPhone(ctx context.Context, phone string) ([]*domain.Record, error) {
	return s.list(ctx, `
        SELECT phone_number, tag, source
        FROM spam_numbers
        WHERE phone_number = ?`, []any{phone})
}

func (s *Store) ListByPhoneAndSources(ctx context.Context, phone string, sources []string) ([]*domain.Record, error) {
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
	return s.list(ctx, `
        SELECT phone_number, tag, source
        FROM spam_numbers
        WHERE phone_number = ? AND source IN (`+strings.Join(placeholders, ",")+`)`, arguments)
}

func (s *Store) list(ctx context.Context, query string, arguments []any) ([]*domain.Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.active == nil {
		return nil, domain.ErrNotFound
	}
	rows, err := s.active.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query baseline records: %w", err)
	}
	defer rows.Close()
	records := make([]*domain.Record, 0)
	for rows.Next() {
		record := &domain.Record{Confidence: 100, HitCount: 1}
		if err := rows.Scan(&record.PhoneNumber, &record.Tag, &record.Source); err != nil {
			return nil, fmt.Errorf("scan baseline record: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate baseline records: %w", err)
	}
	if len(records) == 0 {
		return nil, domain.ErrNotFound
	}
	return records, nil
}

// Close 关闭活动 baseline 句柄。
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.active == nil {
		return nil
	}
	err := s.active.db.Close()
	s.active = nil
	return err
}

func openSnapshot(path string) (*snapshot, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve baseline path: %w", err)
	}
	dsn := sqliteFileURI(absPath, url.Values{
		"mode":      {"ro"},
		"immutable": {"1"},
		"_pragma":   {"query_only(1)", "busy_timeout(5000)"},
	})
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open baseline database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping baseline database: %w", err)
	}
	var version string
	if err := db.QueryRow(`SELECT value FROM metadata WHERE key = 'version'`).Scan(&version); err != nil {
		db.Close()
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("baseline version metadata is missing")
		}
		return nil, fmt.Errorf("read baseline version: %w", err)
	}
	return &snapshot{db: db, path: absPath, version: version}, nil
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
