package provider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
)

const (
	defaultMaxConcurrent  = 1
	defaultBreakerTimeout = 30 * time.Second
	breakerFailureLimit   = 3
)

// SourceConfig 定义单个 Provider 的本地运行限制。
type SourceConfig struct {
	ID             string
	MinInterval    time.Duration
	MaxConcurrent  int
	BreakerTimeout time.Duration
}

// Config 定义 Dispatcher 的显式 source 列表和可注入依赖。
type Config struct {
	Sources    []SourceConfig
	HTTPClient *http.Client
	Metrics    port.Metrics
}

// Dispatcher 并发查询指定 source，并保持各 Provider 的隔离限制。
type Dispatcher struct {
	sources map[string]*sourceRuntime
}

var _ port.ProviderDispatcher = (*Dispatcher)(nil)

// NewDispatcher 只创建配置中显式列出的公开 Provider。
func NewDispatcher(config Config) (*Dispatcher, error) {
	return NewDispatcherWithLogger(config, slog.Default())
}

// NewDispatcherWithLogger 创建使用指定结构化日志器的 Dispatcher。
func NewDispatcherWithLogger(config Config, logger *slog.Logger) (*Dispatcher, error) {
	if len(config.Sources) == 0 {
		return nil, errors.New("at least one provider source is required")
	}
	client := config.HTTPClient
	if client == nil {
		client = newHTTPClient()
	}
	metrics := config.Metrics
	if metrics == nil {
		metrics = port.NoopMetrics{}
	}
	if logger == nil {
		logger = slog.Default()
	}

	sources := make(map[string]*sourceRuntime, len(config.Sources))
	for _, sourceConfig := range config.Sources {
		sourceConfig.ID = strings.TrimSpace(sourceConfig.ID)
		if sourceConfig.ID == "" {
			return nil, errors.New("provider source ID is required")
		}
		if _, exists := sources[sourceConfig.ID]; exists {
			return nil, fmt.Errorf("duplicate provider source: %s", sourceConfig.ID)
		}
		provider, err := createProvider(sourceConfig.ID, client)
		if err != nil {
			return nil, err
		}
		if sourceConfig.MaxConcurrent <= 0 {
			sourceConfig.MaxConcurrent = defaultMaxConcurrent
		}
		if sourceConfig.MinInterval < 0 {
			return nil, fmt.Errorf("provider %s min interval must not be negative", sourceConfig.ID)
		}
		if sourceConfig.BreakerTimeout <= 0 {
			sourceConfig.BreakerTimeout = defaultBreakerTimeout
		}
		sources[sourceConfig.ID] = newSourceRuntimeWithLogger(sourceConfig, provider, metrics, logger)
	}
	return &Dispatcher{sources: sources}, nil
}

func createProvider(sourceID string, client *http.Client) (lookupProvider, error) {
	switch sourceID {
	case SogouSourceID:
		return newSogouProvider(client), nil
	case So360SourceID:
		return newSo360Provider(client), nil
	default:
		return nil, fmt.Errorf("unknown provider source: %s", sourceID)
	}
}

func (d *Dispatcher) LookupAll(ctx context.Context, phone string, sources []string) (map[string]*port.ProviderResult, map[string]error) {
	results := make(map[string]*port.ProviderResult, len(sources))
	errs := make(map[string]error, len(sources))
	var mu sync.Mutex
	var wg sync.WaitGroup
	seen := make(map[string]struct{}, len(sources))

	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		if _, duplicate := seen[source]; duplicate {
			continue
		}
		seen[source] = struct{}{}
		runtime, exists := d.sources[source]
		if !exists {
			mu.Lock()
			errs[source] = fmt.Errorf("provider not configured: %s", source)
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(source string, runtime *sourceRuntime) {
			defer wg.Done()
			result, err := runtime.lookup(ctx, phone)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[source] = err
				return
			}
			results[source] = result
		}(source, runtime)
	}
	wg.Wait()
	return results, errs
}

type sourceRuntime struct {
	config    SourceConfig
	provider  lookupProvider
	metrics   port.Metrics
	logger    *slog.Logger
	semaphore chan struct{}

	scheduleMu sync.Mutex
	lastStart  time.Time

	stateMu             sync.Mutex
	consecutiveFailures int
	cooldownUntil       time.Time
	cooldownErr         error
	openUntil           time.Time
	openErr             error
}

