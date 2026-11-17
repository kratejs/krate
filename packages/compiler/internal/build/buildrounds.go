package build

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// buildRounds lets the pages of one build share their esbuild work. A page
// that needs the result of a job (the check build of a client chunk, the
// resource hints of a real-React component) queues it and waits. When every
// page still building waits, the queued jobs run together: one batched esbuild
// call per kind of job instead of one call per job, so the modules the jobs
// share are parsed once per round instead of once per job.
//
// A waiting page gives its worker slot back, so the pages that have not
// started yet can run until they wait too. Jobs are deduplicated by kind and
// key for the whole build: a page that asks for a job another page queued
// waits for that one.
//
// A page must not hold a lock another page can block on while it waits: a
// page blocked on such a lock is neither waiting nor able to finish, so the
// round would never start.
type buildRounds struct {
	pool *workerPool
	// minRunnable is how few pages may still be running before the queued
	// jobs run: half the worker slots, so the batch uses the cores the
	// waiting pages leave idle instead of waiting for the last page.
	minRunnable int

	mu      sync.Mutex
	live    int // pages started and not finished
	waiters []*roundWaiter
	queued  []*roundJob
	running bool
	early   int // batches started before a round (see enqueue)

	// Eager jobs (see eager) run in the background as the pages make them.
	eagerOnce sync.Once
	eagerCh   chan *roundJob
	eagerDone chan struct{}
}

const (
	// eagerWait is how long the eager runner gathers jobs into one batch.
	eagerWait = time.Second
	// eagerMax is the most jobs one eager batch runs.
	eagerMax = 32
	// eagerParallel is how many eager batches run at once.
	eagerParallel = 2
)

// eager starts j soon, in the background, batched with the other jobs pages
// make meanwhile, instead of when a page first waits for it. It suits jobs
// whose runs take long after their batch (a slow render) and are few: a
// round batches many small jobs better.
func (r *buildRounds) eager(j *roundJob) {
	if r == nil || j == nil || !j.claimed.CompareAndSwap(false, true) {
		return
	}
	j.rounds = r
	r.eagerOnce.Do(func() {
		r.eagerCh = make(chan *roundJob, 1<<16)
		r.eagerDone = make(chan struct{})
		go r.runEager()
	})
	r.eagerCh <- j
}

func (r *buildRounds) runEager() {
	var wg sync.WaitGroup
	defer func() {
		wg.Wait()
		close(r.eagerDone)
	}()
	pool := newWorkerPool(eagerParallel)
	for j := range r.eagerCh {
		batch := []*roundJob{j}
		deadline := time.After(eagerWait)
	gather:
		for len(batch) < eagerMax {
			select {
			case next, ok := <-r.eagerCh:
				if !ok {
					break gather
				}
				batch = append(batch, next)
			case <-deadline:
				break gather
			}
		}
		pool.acquire()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer pool.release()
			runRoundJobs(batch)
			r.mu.Lock()
			r.flushLocked()
			r.mu.Unlock()
		}()
	}
}

// enqueue queues j for a batch now, before any page waits for it. A full
// batch of queued jobs starts at once (see flushLocked), so the batches the
// pages need at the end are mostly done by then.
func (r *buildRounds) enqueue(j *roundJob) {
	if r == nil || j == nil || !j.claimed.CompareAndSwap(false, true) {
		return
	}
	j.rounds = r
	r.mu.Lock()
	r.queued = append(r.queued, j)
	r.flushLocked()
	r.mu.Unlock()
}

const (
	// earlyBatch is how many queued jobs start a batch before a round.
	earlyBatch = clientChunkBatchSize
	// earlyParallel is how many such batches run at once.
	earlyParallel = 2
)

// close stops the eager runner once its queued jobs have run. Call it when
// every page finished.
func (r *buildRounds) close() {
	if r == nil {
		return
	}
	r.eagerOnce.Do(func() {})
	if r.eagerCh != nil {
		close(r.eagerCh)
		<-r.eagerDone
	}
}

// roundJob is one unit of batched work. run is called once with every queued
// job of the same kind; it fills in each job's output.
type roundJob struct {
	kind   string
	key    string
	run    func(jobs []*roundJob)
	input  any
	output any

	claimed atomic.Bool
	done    chan struct{}
	closed  sync.Once
	// rounds is the coordinator the job was queued on (nil when it ran inline).
	rounds *buildRounds
	// detached jobs finish after their batch returns (see detach).
	detached atomic.Bool
}

// detach tells the batch running j that j finishes later, through complete:
// the round ends without it, so the pages that do not wait for j go on.
func (j *roundJob) detach() { j.detached.Store(true) }

// complete finishes a detached job, resuming the pages that wait for it.
func (j *roundJob) complete() {
	j.markDone()
	if r := j.rounds; r != nil {
		r.mu.Lock()
		r.flushLocked()
		r.mu.Unlock()
	}
}

func (j *roundJob) markDone() { j.closed.Do(func() { close(j.done) }) }

type roundWaiter struct {
	jobs    []*roundJob
	resumed chan struct{}
}

// newBuildRounds coordinates pages pages built on pool.
func newBuildRounds(pages int, pool *workerPool) *buildRounds {
	r := &buildRounds{pool: pool, live: pages, minRunnable: 1}
	if pool != nil {
		r.minRunnable = max(1, pool.gate.Slots()/2)
	}
	return r
}

