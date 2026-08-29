package app

import (
	"fmt"
	"log/slog"
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
	// EnablePairing 为 true 时注册配对页并尝试发布会话；传统模式必须显式设为 true。
	EnablePairing bool
	PairingURL    string
	PairingSPKI   string
	RateLimit     RateLimitOptions
	Logger        *slog.Logger
}

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
	dispatcher, err := provider.NewDispatcher(provider.Config{Sources: options.ProviderSources})
	if err != nil {
		return nil, err
	}
	query, err := service.New(options.Repository, dispatcher, port.NoopMetrics{}, options.QueryOptions)
	if err != nil {
		return nil, err
	}
	handler := &httpapi.Handler{
		Service:       query,
		Headers:       security.ServerHeaders{Version: options.Version, APIVersion: "2", InstanceID: options.InstanceID},
		Token:         options.Token,
		Limiter:       security.NewQueryLimiter(options.RateLimit.RequestsPerSecond, options.RateLimit.Burst, options.RateLimit.MaxConcurrent),
		BuildCommit:   options.Commit,
		Capabilities:  options.Capabilities,
		EnablePairing: options.EnablePairing,
	}
	if options.EnablePairing {
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
