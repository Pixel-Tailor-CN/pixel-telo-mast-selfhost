package provider

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
)

const maxResponseBytes = 256 * 1024

var errInvalidProviderResponse = errors.New("invalid provider response")

func invalidProviderResponse(err error) error {
	return fmt.Errorf("%w: %w", errInvalidProviderResponse, err)
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			ForceAttemptHTTP2:   true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many provider redirects")
			}
			if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
				return errors.New("provider HTTPS downgrade redirect rejected")
			}
			return nil
		},
	}
}

func rateLimitError(headers http.Header, cause error) error {
	retryAfter := time.Duration(0)
	value := strings.TrimSpace(headers.Get("Retry-After"))
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		retryAfter = time.Duration(seconds) * time.Second
	} else if retryAt, err := http.ParseTime(value); err == nil {
		retryAfter = time.Until(retryAt)
		if retryAfter < 0 {
			retryAfter = 0
		}
	}
	return &domain.RateLimitError{RetryAfter: retryAfter, Cause: cause}
}

func doRequest(ctx context.Context, client *http.Client, requestURL string, headers map[string]string) ([]byte, int, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("create provider request: %w", err)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "gzip")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("execute provider request: %w", err)
	}
	defer resp.Body.Close()

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzipReader, gzipErr := gzip.NewReader(resp.Body)
		if gzipErr != nil {
			return nil, resp.StatusCode, resp.Header.Clone(), fmt.Errorf("open provider gzip response: %w", gzipErr)
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	body, err := io.ReadAll(io.LimitReader(reader, maxResponseBytes+1))
	if err != nil {
		return nil, resp.StatusCode, resp.Header.Clone(), fmt.Errorf("read provider response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, resp.StatusCode, resp.Header.Clone(), errors.New("provider response exceeds size limit")
	}
	return body, resp.StatusCode, resp.Header.Clone(), nil
}
