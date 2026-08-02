package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/security"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/storage/runtime"
)

func TestInitAndPairingPersistIdentityAndTLS(t *testing.T) {
	dir := t.TempDir()
	if err := runInit([]string{"--dir", dir, "--public-url", "https://127.0.0.1:9443"}); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(dir, "config.yaml")
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	token, err := config.ReadToken(cfg.Auth.TokenFile)
	if err != nil {
		t.Fatal(err)
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
	dir := t.TempDir()
	if err := runInit([]string{"--dir", dir}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cfg.Auth.TokenFile, filepath.Join("", "token")) {
		t.Fatalf("token path = %q", cfg.Auth.TokenFile)
	}
	if !strings.HasSuffix(cfg.Storage.RuntimePath, filepath.Join("", "runtime.db")) {
		t.Fatalf("runtime path = %q", cfg.Storage.RuntimePath)
	}
	token, err := os.ReadFile(cfg.Auth.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != hex.EncodedLen(sha256.Size) {
		t.Fatalf("token length = %d", len(token))
	}
}
