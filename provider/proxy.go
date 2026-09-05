package provider

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ValidateProxyURL 校验显式固定代理；错误文本不包含地址或凭据。空值表示不覆盖客户端代理。
func ValidateProxyURL(raw string) error {
	_, err := parseProxyURL(raw)
	return err
}

func parseProxyURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.Opaque != "" || strings.ContainsAny(raw, "?#") {
		return nil, errors.New("proxy_url must be an HTTP or HTTPS root URL")
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, errors.New("proxy_url port is invalid")
		}
	} else if strings.HasSuffix(parsed.Host, ":") {
		return nil, errors.New("proxy_url port is invalid")
	}
	return parsed, nil
}

func clientWithProxy(base *http.Client, raw string) (*http.Client, error) {
	proxyURL, err := parseProxyURL(raw)
	if err != nil {
		return nil, err
	}
	if proxyURL == nil {
		return base, nil
	}
	transport := base.Transport
	if transport == nil {
		transport = newHTTPClient().Transport
	}
	standard, ok := transport.(*http.Transport)
	if !ok || standard == nil {
		return nil, errors.New("proxy_url requires an HTTP transport")
	}
	clonedTransport := standard.Clone()
	clonedTransport.Proxy = http.ProxyURL(proxyURL)
	clonedClient := *base
	clonedClient.Transport = clonedTransport
	return &clonedClient, nil
}
