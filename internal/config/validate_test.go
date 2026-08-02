package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func validConfig() *Config {
	var cfg Config
	cfg.Server.Listen = "127.0.0.1:8443"
	cfg.Auth.TokenFile = "token"
	cfg.Storage.RuntimePath = "runtime.db"
	cfg.TLS.Mode = "off"
	cfg.Query.Timeout = Duration(2 * time.Second)
	cfg.Query.MaxConcurrent = 4
	cfg.RateLimit.RequestsPerSecond = 1
	cfg.RateLimit.Burst = 5
	cfg.Upstream.ProviderIDs = []string{"sogou"}
	cfg.Baseline.CheckInterval = Duration(24 * time.Hour)
	return &cfg
}

func TestValidateRejectsNoSources(t *testing.T) {
	cfg := validConfig()
	cfg.Upstream.ProviderIDs = nil
	if err := Validate(cfg); err == nil {
		t.Fatal("at least one source is required")
	}
}

func TestValidateRejectsPublicCleartext(t *testing.T) {
	cfg := validConfig()
	cfg.TLS.Mode = "off"
	cfg.Server.Listen = "203.0.113.10:8443"
	if err := Validate(cfg); err == nil {
		t.Fatal("public cleartext must be rejected")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  listen: 127.0.0.1:8443\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("unknown field should fail")
	}
}
