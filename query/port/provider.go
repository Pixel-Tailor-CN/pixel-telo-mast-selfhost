package port

import "context"

// ProviderResult 是 Provider 返回的实现无关查询结论。
type ProviderResult struct {
	IsSpam bool
	Tag    string
	Source string
}

// ProviderDispatcher 按 source 分发上游查询并分别返回成功与错误。
type ProviderDispatcher interface {
	LookupAll(ctx context.Context, phone string, sources []string) (map[string]*ProviderResult, map[string]error)
}
