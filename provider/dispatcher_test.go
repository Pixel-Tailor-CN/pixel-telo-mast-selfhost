package provider

import (
	"context"
	"errors"
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