// roundJobs is the registry of a build's jobs: one job per kind and key.
type roundJobs struct{ m sync.Map }

// job returns the build's job of kind and key, making it with input when it
// is new. run batches the jobs of the kind.
func (js *roundJobs) job(kind, key string, input any, run func([]*roundJob)) *roundJob {
	id := kind + "\x00" + key
	if v, ok := js.m.Load(id); ok {
		return v.(*roundJob)
	}
	j := &roundJob{kind: kind, key: key, run: run, input: input, done: make(chan struct{})}
	v, _ := js.m.LoadOrStore(id, j)
	return v.(*roundJob)
}

// own returns the build's job of kind and key, and whether the caller owns
// it: the owner computes the job itself (not in a round) and ends it with
// buildRounds.finish; everyone else waits for it with buildRounds.wait, which
// counts them as waiting while the owner works.
func (js *roundJobs) own(kind, key string) (*roundJob, bool) {
	j := js.job(kind, key, nil, nil)
	return j, j.claimed.CompareAndSwap(false, true)
}

// reset forgets every job, for a new build.
func (js *roundJobs) reset() { js.m.Clear() }

func (j *roundJob) isDone() bool {
	select {
	case <-j.done:
		return true
	default:
		return false
	}
}

// wait blocks until every job in jobs has run. Without rounds (a single page
// rebuilt in dev, a page built outside the parallel page pass) the jobs run at
// once, batched by kind.
func (r *buildRounds) wait(jobs ...*roundJob) {
	var pending []*roundJob
	for _, j := range jobs {
		if j != nil && !j.isDone() {
			pending = append(pending, j)
		}
	}
	if len(pending) == 0 {
		return
	}
	if r == nil {
		var mine []*roundJob
		for _, j := range pending {
			if j.claimed.CompareAndSwap(false, true) {
				mine = append(mine, j)
			}
		}
		runRoundJobs(mine)
		for _, j := range pending {
			<-j.done
		}
		return
	}
	w := &roundWaiter{jobs: pending, resumed: make(chan struct{})}
	r.mu.Lock()
	for _, j := range pending {
		if j.claimed.CompareAndSwap(false, true) {
			j.rounds = r
			r.queued = append(r.queued, j)
		}
	}
	r.waiters = append(r.waiters, w)
	r.flushLocked()
	r.mu.Unlock()

	if r.pool != nil {
		r.pool.release()
	}
	<-w.resumed
	if r.pool != nil {
		r.pool.acquire()
	}
}

// finish ends an owned job (see roundJobs.own), resuming its waiters.
func (r *buildRounds) finish(j *roundJob) {
	j.markDone()
	if r == nil {
		return
	}
	r.mu.Lock()
	r.flushLocked()
	r.mu.Unlock()
}

// leave records that one page finished.
func (r *buildRounds) leave() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.live--
	r.flushLocked()
	r.mu.Unlock()
}

// flushLocked resumes the waiters whose jobs are done and starts a round when
// jobs are queued and fewer than minRunnable live pages still run.
func (r *buildRounds) flushLocked() {
	kept := r.waiters[:0]
	for _, w := range r.waiters {
		done := true
		for _, j := range w.jobs {
			done = done && j.isDone()
		}
		if done {
			close(w.resumed)
		} else {
			kept = append(kept, w)
		}
	}
	r.waiters = kept
	for len(r.queued) >= earlyBatch && r.early < earlyParallel && !r.running {
		batch := append([]*roundJob(nil), r.queued[:earlyBatch]...)
		r.queued = append([]*roundJob(nil), r.queued[earlyBatch:]...)
		r.early++
		go func() {
			runRoundJobs(batch)
			r.mu.Lock()
			r.early--
			r.flushLocked()
			r.mu.Unlock()
		}()
	}
	if r.running || len(r.queued) == 0 || r.live-len(r.waiters) >= r.minRunnable {
		return
	}
	batch := r.queued
	r.queued = nil
	r.running = true
	go func() {
		runRoundJobs(batch)
		r.mu.Lock()
		r.running = false
		r.flushLocked()
		r.mu.Unlock()
	}()
}

// runRoundJobs runs jobs, one batch per kind, the kinds in parallel, and
// marks every job done. A batch that panics leaves its outputs nil.
func runRoundJobs(jobs []*roundJob) {
	byKind := map[string][]*roundJob{}
	var kinds []string
	for _, j := range jobs {
		if _, ok := byKind[j.kind]; !ok {
			kinds = append(kinds, j.kind)
		}
		byKind[j.kind] = append(byKind[j.kind], j)
	}
	sort.Strings(kinds)
	var wg sync.WaitGroup
	for _, k := range kinds {
		group := byKind[k]
		// The same batch whatever order the pages queued their jobs in.
		sort.Slice(group, func(a, b int) bool { return group[a].key < group[b].key })
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				p := recover()
				if p != nil {
					fmt.Fprintf(os.Stderr, "  %sWarning:%s batched %s build failed: %v\n", cYellow, cReset, group[0].kind, p)
				}
				for _, j := range group {
					if p != nil || !j.detached.Load() {
						j.markDone()
					}
				}
			}()
			group[0].run(group)
		}()
	}
	wg.Wait()
}
