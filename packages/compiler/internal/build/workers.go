package build

import (
	"fmt"
	"io"
	"runtime"

	"github.com/kratejs/krate/packages/compiler/internal/membudget"
)

// workerPool bounds the number of concurrently running esbuild invocations.
// esbuild already parallelizes internally, so spawning one goroutine per page
// without a limit causes CPU/memory thrashing on large sites. All fan-out loops
// in the build pipeline route their work through a pool sized by CPU, which
// also admits fewer jobs while memory is short (see membudget.Gate), down to
// one at a time.
type workerPool struct {
	gate *membudget.Gate
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
	return &workerPool{gate: membudget.NewGate(limit)}
}

// acquire blocks until a worker slot is available.
func (p *workerPool) acquire() {
	p.gate.Acquire()
}

// release returns a worker slot to the pool.
func (p *workerPool) release() {
	p.gate.Release()
}

// reportMemory prints the build's memory budget and how often the build
// waited for memory (with --profile).
func reportMemory(w io.Writer) {
	b, _ := membudget.Current()
	fmt.Fprintf(w, "    memory budget %s (%s); memory pressure held jobs back %d times\n",
		membudget.Format(b.Bytes), b.Source, membudget.Throttled())
}

// devPageWorkers is how many pages a dev rebuild builds at once. A page
// spends much of its time waiting (for another page's parse of a module they
// share, for the layout it renders in), so the usual one worker per core left
// cores idle and made the pages of a large rebuild run in waves. The limit is
// twice the cores at most, and never more than there are pages.
func devPageWorkers(pages int) int {
	n := buildWorkerLimit()
	if pages > n {
		n = min(pages, 2*n)
	}
	return n
}
