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
	"golang.org/x/net/html"
)

const (
	sogouEndpoint  = "https://www.sogou.com/web"
	sogouUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

var (
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

	label, err := parseSogouCard(body, phone)
	if err != nil {
		return nil, invalidProviderResponse(err)
	}
	return &port.ProviderResult{
		IsSpam: label != "",
		Tag:    label,
		Source: SogouSourceID,
	}, nil
}

func parseSogouCard(body []byte, phone string) (string, error) {
	root, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("%w: parse sogou HTML", domain.ErrUpstreamUnavailable)
	}
	expectedDigits := digitsOnly(phone)
	if expectedDigits == "" {
		return "", fmt.Errorf("%w: invalid sogou query", domain.ErrUpstreamUnavailable)
	}

	matched := false
	label := ""
	walkHTML(root, func(node *html.Node) {
		if matched || node.Type != html.ElementNode || !hasAttributePrefix(node, "vrcid", "title") {
			return
		}
		title := nodeText(node)
		titleEnd := strings.Index(title, "号码查询服务")
		if titleEnd < 0 {
			return
		}
		container := node.Parent
		if container == nil || container.Data == "body" || container.Data == "html" {
			return
		}
		if !strings.Contains(digitsOnly(nodeText(container)), expectedDigits) {
			return
		}
		label = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(title[:titleEnd]), "-"))
		matched = true
	})
	if !matched {
		// 兼容 Mast 既有语义：成功页面未命中号码卡时按普通号码处理。
		return "", nil
	}
	return label, nil
}

func hasAttributePrefix(node *html.Node, key, prefix string) bool {
	for _, attribute := range node.Attr {
		if attribute.Key == key && strings.HasPrefix(attribute.Val, prefix) {
			return true
		}
	}
	return false
}
