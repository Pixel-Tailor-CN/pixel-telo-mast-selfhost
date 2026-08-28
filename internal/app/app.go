package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/httpapi"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/baselinesync"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/security"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/storage/baseline"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/storage/runtime"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/provider"
	queryDomain "github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
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
	handler   *httpapi.Handler
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
	if options.Config.Baseline.Enabled {
		activePath, metadataErr := runtimeRepo.GetMetadata(context.Background(), baselinesync.ActivePathMetadataKey)
		pendingPath, pendingErr := runtimeRepo.GetMetadata(context.Background(), "baseline_pending_path")
		if pendingErr != nil && !errors.Is(pendingErr, queryDomain.ErrNotFound) {
			cleanup()
			return nil, fmt.Errorf("load baseline pending path: %w", pendingErr)
		}
		if pendingErr == nil {
			activePath = pendingPath
		}
		if metadataErr == nil || pendingErr == nil {
			activePath, err = filepath.Abs(activePath)
			if err != nil {
				cleanup()
				return nil, fmt.Errorf("resolve baseline active path: %w", err)
			}
			if _, err := os.Stat(activePath); err == nil {
				if err := baseStore.Replace(activePath); err != nil {
					cleanup()
					return nil, fmt.Errorf("load baseline active snapshot: %w", err)
				}
				if pendingErr == nil {
					if err := runtimeRepo.SetMetadata(context.Background(), baselinesync.ActivePathMetadataKey, activePath); err != nil {
						cleanup()
						return nil, fmt.Errorf("promote pending baseline path: %w", err)
					}
					if err := runtimeRepo.DeleteMetadata(context.Background(), "baseline_pending_path"); err != nil {
						cleanup()
						return nil, fmt.Errorf("clear pending baseline path: %w", err)
					}
				}
			} else if !os.IsNotExist(err) {
				cleanup()
				return nil, fmt.Errorf("inspect baseline active snapshot: %w", err)
			}
		} else if !errors.Is(metadataErr, queryDomain.ErrNotFound) {
			cleanup()
			return nil, fmt.Errorf("load baseline active path: %w", metadataErr)
		}
	}
	if options.InstanceID == "" {
		options.InstanceID, err = runtimeRepo.EnsureInstanceID(context.Background())
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("load instance identity: %w", err)
		}
	}
	dispatcher, err := provider.NewDispatcher(provider.Config{Sources: providerSources(options.Config)})
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
		syncManager, err = baselinesync.NewManager(baselinesync.Options{Client: client, Store: baseStore, Metadata: runtimeRepo, ActivePath: options.Config.Storage.RuntimePath + ".baseline.db", CheckInterval: options.Config.Baseline.CheckInterval.Std(), InstanceID: options.InstanceID})
		if err != nil {
			_ = query.Close()
			cleanup()
			return nil, err
		}
	}
	headers := security.ServerHeaders{Version: options.Version, APIVersion: "2", InstanceID: options.InstanceID}
	handler := &httpapi.Handler{Service: query, Headers: headers, Token: options.Token, Limiter: security.NewQueryLimiter(options.Config.RateLimit.RequestsPerSecond, options.Config.RateLimit.Burst, options.Config.Query.MaxConcurrent), BuildCommit: options.Commit, Capabilities: []string{"query_v2", "spki_pairing"}}
	spki := ""
	if options.Config.TLS.Mode != "off" {
		certFile, _ := options.Config.TLSFiles()
		if pin, pinErr := security.CertificateSPKI(certFile); pinErr == nil {
			spki = pin
		}
	}
	if _, err := handler.StartPairingSession(options.Config.TLS.PublicURL, spki, time.Now()); err != nil {
		slog.Info("pairing page not published", "error_type", fmt.Sprintf("%T", err))
	}
	return &App{config: options.Config, server: &http.Server{Addr: options.Config.Server.Listen, Handler: NewRouter(handler)}, runtime: runtimeRepo, baseline: baseStore, query: query, sync: syncManager, handler: handler}, nil
}

func (a *App) PairingPageURL() string {
	if a == nil || a.handler == nil {
		return ""
	}
	return a.handler.PairingPageURL()
}

func providerSources(cfg *config.Config) []provider.SourceConfig {
	result := make([]provider.SourceConfig, 0, len(cfg.Upstream.ProviderIDs))
	for _, id := range cfg.Upstream.ProviderIDs {
		settings := cfg.Providers[id]
		result = append(result, provider.SourceConfig{ID: id, MinInterval: settings.MinInterval.Std(), MaxConcurrent: settings.MaxConcurrent, BreakerTimeout: settings.BreakerTimeout.Std()})
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
	go func() {
		if err := a.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("self-host server stopped unexpectedly", "error", err)
		}
	}()
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
