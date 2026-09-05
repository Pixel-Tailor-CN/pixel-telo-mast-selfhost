package provider

import (
	"context"
	"encoding/json"
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
	so360Endpoint  = "https://open.onebox.so.com/dataApi"
	so360Callback  = "callback"
	so360UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

var (
	so360BlockedPattern = regexp.MustCompile(`(?i)(访问过于频繁|访问频繁|安全验证|验证码|captcha|antispider)`)
	errSo360NoPhoneData = errors.New("so360 returned no phone data")
)

type so360Provider struct {
	client *http.Client
}

type so360Response struct {
	HTML  string `json:"html"`
	Query string `json:"query"`
	Type  string `json:"type"`
}

func newSo360Provider(client *http.Client) lookupProvider {
	return &so360Provider{client: client}
}

func (p *so360Provider) Lookup(ctx context.Context, phone string) (*port.ProviderResult, error) {
	body, status, headers, err := doRequest(ctx, p.client, buildSo360URL(phone), map[string]string{
		"Accept":     "application/javascript, application/json, */*",
		"Referer":    "https://www.so.com/",
		"User-Agent": so360UserAgent,
	})
	if err != nil {
		return nil, err
	}
	if status == http.StatusForbidden || status == http.StatusTooManyRequests || so360BlockedPattern.Match(body) {
		return nil, rateLimitError(headers, errors.New("so360 rate limited"))
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%w: so360 HTTP status %d", domain.ErrUpstreamUnavailable, status)
	}

	label, err := parseSo360Response(body, phone)
	if errors.Is(err, errSo360NoPhoneData) {
		return nil, fmt.Errorf("%w: %w", domain.ErrUpstreamUnavailable, err)
	}
	if err != nil {
		return nil, invalidProviderResponse(err)
	}
	return &port.ProviderResult{
		IsSpam: label != "",
		Tag:    label,
		Source: So360SourceID,
	}, nil
}

func buildSo360URL(phone string) string {
	values := url.Values{}
	values.Set("query", phone)
	values.Set("url", "mobilecheck")
	values.Set("num", "1")
	values.Set("type", "mobilecheck")
	values.Set("src", "onebox")
	values.Set("tpl", "1")
	values.Set("callback", so360Callback)
	return so360Endpoint + "?" + values.Encode()
}

func parseSo360Response(body []byte, phone string) (string, error) {
	payload, err := unwrapSo360JSONP(body)
	if err != nil {
		return "", err
	}
	// OneBox 未提供号码数据时可能返回空字符串；这不代表号码未被标记。
	switch string(payload) {
	case "''", `""`, "null":
		return "", errSo360NoPhoneData
	}
	var response so360Response
	if err := json.Unmarshal(payload, &response); err != nil {
		return "", fmt.Errorf("so360 response decode error: %w", err)
	}
	if response.Type != "mobilecheck" {
		return "", errors.New("so360 unexpected response type")
	}
	if strings.TrimSpace(response.Query) != strings.TrimSpace(phone) {
		return "", errors.New("so360 response query mismatch")
	}
	if strings.TrimSpace(response.HTML) == "" {
		return "", errors.New("so360 response HTML is empty")
	}
	return parseSo360Card(response.HTML, phone)
}

func unwrapSo360JSONP(body []byte) ([]byte, error) {
	wrapped := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(string(body)), ";"))
	prefix := so360Callback + "("
	if !strings.HasPrefix(wrapped, prefix) || !strings.HasSuffix(wrapped, ")") {
		return nil, errors.New("so360 invalid JSONP wrapper")
	}
	payload := strings.TrimSpace(wrapped[len(prefix) : len(wrapped)-1])
	if payload == "" {
		return nil, errors.New("so360 empty JSONP payload")
	}
	return []byte(payload), nil
}

func parseSo360Card(cardHTML, phone string) (string, error) {
	root, err := html.Parse(strings.NewReader(cardHTML))
	if err != nil {
		return "", fmt.Errorf("so360 HTML parse error: %w", err)
	}
	expectedDigits := digitsOnly(phone)
	if expectedDigits == "" {
		return "", errors.New("so360 query has no digits")
	}

	var label string
	markFound := false
	cardMatched := false
	walkHTML(root, func(node *html.Node) {
		if hasClass(node, "mohe-ph-mark") && !markFound {
			markFound = true
			label = nodeText(node)
		}
		if hasClass(node, "mh-des-phone") || hasClass(node, "mh-detail") {
			if strings.Contains(digitsOnly(nodeText(node)), expectedDigits) {
				cardMatched = true
			}
		}
	})
	if !cardMatched {
		return "", errors.New("so360 phone card mismatch")
	}
	if markFound && label == "" {
		return "", errors.New("so360 spam mark is empty")
	}
	return label, nil
}

func walkHTML(node *html.Node, visit func(*html.Node)) {
	visit(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walkHTML(child, visit)
	}
}

func hasClass(node *html.Node, className string) bool {
	if node.Type != html.ElementNode {
		return false
	}
	for _, attr := range node.Attr {
		if attr.Key == "class" {
			for _, token := range strings.Fields(attr.Val) {
				if token == className {
					return true
				}
			}
		}
	}
	return false
}

func nodeText(node *html.Node) string {
	parts := make([]string, 0)
	walkHTML(node, func(current *html.Node) {
		if current.Type == html.TextNode {
			parts = append(parts, current.Data)
		}
	})
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func digitsOnly(value string) string {
	var builder strings.Builder
	for _, char := range value {
		if char >= '0' && char <= '9' {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}
