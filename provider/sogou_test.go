package provider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
)

func TestSogouChallengeRedirectStopsAndCoolsDown(t *testing.T) {
	for _, destination := range []string{"http://www.sogou.com/antispider/?from=private", "https://www.sogou.com/antispider/"} {
		t.Run(destination, func(t *testing.T) {
			calls := 0
			client := newHTTPClient()
			client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {destination}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			d, err := NewDispatcher(Config{Sources: []SourceConfig{{ID: SogouSourceID}}, HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				results, failures := d.LookupAll(context.Background(), "13800138000", []string{SogouSourceID})
				var limited *domain.RateLimitError
				if len(results) != 0 || !errors.As(failures[SogouSourceID], &limited) || limited.RetryAfter <= 0 {
					t.Fatalf("results = %v, failure = %v", results, failures[SogouSourceID])
				}
				if strings.Contains(failures[SogouSourceID].Error(), "13800138000") {
					t.Fatal("phone leaked")
				}
			}
			if calls != 1 {
				t.Fatalf("requests = %d, want one before cooldown", calls)
			}
		})
	}
}

func TestSogouChallengePageHasCooldown(t *testing.T) {
	client := newHTTPClient()
	client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("验证码"))}, nil
	})
	_, err := newSogouProvider(client).Lookup(context.Background(), "13800138000")
	var limited *domain.RateLimitError
	if !errors.As(err, &limited) || limited.RetryAfter != 30*time.Second {
		t.Fatalf("error = %v", err)
	}
}

func TestSogouRedirectDoesNotRelaxOtherPolicies(t *testing.T) {
	for _, destination := range []string{"http://www.sogou.com/other", "http://www.sogou.com.evil.example/antispider/"} {
		client := newHTTPClient()
		calls := 0
		client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {destination}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		})
		_, err := newSogouProvider(client).Lookup(context.Background(), "13800138000")
		if err == nil || errors.Is(err, domain.ErrRateLimited) || calls != 1 {
			t.Fatalf("redirect policy changed: %v", err)
		}
	}
}

func TestSogouRequestPreservesContext(t *testing.T) {
	client := newHTTPClient()
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := newSogouProvider(client).Lookup(ctx, "13800138000")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline lost: %v", err)
	}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	_, err = newSogouProvider(client).Lookup(ctx, "13800138000")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestSogouRejectsPhoneSubstringAndJoinedDigits(t *testing.T) {
	for _, numberText := range []string{"9138001380009", "13800 138000"} {
		body := []byte(`<div><h3 vrcid="title.x">营销推广-号码查询服务</h3><p>` + numberText + `</p></div>`)
		if _, err := parseSogouCard(body, "13800138000"); err == nil {
			t.Fatal("mismatched phone accepted")
		}
	}
}

func TestSogouRedirectPreservesRetryAfterAndCustomPolicy(t *testing.T) {
	client := newHTTPClient()
	client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://www.sogou.com/antispider/"}, "Retry-After": {"7"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	_, err := newSogouProvider(client).Lookup(context.Background(), "13800138000")
	var limited *domain.RateLimitError
	if !errors.As(err, &limited) || limited.RetryAfter != 7*time.Second {
		t.Fatalf("retry-after lost: %v", err)
	}
	policyErr := errors.New("custom redirect policy")
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return policyErr }
	client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://www.sogou.com/other"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	_, err = newSogouProvider(client).Lookup(context.Background(), "13800138000")
	if !errors.Is(err, policyErr) {
		t.Fatal("custom redirect policy lost")
	}
}

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
	got, err := newSogouProvider(client).Lookup(context.Background(), "13800138000")
	if got != nil || !errors.Is(err, errInvalidProviderResponse) {
		t.Fatalf("result = %v, error = %v, want invalid response", got, err)
	}
}

func TestSogouProviderRejectsMismatchedCard(t *testing.T) {
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
	if got != nil || !errors.Is(err, errInvalidProviderResponse) {
		t.Fatalf("result = %v, error = %v, want invalid response", got, err)
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
