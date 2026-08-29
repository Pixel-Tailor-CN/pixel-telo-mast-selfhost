package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/storage/postgres"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/provider"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/service"
)

const vercelInitTimeout = 5 * time.Second

// VercelApp 是 Vercel 部署模式的 Composition Root，不持有本地 TLS、SQLite 或 baseline。
type VercelApp struct {
	Handler   http.Handler
	query     *service.Service
	runtime   *postgres.Repository
	closeOnce sync.Once
}

// BuildVercel 打开 PostgreSQL、加载实例身份并组装只声明 query_v2 的 Router。
func BuildVercel(ctx context.Context, cfg *config.VercelConfig, logger *slog.Logger, version, commit string) (*VercelApp, error) {
	if cfg == nil {
		return nil, errors.New("vercel config is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	initCtx, cancel := context.WithTimeout(ctx, vercelInitTimeout)
	defer cancel()

	runtimeRepo, err := postgres.Open(initCtx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	instanceID, err := runtimeRepo.EnsureInstanceID(initCtx)
	if err != nil {
		_ = runtimeRepo.Close()
		return nil, fmt.Errorf("load instance identity: %w", err)
	}
	composition, err := composeVercel(cfg, runtimeRepo, logger, version, commit, instanceID)
	if err != nil {
		if composition != nil && composition.Query != nil {
			_ = composition.Query.Close()
		}
		_ = runtimeRepo.Close()
		return nil, fmt.Errorf("build vercel application: %w", err)
	}
	return &VercelApp{Handler: composition.Router, query: composition.Query, runtime: runtimeRepo}, nil
}

func composeVercel(cfg *config.VercelConfig, repo port.QueryRepository, logger *slog.Logger, version, commit, instanceID string) (*Composition, error) {
	options, err := vercelCompositionOptions(cfg, repo, logger, version, commit, instanceID)
	if err != nil {
		return nil, err
	}
	return compose(options)
}

func vercelCompositionOptions(cfg *config.VercelConfig, repo port.QueryRepository, logger *slog.Logger, version, commit, instanceID string) (CompositionOptions, error) {
	if cfg == nil {
		return CompositionOptions{}, errors.New("vercel config is required")
	}
	sources := make([]provider.SourceConfig, 0, len(cfg.ProviderIDs))
	for _, id := range cfg.ProviderIDs {
		sources = append(sources, provider.SourceConfig{ID: id})
	}
	return CompositionOptions{
		Repository:      repo,
		ProviderSources: sources,
		QueryOptions: service.Options{
			QueryTimeout:   2 * time.Second,
			SaveTimeout:    500 * time.Millisecond,
			DefaultSources: cfg.ProviderIDs,
			CacheWriteMode: service.CacheWriteSync,
		},
		Token:          cfg.Token,
		Version:        version,
		Commit:         commit,
		InstanceID:     instanceID,
		Capabilities:   []string{"query_v2"},
		DisablePairing: true,
		RateLimit: RateLimitOptions{
			RequestsPerSecond: 1,
			Burst:             5,
			MaxConcurrent:     4,
		},
		Logger: logger,
	}, nil
}

// Close 按逆序关闭 Query Service 和 PostgreSQL 连接池，可重复调用。
func (a *VercelApp) Close() error {
	if a == nil {
		return nil
	}
	var err error
	a.closeOnce.Do(func() {
		if a.query != nil {
			err = errors.Join(err, a.query.Close())
		}
		if a.runtime != nil {
			err = errors.Join(err, a.runtime.Close())
		}
	})
	return err
}
