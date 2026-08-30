package provider

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
)

type lookupFunc func(context.Context, string) (*port.ProviderResult, error)

func (f lookupFunc) Lookup(ctx context.Context, phone string) (*port.ProviderResult, error) {
	return f(ctx, phone)
}

func TestNewDispatcherRejectsEmptySources(t *testing.T) {
	if _, err := NewDispatcher(Config{}); err == nil {
		t.Fatal("empty sources must be rejected")
	}
}

func TestNewDispatcherRejectsUnknownSource(t *testing.T) {
	_, err := NewDispatcher(Config{Sources: []SourceConfig{{ID: "official"}}})
	if err == nil {
		t.Fatal("unknown source must be rejected")
	}
}

func TestSourceRuntimeEnforcesMinimumInterval(t *testing.T) {
	var mu sync.Mutex
	starts := make([]time.Time, 0, 2)
	runtime := newSourceRuntime(SourceConfig{
		ID:            "test",
		MinInterval:   30 * time.Millisecond,
		MaxConcurrent: 1,
	}, lookupFunc(func(context.Context, string) (*port.ProviderResult, error) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		return &port.ProviderResult{Source: "test"}, nil
	}), port.NoopMetrics{})

	for range 2 {
		if _, err := runtime.lookup(context.Background(), "13800138000"); err != nil {
			t.Fatal(err)
		}
	}
	if delta := starts[1].Sub(starts[0]); delta < 25*time.Millisecond {
		t.Fatalf("request interval = %s", delta)
	}
}

func TestSourceRuntimeLimitsConcurrency(t *testing.T) {
	var current atomic.Int32
	var maximum atomic.Int32
	runtime := newSourceRuntime(SourceConfig{ID: "test", MaxConcurrent: 2}, lookupFunc(func(context.Context, string) (*port.ProviderResult, error) {
		now := current.Add(1)
		for {
			old := maximum.Load()
			if now <= old || maximum.CompareAndSwap(old, now) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		current.Add(-1)
		return &port.ProviderResult{Source: "test"}, nil
	}), port.NoopMetrics{})

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := runtime.lookup(context.Background(), "13800138000"); err != nil {
				t.Errorf("lookup error: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := maximum.Load(); got > 2 {
		t.Fatalf("maximum concurrency = %d", got)
	}
}

func TestSourceRuntimeLogsSafeFailureClassification(t *testing.T) {
	const phone = "13800138000"
	const upstreamBody = "private upstream response"
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	runtime := newSourceRuntimeWithLogger(SourceConfig{ID: "test", MaxConcurrent: 1}, lookupFunc(func(context.Context, string) (*port.ProviderResult, error) {
		return nil, invalidProviderResponse(errors.New(upstreamBody))
	}), port.NoopMetrics{}, logger)

	_, err := runtime.lookup(context.Background(), phone)
	if err == nil {
		t.Fatal("expected provider error")
	}
	message := logs.String()
	for _, expected := range []string{`"msg":"provider query failed"`, `"provider":"test"`, `"error_type":"parse_error"`} {
		if !strings.Contains(message, expected) {
			t.Fatalf("log %q does not contain %q", message, expected)
		}
	}
	for _, sensitive := range []string{phone, upstreamBody} {
		if strings.Contains(message, sensitive) {
			t.Fatalf("log contains sensitive value %q: %s", sensitive, message)
		}
	}
}

func TestProviderErrorType(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "parse", err: invalidProviderResponse(errors.New("invalid")), want: "parse_error"},
		{name: "rate limited", err: domain.ErrRateLimited, want: "rate_limited"},
		{name: "timeout", err: domain.ErrUpstreamTimeout, want: "timeout"},
		{name: "deadline", err: context.DeadlineExceeded, want: "timeout"},
		{name: "canceled", err: context.Canceled, want: "canceled"},
		{name: "unavailable", err: domain.ErrUpstreamUnavailable, want: "upstream_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := providerErrorType(test.err); got != test.want {
				t.Fatalf("providerErrorType(%v) = %q, want %q", test.err, got, test.want)
			}
		})
	}
}

func TestSourceRuntimeOpensCircuitAfterThreeFailures(t *testing.T) {
	var calls atomic.Int32
	upstreamErr := errors.New("upstream failed")
	runtime := newSourceRuntime(SourceConfig{
		ID:             "test",
		MaxConcurrent:  1,
		BreakerTimeout: time.Minute,
	}, lookupFunc(func(context.Context, string) (*port.ProviderResult, error) {
		calls.Add(1)
		return nil, upstreamErr
	}), port.NoopMetrics{})

	for range 4 {
		_, _ = runtime.lookup(context.Background(), "13800138000")
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("provider calls = %d, want 3", got)
	}
}

