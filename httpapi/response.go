package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/phone"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
)

type phoneDataResponse struct {
	CardType string `json:"cardType"`
	Province string `json:"province"`
	City     string `json:"city"`
}

type queryResponse struct {
	Phone            string             `json:"phone"`
	IsSpam           bool               `json:"is_spam"`
	Tag              string             `json:"tag"`
	Confidence       int64              `json:"confidence"`
	Source           string             `json:"source"`
	Data             *phoneDataResponse `json:"data"`
	QueryMode        string             `json:"query_mode"`
	RequestedSources []string           `json:"requested_sources,omitempty"`
	EffectiveSources []string           `json:"effective_sources,omitempty"`
	InvalidSources   []string           `json:"invalid_sources,omitempty"`
}

func makeQueryResponse(result *domain.LookupResult, phoneRecord *phone.Record) queryResponse {
	var data *phoneDataResponse
	if phoneRecord != nil {
		data = &phoneDataResponse{CardType: phoneRecord.CardType, Province: phoneRecord.Province, City: phoneRecord.City}
	}
	return queryResponse{
		Phone:            result.Record.PhoneNumber,
		IsSpam:           result.Record.Confidence > 50,
		Tag:              result.Record.Tag,
		Confidence:       result.Record.Confidence,
		Source:           result.Record.Source,
		Data:             data,
		QueryMode:        string(result.QueryMode),
		RequestedSources: result.RequestedSources,
		EffectiveSources: result.EffectiveSources,
		InvalidSources:   result.InvalidSources,
	}
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
