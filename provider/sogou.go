package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
)

const (
	sogouEndpoint  = "https://www.sogou.com/web"
	sogouUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

var (
	sogouTitlePattern   = regexp.MustCompile(`vrcid="title[^"]*">\s*<a[^>]*>([^<]*?)号码查询服务`)
	sogouBlockedPattern = regexp.MustCompile(`(?i)(antispider|window\.imgCode|验证码|访问频繁)`)
)

type sogouProvider struct {
	client *http.Client
}

func newSogouProvider(client *http.Client) lookupProvider {
	return &sogouProvider{client: client}
}

func (p *sogouProvider) Lookup(ctx context.Context, phone string) (*port.ProviderResult, error) {
	values := url.Values{}
	values.Set("query", phone)
	body, status, headers, err := doRequest(ctx, p.client, sogouEndpoint+"?"+values.Encode(), map[string]string{
		"Referer":    "https://www.sogou.com/",
		"User-Agent": sogouUserAgent,
	})
	if err != nil {
		return nil, err
	}
	if status == http.StatusForbidden || status == http.StatusTooManyRequests {
		return nil, rateLimitError(headers, errors.New("sogou rate limited"))
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: sogou HTTP status %d", domain.ErrUpstreamUnavailable, status)
	}
	if sogouBlockedPattern.Match(body) {
		return nil, rateLimitError(headers, errors.New("sogou anti-spider challenge"))
	}

	label := ""
	if match := sogouTitlePattern.FindStringSubmatch(string(body)); len(match) >= 2 {
		label = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(match[1]), "-"))
	}
	return &port.ProviderResult{
		IsSpam: label != "",
		Tag:    label,
		Source: SogouSourceID,
	}, nil
}
