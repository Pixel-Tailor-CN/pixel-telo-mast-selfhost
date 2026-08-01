package runtime

import (
	"context"
	"errors"
	"fmt"
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
