package crash

import (
	"sync"
	"time"
)

// limiter allows a view a few automatic reloads in a time window, so a page
// that crashes the renderer as soon as it loads cannot loop forever.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time
	hits   map[uint64][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, now: time.Now, hits: map[uint64][]time.Time{}}
}

// allow records an attempt for key and reports whether a reload may happen.
func (l *limiter) allow(key uint64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}
