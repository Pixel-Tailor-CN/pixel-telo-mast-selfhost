// Package domain 定义与部署方式和存储实现无关的查询领域模型。
package domain

import "time"

// Record 表示一个 source 对号码给出的查询结论。
type Record struct {
	PhoneNumber string
	Tag         string
	Source      string
	Confidence  int64
	HitCount    int64
	FetchedAt   time.Time
}

// IsSpam 返回该记录是否为骚扰号码结论。
func (r *Record) IsSpam() bool {
	return r != nil && r.Confidence > 0
}
