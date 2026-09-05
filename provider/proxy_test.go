package provider

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProxyConnectReturnsVerifiedCard(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials reached upstream")
		}
		_, err := io.WriteString(w, `<div><h3 vrcid="title.x">营销推广-号码查询服务</h3><p>13800138000</p></div>`)
		if err != nil {
			t.Errorf("write fixture: %v", err)
		}
	}))
	defer upstream.Close()
	var connects atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "www.sogou.com:443" {
			http.Error(w, "unexpected proxy request", 400)
			return
		}
		connects.Add(1)
		if r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpzZWNyZXQ=" {
			t.Error("proxy authentication missing")
		}
		upstreamConn, err := net.DialTimeout("tcp", upstream.Listener.Addr().String(), time.Second)
		if err != nil {
			t.Errorf("dial fixture: %v", err)
			return
		}
		defer upstreamConn.Close()
		conn, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack proxy: %v", err)
			return
		}
		defer conn.Close()
		if _, err := buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			t.Errorf("write CONNECT: %v", err)
			return
		}
		if err := buffer.Flush(); err != nil {
			t.Errorf("flush CONNECT: %v", err)
			return
		}
		done := make(chan error, 1)
		go func() { _, err := io.Copy(upstreamConn, buffer); done <- err }()
		_, copyErr := io.Copy(conn, upstreamConn)
		if copyErr != nil && !errors.Is(copyErr, net.ErrClosed) {
			t.Errorf("copy response: %v", copyErr)
		}
		conn.Close()
		if err := <-done; err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("copy request: %v", err)
		}
	}))
	defer proxy.Close()
	base := newHTTPClient()
	transport := base.Transport.(*http.Transport)
	transport.TLSClientConfig = upstream.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	transport.TLSClientConfig.ServerName = "127.0.0.1"
	d, err := NewDispatcher(Config{HTTPClient: base, Sources: []SourceConfig{{ID: "sogou", ProxyURL: strings.Replace(proxy.URL, "http://", "http://user:secret@", 1)}}})
	if err != nil {
		t.Fatal(err)
	}
	client := d.sources["sogou"].provider.(*sogouProvider).client
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	results, failures := d.LookupAll(ctx, "13800138000", []string{"sogou"})
	result := results["sogou"]
	if len(failures) != 0 || result == nil || !result.IsSpam || result.Tag != "营销推广" || connects.Load() != 1 || upstreamCalls.Load() != 1 {
		t.Fatalf("result = %v, failures = %v", result, failures)
	}
}

func TestValidateProxyURL(t *testing.T) {
	for _, raw := range []string{"", "http://127.0.0.1:8080", "https://user:secret@proxy.example:443/"} {
		if err := ValidateProxyURL(raw); err != nil {
			t.Fatalf("valid proxy rejected: %v", err)
		}
	}
	for _, raw := range []string{"socks5://proxy.example:1080", "proxy.example:8080", "http://", "http://proxy.example/path", "http://proxy.example?secret", "http://proxy.example#secret", "http://proxy.example:0", "http://proxy.example:65536", "http://user:secret@%invalid"} {
		if err := ValidateProxyURL(raw); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe proxy validation: %v", err)
		}
	}
}

func TestSourceProxyIsIsolated(t *testing.T) {
	base := newHTTPClient()
	d, err := NewDispatcher(Config{HTTPClient: base, Sources: []SourceConfig{{ID: "sogou", ProxyURL: "http://proxy.example:8080"}, {ID: "360"}}})
	if err != nil {
		t.Fatal(err)
	}
	sogou := d.sources["sogou"].provider.(*sogouProvider).client
	so360 := d.sources["360"].provider.(*so360Provider).client
	if base.Transport.(*http.Transport).Proxy != nil || so360 != base {
		t.Fatal("shared client mutated")
	}
	proxy, err := sogou.Transport.(*http.Transport).Proxy(&http.Request{})
	if err != nil || proxy.Host != "proxy.example:8080" {
		t.Fatal("source proxy not applied")
	}
	if sogou.CheckRedirect == nil {
		t.Fatal("redirect policy lost")
	}
	if tls := sogou.Transport.(*http.Transport).TLSClientConfig; tls != nil && tls.InsecureSkipVerify {
		t.Fatal("TLS verification disabled")
	}
}

func TestProxyUsesConnectAndPreservesDeadline(t *testing.T) {
	var calls atomic.Int32
	var valid atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		valid.Store(r.Method == http.MethodConnect && r.Host == "www.sogou.com:443")
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	d, err := NewDispatcher(Config{Sources: []SourceConfig{{ID: "sogou", ProxyURL: proxy.URL}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, failures := d.LookupAll(ctx, "13800138000", []string{"sogou"})
	if calls.Load() != 1 || !valid.Load() || failures["sogou"] == nil {
		t.Fatal("HTTPS proxy not used")
	}
	if strings.Contains(failures["sogou"].Error(), "13800138000") {
		t.Fatal("phone leaked")
	}
	ctx, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	_, failures = d.LookupAll(ctx, "13800138000", []string{"sogou"})
	if !errors.Is(failures["sogou"], context.Canceled) || calls.Load() != 1 {
		t.Fatal("cancellation lost")
	}
}

func TestProxyRejectsCustomTransportWithoutLeakingCredentials(t *testing.T) {
	base := newHTTPClient()
	base.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret") })
	_, err := NewDispatcher(Config{HTTPClient: base, Sources: []SourceConfig{{ID: "sogou", ProxyURL: "http://user:secret@proxy.example"}}})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error = %v", err)
	}
	_, _, _, err = doRequest(context.Background(), base, "https://www.sogou.com/web?query=13800138000", nil)
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "13800138000") {
		t.Fatal("request error leaked data")
	}
}
