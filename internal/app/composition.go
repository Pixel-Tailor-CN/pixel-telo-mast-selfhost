package app

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/httpapi"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/security"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/provider"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/service"
	"github.com/gin-gonic/gin"
)

type CompositionOptions struct {
	Repository      port.QueryRepository
	ProviderSources []provider.SourceConfig
	QueryOptions    service.Options
	Token           []byte
	Version         string
	Commit          string
	InstanceID      string
	Capabilities    []string
	// DisablePairing 为 true 时不注册配对页、不发布配对会话。零值保持传统默认。
	DisablePairing bool
	PairingURL     string
	PairingSPKI    string
	RateLimit      RateLimitOptions
	Logger         *slog.Logger
	HTTPClient     *http.Client
}

// RateLimitOptions 注入查询限流。全零表示未注入，compose 会把 Handler.Limiter 留空，
// 由 httpapi.Handler.Register 使用默认 limiter；任一字段非正且并非全零时返回构造错误。
type RateLimitOptions struct {
	RequestsPerSecond float64
	Burst             int
	MaxConcurrent     int
}

type Composition struct {
	Router  *gin.Engine
	Query   *service.Service
	Handler *httpapi.Handler
}

func compose(options CompositionOptions) (*Composition, error) {
	limiter, err := queryLimiter(options.RateLimit)
	if err != nil {
		return nil, err
	}
	dispatcher, err := provider.NewDispatcher(provider.Config{Sources: options.ProviderSources, HTTPClient: options.HTTPClient})
	if err != nil {
		return nil, err
	}
	query, err := service.New(options.Repository, dispatcher, port.NoopMetrics{}, options.QueryOptions)
	if err != nil {
		return nil, err
	}
	handler := &httpapi.Handler{
		Service:        query,
		Headers:        security.ServerHeaders{Version: options.Version, APIVersion: "2", InstanceID: options.InstanceID},
		Token:          options.Token,
		Limiter:        limiter,
		BuildCommit:    options.Commit,
		Capabilities:   options.Capabilities,
		Logger:         options.Logger,
		DisablePairing: options.DisablePairing,
	}
	if !options.DisablePairing {
		if _, err := handler.StartPairingSession(options.PairingURL, options.PairingSPKI, time.Now()); err != nil {
			logger := slog.Default()
			if options.Logger != nil {
				logger = options.Logger
			}
			logger.Info("pairing page not published", "error_type", fmt.Sprintf("%T", err))
		}
	}
	return &Composition{Router: NewRouter(handler, options.Logger), Query: query, Handler: handler}, nil
}

func queryLimiter(options RateLimitOptions) (*security.QueryLimiter, error) {
	if options == (RateLimitOptions{}) {
		return nil, nil
	}
	if options.RequestsPerSecond <= 0 || options.Burst <= 0 || options.MaxConcurrent <= 0 {
		return nil, fmt.Errorf("rate limit options must be positive when any field is set")
	}
	return security.NewQueryLimiter(options.RequestsPerSecond, options.Burst, options.MaxConcurrent), nil
}
