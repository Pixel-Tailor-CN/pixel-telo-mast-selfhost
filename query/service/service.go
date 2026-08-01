// Package service 实现与具体部署和存储无关的 source 优先级查询流程。
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
)

const (
	defaultQueueSize   = 1000
	defaultSaveTimeout = 5 * time.Second
	defaultQueryTime   = 2 * time.Second
)

// Options 定义查询核心的运行参数。
type Options struct {
	QueryTimeout   time.Duration
	SaveTimeout    time.Duration
	AsyncQueueSize int
	DisableCache   bool
	DefaultSources []string
}

// Service 负责缓存判定、缺失 source 补查、优先级选择和异步写回。
type Service struct {
	repo           port.QueryRepository
	dispatcher     port.ProviderDispatcher
	metrics        port.Metrics
	queryTimeout   time.Duration
	saveTimeout    time.Duration
	disableCache   bool
	defaultSources []string
	enabledSources map[string]struct{}
	asyncSaveCh    chan []*domain.Record
	saveMu         sync.RWMutex
	closed         bool
	closeOnce      sync.Once
	wg             sync.WaitGroup
}

// New 创建查询服务。source 必须由 Composition Root 显式配置。
func New(repo port.QueryRepository, dispatcher port.ProviderDispatcher, metrics port.Metrics, options Options) (*Service, error) {
	if repo == nil {
		return nil, errors.New("query repository is required")
	}
	if dispatcher == nil {
		return nil, errors.New("provider dispatcher is required")
	}

	defaultSources := normalizeSources(options.DefaultSources)
	if len(defaultSources) == 0 {
		return nil, errors.New("at least one default source is required")
	}
	if options.QueryTimeout < 0 {
		return nil, errors.New("query timeout must not be negative")
	}
	if options.QueryTimeout == 0 {
		options.QueryTimeout = defaultQueryTime
	}
	if options.SaveTimeout <= 0 {
		options.SaveTimeout = defaultSaveTimeout
	}
	if options.AsyncQueueSize <= 0 {
		options.AsyncQueueSize = defaultQueueSize
	}
	if metrics == nil {
		metrics = port.NoopMetrics{}
	}

	enabledSources := make(map[string]struct{}, len(defaultSources))
	for _, source := range defaultSources {
		enabledSources[source] = struct{}{}
	}

	svc := &Service{
		repo:           repo,
		dispatcher:     dispatcher,
		metrics:        metrics,
		queryTimeout:   options.QueryTimeout,
		saveTimeout:    options.SaveTimeout,
		disableCache:   options.DisableCache,
		defaultSources: defaultSources,
		enabledSources: enabledSources,
		asyncSaveCh:    make(chan []*domain.Record, options.AsyncQueueSize),
	}
	svc.wg.Add(1)
	go svc.asyncWriter()
	return svc, nil
}

// Close 排空并关闭异步写入器。
func (s *Service) Close() error {
	s.closeOnce.Do(func() {
		s.saveMu.Lock()
		s.closed = true
		close(s.asyncSaveCh)
		s.saveMu.Unlock()
		s.wg.Wait()
		slog.Info("query service async writer closed")
	})
	return nil
}

// Lookup 使用服务端默认 source 顺序执行 v1 查询。
func (s *Service) Lookup(ctx context.Context, phone string) (*domain.Record, error) {
	result, err := s.lookupWithMode(ctx, phone, s.defaultSources, s.defaultSources, nil, domain.QueryModeV1)
	if err != nil {
		return nil, err
	}
	return result.Record, nil
}

// LookupWithSources 使用客户端在服务端白名单内指定的 source 顺序执行 v2 查询。
func (s *Service) LookupWithSources(ctx context.Context, phone string, sources []string) (*domain.LookupResult, error) {
	requested := normalizeSources(sources)
	effective := make([]string, 0, len(requested))
	invalid := make([]string, 0)
	for _, source := range requested {
		if _, ok := s.enabledSources[source]; ok {
			effective = append(effective, source)
		} else {
			invalid = append(invalid, source)
		}
	}

	if len(effective) == 0 {
		return s.lookupWithMode(ctx, phone, requested, s.defaultSources, invalid, domain.QueryModeV1Fallback)
	}
	return s.lookupWithMode(ctx, phone, requested, effective, invalid, domain.QueryModeV2)
}

