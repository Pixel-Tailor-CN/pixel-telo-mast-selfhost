package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
)

type stubRepository struct {
	records map[string][]*domain.Record
	saved   chan []*domain.Record
	listErr error
	saveErr error
}

func (r *stubRepository) ListByPhone(_ context.Context, phone string) ([]*domain.Record, error) {
	return r.list(phone, nil)
}

func (r *stubRepository) ListByPhoneAndSources(_ context.Context, phone string, sources []string) ([]*domain.Record, error) {
	return r.list(phone, sources)
}

func (r *stubRepository) list(phone string, sources []string) ([]*domain.Record, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
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
	if r.saveErr != nil {
		return r.saveErr
	}
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

type blockingDispatcher struct {
	started chan struct{}
	release chan struct{}
}

func (d *blockingDispatcher) LookupAll(_ context.Context, _ string, _ []string) (map[string]*port.ProviderResult, map[string]error) {
	close(d.started)
	<-d.release
	return map[string]*port.ProviderResult{
		"a": {Source: "a", IsSpam: true, Tag: "营销"},
	}, nil
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

func TestLookupPreservesDomainTimeout(t *testing.T) {
	dispatcher := &stubDispatcher{errs: map[string]error{"a": domain.ErrUpstreamTimeout}}
	svc, err := New(&stubRepository{}, dispatcher, port.NoopMetrics{}, Options{
		QueryTimeout:   2 * time.Second,
		DefaultSources: []string{"a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	_, err = svc.Lookup(context.Background(), "13800138000")
	if !errors.Is(err, domain.ErrUpstreamTimeout) {
		t.Fatalf("error = %v, want ErrUpstreamTimeout", err)
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

func TestRepositoryErrorsDoNotLeakPhoneToLogs(t *testing.T) {
	const phone = "13800138000"
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previousLogger)

	repo := &stubRepository{
		listErr: errors.New("query " + phone + " failed"),
		saveErr: errors.New("insert " + phone + " failed"),
	}
	dispatcher := &stubDispatcher{results: map[string]*port.ProviderResult{
		"a": {Source: "a", IsSpam: true, Tag: "营销"},
	}}
	svc, err := New(repo, dispatcher, port.NoopMetrics{}, Options{DefaultSources: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Lookup(context.Background(), phone); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), phone) {
		t.Fatalf("logs contain full phone: %s", logs.String())
	}
}

type delayedSaveRepository struct {
	stubRepository
	delay time.Duration
}

func (r *delayedSaveRepository) SaveBatch(ctx context.Context, records []*domain.Record) error {
	timer := time.NewTimer(r.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return r.stubRepository.SaveBatch(ctx, records)
	case <-ctx.Done():
		return ctx.Err()
	}
}

type waitDoneRepository struct {
	stubRepository
}

func (r *waitDoneRepository) SaveBatch(ctx context.Context, _ []*domain.Record) error {
	<-ctx.Done()
	return ctx.Err()
}

type blockingSaveRepository struct {
	stubRepository
	started chan struct{}
	release chan struct{}
}

func (r *blockingSaveRepository) SaveBatch(ctx context.Context, records []*domain.Record) error {
	close(r.started)
	select {
	case <-r.release:
		return r.stubRepository.SaveBatch(ctx, records)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestLookupSyncSaveCompletesBeforeReturn(t *testing.T) {
	saved := make(chan []*domain.Record, 1)
	repo := &delayedSaveRepository{
		stubRepository: stubRepository{saved: saved},
		delay:          80 * time.Millisecond,
	}
	dispatcher := &stubDispatcher{results: map[string]*port.ProviderResult{
		"a": {Source: "a", IsSpam: true, Tag: "营销"},
	}}
	svc, err := New(repo, dispatcher, port.NoopMetrics{}, Options{
		QueryTimeout:   2 * time.Second,
		DefaultSources: []string{"a"},
		CacheWriteMode: CacheWriteSync,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	got, err := svc.Lookup(context.Background(), "13800138000")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Source != "a" || !got.IsSpam() {
		t.Fatalf("record = %#v", got)
	}

	select {
	case records := <-saved:
		if len(records) != 1 || records[0].Source != "a" || !records[0].IsSpam() {
			t.Fatalf("saved records = %#v", records)
		}
	default:
		t.Fatal("Lookup returned before SaveBatch completed")
	}
}

func TestLookupSyncSaveFailurePreservesProviderResult(t *testing.T) {
	saveErr := errors.New("disk full")
	repo := &stubRepository{saveErr: saveErr}
	dispatcher := &stubDispatcher{results: map[string]*port.ProviderResult{
		"a": {Source: "a", IsSpam: true, Tag: "营销"},
	}}
	svc, err := New(repo, dispatcher, port.NoopMetrics{}, Options{
		QueryTimeout:   2 * time.Second,
		DefaultSources: []string{"a"},
		CacheWriteMode: CacheWriteSync,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	got, err := svc.Lookup(context.Background(), "13800138000")
	if err != nil {
		t.Fatalf("save error must not hide provider result: %v", err)
	}
	if got == nil || got.Source != "a" || got.Tag != "营销" || !got.IsSpam() {
		t.Fatalf("record = %#v", got)
	}
}

func TestSyncModeSaveTimeoutReturnsProviderResult(t *testing.T) {
	repo := &waitDoneRepository{}
	dispatcher := &stubDispatcher{results: map[string]*port.ProviderResult{
		"a": {Source: "a", IsSpam: true, Tag: "营销"},
	}}
	svc, err := New(repo, dispatcher, port.NoopMetrics{}, Options{
		QueryTimeout:   2 * time.Second,
		SaveTimeout:    20 * time.Millisecond,
		DefaultSources: []string{"a"},
		CacheWriteMode: CacheWriteSync,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	started := time.Now()
	got, err := svc.Lookup(context.Background(), "13800138000")
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("save timeout must not hide provider result: %v", err)
	}
	if got == nil || got.Source != "a" || !got.IsSpam() {
		t.Fatalf("record = %#v", got)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("Lookup took %v, want to return around save timeout", elapsed)
	}
}

func TestSyncModeDoesNotStartAsyncWriter(t *testing.T) {
	svc, err := New(&stubRepository{}, &stubDispatcher{}, port.NoopMetrics{}, Options{
		DefaultSources: []string{"a"},
		CacheWriteMode: CacheWriteSync,
	})
	if err != nil {
		t.Fatal(err)
	}
	if svc.asyncSaveCh != nil {
		t.Fatal("sync mode must not create async save channel")
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal("Close must be idempotent")
	}
}

func TestSyncModeCloseDoesNotRaceWithInFlightSave(t *testing.T) {
	repo := &blockingSaveRepository{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	dispatcher := &stubDispatcher{results: map[string]*port.ProviderResult{
		"a": {Source: "a", IsSpam: true, Tag: "营销"},
	}}
	svc, err := New(repo, dispatcher, port.NoopMetrics{}, Options{
		QueryTimeout:   2 * time.Second,
		SaveTimeout:    time.Second,
		DefaultSources: []string{"a"},
		CacheWriteMode: CacheWriteSync,
	})
	if err != nil {
		t.Fatal(err)
	}

	lookupDone := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				lookupDone <- fmt.Errorf("lookup panic: %v", recovered)
			}
		}()
		got, lookupErr := svc.Lookup(context.Background(), "13800138000")
		if lookupErr != nil {
			lookupDone <- lookupErr
			return
		}
		if got == nil || got.Source != "a" || !got.IsSpam() {
			lookupDone <- fmt.Errorf("record = %#v", got)
			return
		}
		lookupDone <- nil
	}()

	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("SaveBatch did not start")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- svc.Close() }()
	select {
	case closeErr := <-closeDone:
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not return")
	}

	close(repo.release)
	select {
	case lookupErr := <-lookupDone:
		if lookupErr != nil {
			t.Fatal(lookupErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Lookup did not return")
	}
}

func TestCloseDoesNotRaceWithInFlightLookup(t *testing.T) {
	dispatcher := &blockingDispatcher{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc, err := New(&stubRepository{}, dispatcher, port.NoopMetrics{}, Options{DefaultSources: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}

	lookupDone := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				lookupDone <- fmt.Errorf("lookup panic: %v", recovered)
			}
		}()
		_, lookupErr := svc.Lookup(context.Background(), "13800138000")
		lookupDone <- lookupErr
	}()
	<-dispatcher.started

	closeDone := make(chan error, 1)
	go func() { closeDone <- svc.Close() }()
	select {
	case closeErr := <-closeDone:
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not return")
	}

	close(dispatcher.release)
	select {
	case lookupErr := <-lookupDone:
		if lookupErr != nil {
			t.Fatal(lookupErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Lookup did not return")
	}
}
