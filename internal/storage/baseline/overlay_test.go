package baseline

import (
	"context"
	"testing"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
)

type memoryRepository struct {
	records []*domain.Record
}

func (r *memoryRepository) ListByPhone(context.Context, string) ([]*domain.Record, error) {
	if len(r.records) == 0 {
		return nil, domain.ErrNotFound
	}
	return r.records, nil
}

func (r *memoryRepository) ListByPhoneAndSources(_ context.Context, _ string, sources []string) ([]*domain.Record, error) {
	allowed := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		allowed[source] = struct{}{}
	}
	result := make([]*domain.Record, 0, len(r.records))
	for _, record := range r.records {
		if _, ok := allowed[record.Source]; ok {
			result = append(result, record)
		}
	}
	if len(result) == 0 {
		return nil, domain.ErrNotFound
	}
	return result, nil
}

func (r *memoryRepository) SaveBatch(_ context.Context, records []*domain.Record) error {
	r.records = append(r.records, records...)
	return nil
}

func TestOverlayRuntimeOverridesBaselineByPhoneAndSource(t *testing.T) {
	base := &memoryRepository{records: []*domain.Record{
		{PhoneNumber: "13800138000", Source: "sogou", Tag: "旧标签", Confidence: 100},
		{PhoneNumber: "13800138000", Source: "360", Tag: "广告", Confidence: 100},
	}}
	runtime := &memoryRepository{records: []*domain.Record{
		{PhoneNumber: "13800138000", Source: "sogou", Tag: "新标签", Confidence: 100},
	}}
	repo := NewOverlay(runtime, base)

	got, err := repo.ListByPhone(context.Background(), "13800138000")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("records = %#v", got)
	}
	bySource := map[string]*domain.Record{}
	for _, record := range got {
		bySource[record.Source] = record
	}
	if bySource["sogou"].Tag != "新标签" || bySource["360"].Tag != "广告" {
		t.Fatalf("records = %#v", got)
	}
}
