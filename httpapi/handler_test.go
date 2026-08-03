package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
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

func testHandler(t *testing.T) *Handler {
	t.Helper()
	svc, err := service.New(testRepository{}, testDispatcher{}, nil, service.Options{DefaultSources: []string{"sogou"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return &Handler{Service: svc, Token: bytes.Repeat([]byte("t"), 32), Headers: security.ServerHeaders{Version: "1.0.0", APIVersion: "2", InstanceID: "test"}}
}

func TestSelfHostRouteSetExcludesFeedbackAndMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	testHandler(t).Register(router)
	seen := make(map[string]bool)
	for _, route := range router.Routes() {
		seen[route.Method+" "+route.Path] = true
	}
	want := []string{"GET /api/health", "GET /api/selfhost/v1/info", "GET /api/v2/sources", "POST /api/v2/query"}
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
