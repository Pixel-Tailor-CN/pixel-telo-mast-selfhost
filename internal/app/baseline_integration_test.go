package app

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/baselinesync"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/storage/runtime"
	_ "github.com/glebarez/go-sqlite"
)

func TestBuildRestoresBaselineAndQueryUsesIt(t *testing.T) {
	dir := t.TempDir()
	runtimePath := filepath.Join(dir, "runtime.db")
	baselinePath := filepath.Join(dir, "baseline-20260807000000.db")
	writeIntegrationBaseline(t, baselinePath)

	runtimeRepo, err := runtime.Open(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimeRepo.SetMetadata(context.Background(), baselinesync.ActivePathMetadataKey, baselinePath); err != nil {
		runtimeRepo.Close()
		t.Fatal(err)
	}
	if err := runtimeRepo.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	cfg.Server.Listen = "127.0.0.1:0"
	cfg.Auth.TokenFile = filepath.Join(dir, "token")
	cfg.TLS.Mode = "off"
	cfg.Storage.RuntimePath = runtimePath
	cfg.Baseline.Enabled = true
	cfg.Baseline.CheckInterval = config.Duration(time.Hour)
	cfg.Query.Timeout = config.Duration(2 * time.Second)
	cfg.Query.MaxConcurrent = 1
	cfg.RateLimit.RequestsPerSecond = 1
	cfg.RateLimit.Burst = 1
	cfg.Upstream.ProviderIDs = []string{"sogou"}

	application, err := Build(Options{Config: cfg, Token: []byte("test-token")})
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close(context.Background())

	result, err := application.query.LookupWithSources(context.Background(), "13800138000", []string{"sogou"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Record == nil || result.Record.Tag != "integration" || result.Record.Source != "sogou" {
		t.Fatalf("baseline result = %+v", result.Record)
	}
}

func TestBuildAllowsMissingBaselineFile(t *testing.T) {
	dir := t.TempDir()
	cfg := integrationConfig(dir, filepath.Join(dir, "missing.db"))
	runtimeRepo, err := runtime.Open(cfg.Storage.RuntimePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimeRepo.SetMetadata(context.Background(), baselinesync.ActivePathMetadataKey, filepath.Join(dir, "missing-baseline.db")); err != nil {
		runtimeRepo.Close()
		t.Fatal(err)
	}
	runtimeRepo.Close()

	application, err := Build(Options{Config: cfg, Token: []byte("test-token")})
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close(context.Background())
	if application.baseline.ActivePath() != "" {
		t.Fatalf("missing baseline was loaded: %s", application.baseline.ActivePath())
	}
}

func TestBuildRejectsCorruptBaseline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.db")
	if err := os.WriteFile(path, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := integrationConfig(dir, path)
	runtimeRepo, err := runtime.Open(cfg.Storage.RuntimePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimeRepo.SetMetadata(context.Background(), baselinesync.ActivePathMetadataKey, path); err != nil {
		runtimeRepo.Close()
		t.Fatal(err)
	}
	runtimeRepo.Close()
	if _, err := Build(Options{Config: cfg, Token: []byte("test-token")}); err == nil {
		t.Fatal("corrupt baseline should prevent startup")
	}
}

func integrationConfig(dir, baselinePath string) *config.Config {
	cfg := &config.Config{}
	cfg.Server.Listen = "127.0.0.1:0"
	cfg.Auth.TokenFile = filepath.Join(dir, "token")
	cfg.TLS.Mode = "off"
	cfg.Storage.RuntimePath = filepath.Join(dir, "runtime.db")
	cfg.Baseline.Enabled = true
	cfg.Baseline.CheckInterval = config.Duration(time.Hour)
	cfg.Query.Timeout = config.Duration(2 * time.Second)
	cfg.Query.MaxConcurrent = 1
	cfg.RateLimit.RequestsPerSecond = 1
	cfg.RateLimit.Burst = 1
	cfg.Upstream.ProviderIDs = []string{"sogou"}
	return cfg
}

func writeIntegrationBaseline(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
        CREATE TABLE spam_numbers (phone_number TEXT NOT NULL, tag TEXT NOT NULL, source TEXT NOT NULL, PRIMARY KEY(phone_number));
        CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);
        INSERT INTO metadata(key, value) VALUES ('version', '20260807000000');
        INSERT INTO spam_numbers(phone_number, tag, source) VALUES ('13800138000', 'integration', 'sogou')`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}
