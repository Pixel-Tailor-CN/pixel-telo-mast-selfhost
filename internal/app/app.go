package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/httpapi"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/baselinesync"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/security"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/storage/baseline"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/storage/runtime"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/provider"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/service"
)

type App struct {
	config    *config.Config
	server    *http.Server
	runtime   *runtime.Repository
	baseline  *baseline.Store
	query     *service.Service
	sync      *baselinesync.Manager
	closeOnce sync.Once
}

type Options struct {
	Config         *config.Config
	Token          []byte
	Version        string
	Commit         string
	InstanceID     string
	BaselineClient baselinesync.Client
}

func Build(options Options) (*App, error) {
	if options.Config == nil {
		return nil, errors.New("config is required")
	}
	runtimeRepo, err := runtime.Open(options.Config.Storage.RuntimePath)
	if err != nil {
		return nil, err
	}
	baseStore := baseline.NewStore()
	cleanup := func() { _ = baseStore.Close(); _ = runtimeRepo.Close() }
	if options.InstanceID == "" {
		options.InstanceID, err = runtimeRepo.EnsureInstanceID(context.Background())
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("load instance identity: %w", err)
		}
	}
	dispatcher, err := provider.NewDispatcher(provider.Config{Sources: providerSources(options.Config.Upstream.ProviderIDs)})
	if err != nil {
		cleanup()
		return nil, err
	}
	overlay := baseline.NewOverlay(runtimeRepo, baseStore)
	query, err := service.New(overlay, dispatcher, port.NoopMetrics{}, service.Options{QueryTimeout: options.Config.Query.Timeout.Std(), DefaultSources: options.Config.Upstream.ProviderIDs})
	if err != nil {
		cleanup()
		return nil, err
	}
	var syncManager *baselinesync.Manager
	if options.Config.Baseline.Enabled {
		client := options.BaselineClient
		if client == nil {
			client = baselinesync.NewHTTPClient(nil)
		}
		syncManager, err = baselinesync.NewManager(baselinesync.Options{Client: client, Store: baseStore, ActivePath: options.Config.Storage.RuntimePath + ".baseline.db", CheckInterval: options.Config.Baseline.CheckInterval.Std(), InstanceID: options.InstanceID})
		if err != nil {
			_ = query.Close()
			cleanup()
			return nil, err
		}
	}
	headers := security.ServerHeaders{Version: options.Version, APIVersion: "2", InstanceID: options.InstanceID}
	handler := &httpapi.Handler{Service: query, Headers: headers, Token: options.Token, BuildCommit: options.Commit, Capabilities: []string{"query_v2", "spki_pairing"}}
	return &App{config: options.Config, server: &http.Server{Addr: options.Config.Server.Listen, Handler: NewRouter(handler)}, runtime: runtimeRepo, baseline: baseStore, query: query, sync: syncManager}, nil
}

func providerSources(ids []string) []provider.SourceConfig {
	result := make([]provider.SourceConfig, 0, len(ids))
	for _, id := range ids {
		result = append(result, provider.SourceConfig{ID: id})
	}
	return result
}

func (a *App) Start(ctx context.Context, tlsConfig *tls.Config) error {
	if a.sync != nil && a.config.Baseline.SyncOnStart {
		if err := a.sync.Sync(ctx); err != nil {
			return fmt.Errorf("baseline startup synchronization failed: %w", err)
		}
	}
	listener, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return fmt.Errorf("listen self-host server: %w", err)
	}
	if tlsConfig != nil {
		listener = tls.NewListener(listener, tlsConfig)
	}
	go func() { _ = a.server.Serve(listener) }()
	if a.sync != nil {
		go func() { _ = a.sync.Run(ctx) }()
	}
	return nil
}

func (a *App) Close(ctx context.Context) error {
	var err error
	a.closeOnce.Do(func() {
		if a.server != nil {
			err = a.server.Shutdown(ctx)
		}
		if a.sync != nil {
			_ = a.sync.Close()
		}
		if a.query != nil {
			_ = a.query.Close()
		}
		if a.baseline != nil {
			_ = a.baseline.Close()
		}
		if a.runtime != nil {
			_ = a.runtime.Close()
		}
	})
	return err
}
