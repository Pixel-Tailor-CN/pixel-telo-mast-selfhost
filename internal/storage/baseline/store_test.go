package baseline

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/go-sqlite"
)

func TestStoreLoadsReadOnlyBaseline(t *testing.T) {
	path := createBaselineDatabase(t, "20260801000000", "13800138000", "营销", "sogou")
	store := NewStore()
	if err := store.Replace(path); err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.ActiveVersion() != "20260801000000" {
		t.Fatalf("version = %q", store.ActiveVersion())
	}
	records, err := store.ListByPhone(context.Background(), "13800138000")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Source != "sogou" || records[0].Confidence != 100 {
		t.Fatalf("records = %#v", records)
	}
	if _, err := store.active.db.Exec(`DELETE FROM spam_numbers`); err == nil {
		t.Fatal("baseline connection must be read-only")
	}
}

func createBaselineDatabase(t *testing.T, version, phone, tag, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "baseline.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
        CREATE TABLE spam_numbers (
            phone_number TEXT NOT NULL PRIMARY KEY,
            tag TEXT NOT NULL,
            source TEXT NOT NULL
        );
        CREATE TABLE metadata (
            key TEXT NOT NULL PRIMARY KEY,
            value TEXT NOT NULL
	        );`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO metadata (key, value) VALUES ('version', ?)`, version); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO spam_numbers (phone_number, tag, source) VALUES (?, ?, ?)`, phone, tag, source); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
