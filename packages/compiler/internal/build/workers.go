package build

import "runtime"

// workerPool bounds the number of concurrently running esbuild invocations.
// esbuild already parallelizes internally, so spawning one goroutine per page
// without a limit causes CPU/memory thrashing on large sites. All fan-out loops
// in the build pipeline route their work through a shared pool sized by CPU.
type workerPool struct {
	sem chan struct{}
}

// buildWorkerLimit returns the concurrency cap for build workers. It is capped
// at 16 since each worker may itself run a multi-threaded esbuild instance.
func buildWorkerLimit() int {
	n := runtime.GOMAXPROCS(0)
	if n > 16 {
		return 16
	}
	if n < 1 {
		return 1
	}
	return n
}

// newWorkerPool creates a pool that admits up to limit concurrent tasks.
func newWorkerPool(limit int) *workerPool {
	if limit < 1 {
		limit = 1
	}
	return &workerPool{sem: make(chan struct{}, limit)}
}

// acquire blocks until a worker slot is available.
func (p *workerPool) acquire() {
	p.sem <- struct{}{}
}

// release returns a worker slot to the pool.
func (p *workerPool) release() {
	<-p.sem
}
