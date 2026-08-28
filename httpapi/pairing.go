package httpapi

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/skip2/go-qrcode"
)

const pairingTTL = 5 * time.Minute

//go:embed assets/pairing.html
var pairingHTML string

var pairingTemplate = template.Must(template.New("pairing").Parse(pairingHTML))

type pairingSession struct {
	code      string
	pageURL   string
	line      string
	url       string
	token     string
	spki      string
	qrDataURI template.URL
	expiresAt time.Time
}

type pairingPageData struct {
	URL        string
	Token      string
	SPKIPin    string
	Line       template.HTML
	QRDataURI  template.URL
}

func (h *Handler) StartPairingSession(publicURL, spki string, now time.Time) (string, error) {
	if h == nil {
		return "", fmt.Errorf("handler is required")
	}
	publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if err := validatePairingPublicURL(publicURL); err != nil {
		return "", err
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate pairing page token: %w", err)
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	payload, err := json.Marshal(pairingPayload{URL: publicURL, Token: string(h.Token), SPKIPin: spki})
	if err != nil {
		return "", fmt.Errorf("encode pairing payload: %w", err)
	}
	png, err := qrcode.Encode(string(payload), qrcode.Medium, 280)
	if err != nil {
		return "", fmt.Errorf("encode pairing qr: %w", err)
	}
	session := &pairingSession{
		code:      code,
		pageURL:   publicURL + "/p/" + code,
		line:      string(payload),
		url:       publicURL,
		token:     string(h.Token),
		spki:      spki,
		qrDataURI: template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png)),
		expiresAt: now.Add(pairingTTL),
	}
	h.pairingMu.Lock()
	h.pairing = session
	h.pairingMu.Unlock()
	return session.pageURL, nil
}

func (h *Handler) PairingPageURL() string {
	if h == nil {
		return ""
	}
	h.pairingMu.Lock()
	defer h.pairingMu.Unlock()
	if h.pairing == nil || !time.Now().Before(h.pairing.expiresAt) {
		return ""
	}
	return h.pairing.pageURL
}

func (h *Handler) pairingPage(c *gin.Context) {
	code := c.Param("code")
	h.pairingMu.Lock()
	session := h.pairing
	h.pairingMu.Unlock()
	if session == nil || code != session.code || !time.Now().Before(session.expiresAt) {
		c.Header("Cache-Control", "no-store")
		c.Header("Referrer-Policy", "no-referrer")
		c.Data(http.StatusNotFound, "text/html; charset=utf-8", []byte("<!doctype html><meta charset=utf-8><title>配对链接已失效</title><p>配对页面已过期或不存在。请看服务器启动日志里的新链接，或使用 pairing 命令。</p>"))
		return
	}
	var page bytes.Buffer
	if err := pairingTemplate.Execute(&page, pairingPageData{
		URL:        session.url,
		Token:      session.token,
		SPKIPin:    session.spki,
		Line:       template.HTML(session.line),
		QRDataURI:  session.qrDataURI,
	}); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; script-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Data(http.StatusOK, "text/html; charset=utf-8", page.Bytes())
}

type pairingPayload struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	SPKIPin string `json:"spki_pin"`
}

func validatePairingPublicURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return fmt.Errorf("tls.public_url must be an HTTPS root URL")
	}
	host := parsed.Hostname()
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return fmt.Errorf("tls.public_url host is not reachable")
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		return fmt.Errorf("tls.public_url host is not reachable")
	}
	return nil
}
