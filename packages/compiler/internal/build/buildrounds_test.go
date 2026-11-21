package build

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Pages that wait on jobs get them run together once every page waits, each
// job once, and a page waiting on another page's job shares its result.
func TestBuildRoundsBatchesJobsAcrossPages(t *testing.T) {
	t.Parallel()
	const pages = 20
	pool := newWorkerPool(4)
	r := newBuildRounds(pages, pool)
	var reg roundJobs
	var batches, runs atomic.Int32
	run := func(jobs []*roundJob) {
		batches.Add(1)
		for _, j := range jobs {
			runs.Add(1)
			j.output = j.input.(int) * 10
		}
	}
	var wg sync.WaitGroup
	var bad atomic.Int32
	for p := 0; p < pages; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			defer r.leave()
			pool.acquire()
			defer pool.release()
			// Two rounds per page; key k%5 is shared by four pages.
			for round := 0; round < 2; round++ {
				k := round*100 + p%5
				j := reg.job("test", string(rune('a'+k)), k, run)
				r.wait(j)
				if j.output != k*10 {
					bad.Add(1)
				}
			}
		}(p)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("pages deadlocked")
	}
	if bad.Load() != 0 {
		t.Fatalf("%d pages read a wrong result", bad.Load())
	}
	if got := runs.Load(); got != 10 {
		t.Errorf("ran %d jobs, want 10 (5 distinct keys per round)", got)
	}
	// A round starts when fewer than half the slots have a page to run, so a
	// round's jobs come from most of the pages.
	if got := batches.Load(); got < 2 || got > 6 {
		t.Errorf("ran %d batches, want a few (each page waits twice)", got)
	}
}

// A page that finishes without waiting does not hold up a round, and a page
// waiting on a job queued in an earlier round resumes when it ran.
func TestBuildRoundsPagesThatNeverWait(t *testing.T) {
	t.Parallel()
	pool := newWorkerPool(2)
	r := newBuildRounds(3, pool)
	var reg roundJobs
	run := func(jobs []*roundJob) {
		for _, j := range jobs {
			j.output = true
		}
	}
	var wg sync.WaitGroup
	for p := 0; p < 3; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			defer r.leave()
			pool.acquire()
			defer pool.release()
			if p == 0 {
				return
			}
			a := reg.job("k", "a", nil, run)
			b := reg.job("k", "b", nil, run)
			if p == 1 {
				r.wait(a)
				r.wait(a, b)
			} else {
				r.wait(b)
			}
		}(p)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("pages deadlocked")
	}
}

// Without rounds (a dev rebuild of one page) jobs run at once, and a job two
// callers share still runs once.
func TestBuildRoundsNilRunsInline(t *testing.T) {
	t.Parallel()
	var reg roundJobs
	var runs atomic.Int32
	run := func(jobs []*roundJob) {
		for _, j := range jobs {
			runs.Add(1)
			j.output = 1
		}
	}
	var r *buildRounds
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j := reg.job("k", "same", nil, run)
			r.wait(j)
			if j.output != 1 {
				t.Error("job not run")
			}
		}()
	}
	wg.Wait()
	if runs.Load() != 1 {
		t.Errorf("shared job ran %d times, want 1", runs.Load())
	}
}

// Pages waiting for a job another page computes itself (an async layout's
// part) count as waiting, so the rounds the owner waits on still start.
func TestBuildRoundsOwnedJobWaitersDoNotBlockRounds(t *testing.T) {
	t.Parallel()
	const pages = 8
	pool := newWorkerPool(3)
	r := newBuildRounds(pages, pool)
	var reg roundJobs
	var computed atomic.Int32
	run := func(jobs []*roundJob) {
		for _, j := range jobs {
			j.output = "chunk"
		}
	}
	var wg sync.WaitGroup
	for p := 0; p < pages; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer r.leave()
			pool.acquire()
			defer pool.release()
			j, owner := reg.own("layout", "root")
			if owner {
				computed.Add(1)
				chunk := reg.job("chunk", "c", nil, run)
				r.wait(chunk)
				j.output = chunk.output
				r.finish(j)
			} else {
				r.wait(j)
			}
			if j.output != "chunk" {
				t.Error("a page read the owned job before it finished")
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("pages deadlocked")
	}
	if computed.Load() != 1 {
		t.Errorf("the owned job was computed %d times, want once", computed.Load())
	}
}
