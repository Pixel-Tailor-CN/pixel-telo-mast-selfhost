package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
)

func normalizeSources(sources []string) []string {
	seen := make(map[string]struct{}, len(sources))
	normalized := make([]string, 0, len(sources))
	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		normalized = append(normalized, source)
	}
	return normalized
}

func collectMissingSources(sources []string, records map[string]*domain.Record) []string {
	missing := make([]string, 0, len(sources))
	for _, source := range sources {
		if _, ok := records[source]; !ok {
			missing = append(missing, source)
		}
	}
	return missing
}

func selectReadyResult(sources []string, records map[string]*domain.Record) (*domain.Record, bool) {
	missingSeen := false
	var firstNonSpam *domain.Record
	for _, source := range sources {
		record, ok := records[source]
		if !ok {
			missingSeen = true
			continue
		}
		if firstNonSpam == nil {
			firstNonSpam = record
		}
		if record.IsSpam() {
			return record, !missingSeen
		}
	}
	if firstNonSpam != nil && !missingSeen {
		return firstNonSpam, true
	}
	return firstNonSpam, false
}

func selectAvailableResult(sources []string, records map[string]*domain.Record) *domain.Record {
	var firstNonSpam *domain.Record
	for _, source := range sources {
		record, ok := records[source]
		if !ok {
			continue
		}
		if record.IsSpam() {
			return record
		}
		if firstNonSpam == nil {
			firstNonSpam = record
		}
	}
	return firstNonSpam
}

func firstLookupError(ctx context.Context, errMap map[string]error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return domain.ErrUpstreamTimeout
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var firstErr error
	for _, err := range errMap {
		if err == nil {
			continue
		}
		if errors.Is(err, domain.ErrRateLimited) {
			return domain.ErrRateLimited
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return domain.ErrUpstreamTimeout
		}
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", domain.ErrUpstreamUnavailable, firstErr)
}

func toRecord(phone, source string, info *port.ProviderResult) *domain.Record {
	confidence := int64(0)
	if info.IsSpam {
		confidence = 100
	}
	resultSource := source
	if strings.TrimSpace(info.Source) != "" {
		resultSource = info.Source
	}
	return &domain.Record{
		PhoneNumber: phone,
		Tag:         info.Tag,
		Source:      resultSource,
		Confidence:  confidence,
		HitCount:    1,
		FetchedAt:   time.Now(),
	}
}
