package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
)

func TestRepositoryPersistsSpamWithoutTTL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	repo, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.SaveBatch(context.Background(), []*domain.Record{
		{PhoneNumber: "13800138000", Source: "sogou", Tag: "旧标签", Confidence: 100, HitCount: 1, FetchedAt: now},
		{PhoneNumber: "13900139000", Source: "sogou", Confidence: 0, FetchedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveBatch(context.Background(), []*domain.Record{
		{PhoneNumber: "13800138000", Source: "sogou", Tag: "新标签", Confidence: 100, HitCount: 2, FetchedAt: now.Add(time.Minute)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	repo, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	got, err := repo.ListByPhone(context.Background(), "13800138000")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Tag != "新标签" || got[0].HitCount != 2 {
		t.Fatalf("records = %#v", got)
	}
	if _, err := repo.ListByPhone(context.Background(), "13900139000"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("non-spam lookup error = %v", err)
	}
}

func TestRepositorySchemaExcludesTTLAndFeedback(t *testing.T) {
	repo, err := Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	rows, err := repo.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	joined := strings.Join(names, ",")
	for _, forbidden := range []string{"feedback", "token", "event"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("tables = %s", joined)
		}
	}

	columns, err := repo.db.Query(`PRAGMA table_info(query_records)`)
	if err != nil {
		t.Fatal(err)
	}
	defer columns.Close()
	for columns.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := columns.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(name, "expire") || strings.Contains(name, "ttl") {
			t.Fatalf("unexpected TTL column: %s", name)
		}
	}
}

func TestRepositorySupportsConcurrentWritesAndMetadata(t *testing.T) {
	repo, err := Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	var wg sync.WaitGroup
	for index := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			record := &domain.Record{
				PhoneNumber: fmt.Sprintf("138%08d", index),
				Source:      "sogou",
				Tag:         "营销",
				Confidence:  100,
			}
			if saveErr := repo.SaveBatch(context.Background(), []*domain.Record{record}); saveErr != nil {
				t.Errorf("save error: %v", saveErr)
			}
		}()
	}
	wg.Wait()

	if err := repo.SetMetadata(context.Background(), "instance_id", "instance-1"); err != nil {
		t.Fatal(err)
	}
	value, err := repo.GetMetadata(context.Background(), "instance_id")
	if err != nil || value != "instance-1" {
		t.Fatalf("metadata/error = %q/%v", value, err)
	}
	var journalMode string
	if err := repo.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(journalMode) != "wal" {
		t.Fatalf("journal mode = %q", journalMode)
	}
}

func TestEnsureInstanceIDIsStableAcrossConcurrentCalls(t *testing.T) {
	repo, err := Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	values := make(chan string, 16)
	errors := make(chan error, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, ensureErr := repo.EnsureInstanceID(context.Background())
			if ensureErr != nil {
				errors <- ensureErr
				return
			}
			values <- value
		}()
	}
	wg.Wait()
	close(values)
	close(errors)
	for ensureErr := range errors {
		if ensureErr != nil {
			t.Fatal(ensureErr)
		}
	}
	var first string
	for value := range values {
		if first == "" {
			first = value
			continue
		}
		if value != first {
			t.Fatalf("instance IDs differ: %q and %q", first, value)
		}
	}
	if first == "" {
		t.Fatal("no instance ID returned")
	}
}

func TestOpenEscapesSQLiteURIPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime#query&bar.db")
	repo, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err := repo.SaveBatch(context.Background(), []*domain.Record{{
		PhoneNumber: "13800138000",
		Source:      "sogou",
		Tag:         "marketing",
		Confidence:  100,
		HitCount:    1,
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ListByPhone(context.Background(), "13800138000"); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteFileURIEscapesPathCharacters(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("testdata", "runtime#query?source=foo&bar.db"))
	if err != nil {
		t.Fatal(err)
	}
	uri := sqliteFileURI(path, url.Values{"mode": {"rwc"}})
	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.ToSlash(path)
	if !strings.HasPrefix(wantPath, "/") {
		wantPath = "/" + wantPath
	}
	if parsed.Path != wantPath {
		t.Fatalf("URI path = %q, want %q", parsed.Path, wantPath)
	}
	if parsed.Query().Get("mode") != "rwc" {
		t.Fatalf("URI query = %q, want mode=rwc", parsed.RawQuery)
	}
}

func TestApplyMigrationsSkipsAppliedVersionsAndPreservesOrder(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrations := []migration{
		{version: 1, sql: `CREATE TABLE first_migration (value TEXT NOT NULL);`},
		{version: 2, sql: `INSERT INTO first_migration (value) VALUES ('second');`},
	}
	if err := applyMigrations(context.Background(), db, migrations[:1]); err != nil {
		t.Fatal(err)
	}
	if err := applyMigrations(context.Background(), db, migrations); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM first_migration WHERE value = 'second'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("second migration executions = %d, want 1", count)
	}
	rows, err := db.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var versions []int
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(versions) != "[1 2]" {
		t.Fatalf("migration versions = %v, want [1 2]", versions)
	}
}

func TestOpenSerializesConcurrentMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	start := make(chan struct{})
	errors := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			repository, err := Open(path)
			if err == nil {
				err = repository.Close()
			}
			errors <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatalf("concurrent Open failed: %v", err)
		}
	}
}
