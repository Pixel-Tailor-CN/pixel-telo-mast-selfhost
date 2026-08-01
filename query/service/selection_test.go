package service

import (
	"testing"

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
