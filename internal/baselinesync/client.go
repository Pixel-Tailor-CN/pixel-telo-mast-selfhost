package baselinesync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const defaultCheckURL = "https://pixeltelo.api.mystery0.vip/api/v1/sync/check"

// Manifest 描述官方 baseline 的可下载版本。
type Manifest struct {
	HasUpdate     bool   `json:"has_update"`
	LatestVersion string `json:"latest_version"`
	DownloadURL   string `json:"download_url"`
	SizeBytes     int64  `json:"size_bytes"`
	Checksum      string `json:"checksum"`
}

// Client 负责读取官方 baseline 元数据和下载压缩包。
type Client interface {
	Check(ctx context.Context, currentVersion string) (Manifest, error)
	Download(ctx context.Context, downloadURL string, dst io.Writer) (int64, error)
}

// HTTPClient 使用固定官方地址，URL 不从用户配置读取。
type HTTPClient struct {
	client   *http.Client
	checkURL string
}

func NewHTTPClient(client *http.Client) *HTTPClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPClient{client: client, checkURL: defaultCheckURL}
}

func (c *HTTPClient) Check(ctx context.Context, currentVersion string) (Manifest, error) {
	query := url.Values{"current_version": []string{currentVersion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.checkURL+"?"+query.Encode(), nil)
	if err != nil {
		return Manifest{}, fmt.Errorf("create baseline check request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return Manifest{}, fmt.Errorf("request baseline metadata: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Manifest{}, fmt.Errorf("baseline metadata returned HTTP %d", resp.StatusCode)
	}
	var manifest Manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode baseline metadata: %w", err)
	}
	if manifest.HasUpdate {
		if strings.TrimSpace(manifest.DownloadURL) == "" || manifest.SizeBytes <= 0 || strings.TrimSpace(manifest.Checksum) == "" {
			return Manifest{}, fmt.Errorf("baseline metadata is incomplete")
		}
		if _, err := strconv.ParseInt(manifest.LatestVersion, 10, 64); err != nil {
			return Manifest{}, fmt.Errorf("baseline version is invalid: %w", err)
		}
		parsed, err := url.Parse(manifest.DownloadURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return Manifest{}, fmt.Errorf("baseline download URL must use HTTPS")
		}
	}
	return manifest, nil
}

func (c *HTTPClient) Download(ctx context.Context, downloadURL string, dst io.Writer) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return 0, fmt.Errorf("create baseline download request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download baseline archive: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("baseline archive returned HTTP %d", resp.StatusCode)
	}
	n, err := io.Copy(dst, resp.Body)
	if err != nil {
		return n, fmt.Errorf("write baseline archive: %w", err)
	}
	return n, nil
}
