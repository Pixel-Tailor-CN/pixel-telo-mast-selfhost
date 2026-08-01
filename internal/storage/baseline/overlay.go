package baseline

import (
	"context"
	"errors"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
)

type recordReader interface {
	ListByPhone(ctx context.Context, phone string) ([]*domain.Record, error)
	ListByPhoneAndSources(ctx context.Context, phone string, sources []string) ([]*domain.Record, error)
}

// OverlayRepository 把 baseline 和 Runtime 合并为 Query Service 看到的单一缓存端口。
type OverlayRepository struct {
	runtime  port.QueryRepository
	baseline recordReader
}

var _ port.QueryRepository = (*OverlayRepository)(nil)

// NewOverlay 创建 Runtime 优先的组合 Repository。
func NewOverlay(runtime port.QueryRepository, baseline recordReader) *OverlayRepository {
	return &OverlayRepository{runtime: runtime, baseline: baseline}
}

func (r *OverlayRepository) ListByPhone(ctx context.Context, phone string) ([]*domain.Record, error) {
	baseRecords, baseErr := r.baseline.ListByPhone(ctx, phone)
	runtimeRecords, runtimeErr := r.runtime.ListByPhone(ctx, phone)
	return mergeResults(baseRecords, baseErr, runtimeRecords, runtimeErr)
}

func (r *OverlayRepository) ListByPhoneAndSources(ctx context.Context, phone string, sources []string) ([]*domain.Record, error) {
	baseRecords, baseErr := r.baseline.ListByPhoneAndSources(ctx, phone, sources)
	runtimeRecords, runtimeErr := r.runtime.ListByPhoneAndSources(ctx, phone, sources)
	return mergeResults(baseRecords, baseErr, runtimeRecords, runtimeErr)
}

func (r *OverlayRepository) SaveBatch(ctx context.Context, records []*domain.Record) error {
	return r.runtime.SaveBatch(ctx, records)
}

func mergeResults(baseRecords []*domain.Record, baseErr error, runtimeRecords []*domain.Record, runtimeErr error) ([]*domain.Record, error) {
	if baseErr != nil && !errors.Is(baseErr, domain.ErrNotFound) {
		return nil, baseErr
	}
	if runtimeErr != nil && !errors.Is(runtimeErr, domain.ErrNotFound) {
		return nil, runtimeErr
	}
	merged := make([]*domain.Record, 0, len(baseRecords)+len(runtimeRecords))
	indexByKey := make(map[string]int, len(baseRecords)+len(runtimeRecords))
	for _, record := range baseRecords {
		if record == nil {
			continue
		}
		key := record.PhoneNumber + "\x00" + record.Source
		indexByKey[key] = len(merged)
		merged = append(merged, record)
	}
	for _, record := range runtimeRecords {
		if record == nil {
			continue
		}
		key := record.PhoneNumber + "\x00" + record.Source
		if index, exists := indexByKey[key]; exists {
			merged[index] = record
			continue
		}
		indexByKey[key] = len(merged)
		merged = append(merged, record)
	}
	if len(merged) == 0 {
		return nil, domain.ErrNotFound
	}
	return merged, nil
}
