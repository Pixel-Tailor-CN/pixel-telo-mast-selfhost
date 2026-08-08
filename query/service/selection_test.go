package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
)

func TestSelectReadyResultRequiresAllHigherPrioritySources(t *testing.T) {
	records := map[string]*domain.Record{
		"b": {Source: "b", Confidence: 100},
	}
	got, ready := selectReadyResult([]string{"a", "b", "c"}, records)
	if got == nil || got.Source != "b" {
		t.Fatalf("record = %#v", got)
	}
	if ready {
		t.Fatal("lower-priority spam must wait for missing higher-priority source")
	}
}

func TestSelectReadyResultDoesNotWaitForLowerPrioritySources(t *testing.T) {
	records := map[string]*domain.Record{
		"a": {Source: "a", Confidence: 100},
	}
	got, ready := selectReadyResult([]string{"a", "b"}, records)
	if got == nil || got.Source != "a" || !ready {
		t.Fatalf("record/ready = %#v/%v", got, ready)
	}
}

func TestFirstLookupErrorAlwaysPrioritizesRateLimitOverTimeout(t *testing.T) {
	errs := map[string]error{
		"a": domain.ErrRateLimited,
		"b": domain.ErrUpstreamTimeout,
	}
	for range 1000 {
		got := firstLookupError(context.Background(), errs)
		if !errors.Is(got, domain.ErrRateLimited) {
			t.Fatalf("error = %v, want ErrRateLimited", got)
		}
	}
}

func TestFirstLookupErrorPrioritizesRateLimitWhenQueryContextExpired(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{name: "整体超时", ctx: expiredContext(t)},
		{name: "请求取消", ctx: canceledContext(t)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstLookupError(tt.ctx, map[string]error{
				"sogou": domain.ErrRateLimited,
				"360":   domain.ErrUpstreamTimeout,
			})
			if !errors.Is(got, domain.ErrRateLimited) {
				t.Fatalf("error = %v, want ErrRateLimited", got)
			}
		})
	}
}

func expiredContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
	t.Cleanup(cancel)
	return ctx
}

func canceledContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	t.Cleanup(cancel)
	return ctx
}