func newSourceRuntime(config SourceConfig, provider lookupProvider, metrics port.Metrics) *sourceRuntime {
	return newSourceRuntimeWithLogger(config, provider, metrics, slog.Default())
}

func newSourceRuntimeWithLogger(config SourceConfig, provider lookupProvider, metrics port.Metrics, logger *slog.Logger) *sourceRuntime {
	if logger == nil {
		logger = slog.Default()
	}
	return &sourceRuntime{
		config:    config,
		provider:  provider,
		metrics:   metrics,
		logger:    logger,
		semaphore: make(chan struct{}, config.MaxConcurrent),
	}
}

func (r *sourceRuntime) lookup(ctx context.Context, phone string) (_ *port.ProviderResult, resultErr error) {
	lookupStartedAt := time.Now()
	defer func() {
		if resultErr != nil {
			r.logger.Error("provider query failed",
				"provider", r.config.ID,
				"error_type", providerErrorType(resultErr),
				"latency_ms", time.Since(lookupStartedAt).Milliseconds(),
			)
		}
	}()
	select {
	case r.semaphore <- struct{}{}:
		defer func() { <-r.semaphore }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if err := r.circuitError(time.Now()); err != nil {
		return nil, err
	}
	if err := r.waitForInterval(ctx); err != nil {
		return nil, err
	}
	if err := r.circuitError(time.Now()); err != nil {
		return nil, err
	}

	startedAt := time.Now()
	result, err := r.provider.Lookup(ctx, phone)
	err = normalizeProviderError(err)
	metricResult := "success"
	if err != nil {
		metricResult = providerMetricResult(err)
		if !errors.Is(err, context.Canceled) {
			r.recordFailure(time.Now(), err)
		}
	} else {
		r.recordSuccess()
	}
	r.metrics.ObserveProvider(r.config.ID, metricResult, time.Since(startedAt))
	return result, err
}

func (r *sourceRuntime) waitForInterval(ctx context.Context) error {
	r.scheduleMu.Lock()
	defer r.scheduleMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	wait := time.Until(r.lastStart.Add(r.config.MinInterval))
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.lastStart = time.Now()
	return nil
}

func (r *sourceRuntime) circuitError(now time.Time) error {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if now.Before(r.cooldownUntil) {
		return r.cooldownErr
	}
	if !r.cooldownUntil.IsZero() {
		r.cooldownUntil = time.Time{}
		r.cooldownErr = nil
	}
	if now.Before(r.openUntil) {
		return r.openErr
	}
	if !r.openUntil.IsZero() {
		r.openUntil = time.Time{}
		r.openErr = nil
		r.consecutiveFailures = 0
	}
	return nil
}

func (r *sourceRuntime) recordFailure(now time.Time, err error) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.consecutiveFailures++
	var rateLimit *domain.RateLimitError
	if errors.As(err, &rateLimit) && rateLimit.RetryAfter > 0 {
		r.cooldownUntil = now.Add(rateLimit.RetryAfter)
		r.cooldownErr = err
		return
	}
	if r.consecutiveFailures < breakerFailureLimit {
		return
	}
	duration := r.config.BreakerTimeout
	if errors.As(err, &rateLimit) && rateLimit.RetryAfter > duration {
		duration = rateLimit.RetryAfter
	}
	r.openUntil = now.Add(duration)
	r.openErr = err
}

func (r *sourceRuntime) recordSuccess() {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.consecutiveFailures = 0
	r.openUntil = time.Time{}
	r.openErr = nil
}

func normalizeProviderError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errInvalidProviderResponse):
		return fmt.Errorf("%w: %w", domain.ErrUpstreamUnavailable, err)
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, domain.ErrUpstreamTimeout):
		return domain.ErrUpstreamTimeout
	case errors.Is(err, domain.ErrRateLimited), errors.Is(err, domain.ErrUpstreamUnavailable):
		return err
	default:
		return fmt.Errorf("%w: %v", domain.ErrUpstreamUnavailable, err)
	}
}

func providerErrorType(err error) string {
	switch {
	case errors.Is(err, errInvalidProviderResponse):
		return "parse_error"
	case errors.Is(err, domain.ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, domain.ErrUpstreamTimeout), errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "upstream_unavailable"
	}
}

func providerMetricResult(err error) string {
	switch {
	case errors.Is(err, domain.ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, domain.ErrUpstreamTimeout):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "error"
	}
}
