package config

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validConfig() *Config {
	var cfg Config
	cfg.Log = defaultLogConfig()
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

func TestLoadAcceptsProviderSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := `server:
  listen: "127.0.0.1:8443"
auth:
  token_file: "/tmp/token"
tls:
  mode: "off"
storage:
  runtime_path: "/tmp/runtime.db"
baseline:
  enabled: false
query:
  timeout: "2s"
  max_concurrent: 4
rate_limit:
  requests_per_second: 2
  burst: 7
upstream:
  provider_ids: ["sogou"]
providers:
  sogou:
    min_interval: "1s"
    max_concurrent: 2
    breaker_timeout: "45s"
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	settings := cfg.Providers["sogou"]
	if settings.MinInterval.Std() != time.Second || settings.MaxConcurrent != 2 || settings.BreakerTimeout.Std() != 45*time.Second {
		t.Fatalf("provider settings = %+v", settings)
	}
	if cfg.Log.Level != "info" || cfg.Log.Format != "json" || cfg.Log.Rotation.MaxSizeMB != 100 || cfg.Log.Retention.MaxAge.Std() != 30*24*time.Hour {
		t.Fatalf("default log config = %+v", cfg.Log)
	}
}

func TestValidateRejectsInvalidLogConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "level", mutate: func(cfg *Config) { cfg.Log.Level = "trace" }},
		{name: "format", mutate: func(cfg *Config) { cfg.Log.Format = "xml" }},
		{name: "rotation size", mutate: func(cfg *Config) { cfg.Log.Rotation.MaxSizeMB = 0 }},
		{name: "retention age", mutate: func(cfg *Config) { cfg.Log.Retention.MaxAge = 0 }},
		{name: "retention backups", mutate: func(cfg *Config) { cfg.Log.Retention.MaxBackups = 0 }},
		{name: "retention size", mutate: func(cfg *Config) { cfg.Log.Retention.MaxTotalSizeMB = 0 }},
	}
	overflow := uint64(maxLogSizeMB) + 1
	if overflow <= uint64(^uint(0)>>1) {
		tests = append(tests,
			struct {
				name   string
				mutate func(*Config)
			}{name: "rotation size overflow", mutate: func(cfg *Config) { cfg.Log.Rotation.MaxSizeMB = int(overflow) }},
			struct {
				name   string
				mutate func(*Config)
			}{name: "retention size overflow", mutate: func(cfg *Config) { cfg.Log.Retention.MaxTotalSizeMB = int(overflow) }},
		)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validConfig()
			test.mutate(cfg)
			if err := Validate(cfg); err == nil {
				t.Fatal("invalid log configuration should fail")
			}
		})
	}
}

func TestEnsureTokenCreatesMissingToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "token")
	token, err := EnsureToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != hex.EncodedLen(32) {
		t.Fatalf("token length = %d", len(token))
	}
	decoded, err := hex.DecodeString(string(token))
	if err != nil {
		t.Fatalf("token is not hexadecimal: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("decoded token length = %d", len(decoded))
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(token) {
		t.Fatal("stored token differs from returned token")
	}
}

func TestEnsureTokenReusesExistingToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	want := strings.Repeat("a", 64)
	if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := EnsureToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("token = %q", got)
	}
}

func TestEnsureTokenRejectsInvalidTokenWithoutOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	want := []byte("short")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureToken(path); err == nil {
		t.Fatal("short token should be rejected")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("invalid token was overwritten: %q", got)
	}
}
