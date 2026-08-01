package port

import "time"

// NoopMetrics 是 Self-host 默认使用的空指标实现。
type NoopMetrics struct{}

func (NoopMetrics) ObserveCache(string) {}

func (NoopMetrics) ObserveProvider(string, string, time.Duration) {}

func (NoopMetrics) ObserveLookup(string, string, time.Duration) {}
