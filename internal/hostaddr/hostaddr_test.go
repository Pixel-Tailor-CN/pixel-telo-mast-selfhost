package hostaddr

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListenPort(t *testing.T) {
	if got := ListenPort("0.0.0.0:9443"); got != "9443" {
		t.Fatalf("port = %q", got)
	}
	if got := ListenPort("not-an-address"); got != "8443" {
		t.Fatalf("fallback port = %q", got)
	}
}

func TestHTTPSURLFormatsIPv6(t *testing.T) {
	got := HTTPSURL("2001:db8::1", "8443")
	if got != "https://[2001:db8::1]:8443" {
		t.Fatalf("url = %q", got)
	}
}

func TestLocalCandidatesIncludesLoopbackAndSkipsUnspecified(t *testing.T) {
	candidates := LocalCandidates("8443")
	for _, item := range candidates {
		if item.IP == "0.0.0.0" || strings.HasPrefix(item.IP, "169.254.") || strings.HasPrefix(item.IP, "fe80:") {
			t.Fatalf("unusable candidate: %#v", item)
		}
		if !strings.HasPrefix(item.URL, "https://") {
			t.Fatalf("url = %q", item.URL)
		}
	}
}

func TestLookupPublicIPAcceptsGlobalUnicast(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "203.0.113.10\n")
	}))
	t.Cleanup(server.Close)

	previousClient := publicClient
	previousEndpoints := publicEndpoints
	t.Cleanup(func() {
		publicClient = previousClient
		publicEndpoints = previousEndpoints
	})
	publicClient = server.Client()
	publicClient.Timeout = time.Second
	publicEndpoints = []string{server.URL}

	ip, err := LookupPublicIP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ip.String() != "203.0.113.10" {
		t.Fatalf("ip = %s", ip)
	}
}

func TestLookupPublicIPRejectsPrivateAddress(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "192.168.1.8")
	}))
	t.Cleanup(server.Close)

	previousClient := publicClient
	previousEndpoints := publicEndpoints
	t.Cleanup(func() {
		publicClient = previousClient
		publicEndpoints = previousEndpoints
	})
	publicClient = server.Client()
	publicEndpoints = []string{server.URL}

	if _, err := LookupPublicIP(context.Background()); err == nil {
		t.Fatal("private address should be rejected")
	}
}
