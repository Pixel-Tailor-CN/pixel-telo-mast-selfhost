package security

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
)

func TestPrepareTLSOffReturnsNil(t *testing.T) {
	cfg := &config.Config{}
	cfg.TLS.Mode = "off"
	got, err := PrepareTLS(cfg)
	if err != nil || got != nil {
		t.Fatalf("config/error = %#v/%v", got, err)
	}
}

func TestPrepareTLSAutoGeneratesSAN(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.TLS.Mode = "auto"
	cfg.TLS.PublicURL = "https://127.0.0.1:8443"
	cfg.TLS.CertFile = filepath.Join(dir, "server.crt")
	cfg.TLS.KeyFile = filepath.Join(dir, "server.key")
	prepared, err := PrepareTLS(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if prepared == nil || prepared.MinVersion != tls.VersionTLS12 {
		t.Fatalf("TLS config = %#v", prepared)
	}
	if _, err := os.Stat(cfg.TLS.CertFile); err != nil {
		t.Fatal(err)
	}
}
