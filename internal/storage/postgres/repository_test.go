package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestOpenRejectsEmptyDatabaseURL(t *testing.T) {
	if _, err := Open(context.Background(), "   "); err == nil {
		t.Fatal("expected error for empty database url")
	}
}

func TestOpenFailsOnUnreachableDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := Open(ctx, "postgres://postgres@127.0.0.1:1/mast_test?sslmode=disable"); err == nil {
		t.Fatal("expected error for unreachable database")
	}
}

func TestOpenMigratesIdempotently(t *testing.T) {
	dsn := testDatabaseURL(t)
	first, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	var firstCount int
	if err := first.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&firstCount); err != nil {
		t.Fatal(err)
	}
	if firstCount != 1 {
		t.Fatalf("migration count = %d, want 1", firstCount)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	var secondCount int
	if err := second.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&secondCount); err != nil {
		t.Fatal(err)
	}
	if secondCount != firstCount {
		t.Fatalf("migration count after reopen = %d, want %d", secondCount, firstCount)
	}
	var version int
	if err := second.db.QueryRow(`SELECT version FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
	}
}

func TestOpenSerializesConcurrentMigrations(t *testing.T) {
	dsn := testDatabaseURL(t)
	start := make(chan struct{})
	errs := make(chan error, 8)
	for range 8 {
		go func() {
			<-start
			repository, err := Open(context.Background(), dsn)
			if err == nil {
				err = repository.Close()
			}
			errs <- err
		}()
	}
	close(start)
	for range 8 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent Open failed: %v", err)
		}
	}
}

func TestRepositoryPersistsSpamWithoutTTL(t *testing.T) {
	dsn := testDatabaseURL(t)
	repo, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.SaveBatch(context.Background(), []*domain.Record{
		{PhoneNumber: "13800138000", Source: "sogou", Tag: "旧标签", Confidence: 100, HitCount: 1, FetchedAt: now},
		{PhoneNumber: "13900139000", Source: "sogou", Confidence: 0, FetchedAt: now},
		nil,
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

	repo, err = Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	got, err := repo.ListByPhone(context.Background(), "13800138000")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Tag != "新标签" || got[0].HitCount != 2 || got[0].Confidence != 100 {
		t.Fatalf("records = %#v", got)
	}
	if !got[0].FetchedAt.UTC().Equal(now.Add(time.Minute)) {
		t.Fatalf("fetched_at = %v, want %v", got[0].FetchedAt, now.Add(time.Minute))
	}
	if _, err := repo.ListByPhone(context.Background(), "13900139000"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("non-spam lookup error = %v", err)
	}
}

func TestRepositorySchemaExcludesTTLAndFeedback(t *testing.T) {
	repo := openTestRepository(t)
	rows, err := repo.db.Query(`
        SELECT table_name
        FROM information_schema.tables
        WHERE table_schema = current_schema()
        ORDER BY table_name`)
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
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(names, ",")
	for _, forbidden := range []string{"feedback", "token", "event"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("tables = %s", joined)
		}
	}

	columns, err := repo.db.Query(`
        SELECT column_name
        FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'query_records'`)
	if err != nil {
		t.Fatal(err)
	}
	defer columns.Close()
	for columns.Next() {
		var name string
		if err := columns.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(name, "expire") || strings.Contains(name, "ttl") {
			t.Fatalf("unexpected TTL column: %s", name)
		}
	}
	if err := columns.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestListByPhoneAndSourcesFilters(t *testing.T) {
	repo := openTestRepository(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.SaveBatch(context.Background(), []*domain.Record{
		{PhoneNumber: "13800138000", Source: "sogou", Tag: "营销", Confidence: 100, HitCount: 1, FetchedAt: now},
		{PhoneNumber: "13800138000", Source: "360", Tag: "骚扰", Confidence: 100, HitCount: 2, FetchedAt: now},
		{PhoneNumber: "13800138000", Source: "baidu", Tag: "中介", Confidence: 100, HitCount: 3, FetchedAt: now},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ListByPhoneAndSources(context.Background(), "13800138000", []string{"360", "sogou"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("records = %#v", got)
	}
	if got[0].Source != "360" || got[1].Source != "sogou" {
		t.Fatalf("order/sources = %#v", got)
	}

	if _, err := repo.ListByPhoneAndSources(context.Background(), "13800138000", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("empty sources error = %v", err)
	}
	if _, err := repo.ListByPhoneAndSources(context.Background(), "13800138000", []string{"missing"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown source error = %v", err)
	}
}

func TestListByPhoneNotFound(t *testing.T) {
	repo := openTestRepository(t)
	if _, err := repo.ListByPhone(context.Background(), "13800138000"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v", err)
	}
}

func TestListByPhoneRespectsCanceledContext(t *testing.T) {
	repo := openTestRepository(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := repo.ListByPhone(ctx, "13800138000")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestMetadataCRUD(t *testing.T) {
	repo := openTestRepository(t)
	ctx := context.Background()
	if _, err := repo.GetMetadata(ctx, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing metadata error = %v", err)
	}
	if err := repo.SetMetadata(ctx, "instance_id", "instance-1"); err != nil {
		t.Fatal(err)
	}
	value, err := repo.GetMetadata(ctx, "instance_id")
	if err != nil || value != "instance-1" {
		t.Fatalf("metadata/error = %q/%v", value, err)
	}
	if err := repo.SetMetadata(ctx, "instance_id", "instance-2"); err != nil {
		t.Fatal(err)
	}
	value, err = repo.GetMetadata(ctx, "instance_id")
	if err != nil || value != "instance-2" {
		t.Fatalf("updated metadata/error = %q/%v", value, err)
	}
	if err := repo.DeleteMetadata(ctx, "instance_id"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetMetadata(ctx, "instance_id"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted metadata error = %v", err)
	}
	if err := repo.DeleteMetadata(ctx, "instance_id"); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureInstanceIDIsStableAcrossConcurrentCalls(t *testing.T) {
	repo := openTestRepository(t)
	values := make(chan string, 16)
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, ensureErr := repo.EnsureInstanceID(context.Background())
			if ensureErr != nil {
				errs <- ensureErr
				return
			}
			values <- value
		}()
	}
	wg.Wait()
	close(values)
	close(errs)
	for ensureErr := range errs {
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
	again, err := repo.EnsureInstanceID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Fatalf("instance ID after concurrent ensure = %q, want %q", again, first)
	}
	stored, err := repo.GetMetadata(context.Background(), "instance_id")
	if err != nil || stored != first {
		t.Fatalf("stored instance ID = %q/%v, want %q", stored, err, first)
	}
}

func openTestRepository(t *testing.T) *Repository {
	t.Helper()
	repo, err := Open(context.Background(), testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repo.Close(); err != nil {
			t.Errorf("close repository: %v", err)
		}
	})
	return repo
}

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("MAST_TEST_DATABASE_URL"))
	if raw == "" {
		t.Skip("MAST_TEST_DATABASE_URL is not set")
	}
	schema := "mast_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := validateSchemaName(schema); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupDB, cleanupErr := sql.Open("pgx", raw)
		if cleanupErr != nil {
			t.Errorf("open cleanup database: %v", cleanupErr)
			return
		}
		defer cleanupDB.Close()
		if _, cleanupErr := cleanupDB.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); cleanupErr != nil {
			t.Errorf("drop schema %s: %v", schema, cleanupErr)
		}
	})
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse test database url: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func validateSchemaName(name string) error {
	if name == "" {
		return errors.New("schema name is required")
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return fmt.Errorf("unsafe schema name %q", name)
		}
	}
	return nil
}
