package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
)

func TestSo360EmptyResultIsUnavailable(t *testing.T) {
	for _, body := range []string{` callback('')`, `callback("");`, `callback(null)`, "\n callback( '' ); \n"} {
		t.Run(body, func(t *testing.T) {
			client := newHTTPClient()
			client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			result, err := newSo360Provider(client).Lookup(context.Background(), "4001001000")
			if result != nil || !errors.Is(err, domain.ErrUpstreamUnavailable) {
				t.Fatalf("result = %v, error = %v, want unavailable without result", result, err)
			}
			if errors.Is(err, errInvalidProviderResponse) || strings.Contains(err.Error(), "decode error") {
				t.Fatalf("empty result classified as parse failure: %v", err)
			}
			if !strings.Contains(err.Error(), "so360 returned no phone data") {
				t.Fatalf("missing empty result diagnostic: %v", err)
			}
		})
	}
}

func TestSo360RejectsInvalidResponses(t *testing.T) {
	for _, body := range []string{
		`other('')`, `callback()`, `callback('unexpected')`, `callback('');alert(1)`,
		`callback({"type":"other","query":"4001001000","html":"x"})`,
		`callback({"type":"mobilecheck","query":"4001001001","html":"x"})`,
		`callback({"type":"mobilecheck","query":"4001001000","html":""})`,
		`callback({"type":"mobilecheck","query":"4001001000","html":"<span class='mh-detail'>4001001001</span>"})`,
		`callback({"type":"mobilecheck","query":"4001001000","html":"<span class='mh-detail'>4001001000</span><span class='mohe-ph-mark'></span>"})`,
	} {
		t.Run(body, func(t *testing.T) {
			if _, err := parseSo360Response([]byte(body), "4001001000"); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

func TestParseSo360Response(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		phone string
		want  string
	}{
		{
			name:  "标记号码",
			phone: "13120886542",
			body:  `callback({"html":"<span class='mh-des-phone'>13120886542</span><span class='mohe-ph-mark'>广告推销</span>","query":"13120886542","type":"mobilecheck"})`,
			want:  "广告推销",
		},
		{
			name:  "普通号码",
			phone: "13245678901",
			body:  `callback({"html":"<span class='mh-des-phone'>13245678901</span>","query":"13245678901","type":"mobilecheck"})`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSo360Response([]byte(tt.body), tt.phone)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("label = %q, want %q", got, tt.want)
			}
		})
	}
}
