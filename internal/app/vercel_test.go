package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestVercelRouterServesHealthInfoSourcesAndQuery(t *testing.T) {
	token := bytes.Repeat([]byte("t"), 32)
	composition, err := composeVercel(&config.VercelConfig{
		Token:       token,
		ProviderIDs: []string{"sogou"},
	}, stubRepository{}, slog.Default(), "vercel-version", "vercel-commit", "vercel-instance")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = composition.Query.Close() })

	if composition.Handler.EnablePairing {
		t.Fatal("EnablePairing should remain false")
	}
	if got := composition.Handler.Capabilities; len(got) != 1 || got[0] != "query_v2" {
		t.Fatalf("handler capabilities = %#v", got)
	}

	router := composition.Router
	if rec := serve(router, http.MethodGet, "/api/health", "", false, token); rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec := serve(router, http.MethodGet, "/", "", false, token); rec.Code != http.StatusOK {
		t.Fatalf("home status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if rec := serve(router, http.MethodGet, "/api/selfhost/v1/info", "", false, token); rec.Code != http.StatusUnauthorized {
		t.Fatalf("info without token status = %d, body = %s", rec.Code, rec.Body.String())
	}
	info := serve(router, http.MethodGet, "/api/selfhost/v1/info", "", true, token)
	if info.Code != http.StatusOK {
		t.Fatalf("info status = %d, body = %s", info.Code, info.Body.String())
	}
	var payload struct {
		Version      string   `json:"version"`
		InstanceID   string   `json:"instance_id"`
		BuildCommit  string   `json:"build_commit"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(info.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Version != "vercel-version" || payload.InstanceID != "vercel-instance" || payload.BuildCommit != "vercel-commit" {
		t.Fatalf("info identity = %#v", payload)
	}
	if len(payload.Capabilities) != 1 || payload.Capabilities[0] != "query_v2" {
		t.Fatalf("capabilities = %#v", payload.Capabilities)
	}

	if rec := serve(router, http.MethodGet, "/api/v2/sources", "", false, token); rec.Code != http.StatusUnauthorized {
		t.Fatalf("sources without token status = %d", rec.Code)
	}
	sources := serve(router, http.MethodGet, "/api/v2/sources", "", true, token)
	if sources.Code != http.StatusOK {
		t.Fatalf("sources status = %d, body = %s", sources.Code, sources.Body.String())
	}
	var sourcePayload struct {
		DefaultSources []string `json:"default_sources"`
	}
	if err := json.Unmarshal(sources.Body.Bytes(), &sourcePayload); err != nil {
		t.Fatal(err)
	}
	if len(sourcePayload.DefaultSources) != 1 || sourcePayload.DefaultSources[0] != "sogou" {
		t.Fatalf("default sources = %#v", sourcePayload.DefaultSources)
	}

	if rec := serve(router, http.MethodPost, "/api/v2/query", `{}`, false, token); rec.Code != http.StatusUnauthorized {
		t.Fatalf("query without token status = %d", rec.Code)
	}
	query := serve(router, http.MethodPost, "/api/v2/query", `{}`, true, token)
	if query.Code != http.StatusBadRequest {
		t.Fatalf("query status = %d, body = %s", query.Code, query.Body.String())
	}

	for _, path := range []string{"/p/code", "/api/v1/query", "/metrics"} {
		rec := serve(router, http.MethodGet, path, "", true, token)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, body = %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestVercelCompositionRejectsUnknownProvider(t *testing.T) {
	token := bytes.Repeat([]byte("t"), 32)
	composition, err := composeVercel(&config.VercelConfig{
		Token:       token,
		ProviderIDs: []string{"unknown"},
	}, stubRepository{}, slog.Default(), "vercel-version", "vercel-commit", "vercel-instance")
	if err == nil {
		_ = composition.Query.Close()
		t.Fatal("expected unknown provider error")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error %q should mention unknown provider", err)
	}
}

func TestBuildVercelRequiresConfig(t *testing.T) {
	application, err := BuildVercel(context.Background(), nil, slog.Default(), "v", "c")
	if err == nil {
		_ = application.Close()
		t.Fatal("expected error")
	}
	if application != nil {
		t.Fatal("application should be nil on error")
	}
}

func TestBuildVercelRejectsInvalidDatabaseURLWithoutLeakingSecret(t *testing.T) {
	const sentinel = "sentinel-secret-password%"
	cfg := &config.VercelConfig{
		DatabaseURL: "postgres://user:" + sentinel + "zz@127.0.0.1:5432/mast",
		Token:       bytes.Repeat([]byte("t"), 32),
		ProviderIDs: []string{"sogou"},
	}
	application, err := BuildVercel(context.Background(), cfg, slog.Default(), "v", "c")
	if err == nil {
		_ = application.Close()
		t.Fatal("expected error")
	}
	message := err.Error()
	if !strings.Contains(message, "open postgres") {
		t.Fatalf("error %q should mention open postgres", message)
	}
	if strings.Contains(message, sentinel) || strings.Contains(message, cfg.DatabaseURL) {
		t.Fatalf("error leaked database url: %q", message)
	}
	if strings.Contains(message, string(cfg.Token)) {
		t.Fatalf("error leaked token: %q", message)
	}
}

func TestBuildVercelServesCachedQueryFromPostgres(t *testing.T) {
	application, token := buildTestVercel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := application.runtime.SaveBatch(ctx, []*domain.Record{{
		PhoneNumber: "13800138000",
		Source:      "sogou",
		Tag:         "营销",
		Confidence:  80,
		HitCount:    1,
		FetchedAt:   time.Now().UTC(),
	}}); err != nil {
		t.Fatal(err)
	}

	if rec := serve(application.Handler, http.MethodGet, "/api/health", "", false, token); rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, body = %s", rec.Code, rec.Body.String())
	}

	info := serve(application.Handler, http.MethodGet, "/api/selfhost/v1/info", "", true, token)
	if info.Code != http.StatusOK {
		t.Fatalf("info status = %d, body = %s", info.Code, info.Body.String())
	}
	var payload struct {
		InstanceID   string   `json:"instance_id"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(info.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(payload.InstanceID); err != nil {
		t.Fatalf("instance ID = %q: %v", payload.InstanceID, err)
	}
	if len(payload.Capabilities) != 1 || payload.Capabilities[0] != "query_v2" {
		t.Fatalf("capabilities = %#v", payload.Capabilities)
	}

	query := serve(application.Handler, http.MethodPost, "/api/v2/query", `{"number":"13800138000","sources":["sogou"]}`, true, token)
	if query.Code != http.StatusOK {
		t.Fatalf("query status = %d, body = %s", query.Code, query.Body.String())
	}
	if !bytes.Contains(query.Body.Bytes(), []byte(`"tag":"营销"`)) {
		t.Fatalf("query body = %s", query.Body.String())
	}

	for _, path := range []string{"/p/code", "/api/v1/query", "/metrics"} {
		rec := serve(application.Handler, http.MethodGet, path, "", true, token)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, body = %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestBuildVercelRejectsUnknownProvider(t *testing.T) {
	cfg := &config.VercelConfig{
		DatabaseURL: vercelTestDatabaseURL(t),
		Token:       bytes.Repeat([]byte("t"), 32),
		ProviderIDs: []string{"unknown"},
	}
	application, err := BuildVercel(context.Background(), cfg, slog.Default(), "v", "c")
	if err == nil {
		_ = application.Close()
		t.Fatal("expected unknown provider error")
	}
	if application != nil {
		t.Fatal("application should be nil on error")
	}
	message := err.Error()
	if !strings.Contains(message, "build vercel application") {
		t.Fatalf("error %q should mention build vercel application", message)
	}
	if !strings.Contains(message, "unknown") {
		t.Fatalf("error %q should mention unknown provider", message)
	}
	if strings.Contains(message, cfg.DatabaseURL) || strings.Contains(message, string(cfg.Token)) {
		t.Fatalf("error leaked secret: %q", message)
	}
}

func TestBuildVercelCloseIsIdempotent(t *testing.T) {
	application, _ := buildTestVercel(t)
	if err := application.Close(); err != nil {
		t.Fatal(err)
	}
	if err := application.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestVercelAppCloseNilIsSafe(t *testing.T) {
	var application *VercelApp
	if err := application.Close(); err != nil {
		t.Fatal(err)
	}
}

func buildTestVercel(t *testing.T) (*VercelApp, []byte) {
	t.Helper()
	token := bytes.Repeat([]byte("t"), 32)
	application, err := BuildVercel(context.Background(), &config.VercelConfig{
		DatabaseURL: vercelTestDatabaseURL(t),
		Token:       token,
		ProviderIDs: []string{"sogou"},
	}, slog.Default(), "vercel-version", "vercel-commit")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := application.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			t.Errorf("close vercel app: %v", err)
		}
	})
	return application, token
}

func vercelTestDatabaseURL(t *testing.T) string {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("MAST_TEST_DATABASE_URL"))
	if raw == "" {
		t.Skip("MAST_TEST_DATABASE_URL is not set")
	}
	schema := "mast_vercel_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	for _, r := range schema {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			t.Fatalf("unsafe schema name %q", schema)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanupDB, cleanupErr := sql.Open("pgx", raw)
		if cleanupErr != nil {
			t.Errorf("open cleanup database: %v", cleanupErr)
			return
		}
		defer cleanupDB.Close()
		if _, cleanupErr := cleanupDB.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); cleanupErr != nil {
			t.Errorf("drop schema %s: %v", schema, cleanupErr)
		}
	})
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse test database url: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