// ListSources 返回当前实例通过本地配置启用的 source。
func (s *Service) ListSources() domain.SourceListResult {
	defaults := append([]string(nil), s.defaultSources...)
	available := make([]domain.SourceDescriptor, 0, len(defaults))
	for index, source := range defaults {
		available = append(available, domain.SourceDescriptor{ID: source, Priority: index + 1})
	}
	return domain.SourceListResult{DefaultSources: defaults, AvailableSources: available}
}

func (s *Service) lookupWithMode(ctx context.Context, phone string, requested, effective, invalid []string, mode domain.QueryMode) (_ *domain.LookupResult, err error) {
	startedAt := time.Now()
	defer func() {
		result := "success"
		if err != nil {
			result = "error"
		}
		s.metrics.ObserveLookup(string(mode), result, time.Since(startedAt))
	}()

	records := make(map[string]*domain.Record, len(effective))
	if !s.disableCache {
		cached, cacheErr := s.listCached(ctx, phone, effective, mode)
		switch {
		case cacheErr == nil:
			s.metrics.ObserveCache("hit")
			maps.Copy(records, cached)
		case errors.Is(cacheErr, domain.ErrNotFound):
			s.metrics.ObserveCache("miss")
		default:
			s.metrics.ObserveCache("error")
			slog.Error("cache lookup failed", "error_type", errorType(cacheErr))
		}
	}

	if cachedResult, ready := selectReadyResult(effective, records); ready {
		return newLookupResult(cachedResult, mode, requested, effective, invalid), nil
	}

	missing := collectMissingSources(effective, records)
	if len(missing) > 0 {
		queryCtx, cancel := context.WithTimeout(ctx, s.queryTimeout)
		defer cancel()

		infoMap, errMap := s.dispatcher.LookupAll(queryCtx, phone, missing)
		spamRecords := make([]*domain.Record, 0, len(infoMap))
		for source, info := range infoMap {
			if info == nil {
				continue
			}
			record := toRecord(phone, source, info)
			records[source] = record
			if info.IsSpam {
				spamRecords = append(spamRecords, record)
			}
		}
		if len(spamRecords) > 0 && !s.disableCache {
			s.enqueueSave(spamRecords)
		}

		if result := selectAvailableResult(effective, records); result != nil {
			return newLookupResult(result, mode, requested, effective, invalid), nil
		}
		if lookupErr := firstLookupError(queryCtx, errMap); lookupErr != nil {
			return nil, lookupErr
		}
	}

	if result := selectAvailableResult(effective, records); result != nil {
		return newLookupResult(result, mode, requested, effective, invalid), nil
	}
	return nil, domain.ErrNotFound
}

func errorType(err error) string {
	return fmt.Sprintf("%T", err)
}

func (s *Service) listCached(ctx context.Context, phone string, sources []string, mode domain.QueryMode) (map[string]*domain.Record, error) {
	var (
		items []*domain.Record
		err   error
	)
	if mode == domain.QueryModeV1 {
		items, err = s.repo.ListByPhone(ctx, phone)
	} else {
		items, err = s.repo.ListByPhoneAndSources(ctx, phone, sources)
	}
	if err != nil {
		return nil, err
	}

	allowed := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		allowed[source] = struct{}{}
	}
	records := make(map[string]*domain.Record, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		if _, ok := allowed[item.Source]; ok {
			records[item.Source] = item
		}
	}
	if len(records) == 0 {
		return nil, domain.ErrNotFound
	}
	return records, nil
}

func newLookupResult(record *domain.Record, mode domain.QueryMode, requested, effective, invalid []string) *domain.LookupResult {
	return &domain.LookupResult{
		Record:           record,
		QueryMode:        mode,
		RequestedSources: append([]string(nil), requested...),
		EffectiveSources: append([]string(nil), effective...),
		InvalidSources:   append([]string(nil), invalid...),
	}
}
