package logging

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestAccessLogsSafeRouteAndRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
	router := gin.New()
	router.Use(Access(logger), Recovery(logger))
	router.GET("/items/:id", func(c *gin.Context) { c.Status(http.StatusNotFound) })

	wantID := uuid.NewString()
	request := httptest.NewRequest(http.MethodGet, "/items/secret?number=13800138000", nil)
	request.Header.Set(requestIDHeader, wantID)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	logs := output.String()
	if response.Header().Get(requestIDHeader) != wantID {
		t.Fatalf("request ID = %q", response.Header().Get(requestIDHeader))
	}
	for _, want := range []string{"level=WARN", "route=/items/:id", "status=404", "request_id=" + wantID} {
		if !strings.Contains(logs, want) {
			t.Fatalf("missing %q in %s", want, logs)
		}
	}
	for _, secret := range []string{"secret", "13800138000"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("sensitive value %q leaked in %s", secret, logs)
		}
	}
}

func TestRecoveryDoesNotLogPanicValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
	router := gin.New()
	router.Use(Access(logger), Recovery(logger))
	router.GET("/panic", func(*gin.Context) { panic("phone=13800138000") })

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
	logs := output.String()
	if !strings.Contains(logs, "http request panic recovered") || !strings.Contains(logs, "level=ERROR") {
		t.Fatalf("panic log missing: %s", logs)
	}
	if strings.Contains(logs, "13800138000") {
		t.Fatalf("panic value leaked: %s", logs)
	}
	requestID := response.Header().Get(requestIDHeader)
	if _, err := uuid.Parse(requestID); err != nil {
		t.Fatalf("generated request ID = %q", requestID)
	}
	if strings.Count(logs, "request_id="+requestID) != 2 {
		t.Fatalf("recovery and access logs do not share request ID %q: %s", requestID, logs)
	}
}
