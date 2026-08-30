package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/provider"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/service"
	"github.com/gin-gonic/gin"
)

type stubRepository struct{}

func (stubRepository) ListByPhone(context.Context, string) ([]*domain.Record, error) {
	return nil, domain.ErrNotFound
}

func (stubRepository) ListByPhoneAndSources(context.Context, string, []string) ([]*domain.Record, error) {
	return nil, domain.ErrNotFound
}

func (stubRepository) SaveBatch(context.Context, []*domain.Record) error { return nil }

func TestComposeRegistersSharedRoutesWithoutPairing(t *testing.T) {
	token := bytes.Repeat([]byte("t"), 32)
	composition, err := compose(CompositionOptions{
		Repository:      stubRepository{},
		ProviderSources: []provider.SourceConfig{{ID: "sogou"}},
		QueryOptions:    service.Options{DefaultSources: []string{"sogou"}},
		Token:           token,
		Version:         "compose-version",
		Commit:          "compose-commit",
		InstanceID:      "compose-instance",
		Capabilities:    []string{"query_v2"},
		DisablePairing:  true,
		RateLimit:       RateLimitOptions{RequestsPerSecond: 1, Burst: 1, MaxConcurrent: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = composition.Query.Close() })

	if !composition.Handler.DisablePairing {
		t.Fatal("DisablePairing should remain true")
	}

	seen := routeSet(composition.Router)
	want := []string{"GET /", "GET /api/health", "GET /api/selfhost/v1/info", "GET /api/v2/sources", "POST /api/v2/query"}
	for _, route := range want {
		if !seen[route] {
			t.Fatalf("missing route %s in %#v", route, seen)
		}
	}
	if seen["GET /p/:code"] {
		t.Fatal("pairing route should not be registered")
	}

	if rec := serve(composition.Router, http.MethodGet, "/", "", false, token); rec.Code != http.StatusOK {
		t.Fatalf("home status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec := serve(composition.Router, http.MethodGet, "/api/health", "", false, token); rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec := serve(composition.Router, http.MethodGet, "/p/code", "", false, token); rec.Code != http.StatusNotFound {
		t.Fatalf("pairing status = %d, body = %s", rec.Code, rec.Body.String())
	}

	info := serve(composition.Router, http.MethodGet, "/api/selfhost/v1/info", "", true, token)
	if info.Code != http.StatusOK {
		t.Fatalf("info status = %d, body = %s", info.Code, info.Body.String())
	}
	var payload struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(info.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Capabilities) != 1 || payload.Capabilities[0] != "query_v2" {
		t.Fatalf("capabilities = %#v", payload.Capabilities)
	}

	sources := serve(composition.Router, http.MethodGet, "/api/v2/sources", "", true, token)
	if sources.Code != http.StatusOK {
		t.Fatalf("sources status = %d, body = %s", sources.Code, sources.Body.String())
	}
}

func TestComposeUsesInjectedLoggerForProviderAndFinalFailure(t *testing.T) {
	const phone = "13800138000"
	const upstreamBody = "private upstream response"
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	token := bytes.Repeat([]byte("t"), 32)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(upstreamBody)),
		}, nil
	})}
	composition, err := compose(CompositionOptions{
		Repository:      stubRepository{},
		ProviderSources: []provider.SourceConfig{{ID: "360"}},
		QueryOptions:    service.Options{DefaultSources: []string{"360"}},
		Token:           token,
		Capabilities:    []string{"query_v2"},
		Logger:          logger,
		HTTPClient:      client,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = composition.Query.Close() })

	recorder := serve(composition.Router, http.MethodPost, "/api/v2/query", `{"number":"`+phone+`","sources":["360"]}`, true, token)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	message := logs.String()
	for _, expected := range []string{`"msg":"provider query failed"`, `"provider":"360"`, `"error_type":"parse_error"`, `"msg":"query failed"`} {
		if !strings.Contains(message, expected) {
			t.Fatalf("log %q does not contain %q", message, expected)
		}
	}
	for _, sensitive := range []string{phone, upstreamBody, string(token)} {
		if strings.Contains(message, sensitive) {
			t.Fatalf("log contains sensitive value %q: %s", sensitive, message)
		}
	}
}

