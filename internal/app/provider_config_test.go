package app

import (
	"testing"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
)

func TestProviderProxyComposition(t *testing.T) {
	const proxy = "http://127.0.0.1:8080"
	cfg := &config.Config{}
	cfg.Upstream.ProviderIDs = []string{"sogou", "360"}
	cfg.Providers = map[string]config.ProviderConfig{"sogou": {ProxyURL: proxy}}
	sources := providerSources(cfg)
	if sources[0].ProxyURL != proxy || sources[1].ProxyURL != "" {
		t.Fatal("traditional proxy mapping failed")
	}
	options, err := vercelCompositionOptions(&config.VercelConfig{ProviderIDs: cfg.Upstream.ProviderIDs, ProviderProxies: map[string]string{"sogou": proxy}}, nil, nil, "test", "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	if options.ProviderSources[0].ProxyURL != proxy || options.ProviderSources[1].ProxyURL != "" {
		t.Fatal("vercel proxy mapping failed")
	}
}
