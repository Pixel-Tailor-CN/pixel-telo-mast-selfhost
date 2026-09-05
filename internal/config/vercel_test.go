package config

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

var validVercelToken = strings.Repeat("x", 32)

func TestLoadVercelProviderProxies(t *testing.T) {
	env := validVercelEnv()
	env["MAST_SOGOU_PROXY_URL"] = "http://127.0.0.1:8080"
	env["MAST_360_PROXY_URL"] = "https://proxy.example:443"
	cfg, err := LoadVercel(getenvFrom(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProviderProxies["sogou"] != env["MAST_SOGOU_PROXY_URL"] || cfg.ProviderProxies["360"] != env["MAST_360_PROXY_URL"] {
		t.Fatal("proxy settings lost")
	}
	env["MAST_SOGOU_PROXY_URL"] = "http://user:secret@proxy.example/path"
	if _, err := LoadVercel(getenvFrom(env)); err == nil || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "MAST_SOGOU_PROXY_URL") {
		t.Fatalf("error = %v", err)
	}
}

func validVercelEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":      "postgres://postgres@localhost:5432/mast",
		"MAST_TOKEN":        validVercelToken,
		"MAST_PROVIDER_IDS": "sogou",
	}
}

func getenvFrom(env map[string]string) func(string) string {
	return func(key string) string {
		return env[key]
	}
}

func withEnv(base map[string]string, key, value string) map[string]string {
	next := make(map[string]string, len(base)+1)
	for k, v := range base {
		next[k] = v
	}
	next[key] = value
	return next
}

func withoutEnv(base map[string]string, key string) map[string]string {
	next := make(map[string]string, len(base))
	for k, v := range base {
		if k == key {
			continue
		}
		next[k] = v
	}
	return next
}

func TestLoadVercelRejectsInvalidEnvironment(t *testing.T) {
	valid := validVercelEnv()
	shortToken := strings.Repeat("s", 31)
	tests := []struct {
		name    string
		env     map[string]string
		wantVar string
		secret  string
	}{
		{
			name:    "missing DATABASE_URL",
			env:     withoutEnv(valid, "DATABASE_URL"),
			wantVar: "DATABASE_URL",
		},
		{
			name:    "blank DATABASE_URL",
			env:     withEnv(valid, "DATABASE_URL", "  \t  "),
			wantVar: "DATABASE_URL",
		},
		{
			name:    "missing MAST_TOKEN",
			env:     withoutEnv(valid, "MAST_TOKEN"),
			wantVar: "MAST_TOKEN",
		},
		{
			name:    "blank MAST_TOKEN",
			env:     withEnv(valid, "MAST_TOKEN", "   "),
			wantVar: "MAST_TOKEN",
		},
		{
			name:    "short MAST_TOKEN",
			env:     withEnv(valid, "MAST_TOKEN", shortToken),
			wantVar: "MAST_TOKEN",
			secret:  shortToken,
		},
		{
			name:    "missing MAST_PROVIDER_IDS",
			env:     withoutEnv(valid, "MAST_PROVIDER_IDS"),
			wantVar: "MAST_PROVIDER_IDS",
		},
		{
			name:    "empty MAST_PROVIDER_IDS",
			env:     withEnv(valid, "MAST_PROVIDER_IDS", ""),
			wantVar: "MAST_PROVIDER_IDS",
		},
		{
			name:    "blank items MAST_PROVIDER_IDS",
			env:     withEnv(valid, "MAST_PROVIDER_IDS", " , , "),
			wantVar: "MAST_PROVIDER_IDS",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := LoadVercel(getenvFrom(test.env))
			if err == nil {
				t.Fatal("expected error")
			}
			if cfg != nil {
				t.Fatal("config should be nil on error")
			}
			message := err.Error()
			if !strings.Contains(message, test.wantVar) {
				t.Fatalf("error %q should mention %s", message, test.wantVar)
			}
			if test.secret != "" && strings.Contains(message, test.secret) {
				t.Fatalf("error %q leaked secret", message)
			}
			if strings.Contains(message, valid["DATABASE_URL"]) {
				t.Fatalf("error %q leaked DATABASE_URL", message)
			}
			if strings.Contains(message, validVercelToken) {
				t.Fatalf("error %q leaked MAST_TOKEN", message)
			}
		})
	}
}

func TestLoadVercelNormalizesProviderIDs(t *testing.T) {
	env := validVercelEnv()
	env["MAST_PROVIDER_IDS"] = " sogou,360,sogou, ,360 "
	cfg, err := LoadVercel(getenvFrom(env))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"sogou", "360"}
	if !reflect.DeepEqual(cfg.ProviderIDs, want) {
		t.Fatalf("ProviderIDs = %#v, want %#v", cfg.ProviderIDs, want)
	}
}

func TestLoadVercelKeepsUnknownProviderIDs(t *testing.T) {
	env := validVercelEnv()
	env["MAST_PROVIDER_IDS"] = "sogou,unknown"
	cfg, err := LoadVercel(getenvFrom(env))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"sogou", "unknown"}
	if !reflect.DeepEqual(cfg.ProviderIDs, want) {
		t.Fatalf("ProviderIDs = %#v, want %#v", cfg.ProviderIDs, want)
	}
}

func TestLoadVercelTrimsRequiredValues(t *testing.T) {
	env := validVercelEnv()
	env["DATABASE_URL"] = "  postgres://postgres@localhost:5432/mast  "
	env["MAST_TOKEN"] = "  " + validVercelToken + "  "
	cfg, err := LoadVercel(getenvFrom(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://postgres@localhost:5432/mast" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if !bytes.Equal(cfg.Token, []byte(validVercelToken)) {
		t.Fatalf("Token = %q", cfg.Token)
	}
}

func TestLoadVercelUsesOsGetenvWhenNil(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://postgres@localhost:5432/mast")
	t.Setenv("MAST_TOKEN", validVercelToken)
	t.Setenv("MAST_PROVIDER_IDS", "sogou")
	cfg, err := LoadVercel(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://postgres@localhost:5432/mast" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if !bytes.Equal(cfg.Token, []byte(validVercelToken)) {
		t.Fatalf("Token = %q", cfg.Token)
	}
	if !reflect.DeepEqual(cfg.ProviderIDs, []string{"sogou"}) {
		t.Fatalf("ProviderIDs = %#v", cfg.ProviderIDs)
	}
}
