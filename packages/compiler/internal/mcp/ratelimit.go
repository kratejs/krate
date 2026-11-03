package mcp

import (
	"net"
	"strings"
	"sync"
	"time"
)

type rateBucket struct {
	start time.Time
	count int
}

type rateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]*rateBucket
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	if limit <= 0 || window <= 0 {
		return nil
	}
	return &rateLimiter{limit: limit, window: window, buckets: map[string]*rateBucket{}}
}

// allow reports whether key may make another request and, when refused, how
// long until the window resets.
func (r *rateLimiter) allow(key string) (bool, time.Duration) {
	if r == nil {
		return true, 0
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buckets) > 4096 {
		for k, b := range r.buckets {
			if now.Sub(b.start) >= r.window {
				delete(r.buckets, k)
			}
		}
	}
	b := r.buckets[key]
	if b == nil || now.Sub(b.start) >= r.window {
		r.buckets[key] = &rateBucket{start: now, count: 1}
		return true, 0
	}
	if b.count >= r.limit {
		return false, r.window - now.Sub(b.start)
	}
	b.count++
	return true, 0
}

func rateLimitKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return strings.Trim(host, "[]")
}
