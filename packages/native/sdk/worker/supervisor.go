package worker

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
)

// Proc is one running worker process.
type Proc interface {
	// Post delivers one message: args is a wire frame holding the argument
	// array [message] for the worker's message function.
	Post(ctx context.Context, args []byte) error
	// Exited is closed when the process ended.
	Exited() <-chan struct{}
	// Result reports how it ended; valid after Exited is closed.
	Result() (code int, err error)
	Pid() int
	// Stop asks the process to exit and kills it after a grace period.
	Stop()
}

// Runner starts worker processes. The shell implements it with host child
// processes; tests use a fake.
type Runner interface {
	// Start launches one process for the instance and returns when it is ready.
	Start(ctx context.Context, inst Instance) (Proc, error)
}

// Instance is what a Runner needs to start a process.
type Instance struct {
	ID   string
	Spec Spec
	// Args and Env come from the start request.
	Args []string
	Env  map[string]string
}

// Owner is the port owner id of the instance.
func (i Instance) Owner() string { return "worker:" + i.ID }

// States of a worker instance.
const (
	StateStarting   = "starting"
	StateRunning    = "running"
	StateRestarting = "restarting"
	StateStopped    = "stopped"
	StateFailed     = "failed"
)

// Info describes one instance.
type Info struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	State    string `json:"state"`
	Pid      int    `json:"pid"`
	Restarts int    `json:"restarts"`
	// ExitCode is the last exit code (-1 for a signal); null while it never exited.
	ExitCode *int   `json:"exitCode,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Event kinds.
const (
	EventStarted    = "started"
	EventExit       = "exit"
	EventCrash      = "crash"
	EventRestarting = "restarting"
	EventStopped    = "stopped"
	EventFailed     = "failed"
	EventMessage    = "message"
)

// Event is delivered to subscribers of the worker events stream. Data is a
// wire frame holding the message of an EventMessage.
type Event struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Pid      int    `json:"pid,omitempty"`
	ExitCode *int   `json:"exitCode,omitempty"`
	Error    string `json:"error,omitempty"`
	// Attempt and DelayMs describe an EventRestarting.
	Attempt int `json:"attempt,omitempty"`
	DelayMs int `json:"delayMs,omitempty"`
	// Data is the wire frame of the message (EventMessage only).
	Data []byte `json:"-"`
}

type inst struct {
	in      Instance
	mu      sync.Mutex
	state   string
	proc    Proc
	restart int
	streak  int
	stopReq bool
	exit    *int
	err     string
	cancel  context.CancelFunc
	done    chan struct{}

	// backlog holds the events published before any subscriber for this
	// instance attached (guarded by Supervisor.mu). The page opens its
	// subscription over several round trips after the start call returned, and
	// a worker can answer before that; the first subscriber gets the backlog.
	backlog      []Event
	backlogBytes int
	attached     bool
}

// Bounds of the backlog of an instance nobody subscribed to yet.
const (
	maxBacklogEvents = 256
	maxBacklogBytes  = 4 << 20
)

// Supervisor starts declared workers and keeps them running per their policy.
type Supervisor struct {
	runner Runner
	specs  map[string]Spec
	// OnStart and OnEnd are called (may be nil) when an instance first starts
	// and when it ends for good (stopped or failed), with its port owner id.
	OnStart func(Instance)
	OnEnd   func(Instance)
	// Sleep waits for d; tests replace it. It returns false if ctx ended.
	sleep func(ctx context.Context, d time.Duration) bool

	mu    sync.Mutex
	next  int
	insts map[string]*inst
	subs  map[*sub]struct{}
}

type sub struct {
	id string // "" = all instances
	ch chan Event
}

// NewSupervisor returns a Supervisor for the declared specs.
func NewSupervisor(r Runner, specs []Spec) (*Supervisor, error) {
	m := map[string]Spec{}
	for _, s := range specs {
		if err := s.Validate(); err != nil {
			return nil, err
		}
		if _, dup := m[s.Name]; dup {
			return nil, fmt.Errorf("worker %q is declared twice", s.Name)
		}
		m[s.Name] = s
	}
	return &Supervisor{runner: r, specs: m, insts: map[string]*inst{}, subs: map[*sub]struct{}{}, sleep: realSleep}, nil
}

func realSleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// Specs lists the declared workers, sorted by name.
func (s *Supervisor) Specs() []Spec {
	out := make([]Spec, 0, len(s.specs))
	for _, sp := range s.specs {
		out = append(out, sp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Spec returns the declaration of a worker name.
func (s *Supervisor) Spec(name string) (Spec, bool) {
	sp, ok := s.specs[name]
	return sp, ok
}

// Start starts an instance of a declared worker and returns its id once the
// process is ready. A restart policy keeps it running afterwards.
func (s *Supervisor) Start(ctx context.Context, name string, args []string, env map[string]string) (string, error) {
	sp, ok := s.specs[name]
	if !ok {
		return "", krate.Errorf("not_found", "no worker %q is declared (declare it in native.workers)", name)
	}
	if err := ValidateEnv(env); err != nil {
		return "", krate.Errorf("bad_args", "worker %q: %v", name, err)
	}
	s.mu.Lock()
	s.next++
	id := "w" + strconv.Itoa(s.next)
	in := &inst{in: Instance{ID: id, Spec: sp, Args: args, Env: env}, state: StateStarting, done: make(chan struct{})}
	s.insts[id] = in
	s.mu.Unlock()

	runCtx, cancel := context.WithCancel(context.Background())
	in.cancel = cancel
	p, err := s.runner.Start(ctx, in.in)
	if err != nil {
		cancel()
		s.mu.Lock()
		delete(s.insts, id)
		s.mu.Unlock()
		return "", krate.Errorf("start_failed", "worker %q: %v", name, err)
	}
	if s.OnStart != nil {
		s.OnStart(in.in)
	}
	in.mu.Lock()
	in.proc, in.state = p, StateRunning
	in.mu.Unlock()
	s.publish(Event{Kind: EventStarted, ID: id, Name: name, Pid: p.Pid()})
	go s.supervise(runCtx, in, time.Now())
	return id, nil
}

// supervise waits for the process to end and applies the restart policy.
func (s *Supervisor) supervise(ctx context.Context, in *inst, started time.Time) {
	defer close(in.done)
	for {
		in.mu.Lock()
		p := in.proc
		in.mu.Unlock()
		select {
		case <-p.Exited():
		case <-ctx.Done():
			p.Stop()
			<-p.Exited()
		}
		code, err := p.Result()
		in.mu.Lock()
		ex := Exit{Code: code, Err: err, Requested: in.stopReq, Ran: time.Since(started)}
		c := code
		in.exit = &c
		if err != nil {
			in.err = err.Error()
		}
		d := in.in.Spec.Restart.Decide(ex, in.streak)
		in.mu.Unlock()
		ev := Event{Kind: EventExit, ID: in.in.ID, Name: in.in.Spec.Name, Pid: p.Pid(), ExitCode: &c}
		if err != nil {
			ev.Error = err.Error()
		}
		if ex.Failed() {
			ev.Kind = EventCrash
		}
		s.publish(ev)
		if !d.Restart {
			state, kind := StateStopped, EventStopped
			if d.GaveUp {
				state, kind = StateFailed, EventFailed
			}
			in.mu.Lock()
			in.state = state
			in.mu.Unlock()
			s.publish(Event{Kind: kind, ID: in.in.ID, Name: in.in.Spec.Name, ExitCode: &c, Error: ev.Error})
			s.end(in)
			return
		}
		in.mu.Lock()
		in.state = StateRestarting
		in.streak = d.Streak
		in.restart++
		n := in.restart
		in.mu.Unlock()
		s.publish(Event{Kind: EventRestarting, ID: in.in.ID, Name: in.in.Spec.Name, Attempt: n, DelayMs: int(d.Delay / time.Millisecond)})
		for {
			if !s.sleep(ctx, d.Delay) {
				s.finish(in, StateStopped, EventStopped)
				return
			}
			np, err := s.runner.Start(ctx, in.in)
			if err == nil {
				in.mu.Lock()
				in.proc, in.state = np, StateRunning
				in.mu.Unlock()
				started = time.Now()
				s.publish(Event{Kind: EventStarted, ID: in.in.ID, Name: in.in.Spec.Name, Pid: np.Pid()})
				break
			}
			// A failed start counts as a crash of the restart loop.
			dd := in.in.Spec.Restart.Decide(Exit{Code: 1, Err: err}, in.streak)
			in.mu.Lock()
			in.err = err.Error()
			in.mu.Unlock()
			if !dd.Restart {
				s.finish(in, StateFailed, EventFailed)
				return
			}
			in.mu.Lock()
			in.streak = dd.Streak
			in.restart++
			in.mu.Unlock()
			d = dd
			s.publish(Event{Kind: EventRestarting, ID: in.in.ID, Name: in.in.Spec.Name, Attempt: in.restart, DelayMs: int(d.Delay / time.Millisecond), Error: err.Error()})
		}
	}
}

func (s *Supervisor) finish(in *inst, state, kind string) {
	in.mu.Lock()
	in.state = state
	msg := in.err
	in.mu.Unlock()
	s.publish(Event{Kind: kind, ID: in.in.ID, Name: in.in.Spec.Name, Error: msg})
	s.end(in)
}

func (s *Supervisor) end(in *inst) {
	if s.OnEnd != nil {
		s.OnEnd(in.in)
	}
}

// Stop stops an instance and keeps it from restarting. It returns once the
// process has exited.
func (s *Supervisor) Stop(id string) error {
	s.mu.Lock()
	in := s.insts[id]
	s.mu.Unlock()
	if in == nil {
		return krate.Errorf("not_found", "no worker instance %q", id)
	}
	in.mu.Lock()
	in.stopReq = true
	in.mu.Unlock()
	in.cancel()
	<-in.done
	return nil
}

// StopAll stops every instance (the app is quitting).
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.insts))
	for id := range s.insts {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		_ = s.Stop(id)
	}
}

// Alive reports whether the instance exists and is not stopped or failed.
func (s *Supervisor) Alive(id string) bool {
	s.mu.Lock()
	in := s.insts[id]
	s.mu.Unlock()
	if in == nil {
		return false
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.state != StateStopped && in.state != StateFailed
}

// Spec of an instance.
func (s *Supervisor) SpecOf(id string) (Spec, bool) {
	s.mu.Lock()
	in := s.insts[id]
	s.mu.Unlock()
	if in == nil {
		return Spec{}, false
	}
	return in.in.Spec, true
}

// List describes every instance, by id.
func (s *Supervisor) List() []Info {
	s.mu.Lock()
	ins := make([]*inst, 0, len(s.insts))
	for _, in := range s.insts {
		ins = append(ins, in)
	}
	s.mu.Unlock()
	out := make([]Info, 0, len(ins))
	for _, in := range ins {
		in.mu.Lock()
		i := Info{ID: in.in.ID, Name: in.in.Spec.Name, State: in.state, Restarts: in.restart, ExitCode: in.exit, Error: in.err}
		if in.proc != nil && in.state == StateRunning {
			i.Pid = in.proc.Pid()
		}
		in.mu.Unlock()
		out = append(out, i)
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := strconv.Atoi(out[i].ID[1:])
		b, _ := strconv.Atoi(out[j].ID[1:])
		return a < b
	})
	return out
}

// ErrNotRunning is returned by Post for an instance that has no process now.
var ErrNotRunning = errors.New("worker is not running")

// Post delivers a message to the worker. args is the wire frame of [message].
func (s *Supervisor) Post(ctx context.Context, id string, args []byte) error {
	s.mu.Lock()
	in := s.insts[id]
	s.mu.Unlock()
	if in == nil {
		return krate.Errorf("not_found", "no worker instance %q", id)
	}
	in.mu.Lock()
	p, state := in.proc, in.state
	in.mu.Unlock()
	if p == nil || state != StateRunning {
		return krate.Errorf("not_running", "worker %s is %s", id, state)
	}
	return p.Post(ctx, args)
}

// Emit publishes a message a worker sent to the host.
func (s *Supervisor) Emit(id string, data []byte) error {
	s.mu.Lock()
	in := s.insts[id]
	s.mu.Unlock()
	if in == nil {
		return krate.Errorf("not_found", "no worker instance %q", id)
	}
	s.publish(Event{Kind: EventMessage, ID: id, Name: in.in.Spec.Name, Data: data})
	return nil
}

// Subscribe returns a channel of events for the instance id ("" for all) and
// a cancel function. A subscriber that falls 512 events behind loses the
// oldest.
func (s *Supervisor) Subscribe(id string) (<-chan Event, func()) {
	sb := &sub{id: id, ch: make(chan Event, 512)}
	s.mu.Lock()
	s.subs[sb] = struct{}{}
	if in := s.insts[id]; in != nil && !in.attached {
		in.attached = true
		for _, ev := range in.backlog {
			sb.ch <- ev // at most maxBacklogEvents, below the capacity
		}
		in.backlog, in.backlogBytes = nil, 0
	}
	s.mu.Unlock()
	return sb.ch, func() {
		s.mu.Lock()
		delete(s.subs, sb)
		s.mu.Unlock()
	}
}

func (s *Supervisor) publish(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in := s.insts[ev.ID]; in != nil && !in.attached {
		in.backlog = append(in.backlog, ev)
		in.backlogBytes += len(ev.Data)
		for len(in.backlog) > maxBacklogEvents || (in.backlogBytes > maxBacklogBytes && len(in.backlog) > 1) {
			in.backlogBytes -= len(in.backlog[0].Data)
			in.backlog = in.backlog[1:]
		}
	}
	for sb := range s.subs {
		if sb.id != "" && sb.id != ev.ID {
			continue
		}
		select {
		case sb.ch <- ev:
		default:
			select {
			case <-sb.ch:
			default:
			}
			select {
			case sb.ch <- ev:
			default:
			}
		}
	}
}
