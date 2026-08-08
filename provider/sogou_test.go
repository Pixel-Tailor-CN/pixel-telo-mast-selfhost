package provider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
)

func TestSogouProviderParsesMarkedAndNormalNumbers(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantSpam bool
		wantTag  string
	}{
		{
			name:     "标记号码",
			body:     `<div class="result-card"><h3 vrcid="title.x"><a>营销推广-号码查询服务</a></h3><p>13800138000</p></div>`,
			wantSpam: true,
			wantTag:  "营销推广",
		},
		{
			name: "普通号码",
			body: `<div class="result-card"><h3 vrcid="title.x"><a>号码查询服务</a></h3><p>13800138000</p></div>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newHTTPClient()
			client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(tt.body)),
				}, nil
			})
			got, err := newSogouProvider(client).Lookup(context.Background(), "13800138000")
			if err != nil {
				t.Fatal(err)
			}
			if got.IsSpam != tt.wantSpam || got.Tag != tt.wantTag || got.Source != SogouSourceID {
				t.Fatalf("result = %#v", got)
			}
		})
	}
}

func TestSogouProviderTreatsUnrecognizedSuccessPageAsNormal(t *testing.T) {
	client := newHTTPClient()
	client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString("<html>login page</html>")),
		}, nil
	})
	got, err := newSogouProvider(client).Lookup(context.Background(), "13800138000")
	if err != nil {
		t.Fatalf("Lookup error: %v", err)
	}
	if got.IsSpam || got.Tag != "" || got.Source != SogouSourceID {
		t.Fatalf("result = %#v, want normal sogou result", got)
	}
}

func TestSogouProviderTreatsMismatchedCardAsNormal(t *testing.T) {
	client := newHTTPClient()
	client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		body := `<div id="search-input">13800138000</div><div class="result-card"><h3 vrcid="title.x"><a>营销推广-号码查询服务</a></h3><p>13900139000</p></div>`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(body)),
		}, nil
	})
	got, err := newSogouProvider(client).Lookup(context.Background(), "13800138000")
	if err != nil {
		t.Fatalf("Lookup error: %v", err)
	}
	if got.IsSpam || got.Tag != "" || got.Source != SogouSourceID {
		t.Fatalf("result = %#v, want normal sogou result", got)
	}
}

func TestSogouProviderDoesNotDowngradeExplicitUpstreamErrors(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantError error
	}{
		{name: "反爬页面", status: http.StatusOK, body: `<script>window.imgCode = true</script>`, wantError: domain.ErrRateLimited},
		{name: "禁止访问", status: http.StatusForbidden, wantError: domain.ErrRateLimited},
		{name: "上游错误", status: http.StatusInternalServerError, wantError: domain.ErrUpstreamUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newHTTPClient()
			client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tt.status,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(tt.body)),
				}, nil
			})

			if _, err := newSogouProvider(client).Lookup(context.Background(), "13800138000"); !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
		})
	}
}
