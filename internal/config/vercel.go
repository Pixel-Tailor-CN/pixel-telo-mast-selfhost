package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/provider"
)

// VercelConfig 是 Vercel 部署模式使用的环境配置，不构造完整 YAML Config。
type VercelConfig struct {
	DatabaseURL     string
	Token           []byte
	ProviderIDs     []string
	ProviderProxies map[string]string
}

// LoadVercel 从环境变量读取并校验 Vercel 配置。getenv 为 nil 时使用 os.Getenv。
// 错误只包含变量名，不包含变量值；未知 Provider 留给应用构造时的 Dispatcher 拒绝。
func LoadVercel(getenv func(string) string) (*VercelConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	databaseURL := strings.TrimSpace(getenv("DATABASE_URL"))
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	token, err := normalizeAuthToken([]byte(getenv("MAST_TOKEN")))
	if err != nil {
		return nil, fmt.Errorf("MAST_TOKEN: %w", err)
	}

	providerIDs := parseProviderIDs(getenv("MAST_PROVIDER_IDS"))
	if len(providerIDs) == 0 {
		return nil, fmt.Errorf("MAST_PROVIDER_IDS is required")
	}

	proxies := make(map[string]string)
	for id, key := range map[string]string{"sogou": "MAST_SOGOU_PROXY_URL", "360": "MAST_360_PROXY_URL"} {
		raw := strings.TrimSpace(getenv(key))
		if err := provider.ValidateProxyURL(raw); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		if raw != "" {
			proxies[id] = raw
		}
	}
	return &VercelConfig{
		DatabaseURL:     databaseURL,
		Token:           token,
		ProviderIDs:     providerIDs,
		ProviderProxies: proxies,
	}, nil
}

func parseProviderIDs(raw string) []string {
	parts := strings.Split(raw, ",")
	ids := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}
