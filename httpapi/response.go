package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
)

type recordResponse struct {
	PhoneNumber string `json:"phone_number"`
	Tag         string `json:"tag"`
	Source      string `json:"source"`
	Confidence  int64  `json:"confidence"`
}

type queryResponse struct {
	Record           *recordResponse `json:"record"`
	QueryMode        string          `json:"query_mode"`
	RequestedSources []string        `json:"requested_sources,omitempty"`
	EffectiveSources []string        `json:"effective_sources,omitempty"`
	InvalidSources   []string        `json:"invalid_sources,omitempty"`
}

func makeQueryResponse(result *domain.LookupResult) queryResponse {
	return queryResponse{Record: &recordResponse{PhoneNumber: result.Record.PhoneNumber, Tag: result.Record.Tag, Source: result.Record.Source, Confidence: result.Record.Confidence}, QueryMode: string(result.QueryMode), RequestedSources: result.RequestedSources, EffectiveSources: result.EffectiveSources, InvalidSources: result.InvalidSources}
}

func errorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrRateLimited):
		return http.StatusTooManyRequests, "rate_limited"
	case errors.Is(err, domain.ErrUpstreamTimeout):
		return http.StatusGatewayTimeout, "upstream_timeout"
	case errors.Is(err, domain.ErrUpstreamUnavailable), errors.Is(err, domain.ErrNotFound):
		return http.StatusServiceUnavailable, "upstream_unavailable"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "upstream_timeout"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func validPhone(phone string) bool {
	phone = strings.TrimSpace(phone)
	if len(phone) < 7 || len(phone) > 20 {
		return false
	}
	for _, char := range phone {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
