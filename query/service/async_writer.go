package service

import (
	"context"
	"log/slog"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
)

func (s *Service) enqueueSave(records []*domain.Record) {
	select {
	case s.asyncSaveCh <- records:
	default:
		slog.Warn("async save channel full, dropping records", "count", len(records))
	}
}

func (s *Service) asyncWriter() {
	defer s.wg.Done()
	for records := range s.asyncSaveCh {
		s.saveSafe(records)
	}
}

func (s *Service) saveSafe(records []*domain.Record) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("panic in query async writer", "error", recovered)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), s.saveTimeout)
	defer cancel()
	if err := s.repo.SaveBatch(ctx, records); err != nil {
		slog.Error("failed to save query records asynchronously", "count", len(records), "error", err)
	}
}
