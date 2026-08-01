package provider

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
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

func TestSogouProviderRejectsUnrecognizedSuccessPage(t *testing.T) {
	client := newHTTPClient()
	client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString("<html>login page</html>")),
		}, nil
	})
	if _, err := newSogouProvider(client).Lookup(context.Background(), "13800138000"); err == nil {
		t.Fatal("unrecognized page must fail")
	}
}

func TestSogouProviderRejectsMismatchedCardDespitePageEcho(t *testing.T) {
	client := newHTTPClient()
	client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		body := `<div id="search-input">13800138000</div><div class="result-card"><h3 vrcid="title.x"><a>营销推广-号码查询服务</a></h3><p>13900139000</p></div>`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(body)),
		}, nil
	})
	if _, err := newSogouProvider(client).Lookup(context.Background(), "13800138000"); err == nil {
		t.Fatal("mismatched card must fail")
	}
}
