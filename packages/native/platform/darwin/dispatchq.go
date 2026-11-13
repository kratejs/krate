//go:build darwin

package darwin

import "sync"

// workQueue maps small integer keys to pending Go closures. The main-thread
// dispatch passes the key through dispatch_async_f's context pointer, so one
// purego callback serves every Dispatch call.
type workQueue struct {
	mu  sync.Mutex
	seq uintptr
	m   map[uintptr]func()
}

func newWorkQueue() *workQueue {
	return &workQueue{m: make(map[uintptr]func())}
}

// add stores fn and returns its nonzero key.
func (q *workQueue) add(fn func()) uintptr {
	q.mu.Lock()
	defer q.mu.Unlock()
	for {
		q.seq++
		if q.seq == 0 {
			continue
		}
		if _, busy := q.m[q.seq]; !busy {
			break
		}
	}
	q.m[q.seq] = fn
	return q.seq
}

// take removes and returns the closure for key, or nil if there is none.
func (q *workQueue) take(key uintptr) func() {
	q.mu.Lock()
	defer q.mu.Unlock()
	fn := q.m[key]
	delete(q.m, key)
	return fn
}

// len reports the number of pending closures.
func (q *workQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.m)
}
