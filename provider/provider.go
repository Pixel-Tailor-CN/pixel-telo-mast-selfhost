// Package provider 实现 Self-host 可显式启用的公开数据源。
package provider

import (
	"context"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/port"
)

const (
	SogouSourceID = "sogou"
	So360SourceID = "360"
)

type lookupProvider interface {
	Lookup(ctx context.Context, phone string) (*port.ProviderResult, error)
}
