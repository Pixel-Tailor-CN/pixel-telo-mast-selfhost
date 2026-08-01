package domain

import "errors"

var (
	// ErrNotFound 表示本地与上游均没有可用查询结论。
	ErrNotFound = errors.New("query record not found")
	// ErrRateLimited 表示上游拒绝请求或触发反爬限制。
	ErrRateLimited = errors.New("provider rate limited")
	// ErrUpstreamUnavailable 表示上游暂时不可用。
	ErrUpstreamUnavailable = errors.New("upstream unavailable")
	// ErrUpstreamTimeout 表示上游查询超时。
	ErrUpstreamTimeout = errors.New("upstream timeout")
)

// QueryMode 表示请求实际采用的查询模式。
type QueryMode string

const (
	QueryModeV1         QueryMode = "v1"
	QueryModeV2         QueryMode = "v2"
	QueryModeV1Fallback QueryMode = "v1_fallback"
)

// LookupResult 描述 v2 查询结果及 source 清洗信息。
type LookupResult struct {
	Record           *Record
	QueryMode        QueryMode
	RequestedSources []string
	EffectiveSources []string
	InvalidSources   []string
}

// SourceDescriptor 描述一个可用 source 及其默认优先级。
type SourceDescriptor struct {
	ID       string
	Priority int
}

// SourceListResult 描述当前实例通过本地配置启用的 source。
type SourceListResult struct {
	DefaultSources   []string
	AvailableSources []SourceDescriptor
}
