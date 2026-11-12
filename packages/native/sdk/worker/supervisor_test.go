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

func TestStartUnknownWorker(t *testing.T) {
	sup := newSup(t, &fakeRunner{}, spec("never", 0))
	if _, err := sup.Start(context.Background(), "nope", nil, nil); err == nil {
		t.Fatal("started an undeclared worker")
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

func TestNeverPolicyStaysStopped(t *testing.T) {
	r := &fakeRunner{}
	sup := newSup(t, r, spec(RestartNever, 0))
	ch, cancel := sup.Subscribe("")
	defer cancel()
	if _, err := sup.Start(context.Background(), "job", nil, nil); err != nil {
		t.Fatal(err)
	}
	r.proc(t, 0).end(1, nil)
	waitKind(t, ch, EventCrash)
	waitKind(t, ch, EventStopped)
}

func TestCleanExitDoesNotRestartOnFailure(t *testing.T) {
	r := &fakeRunner{}
	sup := newSup(t, r, spec(RestartOnFailure, 3))
	ch, cancel := sup.Subscribe("")
	defer cancel()
	_, _ = sup.Start(context.Background(), "job", nil, nil)
	r.proc(t, 0).end(0, nil)
	waitKind(t, ch, EventExit)
	waitKind(t, ch, EventStopped)
}

func TestGivesUpAfterMax(t *testing.T) {
	r := &fakeRunner{}
	sup := newSup(t, r, spec(RestartAlways, 2))
	ch, cancel := sup.Subscribe("")
	defer cancel()
	id, _ := sup.Start(context.Background(), "job", nil, nil)
	for i := 0; i < 3; i++ {
		r.proc(t, i).end(1, nil)
		if i < 2 {
			waitKind(t, ch, EventRestarting)
		}
	}
	waitKind(t, ch, EventFailed)
	if sup.Alive(id) {
		t.Fatal("a failed worker is not alive")
	}
	if got := sup.List()[0].State; got != StateFailed {
		t.Fatal(got)
	}
}

func TestFailedRestartStartsAreRetried(t *testing.T) {
	r := &fakeRunner{}
	sup := newSup(t, r, spec(RestartOnFailure, 4))
	ch, cancel := sup.Subscribe("")
	defer cancel()
	_, _ = sup.Start(context.Background(), "job", nil, nil)
	r.mu.Lock()
	r.fail = 2
	r.mu.Unlock()
	r.proc(t, 0).end(1, nil)
	waitKind(t, ch, EventRestarting)
	waitKind(t, ch, EventRestarting)
	waitKind(t, ch, EventRestarting)
	waitKind(t, ch, EventStarted)
}

func TestMessagesBothWays(t *testing.T) {
	r := &fakeRunner{}
	sup := newSup(t, r, spec(RestartNever, 0))
	id, _ := sup.Start(context.Background(), "job", nil, nil)
	ch, cancel := sup.Subscribe(id)
	defer cancel()
	if err := sup.Post(context.Background(), id, []byte("args")); err != nil {
		t.Fatal(err)
	}
	if got := <-r.proc(t, 0).posts; string(got) != "args" {
		t.Fatal(string(got))
	}
	if err := sup.Emit(id, []byte("frame")); err != nil {
		t.Fatal(err)
	}
	if ev := waitKind(t, ch, EventMessage); string(ev.Data) != "frame" || ev.Name != "job" {
		t.Fatalf("%+v", ev)
	}
	r.proc(t, 0).end(0, nil)
	waitKind(t, ch, EventStopped)
	if err := sup.Post(context.Background(), id, nil); err == nil {
		t.Fatal("posting to a stopped worker should fail")
	}
}

func TestSubscribeFiltersByInstance(t *testing.T) {
	r := &fakeRunner{}
	sup := newSup(t, r, spec(RestartNever, 0))
	a, _ := sup.Start(context.Background(), "job", nil, nil)
	b, _ := sup.Start(context.Background(), "job", nil, nil)
	ch, cancel := sup.Subscribe(b)
	defer cancel()
	_ = sup.Emit(a, []byte("a"))
	_ = sup.Emit(b, []byte("b"))
	if ev := waitKind(t, ch, EventMessage); string(ev.Data) != "b" {
		t.Fatalf("got %q", ev.Data)
	}
}

func TestDecide(t *testing.T) {
	r := Restart{Policy: RestartOnFailure, Max: 3, BackoffMs: 100}
	if d := r.Decide(Exit{Code: 1}, 0); !d.Restart || d.Delay != 100*time.Millisecond || d.Streak != 1 {
		t.Fatalf("%+v", d)
	}
	if d := r.Decide(Exit{Code: 1}, 2); !d.Restart || d.Delay != 400*time.Millisecond {
		t.Fatalf("backoff doubles: %+v", d)
	}
	if d := r.Decide(Exit{Code: 1}, 3); d.Restart || !d.GaveUp {
		t.Fatalf("%+v", d)
	}
	if d := r.Decide(Exit{Code: 1, Ran: StableAfter}, 3); !d.Restart || d.Streak != 1 {
		t.Fatalf("a long run resets the count: %+v", d)
	}
	if d := r.Decide(Exit{Code: 0}, 0); d.Restart {
		t.Fatal("clean exit under onFailure")
	}
	if d := r.Decide(Exit{Code: 1, Requested: true}, 0); d.Restart {
		t.Fatal("requested stop restarted")
	}
	if d := (Restart{}).Decide(Exit{Code: 1}, 0); d.Restart {
		t.Fatal("default policy is never")
	}
	big := Restart{Policy: RestartAlways, Max: 100, BackoffMs: 1000}
	if d := big.Decide(Exit{Code: 1}, 20); d.Delay != maxBackoff {
		t.Fatalf("delay is capped: %v", d.Delay)
	}
}

func TestSpecValidate(t *testing.T) {
	good := Spec{Name: "a-b.c_1", Runtime: "node", Entry: "w.mjs"}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, s := range []Spec{
		{Name: "", Runtime: "node", Entry: "x"},
		{Name: "../x", Runtime: "node", Entry: "x"},
		{Name: "a", Runtime: "python", Entry: "x"},
		{Name: "a", Runtime: "node"},
		{Name: "a", Runtime: "node", Entry: "x", Restart: Restart{Policy: "sometimes"}},
		{Name: "a", Runtime: "node", Entry: "x", Restart: Restart{Max: -1}},
	} {
		if s.Validate() == nil {
			t.Errorf("accepted %+v", s)
		}
	}
	if _, err := NewSupervisor(nil, []Spec{good, good}); err == nil {
		t.Fatal("duplicate names")
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
