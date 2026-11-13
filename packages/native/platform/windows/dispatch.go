//go:build windows

package windows

import (
	"fmt"
	"os"
	"sync"
)

// queue is the Go-side function queue behind Dispatch. A wake (a posted
// window message) is requested only when none is outstanding, so thousands of
// concurrent pushes cost a single message.
type queue struct {
	mu      sync.Mutex
	fns     []func()
	pending bool
}

// push appends fn and reports whether the caller must wake the main thread.
func (q *queue) push(fn func()) (wake bool) {
	q.mu.Lock()
	q.fns = append(q.fns, fn)
	wake = !q.pending
	q.pending = true
	q.mu.Unlock()
	return wake
}

// wakeFailed undoes the pending flag when the wake could not be posted, so
// the next push tries again.
func (q *queue) wakeFailed() {
	q.mu.Lock()
	q.pending = false
	q.mu.Unlock()
}

// drain runs queued functions until the queue is empty. It is safe to call
// re-entrantly (a nested message loop may drain again).
//
// The pending flag is cleared when a batch is taken, not after it ran: a
// function in the batch can run a modal loop (MessageBoxW, a file dialog),
// and a Dispatch from another goroutine meanwhile must post its own wake for
// that loop to deliver, instead of waiting for the modal call to return.
func (q *queue) drain() {
	for {
		q.mu.Lock()
		fns := q.fns
		q.fns = nil
		q.pending = false
		if len(fns) == 0 {
			q.mu.Unlock()
			return
		}
		q.mu.Unlock()
		for _, fn := range fns {
			runRecover(fn)
		}
	}
}

func runRecover(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "krate native: panic on main thread: %v\n", r)
		}
	}()
	fn()
}
