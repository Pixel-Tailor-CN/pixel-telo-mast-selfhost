package httpapi

import (
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestPairingPageIsPublicAndExpiresInMemory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := testHandler(t)
	handler.Token = []byte("pairing-token-value")
	pageURL, err := handler.StartPairingSession("https://192.168.1.8:8443", "sha256/abc", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pageURL, "https://192.168.1.8:8443/p/") {
		t.Fatalf("page url = %q", pageURL)
	}
	code := strings.TrimPrefix(pageURL, "https://192.168.1.8:8443/p/")
	router := gin.New()
	handler.Register(router)

	ok := httptest.NewRequest(http.MethodGet, "/p/"+code, nil)
	okRec := httptest.NewRecorder()
	router.ServeHTTP(okRec, ok)
	if okRec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", okRec.Code, okRec.Body.String())
	}
	body := html.UnescapeString(okRec.Body.String())
	if !strings.Contains(body, "pairing-token-value") || !strings.Contains(body, "data:image/png;base64,") {
		t.Fatalf("page missing pairing content: %s", body)
	}
	if !strings.Contains(body, `"url":"https://192.168.1.8:8443"`) || !strings.Contains(body, `"spki_pin":"sha256/abc"`) {
		t.Fatalf("qr json payload = %s", body)
	}
	if strings.Contains(pageURL, "pairing-token-value") {
		t.Fatal("pairing page URL must not contain the API token")
	}
	if auth := okRec.Header().Get("WWW-Authenticate"); auth != "" {
		t.Fatal("pairing page must not require bearer auth")
	}

	wrong := httptest.NewRecorder()
	router.ServeHTTP(wrong, httptest.NewRequest(http.MethodGet, "/p/not-the-code", nil))
	if wrong.Code != http.StatusNotFound {
		t.Fatalf("wrong code status = %d", wrong.Code)
	}

	handler.pairingMu.Lock()
	handler.pairing.expiresAt = time.Now().Add(-time.Second)
	handler.pairingMu.Unlock()
	expired := httptest.NewRecorder()
	router.ServeHTTP(expired, httptest.NewRequest(http.MethodGet, "/p/"+code, nil))
	if expired.Code != http.StatusNotFound {
		t.Fatalf("expired status = %d", expired.Code)
	}
	if handler.PairingPageURL() != "" {
		t.Fatal("expired pairing url should be empty")
	}
}

func TestStartPairingSessionRejectsUnspecifiedPublicURL(t *testing.T) {
	handler := testHandler(t)
	if _, err := handler.StartPairingSession("https://0.0.0.0:8443", "", time.Now()); err == nil {
		t.Fatal("unspecified public url should be rejected")
	}
	if _, err := handler.StartPairingSession("", "", time.Now()); err == nil {
		t.Fatal("empty public url should be rejected")
	}
}
