package port

import "time"

// Metrics 定义查询核心可选的指标记录端口。
type Metrics interface {
	ObserveCache(result string)
	ObserveProvider(source, result string, duration time.Duration)
	ObserveLookup(mode, result string, duration time.Duration)
}
