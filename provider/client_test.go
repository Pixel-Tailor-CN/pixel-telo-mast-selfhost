package provider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestClientVerifiesTLS(t *testing.T) {
	client := newHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS verification must be enabled")
	}
}

func TestSogouRateLimitPreservesRetryAfter(t *testing.T) {
	client := newHTTPClient()
	client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": []string{"3"}},
			Body:       io.NopCloser(bytes.NewReader(nil)),
		}, nil
	})
	_, err := newSogouProvider(client).Lookup(context.Background(), "13800138000")
	if !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("error = %v, want ErrRateLimited", err)
	}
	var rateLimit *domain.RateLimitError
	if !errors.As(err, &rateLimit) || rateLimit.RetryAfter != 3*time.Second {
		t.Fatalf("rate limit error = %#v", rateLimit)
	}
}
