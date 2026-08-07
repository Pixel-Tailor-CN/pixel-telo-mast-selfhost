package baseline

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
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

func TestStoreCloseIsTerminalDuringReplace(t *testing.T) {
	store := NewStore()
	candidateDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	opened := make(chan struct{})
	release := make(chan struct{})
	store.openSnapshot = func(string) (*snapshot, error) {
		close(opened)
		<-release
		return &snapshot{db: candidateDB, path: "candidate", version: "next"}, nil
	}

	replaceResult := make(chan error, 1)
	go func() {
		replaceResult <- store.Replace("ignored")
	}()
	<-opened
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-replaceResult; !errors.Is(err, errStoreClosed) {
		t.Fatalf("replace error = %v, want errStoreClosed", err)
	}
	if store.ActivePath() != "" || store.ActiveVersion() != "" {
		t.Fatalf("closed store restored an active snapshot: path=%q version=%q", store.ActivePath(), store.ActiveVersion())
	}
	if err := candidateDB.Ping(); err == nil {
		t.Fatal("candidate database must be closed after terminal Close")
	}
}

func TestStoreEscapesSQLiteURIPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline#query&bar.db")
	createBaselineDatabaseAt(t, path, "20260801000000", "13800138000", "marketing", "sogou")
	store := NewStore()
	if err := store.Replace(path); err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.ListByPhone(context.Background(), "13800138000"); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteFileURIEscapesPathCharacters(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("testdata", "baseline#query?source=foo&bar.db"))
	if err != nil {
		t.Fatal(err)
	}
	uri := sqliteFileURI(path, url.Values{"mode": {"ro"}})
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
	if parsed.Query().Get("mode") != "ro" {
		t.Fatalf("URI query = %q, want mode=ro", parsed.RawQuery)
	}
}

func createBaselineDatabase(t *testing.T, version, phone, tag, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "baseline.db")
	createBaselineDatabaseAt(t, path, version, phone, tag, source)
	return path
}

func createBaselineDatabaseAt(t *testing.T, path, version, phone, tag, source string) {
	t.Helper()
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
}