func TestComposeUsesDefaultLimiterWhenRateLimitUnset(t *testing.T) {
	token := bytes.Repeat([]byte("t"), 32)
	composition, err := compose(CompositionOptions{
		Repository:      stubRepository{},
		ProviderSources: []provider.SourceConfig{{ID: "sogou"}},
		QueryOptions:    service.Options{DefaultSources: []string{"sogou"}},
		Token:           token,
		Version:         "compose-version",
		Commit:          "compose-commit",
		InstanceID:      "compose-instance",
		Capabilities:    []string{"query_v2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = composition.Query.Close() })

	if composition.Handler.Limiter == nil {
		t.Fatal("default limiter should be installed during register")
	}

	rec := serve(composition.Router, http.MethodPost, "/api/v2/query", `{}`, true, token)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("unset rate limit must not reject v2 query: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestComposeRejectsPartialNonPositiveRateLimit(t *testing.T) {
	token := bytes.Repeat([]byte("t"), 32)
	base := CompositionOptions{
		Repository:      stubRepository{},
		ProviderSources: []provider.SourceConfig{{ID: "sogou"}},
		QueryOptions:    service.Options{DefaultSources: []string{"sogou"}},
		Token:           token,
		Capabilities:    []string{"query_v2"},
	}
	cases := []RateLimitOptions{
		{RequestsPerSecond: 1},
		{Burst: 1},
		{MaxConcurrent: 1},
		{RequestsPerSecond: 1, Burst: 1},
		{RequestsPerSecond: 1, Burst: 1, MaxConcurrent: 0},
		{RequestsPerSecond: -1, Burst: 1, MaxConcurrent: 1},
		{RequestsPerSecond: 1, Burst: -1, MaxConcurrent: 1},
		{RequestsPerSecond: 1, Burst: 1, MaxConcurrent: -1},
		{RequestsPerSecond: -1, Burst: -1, MaxConcurrent: -1},
	}
	for _, rateLimit := range cases {
		options := base
		options.RateLimit = rateLimit
		composition, err := compose(options)
		if err == nil {
			_ = composition.Query.Close()
			t.Fatalf("expected error for %#v", rateLimit)
		}
	}
}

func TestBuildKeepsTraditionalPairingCapabilityAndRoute(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.Server.Listen = "127.0.0.1:0"
	cfg.Auth.TokenFile = filepath.Join(dir, "token")
	cfg.TLS.Mode = "off"
	cfg.Storage.RuntimePath = filepath.Join(dir, "runtime.db")
	cfg.Query.Timeout = config.Duration(2 * time.Second)
	cfg.Query.MaxConcurrent = 1
	cfg.RateLimit.RequestsPerSecond = 1
	cfg.RateLimit.Burst = 1
	cfg.Upstream.ProviderIDs = []string{"sogou"}

	token := bytes.Repeat([]byte("t"), 32)
	application, err := Build(Options{Config: cfg, Token: token, Version: "1.2.3", Commit: "abc123", InstanceID: "instance-1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close(context.Background()) })

	if application.handler.DisablePairing {
		t.Fatal("traditional handler must keep pairing enabled by default")
	}
	if got := application.handler.Capabilities; len(got) != 2 || got[0] != "query_v2" || got[1] != "spki_pairing" {
		t.Fatalf("capabilities = %#v", got)
	}

	router, ok := application.server.Handler.(*gin.Engine)
	if !ok {
		t.Fatalf("router type = %T", application.server.Handler)
	}
	if !routeSet(router)["GET /p/:code"] {
		t.Fatal("traditional pairing route is missing")
	}

	info := serve(router, http.MethodGet, "/api/selfhost/v1/info", "", true, token)
	if info.Code != http.StatusOK {
		t.Fatalf("info status = %d, body = %s", info.Code, info.Body.String())
	}
	var payload struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(info.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Capabilities) != 2 || payload.Capabilities[0] != "query_v2" || payload.Capabilities[1] != "spki_pairing" {
		t.Fatalf("info capabilities = %#v", payload.Capabilities)
	}
}

func routeSet(router *gin.Engine) map[string]bool {
	seen := make(map[string]bool, len(router.Routes()))
	for _, route := range router.Routes() {
		seen[route.Method+" "+route.Path] = true
	}
	return seen
}

func serve(router http.Handler, method, path, body string, auth bool, token []byte) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	request := httptest.NewRequest(method, path, reader)
	if auth {
		request.Header.Set("Authorization", "Bearer "+string(token))
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
