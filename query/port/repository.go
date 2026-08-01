// Package port 定义查询核心依赖的输入输出端口。
package port

import (
	"context"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/query/domain"
)

// QueryRepository 提供 source 级查询缓存读写能力。
type QueryRepository interface {
	ListByPhone(ctx context.Context, phone string) ([]*domain.Record, error)
	ListByPhoneAndSources(ctx context.Context, phone string, sources []string) ([]*domain.Record, error)
	SaveBatch(ctx context.Context, records []*domain.Record) error
}
