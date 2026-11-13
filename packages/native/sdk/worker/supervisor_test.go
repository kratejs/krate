package worker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeProc struct {
	pid    int
	exited chan struct{}
	once   sync.Once
	code   int
	err    error
	posts  chan []byte
}

func newProc(pid int) *fakeProc {
	return &fakeProc{pid: pid, exited: make(chan struct{}), posts: make(chan []byte, 16)}
}

func (p *fakeProc) Post(_ context.Context, a []byte) error { p.posts <- a; return nil }
func (p *fakeProc) Exited() <-chan struct{}                { return p.exited }
func (p *fakeProc) Result() (int, error)                   { return p.code, p.err }
func (p *fakeProc) Pid() int                               { return p.pid }
func (p *fakeProc) Stop()                                  { p.end(0, nil) }
func (p *fakeProc) end(code int, err error) {
	p.once.Do(func() { p.code, p.err = code, err; close(p.exited) })
}

type fakeRunner struct {
	mu    sync.Mutex
	procs []*fakeProc
	fail  int // fail this many starts
}

func (r *fakeRunner) Start(_ context.Context, in Instance) (Proc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail > 0 {
		r.fail--
		return nil, errors.New("cannot start")
	}
	p := newProc(100 + len(r.procs))
	r.procs = append(r.procs, p)
	return p, nil
}

func (r *fakeRunner) proc(t *testing.T, i int) *fakeProc {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		if i < len(r.procs) {
			p := r.procs[i]
			r.mu.Unlock()
			return p
		}
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("process %d never started", i)
	return nil
}

func spec(policy string, max int) Spec {
	return Spec{Name: "job", Runtime: "exec", Entry: "bin/job", Restart: Restart{Policy: policy, Max: max, BackoffMs: 1}}
}

func newSup(t *testing.T, r Runner, s Spec) *Supervisor {
	t.Helper()
	sup, err := NewSupervisor(r, []Spec{s})
	if err != nil {
		t.Fatal(err)
	}
	sup.sleep = func(ctx context.Context, d time.Duration) bool { return ctx.Err() == nil }
	t.Cleanup(sup.StopAll)
	return sup
}

func waitKind(t *testing.T, ch <-chan Event, kind string) Event {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Kind == kind {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %q event", kind)
		}
	}
}

func TestCrashRestartsOnFailure(t *testing.T) {
	r := &fakeRunner{}
	sup := newSup(t, r, spec(RestartOnFailure, 3))
	ch, cancel := sup.Subscribe("")
	defer cancel()
	id, err := sup.Start(context.Background(), "job", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitKind(t, ch, EventStarted)
	r.proc(t, 0).end(2, nil)
	ev := waitKind(t, ch, EventCrash)
	if ev.ExitCode == nil || *ev.ExitCode != 2 {
		t.Fatalf("crash event: %+v", ev)
	}
	waitKind(t, ch, EventRestarting)
	waitKind(t, ch, EventStarted)
	r.proc(t, 1)
	info := sup.List()
	if len(info) != 1 || info[0].ID != id || info[0].Restarts != 1 || info[0].State != StateRunning {
		t.Fatalf("%+v", info)
	}
	if err := sup.Stop(id); err != nil {
		t.Fatal(err)
	}
	waitKind(t, ch, EventStopped)
	if sup.Alive(id) {
		t.Fatal("still alive after Stop")
	}
}

// The environment a start request adds cannot change how the worker's runtime
// or the dynamic loader loads code, or replace what the shell sets: a page that
// may start a worker would otherwise run code of its choice in it
// (NODE_OPTIONS=--import=data:..., LD_PRELOAD) or point it at another SDK
// endpoint (KRATE_GOHOST_PARENT). Plain variables still pass.
func TestStartRefusesLoaderAndShellEnv(t *testing.T) {
	r := &fakeRunner{}
	sup := newSup(t, r, spec(RestartNever, 0))
	for _, env := range []map[string]string{
		{"NODE_OPTIONS": "--import=data:text/javascript,process.exit(7)"},
		{"node_options": "--require=/tmp/x.js"},
		{"LD_PRELOAD": "/tmp/x.so"},
		{"DYLD_INSERT_LIBRARIES": "/tmp/x.dylib"},
		{"KRATE_GOHOST_PARENT": "unix:/tmp/other.sock"},
		{"KRATE_APP_DIR": "/tmp"},
		{"BUN_OPTIONS": "--preload=/tmp/x.ts"},
		{"DENO_DIR": "/tmp/cache"},
		{"PATH": "/tmp"},
		{"A=B": "c"},
		{"": "c"},
		{"OK": "nul\x00byte"},
	} {
		if _, err := sup.Start(context.Background(), "job", nil, env); err == nil {
			t.Errorf("started with env %q", env)
		} else if !strings.Contains(err.Error(), "bad_args") && !strings.Contains(err.Error(), "environment") {
			t.Errorf("env %q: unexpected error %v", env, err)
		}
	}
	if n := len(r.procs); n != 0 {
		t.Fatalf("%d processes were started", n)
	}
	if _, err := sup.Start(context.Background(), "job", nil, map[string]string{"JOB_MODE": "fast", "NODE_ENV": "test"}); err != nil {
		t.Fatalf("plain variables: %v", err)
	}
}
