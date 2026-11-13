//go:build darwin

package darwin

import "sync"

// pathQueue hands batches of paths (files or URLs) to a handler that may be
// set after the first batch arrived. The operating system can ask a starting
// app to open a file before its code had the chance to say who wants to hear
// about it; those batches are held and delivered in order when the handler is
// set.
type pathQueue struct {
	mu   sync.Mutex
	fn   func([]string)
	held [][]string
}

// maxHeldBatches bounds what a handler-less queue keeps.
const maxHeldBatches = 64

// push delivers paths to the handler, or holds them.
func (q *pathQueue) push(paths []string) {
	if len(paths) == 0 {
		return
	}
	q.mu.Lock()
	fn := q.fn
	if fn == nil {
		if len(q.held) < maxHeldBatches {
			q.held = append(q.held, append([]string(nil), paths...))
		}
		q.mu.Unlock()
		return
	}
	q.mu.Unlock()
	fn(paths)
}

// setHandler sets the handler and delivers what was held, in order, before any
// later push can overtake it.
func (q *pathQueue) setHandler(fn func([]string)) {
	q.mu.Lock()
	q.fn = fn
	held := q.held
	q.held = nil
	q.mu.Unlock()
	if fn == nil {
		return
	}
	for _, b := range held {
		fn(b)
	}
}
