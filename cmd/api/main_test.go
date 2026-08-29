package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

var validAPIToken = strings.Repeat("x", 32)

func TestCloseApplicationPreservesRunAndCloseErrors(t *testing.T) {
	runErr := errors.New("run failed")
	closeErr := errors.New("close failed")
	got := closeApplication(runErr, func() error { return closeErr })
	if !errors.Is(got, runErr) || !errors.Is(got, closeErr) {
		t.Fatalf("joined error = %v", got)
	}
}

func TestNewServerUsesPortAddress(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	server, err := newServer("8080", handler)
	if err != nil {
		t.Fatal(err)
	}
	if server.Addr != ":8080" {
		t.Fatalf("addr = %q", server.Addr)
	}
	if server.Handler == nil {
		t.Fatal("handler is nil")
	}
}

func TestNewServerRejectsBlankPort(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, port := range []string{"", "  ", "\t"} {
		server, err := newServer(port, handler)
		if err == nil {
			t.Fatalf("port %q: expected error", port)
		}
		if server != nil {
			t.Fatal("server should be nil on error")
		}
		if !strings.Contains(err.Error(), "PORT") {
			t.Fatalf("error %q should mention PORT", err)
		}
	}
}

func TestRunRejectsInvalidConfigWithoutLeakingSecrets(t *testing.T) {
	databaseURL := "postgres://user:super-secret-db-url@localhost:5432/mast"
	tests := []struct {
		name    string
		env     map[string]string
		wantVar string
		secret  string
	}{
		{
			name:    "missing DATABASE_URL",
			env:     map[string]string{"MAST_TOKEN": validAPIToken, "MAST_PROVIDER_IDS": "sogou", "PORT": "8080"},
			wantVar: "DATABASE_URL",
		},
		{
			name:    "short MAST_TOKEN",
			env:     map[string]string{"DATABASE_URL": databaseURL, "MAST_TOKEN": "short-token-value", "MAST_PROVIDER_IDS": "sogou", "PORT": "8080"},
			wantVar: "MAST_TOKEN",
			secret:  "short-token-value",
		},
		{
			name:    "missing MAST_PROVIDER_IDS",
			env:     map[string]string{"DATABASE_URL": databaseURL, "MAST_TOKEN": validAPIToken, "PORT": "8080"},
			wantVar: "MAST_PROVIDER_IDS",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := run(context.Background(), getenvFrom(test.env))
			if err == nil {
				t.Fatal("expected error")
			}
			message := err.Error()
			if !strings.Contains(message, "load vercel config") {
				t.Fatalf("error %q should mention load vercel config", message)
			}
			if !strings.Contains(message, test.wantVar) {
				t.Fatalf("error %q should mention %s", message, test.wantVar)
			}
			if test.secret != "" && strings.Contains(message, test.secret) {
				t.Fatalf("error %q leaked secret", message)
			}
			if strings.Contains(message, databaseURL) || strings.Contains(message, "super-secret-db-url") {
				t.Fatalf("error %q leaked DATABASE_URL", message)
			}
			if strings.Contains(message, validAPIToken) {
				t.Fatalf("error %q leaked MAST_TOKEN", message)
			}
		})
	}
}

func TestRunRejectsInvalidDatabaseURLWithoutLeakingSecret(t *testing.T) {
	const sentinel = "sentinel-secret-password%"
	databaseURL := "postgres://user:" + sentinel + "zz@127.0.0.1:5432/mast"
	err := run(context.Background(), getenvFrom(map[string]string{
		"DATABASE_URL":      databaseURL,
		"MAST_TOKEN":        validAPIToken,
		"MAST_PROVIDER_IDS": "sogou",
		"PORT":              "8080",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	message := err.Error()
	if !strings.Contains(message, "build vercel application") && !strings.Contains(message, "open postgres") {
		t.Fatalf("error %q should mention build or open postgres", message)
	}
	if strings.Contains(message, sentinel) || strings.Contains(message, databaseURL) {
		t.Fatalf("error %q leaked DATABASE_URL", message)
	}
	if strings.Contains(message, validAPIToken) {
		t.Fatalf("error %q leaked MAST_TOKEN", message)
	}
}

func getenvFrom(env map[string]string) func(string) string {
	return func(key string) string {
		return env[key]
	}
}
