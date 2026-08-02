package security

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type QueryLimiter struct {
	mu        sync.Mutex
	last      time.Time
	tokens    float64
	rate      float64
	burst     float64
	semaphore chan struct{}
}

func NewQueryLimiter(requestsPerSecond float64, burst, maxConcurrent int) *QueryLimiter {
	return &QueryLimiter{rate: requestsPerSecond, burst: float64(burst), tokens: float64(burst), semaphore: make(chan struct{}, maxConcurrent)}
}

func (l *QueryLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.allow() {
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limited", "code": "rate_limited"})
			return
		}
		select {
		case l.semaphore <- struct{}{}:
			defer func() { <-l.semaphore }()
			c.Next()
		default:
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "concurrency limited", "code": "rate_limited"})
		}
	}
}

func (l *QueryLimiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.last.IsZero() {
		l.last = now
	}
	l.tokens += now.Sub(l.last).Seconds() * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
	l.last = now
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}
