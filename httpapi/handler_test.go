package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	stdhtml "html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/security"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/service"
	"github.com/gin-gonic/gin"
)

type testRepository struct{}

func (testRepository) ListByPhone(context.Context, string) ([]*domain.Record, error) {
	return nil, domain.ErrNotFound
}
func (testRepository) ListByPhoneAndSources(context.Context, string, []string) ([]*domain.Record, error) {
	return nil, domain.ErrNotFound
}
func (testRepository) SaveBatch(context.Context, []*domain.Record) error { return nil }

type testDispatcher struct{}

func (testDispatcher) LookupAll(context.Context, string, []string) (map[string]*port.ProviderResult, map[string]error) {
	return map[string]*port.ProviderResult{"sogou": {Source: "sogou", IsSpam: true, Tag: "营销"}}, nil
}

type errorDispatcher struct{ err error }

func (d errorDispatcher) LookupAll(context.Context, string, []string) (map[string]*port.ProviderResult, map[string]error) {
	return nil, map[string]error{"sogou": d.err}
}

func testHandler(t *testing.T) *Handler {
	t.Helper()
	svc, err := service.New(testRepository{}, testDispatcher{}, nil, service.Options{DefaultSources: []string{"sogou"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return &Handler{Service: svc, Token: bytes.Repeat([]byte("t"), 32), Headers: security.ServerHeaders{Version: "1.0.0", APIVersion: "2", InstanceID: "test"}}
}

func TestAppFacingVersionIsStrictSemverWhileHomeKeepsBuildVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := testHandler(t)
	handler.Headers.Version = "0.2.1-dev+abcdef0"
	router := gin.New()
	handler.Register(router)

	homeRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	homeRecorder := httptest.NewRecorder()
	router.ServeHTTP(homeRecorder, homeRequest)
	if !strings.Contains(stdhtml.UnescapeString(homeRecorder.Body.String()), "v0.2.1-dev+abcdef0") {
		t.Fatalf("home body does not keep build version: %s", homeRecorder.Body.String())
	}

	infoRequest := httptest.NewRequest(http.MethodGet, "/api/selfhost/v1/info", nil)
	infoRequest.Header.Set("Authorization", "Bearer "+string(handler.Token))
	infoRecorder := httptest.NewRecorder()
	router.ServeHTTP(infoRecorder, infoRequest)
	if infoRecorder.Code != http.StatusOK {
		t.Fatalf("info status = %d, body = %s", infoRecorder.Code, infoRecorder.Body.String())
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(infoRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Version != "0.2.1" {
		t.Fatalf("info version = %q", payload.Version)
	}
	if got := infoRecorder.Header().Get("X-Pixel-Telo-Server-Version"); got != "0.2.1" {
		t.Fatalf("server version header = %q", got)
	}
}

func TestAppFacingVersionNormalization(t *testing.T) {
	tests := map[string]string{
		"1.2.3":             "1.2.3",
		"v1.2.3":            "1.2.3",
		"1.2.3-dev+abcdef0": "1.2.3",
		"v1.2.3+build":      "1.2.3",
		"dev+abcdef0":       "0.0.0",
		"unknown":           "0.0.0",
		"1.2":               "0.0.0",
		"01.2.3":            "0.0.0",
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			if got := appFacingVersion(input); got != want {
				t.Fatalf("appFacingVersion(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestSelfHostRouteSetExcludesFeedbackAndMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	testHandler(t).Register(router)
	seen := make(map[string]bool)
	for _, route := range router.Routes() {
		seen[route.Method+" "+route.Path] = true
	}
	want := []string{"GET /", "GET /api/health", "GET /p/:code", "GET /api/selfhost/v1/info", "GET /api/v2/sources", "POST /api/v2/query"}
	if len(seen) != len(want) {
		t.Fatalf("routes = %#v", seen)
	}
	for _, route := range want {
		if !seen[route] {
			t.Fatalf("missing route %s", route)
		}
	}
	if seen["POST /api/v2/query/feedback"] || seen["GET /metrics"] {
		t.Fatal("forbidden route registered")
	}
}

func TestRegisterKeepsPairingRouteOnZeroValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := testHandler(t)
	if handler.DisablePairing {
		t.Fatal("zero-value Handler must keep pairing enabled")
	}
	handler.Register(router)
	if !routerHasPairingRoute(router) {
		t.Fatal("zero-value Handler dropped pairing route")
	}
}

func TestRegisterOmitsPairingRouteWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := testHandler(t)
	handler.DisablePairing = true
	handler.Register(router)

	if routerHasPairingRoute(router) {
		t.Fatal("pairing route registered")
	}
	request := httptest.NewRequest(http.MethodGet, "/p/code", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestHomePageIsPublicAndShowsRuntimeSummary(t *testing.T) {
	router := gin.New()
	handler := testHandler(t)
	handler.Headers.InstanceID = "private-instance-id"
	handler.BuildCommit = "private-build-commit"
	handler.Register(router)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	for _, expected := range []string{"Pixel Telo Mast Self-host", "运行正常", "1.0.0", "API 版本", "2", "sogou"} {
		if !bytes.Contains(recorder.Body.Bytes(), []byte(expected)) {
			t.Fatalf("page does not contain %q", expected)
		}
	}
	for _, sensitive := range []string{"private-instance-id", "private-build-commit", string(handler.Token)} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(sensitive)) {
			t.Fatalf("page contains sensitive value %q", sensitive)
		}
	}
	if recorder.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("Content-Security-Policy is missing")
	}
}

func TestV1QueryRouteIsNotRegistered(t *testing.T) {
	router := gin.New()
	testHandler(t).Register(router)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/query?number=13800138000", nil)
	request.Header.Set("Authorization", "Bearer "+string(bytes.Repeat([]byte("t"), 32)))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestQueryV2LogsSafeFinalFailure(t *testing.T) {
	const phone = "13800138000"
	const privateDetail = "private upstream response"
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	svc, err := service.New(testRepository{}, errorDispatcher{err: errors.New(privateDetail)}, nil, service.Options{DefaultSources: []string{"sogou"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	handler := &Handler{Service: svc, Token: bytes.Repeat([]byte("t"), 32), Logger: logger}
	router := gin.New()
	handler.Register(router)
	request := httptest.NewRequest(http.MethodPost, "/api/v2/query", bytes.NewBufferString(`{"number":"`+phone+`","sources":["sogou"]}`))
	request.Header.Set("Authorization", "Bearer "+string(handler.Token))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	requestID := recorder.Header().Get("X-Request-ID")
	message := logs.String()
	for _, expected := range []string{`"msg":"query failed"`, `"status":503`, `"error_type":"upstream_unavailable"`, requestID} {
		if !strings.Contains(message, expected) {
			t.Fatalf("log %q does not contain %q", message, expected)
		}
	}
	for _, sensitive := range []string{phone, privateDetail, string(handler.Token)} {
		if strings.Contains(message, sensitive) {
			t.Fatalf("log contains sensitive value %q: %s", sensitive, message)
		}
	}
}

func TestQueryV2OmitsFeedbackToken(t *testing.T) {
	router := gin.New()
	testHandler(t).Register(router)
	request := httptest.NewRequest("POST", "/api/v2/query", bytes.NewBufferString(`{"number":"13800138000","sources":["sogou"]}`))
	request.Header.Set("Authorization", "Bearer "+string(bytes.Repeat([]byte("t"), 32)))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("feedback_token")) {
		t.Fatalf("response contains feedback_token: %s", recorder.Body.String())
	}
}

func TestQueryV2UsesFlatAppCompatibleResponse(t *testing.T) {
	router := gin.New()
	testHandler(t).Register(router)
	request := httptest.NewRequest(http.MethodPost, "/api/v2/query", bytes.NewBufferString(`{"number":"13800138000","sources":["unknown","sogou"]}`))
	request.Header.Set("Authorization", "Bearer "+string(bytes.Repeat([]byte("t"), 32)))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if _, exists := response["record"]; exists {
		t.Fatalf("response contains legacy record: %s", recorder.Body.String())
	}
	for _, field := range []string{"phone", "is_spam", "tag", "confidence", "source", "data", "query_mode", "requested_sources", "effective_sources"} {
		if _, exists := response[field]; !exists {
			t.Fatalf("response does not contain %q: %s", field, recorder.Body.String())
		}
	}
	if response["phone"] != "13800138000" || response["is_spam"] != true || response["tag"] != "营销" || response["source"] != "sogou" {
		t.Fatalf("response = %#v", response)
	}
	invalidSources, ok := response["invalid_sources"].([]any)
	if !ok || len(invalidSources) != 1 || invalidSources[0] != "unknown" {
		t.Fatalf("invalid_sources = %#v", response["invalid_sources"])
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v", response["data"])
	}
	for _, field := range []string{"cardType", "province", "city"} {
		if _, exists := data[field]; !exists {
			t.Fatalf("data does not contain %q: %#v", field, data)
		}
	}
}

func TestQueryV2ReturnsNullForMissingPhoneData(t *testing.T) {
	router := gin.New()
	testHandler(t).Register(router)
	request := httptest.NewRequest(http.MethodPost, "/api/v2/query", bytes.NewBufferString(`{"number":"1234567","sources":["sogou"]}`))
	request.Header.Set("Authorization", "Bearer "+string(bytes.Repeat([]byte("t"), 32)))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	data, exists := response["data"]
	if !exists || data != nil {
		t.Fatalf("data = %#v, body = %s", data, recorder.Body.String())
	}
}

func routerHasPairingRoute(router *gin.Engine) bool {
	for _, route := range router.Routes() {
		if route.Method+" "+route.Path == "GET /p/:code" {
			return true
		}
	}
	return false
}
