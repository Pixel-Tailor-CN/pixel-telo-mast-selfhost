package service

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
)

type stubRepository struct {
	records map[string][]*domain.Record
	saved   chan []*domain.Record
}

func (r *stubRepository) ListByPhone(_ context.Context, phone string) ([]*domain.Record, error) {
	return r.list(phone, nil)
}

func (r *stubRepository) ListByPhoneAndSources(_ context.Context, phone string, sources []string) ([]*domain.Record, error) {
	return r.list(phone, sources)
}

func (r *stubRepository) list(phone string, sources []string) ([]*domain.Record, error) {
	items := r.records[phone]
	if len(items) == 0 {
		return nil, domain.ErrNotFound
	}
	if len(sources) == 0 {
		return items, nil
	}
	allowed := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		allowed[source] = struct{}{}
	}
	filtered := make([]*domain.Record, 0, len(items))
	for _, item := range items {
		if _, ok := allowed[item.Source]; ok {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		return nil, domain.ErrNotFound
	}
	return filtered, nil
}

func (r *stubRepository) SaveBatch(_ context.Context, records []*domain.Record) error {
	if r.saved != nil {
		r.saved <- records
	}
	return nil
}

type stubDispatcher struct {
	mu      sync.Mutex
	results map[string]*port.ProviderResult
	errs    map[string]error
	calls   []string
}

func (d *stubDispatcher) LookupAll(_ context.Context, _ string, sources []string) (map[string]*port.ProviderResult, map[string]error) {
	d.mu.Lock()
	d.calls = append(d.calls, sources...)
	d.mu.Unlock()
	return d.results, d.errs
}

func (d *stubDispatcher) calledSources() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.calls...)
}

func TestLookupWithSourcesPreservesMastPriority(t *testing.T) {
	tests := []struct {
		name       string
		cached     []*domain.Record
		upstream   map[string]*port.ProviderResult
		wantSource string
		wantCalls  []string
	}{
		{
			name:       "高优骚扰直接返回",
			cached:     []*domain.Record{{PhoneNumber: "13800138000", Source: "a", Confidence: 100}},
			wantSource: "a",
		},
		{
			name:       "低优骚扰补查高优",
			cached:     []*domain.Record{{PhoneNumber: "13800138000", Source: "b", Confidence: 100}},
			upstream:   map[string]*port.ProviderResult{"a": {Source: "a", IsSpam: false}},
			wantSource: "b",
			wantCalls:  []string{"a"},
		},
		{
			name: "全部非骚扰返回首个成功",
			upstream: map[string]*port.ProviderResult{
				"a": {Source: "a", IsSpam: false},
				"b": {Source: "b", IsSpam: false},
			},
			wantSource: "a",
			wantCalls:  []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubRepository{records: map[string][]*domain.Record{"13800138000": tt.cached}}
			dispatcher := &stubDispatcher{results: tt.upstream}
			svc, err := New(repo, dispatcher, port.NoopMetrics{}, Options{
				QueryTimeout:   2 * time.Second,
				DefaultSources: []string{"a", "b"},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer svc.Close()

			got, err := svc.LookupWithSources(context.Background(), "13800138000", []string{"a", "b"})
			if err != nil {
				t.Fatal(err)
			}
			if got.Record.Source != tt.wantSource {
				t.Fatalf("source = %q, want %q", got.Record.Source, tt.wantSource)
			}
			if calls := dispatcher.calledSources(); !reflect.DeepEqual(calls, tt.wantCalls) {
				t.Fatalf("calls = %#v, want %#v", calls, tt.wantCalls)
			}
		})
	}
}

func TestLookupWithSourcesFallsBackWhenAllSourcesInvalid(t *testing.T) {
	repo := &stubRepository{records: map[string][]*domain.Record{
		"13800138000": {{PhoneNumber: "13800138000", Source: "a", Confidence: 100}},
	}}
	svc, err := New(repo, &stubDispatcher{}, port.NoopMetrics{}, Options{
		QueryTimeout:   2 * time.Second,
		DefaultSources: []string{"a", "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	got, err := svc.LookupWithSources(context.Background(), "13800138000", []string{"unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if got.QueryMode != domain.QueryModeV1Fallback {
		t.Fatalf("query mode = %q", got.QueryMode)
	}
	if !reflect.DeepEqual(got.InvalidSources, []string{"unknown"}) {
		t.Fatalf("invalid sources = %#v", got.InvalidSources)
	}
}

func TestNewRejectsEmptyDefaultSources(t *testing.T) {
	_, err := New(&stubRepository{}, &stubDispatcher{}, port.NoopMetrics{}, Options{})
	if err == nil {
		t.Fatal("empty default sources must be rejected")
	}
}

func TestLookupWritesOnlySpamResultsAsynchronously(t *testing.T) {
	saved := make(chan []*domain.Record, 1)
	repo := &stubRepository{saved: saved}
	dispatcher := &stubDispatcher{results: map[string]*port.ProviderResult{
		"a": {Source: "a", IsSpam: true, Tag: "营销"},
		"b": {Source: "b", IsSpam: false},
	}}
	svc, err := New(repo, dispatcher, port.NoopMetrics{}, Options{
		QueryTimeout:   2 * time.Second,
		DefaultSources: []string{"a", "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	if _, err := svc.Lookup(context.Background(), "13800138000"); err != nil {
		t.Fatal(err)
	}
	select {
	case records := <-saved:
		if len(records) != 1 || records[0].Source != "a" || !records[0].IsSpam() {
			t.Fatalf("saved records = %#v", records)
		}
	case <-time.After(time.Second):
		t.Fatal("asynchronous save did not complete")
	}
}

func TestLookupPreservesContextCancellation(t *testing.T) {
	dispatcher := &stubDispatcher{errs: map[string]error{"a": context.Canceled}}
	svc, err := New(&stubRepository{}, dispatcher, port.NoopMetrics{}, Options{
		QueryTimeout:   2 * time.Second,
		DefaultSources: []string{"a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = svc.Lookup(ctx, "13800138000")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestLookupPrioritizesRateLimitedError(t *testing.T) {
	dispatcher := &stubDispatcher{errs: map[string]error{
		"a": errors.New("parse failed"),
		"b": domain.ErrRateLimited,
	}}
	svc, err := New(&stubRepository{}, dispatcher, port.NoopMetrics{}, Options{
		QueryTimeout:   2 * time.Second,
		DefaultSources: []string{"a", "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	_, err = svc.Lookup(context.Background(), "13800138000")
	if !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("error = %v, want ErrRateLimited", err)
	}
}

func TestListSourcesReturnsConfiguredPriorityOrder(t *testing.T) {
	svc, err := New(&stubRepository{}, &stubDispatcher{}, port.NoopMetrics{}, Options{
		DefaultSources: []string{"a", "b", "a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	got := svc.ListSources()
	if !reflect.DeepEqual(got.DefaultSources, []string{"a", "b"}) {
		t.Fatalf("default sources = %#v", got.DefaultSources)
	}
	if len(got.AvailableSources) != 2 || got.AvailableSources[1].Priority != 2 {
		t.Fatalf("available sources = %#v", got.AvailableSources)
	}
}