func TestNormalizeProviderErrorPreservesRateLimit(t *testing.T) {
	err := &domain.RateLimitError{RetryAfter: time.Minute, Cause: errors.New("limited")}
	if got := normalizeProviderError(err); !errors.Is(got, domain.ErrRateLimited) {
		t.Fatalf("error = %v", got)
	}
}

func TestSourceRuntimeHonorsRetryAfterImmediately(t *testing.T) {
	var calls atomic.Int32
	runtime := newSourceRuntime(SourceConfig{
		ID:             "test",
		MaxConcurrent:  1,
		BreakerTimeout: time.Second,
	}, lookupFunc(func(context.Context, string) (*port.ProviderResult, error) {
		calls.Add(1)
		return nil, &domain.RateLimitError{RetryAfter: time.Minute, Cause: errors.New("limited")}
	}), port.NoopMetrics{})

	_, _ = runtime.lookup(context.Background(), "13800138000")
	_, secondErr := runtime.lookup(context.Background(), "13800138000")
	if !errors.Is(secondErr, domain.ErrRateLimited) {
		t.Fatalf("second error = %v", secondErr)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

func TestSourceRuntimeDoesNotCountClientCancellation(t *testing.T) {
	var calls atomic.Int32
	runtime := newSourceRuntime(SourceConfig{
		ID:             "test",
		MaxConcurrent:  1,
		BreakerTimeout: time.Minute,
	}, lookupFunc(func(context.Context, string) (*port.ProviderResult, error) {
		calls.Add(1)
		return nil, context.Canceled
	}), port.NoopMetrics{})

	for range 4 {
		_, _ = runtime.lookup(context.Background(), "13800138000")
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("provider calls = %d, want 4", got)
	}
}

func TestCanceledIntervalWaitDoesNotReserveFutureSlot(t *testing.T) {
	runtime := newSourceRuntime(SourceConfig{
		ID:            "test",
		MinInterval:   time.Minute,
		MaxConcurrent: 1,
	}, lookupFunc(func(context.Context, string) (*port.ProviderResult, error) {
		return &port.ProviderResult{Source: "test"}, nil
	}), port.NoopMetrics{})
	if _, err := runtime.lookup(context.Background(), "13800138000"); err != nil {
		t.Fatal(err)
	}
	firstStart := runtime.lastStart

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = runtime.lookup(ctx, "13800138000")
	if !runtime.lastStart.Equal(firstStart) {
		t.Fatalf("last start changed from %s to %s", firstStart, runtime.lastStart)
	}
}

func TestQueuedLookupRechecksCircuitBeforeProviderCall(t *testing.T) {
	var calls atomic.Int32
	runtime := newSourceRuntime(SourceConfig{
		ID:             "test",
		MinInterval:    15 * time.Millisecond,
		MaxConcurrent:  4,
		BreakerTimeout: time.Minute,
	}, lookupFunc(func(context.Context, string) (*port.ProviderResult, error) {
		calls.Add(1)
		return nil, errors.New("upstream failed")
	}), port.NoopMetrics{})

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = runtime.lookup(context.Background(), "13800138000")
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != 3 {
		t.Fatalf("provider calls = %d, want 3", got)
	}
}

func TestInFlightSuccessDoesNotClearRetryAfterCooldown(t *testing.T) {
	var calls atomic.Int32
	started := make(chan int32, 2)
	releaseRateLimit := make(chan struct{})
	releaseSuccess := make(chan struct{})
	runtime := newSourceRuntime(SourceConfig{
		ID:            "test",
		MaxConcurrent: 2,
	}, lookupFunc(func(context.Context, string) (*port.ProviderResult, error) {
		call := calls.Add(1)
		started <- call
		if call == 1 {
			<-releaseRateLimit
			return nil, &domain.RateLimitError{RetryAfter: time.Minute, Cause: errors.New("limited")}
		}
		<-releaseSuccess
		return &port.ProviderResult{Source: "test"}, nil
	}), port.NoopMetrics{})

	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := runtime.lookup(context.Background(), "13800138000")
			results <- err
		}()
	}
	<-started
	<-started
	close(releaseRateLimit)
	if err := <-results; !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("first completed error = %v", err)
	}
	close(releaseSuccess)
	if err := <-results; err != nil {
		t.Fatalf("second completed error = %v", err)
	}

	_, thirdErr := runtime.lookup(context.Background(), "13800138000")
	if !errors.Is(thirdErr, domain.ErrRateLimited) {
		t.Fatalf("third error = %v", thirdErr)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("provider calls = %d, want 2", got)
	}
}
