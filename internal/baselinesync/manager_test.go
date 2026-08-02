package baselinesync

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/storage/baseline"
	_ "github.com/glebarez/go-sqlite"
)

type fakeClient struct {
	manifest Manifest
	archive  []byte
	checkErr error
}

func (f *fakeClient) Check(context.Context, string) (Manifest, error) {
	if f.checkErr != nil {
		return Manifest{}, f.checkErr
	}
	return f.manifest, nil
}

func (f *fakeClient) Download(_ context.Context, _ string, dst io.Writer) (int64, error) {
	n, err := dst.Write(f.archive)
	return int64(n), err
}

func TestSyncFailureKeepsPreviousDatabase(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "baseline.db")
	writeDatabase(t, active, "20260729000000", "old")
	store := baseline.NewStore()
	if err := store.Replace(active); err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := NewManager(Options{Client: &fakeClient{checkErr: errors.New("download failed")}, Store: store, ActivePath: active})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Sync(context.Background()); err == nil {
		t.Fatal("sync should fail")
	}
	if got := store.ActiveVersion(); got != "20260729000000" {
		t.Fatalf("active version = %s", got)
	}
}

func TestSyncReplacesOnlyAfterCompleteValidation(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "baseline.db")
	writeDatabase(t, active, "20260729000000", "old")
	store := baseline.NewStore()
	if err := store.Replace(active); err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	archive := makeArchive(t, "20260730000000", "new")
	hash := sha256.Sum256(archive)
	manager, err := NewManager(Options{
		Client: &fakeClient{manifest: Manifest{HasUpdate: true, LatestVersion: "20260730000000", DownloadURL: "https://example.test/baseline.zip", SizeBytes: int64(len(archive)), Checksum: hex.EncodeToString(hash[:])}, archive: archive},
		Store:  store, ActivePath: active,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.ActiveVersion(); got != "20260730000000" {
		t.Fatalf("active version = %s", got)
	}
}

func writeDatabase(t *testing.T, path, version, tag string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE spam_numbers (phone_number TEXT NOT NULL, tag TEXT NOT NULL, source TEXT NOT NULL, PRIMARY KEY(phone_number)); CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL); INSERT INTO metadata(key,value) VALUES ('version', ?); INSERT INTO spam_numbers VALUES ('13800138000', ?, 'sogou')`, version, tag)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func makeArchive(t *testing.T, version, tag string) []byte {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "baseline.db")
	writeDatabase(t, dbPath, version, tag)
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	entry, err := archive.Create("baseline.db")
	if err != nil {
		t.Fatal(err)
	}
	db, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(db); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
