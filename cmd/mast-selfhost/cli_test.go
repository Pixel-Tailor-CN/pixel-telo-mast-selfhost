package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/security"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/storage/runtime"
)

func TestPrepareServeAndPairingPersistIdentityAndTLS(t *testing.T) {
	dir := t.TempDir()
	if err := runInit([]string{"--dir", dir}); err != nil {
		t.Fatal(err)
	}
	makeTestConfigRunnable(t, filepath.Join(dir, "config.yaml"))

	configPath := filepath.Join(dir, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configured := strings.Replace(string(data), "mode: \"off\"", "mode: \"auto\"", 1)
	configured = strings.Replace(configured, "public_url: \"\"", "public_url: \"https://127.0.0.1:9443\"", 1)
	if err := os.WriteFile(configPath, []byte(configured), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, token, tlsConfig, err := prepareServe(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if tlsConfig == nil {
		t.Fatal("auto TLS config is nil")
	}
	certFile, keyFile := cfg.TLSFiles()
	for _, path := range []string{certFile, keyFile} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("TLS file %s: %v", path, err)
		}
	}
	certBefore, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatal(err)
	}
	first, err := buildPairingInfo(cfg, token)
	if err != nil {
		t.Fatal(err)
	}
	if first.URL != "https://127.0.0.1:9443" || first.Token == "" || first.InstanceID == "" || first.SPKIPin == "" {
		t.Fatalf("pairing info = %#v", first)
	}

	repo, err := runtime.Open(cfg.Storage.RuntimePath)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.EnsureInstanceID(context.Background())
	_ = repo.Close()
	if err != nil {
		t.Fatal(err)
	}
	if persisted != first.InstanceID {
		t.Fatalf("persisted instance ID = %q, pairing = %q", persisted, first.InstanceID)
	}

	second, err := buildPairingInfo(cfg, token)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("pairing changed after restart: first=%#v second=%#v", first, second)
	}
	certAfter, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(certBefore) != sha256.Sum256(certAfter) {
		t.Fatal("auto TLS certificate changed across pairing calls")
	}
	if _, err := security.CertificateSPKI(certFile); err != nil {
		t.Fatal(err)
	}
}

func TestInitUsesConfiguredPaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "O'Brien")
	if err := runInit([]string{"--dir", dir}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "O'Brien") || !strings.Contains(string(data), "token") || !strings.Contains(string(data), "runtime.db") {
		t.Fatalf("configured paths missing: %s", data)
	}
}

func TestInitOnlyCreatesConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := runInit([]string{"--dir", dir}); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.yaml" {
		t.Fatalf("initialized files = %#v", entries)
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "provider_ids: []") {
		t.Fatalf("init should require explicit providers: %s", data)
	}
	for name, path := range map[string]string{
		"token":   filepath.Join(dir, "token"),
		"runtime": filepath.Join(dir, "runtime.db"),
	} {
		if !filepath.IsAbs(path) {
			t.Fatalf("%s path is not absolute: %q", name, path)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should not exist after init: %v", name, err)
		}
	}
	certFile := filepath.Join(dir, "runtime.db.crt")
	keyFile := filepath.Join(dir, "runtime.db.key")
	for name, path := range map[string]string{"certificate": certFile, "private key": keyFile} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should not exist after init: %v", name, err)
		}
	}
}

func TestInitRejectsExistingConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	want := []byte("existing config\n")
	if err := os.WriteFile(configPath, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runInit([]string{"--dir", dir}); err == nil {
		t.Fatal("existing config should not be overwritten")
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("existing config changed: %q", got)
	}
}

func TestInitRejectsPublicURLFlag(t *testing.T) {
	if err := runInit([]string{"--dir", t.TempDir(), "--public-url", "https://127.0.0.1:9443"}); err == nil {
		t.Fatal("removed --public-url flag should be rejected")
	}
}

func TestPrepareServeRejectsMissingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")
	if _, _, _, err := prepareServe(path); err == nil {
		t.Fatal("missing config should be rejected")
	}
}

func TestPrepareServeRejectsDirectory(t *testing.T) {
	if _, _, _, err := prepareServe(t.TempDir()); err == nil {
		t.Fatal("config directory should be rejected")
	}
}

func TestPrepareServeValidatesBeforeCreatingToken(t *testing.T) {
	dir := t.TempDir()
	if err := runInit([]string{"--dir", dir}); err != nil {
		t.Fatal(err)
	}
	makeTestConfigRunnable(t, filepath.Join(dir, "config.yaml"))
	configPath := filepath.Join(dir, "config.yaml")
	file, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("unknown: true\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := prepareServe(configPath); err == nil {
		t.Fatal("invalid config should be rejected")
	}
	if _, err := os.Stat(filepath.Join(dir, "token")); !os.IsNotExist(err) {
		t.Fatalf("token should not be created for invalid config: %v", err)
	}
	for name, path := range map[string]string{
		"runtime":     filepath.Join(dir, "runtime.db"),
		"certificate": filepath.Join(dir, "runtime.db.crt"),
		"private key": filepath.Join(dir, "runtime.db.key"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should not be created for invalid config: %v", name, err)
		}
	}
}

func TestPrepareServeCreatesAndReusesToken(t *testing.T) {
	dir := t.TempDir()
	if err := runInit([]string{"--dir", dir}); err != nil {
		t.Fatal(err)
	}
	makeTestConfigRunnable(t, filepath.Join(dir, "config.yaml"))
	configPath := filepath.Join(dir, "config.yaml")
	cfg, first, tlsConfig, err := prepareServe(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if tlsConfig != nil {
		t.Fatalf("TLS config = %#v", tlsConfig)
	}
	if len(first) != 64 {
		t.Fatalf("token length = %d", len(first))
	}
	secondConfig, second, _, err := prepareServe(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Fatal("token changed during repeated serve preparation")
	}
	if secondConfig.Auth.TokenFile != cfg.Auth.TokenFile {
		t.Fatal("config changed during repeated serve preparation")
	}
}

func makeTestConfigRunnable(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), "provider_ids: []", "provider_ids: [\"sogou\"]", 1)
	if updated == string(data) {
		t.Fatal("empty provider list was not found")
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestServeConfigPathUsesDataDirectory(t *testing.T) {
	got, err := serveConfigPath(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "config.yaml" {
		t.Fatalf("default config path = %q", got)
	}

	dir := filepath.Join(t.TempDir(), "data")
	got, err = serveConfigPath([]string{"--dir", dir})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "config.yaml")
	if got != want {
		t.Fatalf("config path = %q, want %q", got, want)
	}
}

func TestServeConfigPathRejectsConfigFlag(t *testing.T) {
	if _, err := serveConfigPath([]string{"--config", "config.yaml"}); err == nil {
		t.Fatal("removed --config flag should be rejected")
	}
}

func TestPairingUsesDataDirectory(t *testing.T) {
	command := newPairingCommand()
	if err := command.ParseFlags(nil); err != nil {
		t.Fatal(err)
	}
	dir, err := command.Flags().GetString("dir")
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Join(dir, "config.yaml"); got != "config.yaml" {
		t.Fatalf("default pairing config path = %q", got)
	}

	command = newPairingCommand()
	if err := command.ParseFlags([]string{"--dir", filepath.Join("data", "instance")}); err != nil {
		t.Fatal(err)
	}
	dir, err = command.Flags().GetString("dir")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("data", "instance", "config.yaml")
	if got := filepath.Join(dir, "config.yaml"); got != want {
		t.Fatalf("pairing config path = %q, want %q", got, want)
	}
}

func TestPairingRejectsConfigFlag(t *testing.T) {
	command := newPairingCommand()
	if err := command.ParseFlags([]string{"--config", "config.yaml"}); err == nil {
		t.Fatal("removed --config flag should be rejected")
	}
}

func TestRunServeContextLogsLifecycle(t *testing.T) {
	dir := t.TempDir()
	if err := runInit([]string{"--dir", dir}); err != nil {
		t.Fatal(err)
	}
	makeTestConfigRunnable(t, filepath.Join(dir, "config.yaml"))
	configPath := filepath.Join(dir, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "127.0.0.1:8443", "127.0.0.1:0", 1))
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previousLogger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runServeContext(ctx, []string{"--dir", dir}); err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{
		"configuration loaded",
		"starting self-host server",
		"self-host server started",
		"shutdown signal received",
		"self-host server stopped",
	} {
		if !strings.Contains(logs.String(), "msg=\""+message+"\"") {
			t.Fatalf("missing log %q in %s", message, logs.String())
		}
	}
}
