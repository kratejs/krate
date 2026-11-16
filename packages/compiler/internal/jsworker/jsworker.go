// Package jsworker runs build-time JavaScript in long-lived Node or Bun worker
// processes instead of the embedded QuickJS engine, which is much slower and
// serializes concurrent runtimes on its allocator.
//
// Each evaluation runs in a fresh vm context with the globals the embedded
// runtime provides (its web API prelude, process.env and the minimal require),
// so module state never carries from one evaluation to the next. A pool that
// cannot start a worker, or whose worker dies, reports ErrUnavailable from then
// on until it is closed, and callers run the code in QuickJS instead.
package jsworker

import (
	"bufio"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/execx"
	"github.com/kratejs/krate/packages/native/jsruntime"
)

//go:embed driver.js
var driverTemplate string

// EnvVar selects the build-time JavaScript engine: quickjs, node or bun.
// Unset (or auto) prefers the project's runtime, then node, then bun.
const EnvVar = "KRATE_BUILD_JS"

// ErrUnavailable means no worker can run the request; run it in QuickJS.
var ErrUnavailable = errors.New("jsworker: no JavaScript worker available")

// Request is one evaluation.
type Request struct {
	// Code is the JavaScript to run.
	Code string
	// Module evaluates Code as an ES module bundle: strict, top-level
	// bindings stay local, and a throw is not reported (QuickJS's
	// ExecuteModule). Otherwise Code runs as a classic script.
	Module bool
	// Env, when non-empty, becomes process.env.
	Env map[string]string
	// Await waits, after Code runs, until Until holds or nothing the code
	// started (timers, fetches) is pending.
	Await bool
	// Until is an expression evaluated in the context while awaiting.
	Until string
	// Reads are expressions evaluated in the context at the end.
	Reads []string
	// Timeout bounds the whole evaluation (default KRATE_JS_TIMEOUT, 30s).
	Timeout time.Duration
	// Label names the kind of evaluation for Pool.Observe.
	Label string
}

// Value is the result of one read.
type Value struct {
	// Set is false for null and undefined.
	Set bool
	// IsString reports a string value; S is String(value) otherwise.
	IsString bool
	S        string
	// Err is the text of the error the expression threw.
	Err string
}

// Result is the outcome of an evaluation.
type Result struct {
	// Threw reports that a script threw; Err is the thrown value as text
	// ("TypeError: message").
	Threw bool
	Err   string
	// Unsupported reports code the worker cannot run (it only parses in the
	// embedded engine, for example a module using import.meta).
	Unsupported bool
	Reads       []Value
}

// Select returns the runtime the build should run JavaScript on and its
// executable path, or "" for QuickJS. preferred lists the project's runtime
// settings in priority order; node and bun among them win.
func Select(preferred ...string) (name, path string) {
	return selectWith(exec.LookPath, os.Getenv(EnvVar), preferred...)
}

func selectWith(look func(string) (string, error), env string, preferred ...string) (string, string) {
	switch v := strings.ToLower(strings.TrimSpace(env)); v {
	case "quickjs", "off", "0", "none":
		return "", ""
	case "node", "bun":
		if p, err := look(v); err == nil {
			return v, p
		}
		return "", ""
	}
	var order []string
	for _, p := range preferred {
		switch v := strings.ToLower(strings.TrimSpace(p)); v {
		case "node", "bun":
			order = append(order, v)
		}
	}
	order = append(order, "node", "bun")
	for _, n := range order {
		if p, err := look(n); err == nil {
			return n, p
		}
	}
	return "", ""
}

// DefaultSize is the number of worker processes a pool runs at most.
func DefaultSize() int {
	if v, err := strconv.Atoi(os.Getenv("KRATE_BUILD_JS_WORKERS")); err == nil && v > 0 {
		return v
	}
	return max(1, min(4, runtime.NumCPU()/3))
}

// Pool runs evaluations on up to Size worker processes, started on demand.
type Pool struct {
	// Name is the runtime ("node" or "bun") and Path its executable.
	Name, Path string
	// Size is the most workers that run at once.
	Size int
	// Stdout and Stderr receive the console lines of evaluated code
	// (default os.Stdout and os.Stderr).
	Stdout, Stderr io.Writer
	// IdleTimeout stops idle workers after that long without a request
	// (default 30s; negative disables it).
	IdleTimeout time.Duration
	// Observe, when set, is called with the label and duration of every
	// evaluation a worker ran.
	Observe func(label string, d time.Duration)

	mu      sync.Mutex
	cond    *sync.Cond
	idle    []*worker
	running int
	broken  error
	gen     int
	idleT   *time.Timer
	// live holds every worker started, for PIDs.
	live map[*worker]bool
}

// New returns a pool for runtime name at path; it starts no process yet.
func New(name, path string, size int) *Pool {
	if size <= 0 {
		size = DefaultSize()
	}
	return &Pool{Name: name, Path: path, Size: size}
}

// Broken returns why the pool stopped using workers, or nil.
func (p *Pool) Broken() error {
	if p == nil {
		return ErrUnavailable
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.broken
}

// Run evaluates req on a worker. It returns ErrUnavailable (wrapped) when no
// worker could run it; the caller then runs it in QuickJS.
func (p *Pool) Run(req Request) (Result, error) {
	if p == nil {
		return Result{}, ErrUnavailable
	}
	w, gen, err := p.acquire()
	if err != nil {
		return Result{}, err
	}
	if usesGlobal(req.Code, w.missing) {
		// The code may use a global only the embedded engine has (Node 22
		// has no Float16Array): it runs there, as it did before workers.
		p.release(w, gen)
		return Result{Unsupported: true}, nil
	}
	t0 := time.Now()
	res, err := w.run(req, p.out(), p.errOut())
	if p.Observe != nil && req.Label != "" {
		p.Observe(req.Label, time.Since(t0))
	}
	if err != nil {
		p.fail(w, gen, err)
		return Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	p.release(w, gen)
	return res, nil
}

func (p *Pool) out() io.Writer {
	if p.Stdout != nil {
		return p.Stdout
	}
	return os.Stdout
}

func (p *Pool) errOut() io.Writer {
	if p.Stderr != nil {
		return p.Stderr
	}
	return os.Stderr
}

func (p *Pool) acquire() (*worker, int, error) {
	p.mu.Lock()
	if p.cond == nil {
		p.cond = sync.NewCond(&p.mu)
	}
	for {
		if p.broken != nil {
			err := p.broken
			p.mu.Unlock()
			return nil, 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		if n := len(p.idle); n > 0 {
			w := p.idle[n-1]
			p.idle = p.idle[:n-1]
			p.running++
			gen := p.gen
			p.mu.Unlock()
			return w, gen, nil
		}
		if p.running < p.Size {
			p.running++
			gen := p.gen
			p.mu.Unlock()
			w, err := start(p.Name, p.Path)
			if err != nil {
				p.mu.Lock()
				p.running--
				if p.gen == gen && p.broken == nil {
					p.broken = err
				}
				p.cond.Broadcast()
				p.mu.Unlock()
				return nil, 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
			}
			p.mu.Lock()
			if p.live == nil {
				p.live = map[*worker]bool{}
			}
			p.live[w] = true
			p.mu.Unlock()
			return w, gen, nil
		}
		p.cond.Wait()
	}
}

func (p *Pool) release(w *worker, gen int) {
	p.mu.Lock()
	p.running--
	if gen != p.gen || p.broken != nil {
		p.mu.Unlock()
		w.stop()
		p.mu.Lock()
	} else {
		p.idle = append(p.idle, w)
		p.armIdle()
	}
	p.cond.Broadcast()
	p.mu.Unlock()
}

// fail stops w and every other worker: a worker that died once is not trusted
// for the rest of the build.
func (p *Pool) fail(w *worker, gen int, err error) {
	w.stop()
	p.mu.Lock()
	p.running--
	var idle []*worker
	if gen == p.gen && p.broken == nil {
		p.broken = err
		idle, p.idle = p.idle, nil
	}
	p.cond.Broadcast()
	p.mu.Unlock()
	for _, x := range idle {
		x.stop()
	}
}

// armIdle (re)starts the idle timer; p.mu is held.
func (p *Pool) armIdle() {
	d := p.IdleTimeout
	if d == 0 {
		d = 30 * time.Second
	}
	if d < 0 {
		return
	}
	if p.idleT != nil {
		p.idleT.Stop()
	}
	gen := p.gen
	p.idleT = time.AfterFunc(d, func() {
		p.mu.Lock()
		if p.gen != gen || p.running > 0 {
			p.mu.Unlock()
			return
		}
		idle := p.idle
		p.idle = nil
		p.mu.Unlock()
		for _, w := range idle {
			w.stop()
		}
	})
}

// Close stops every worker and waits for them to exit. Workers busy with a
// request stop when it finishes. The pool may be used again afterwards: it
// starts new workers and forgets an earlier failure.
func (p *Pool) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	idle := p.idle
	p.idle = nil
	p.gen++
	p.broken = nil
	if p.idleT != nil {
		p.idleT.Stop()
		p.idleT = nil
	}
	if p.cond != nil {
		p.cond.Broadcast()
	}
	p.mu.Unlock()
	for _, w := range idle {
		w.stop()
	}
}

// Running returns the number of worker processes alive (idle or busy), for
// tests.
func (p *Pool) Running() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.idle) + p.running
}

// PIDs returns the process IDs of the workers alive, for tests.
func (p *Pool) PIDs() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []int
	for w := range p.live {
		select {
		case <-w.exited:
			delete(p.live, w)
		default:
			out = append(out, w.cmd.Process.Pid)
		}
	}
	return out
}

// Driver returns the worker driver script.
func Driver() string {
	src, _ := json.Marshal(jsruntime.NodeGlobalsSource())
	return strings.Replace(driverTemplate, "__KRATE_NODE_GLOBALS__", string(src), 1)
}

var (
	driverOnce sync.Once
	driverSrc  string
)

type worker struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	out     *bufio.Reader
	exited  chan struct{}
	stopped sync.Once
	seq     int
	version string
	// missing lists the globals of the embedded engine an evaluation on this
	// worker does not have.
	missing []string
}

// startTimeout bounds how long a worker may take to report ready.
const startTimeout = 15 * time.Second

func start(name, path string) (*worker, error) {
	driverOnce.Do(func() { driverSrc = Driver() })
	args := []string{"-e", driverSrc}
	cmd := exec.Command(path, args...)
	execx.ConfigureTree(cmd)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	outPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", name, err)
	}
	w := &worker{cmd: cmd, in: in, out: bufio.NewReaderSize(outPipe, 1<<16), exited: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(w.exited)
	}()
	ready := make(chan error, 1)
	go func() {
		var msg reply
		err := readFrame(w.out, &msg)
		if err == nil && msg.T != "ready" {
			err = fmt.Errorf("unexpected first message %q", msg.T)
		}
		w.version = msg.V
		w.missing = missingGlobals(msg.G)
		ready <- err
	}()
	select {
	case err := <-ready:
		if err != nil {
			w.stop()
			return nil, fmt.Errorf("%s worker did not start: %w", name, err)
		}
	case <-time.After(startTimeout):
		w.stop()
		return nil, fmt.Errorf("%s worker did not start within %s", name, startTimeout)
	}
	return w, nil
}

// stop closes the worker's stdin, which ends it, and kills it if it has not
// exited shortly after.
func (w *worker) stop() {
	w.stopped.Do(func() {
		_ = w.in.Close()
		select {
		case <-w.exited:
		case <-time.After(2 * time.Second):
			execx.KillTree(w.cmd)
			<-w.exited
		}
	})
}

type header struct {
	ID        int               `json:"id"`
	Module    bool              `json:"module"`
	Env       map[string]string `json:"env,omitempty"`
	Await     bool              `json:"await"`
	Until     string            `json:"until,omitempty"`
	Reads     []string          `json:"reads"`
	TimeoutMs int64             `json:"timeoutMs"`
}

type readValue struct {
	S   *string `json:"s"`
	Str bool    `json:"str"`
	E   *string `json:"e"`
}

type reply struct {
	T           string       `json:"t"`
	V           string       `json:"v"`
	G           []string     `json:"g"`
	ID          int          `json:"id"`
	K           int          `json:"k"`
	S           string       `json:"s"`
	Threw       bool         `json:"threw"`
	Err         string       `json:"err"`
	Unsupported bool         `json:"unsupported"`
	Reads       []*readValue `json:"reads"`
}

// graceTimeout is how long past the request's own timeout the worker may take
// to answer before it is killed.
const graceTimeout = 10 * time.Second

// Timeout is the default evaluation timeout, KRATE_JS_TIMEOUT as the embedded
// runtime reads it.
func Timeout() time.Duration {
	if v := os.Getenv("KRATE_JS_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 30 * time.Second
}

func (w *worker) run(req Request, stdout, stderr io.Writer) (Result, error) {
	w.seq++
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = Timeout()
	}
	h := header{ID: w.seq, Module: req.Module, Await: req.Await, Until: req.Until, Reads: req.Reads, TimeoutMs: timeout.Milliseconds()}
	if len(req.Env) > 0 {
		h.Env = req.Env
	}
	if h.Reads == nil {
		h.Reads = []string{}
	}
	head, err := json.Marshal(h)
	if err != nil {
		return Result{}, err
	}
	kill := time.AfterFunc(timeout+graceTimeout, func() { execx.KillTree(w.cmd) })
	defer kill.Stop()
	if err := writeFrames(w.in, head, []byte(req.Code)); err != nil {
		return Result{}, fmt.Errorf("writing to worker: %w", err)
	}
	for {
		var msg reply
		if err := readFrame(w.out, &msg); err != nil {
			return Result{}, fmt.Errorf("reading from worker: %w", err)
		}
		switch msg.T {
		case "log":
			switch msg.K {
			case 1:
				fmt.Fprintln(stderr, "WARN: "+msg.S)
			case 2:
				fmt.Fprintln(stderr, "ERROR: "+msg.S)
			default:
				fmt.Fprintln(stdout, msg.S)
			}
		case "r":
			if msg.ID != w.seq {
				return Result{}, fmt.Errorf("worker answered request %d, want %d", msg.ID, w.seq)
			}
			res := Result{Threw: msg.Threw, Err: msg.Err, Unsupported: msg.Unsupported}
			for _, r := range msg.Reads {
				switch {
				case r == nil:
					res.Reads = append(res.Reads, Value{})
				case r.E != nil:
					res.Reads = append(res.Reads, Value{Err: *r.E})
				case r.S != nil:
					res.Reads = append(res.Reads, Value{Set: true, IsString: r.Str, S: *r.S})
				default:
					res.Reads = append(res.Reads, Value{})
				}
			}
			return res, nil
		default:
			return Result{}, fmt.Errorf("unexpected worker message %q", msg.T)
		}
	}
}

// maxFrame bounds a frame from the worker.
const maxFrame = 1 << 30

func writeFrames(w io.Writer, frames ...[]byte) error {
	var bw = bufio.NewWriterSize(w, 1<<16)
	for _, f := range frames {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(f)))
		if _, err := bw.Write(n[:]); err != nil {
			return err
		}
		if _, err := bw.Write(f); err != nil {
			return err
		}
	}
	return bw.Flush()
}

func readFrame(r *bufio.Reader, v any) error {
	var n [4]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return err
	}
	size := binary.BigEndian.Uint32(n[:])
	if size > maxFrame {
		return fmt.Errorf("frame of %d bytes", size)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	return json.Unmarshal(buf, v)
}

var (
	engineGlobalsOnce sync.Once
	engineGlobals     []string
)

// missingGlobals returns the globals of the embedded engine that are not
// among have, the globals of a worker's evaluation context. Krate's own
// helpers are left out: the worker installs its own.
func missingGlobals(have []string) []string {
	engineGlobalsOnce.Do(func() {
		rt, err := jsruntime.New()
		if err != nil {
			return
		}
		defer rt.Close()
		v, err := rt.Execute(`Object.getOwnPropertyNames(globalThis).filter((n) => !/^__krate|^_go_|^__require/.test(n)).join(",")`)
		if s, ok := v.(string); err == nil && ok && s != "" {
			engineGlobals = strings.Split(s, ",")
		}
	})
	if len(have) == 0 {
		return nil
	}
	got := make(map[string]bool, len(have))
	for _, n := range have {
		got[n] = true
	}
	var out []string
	for _, n := range engineGlobals {
		if !got[n] {
			out = append(out, n)
		}
	}
	return out
}

// usesGlobal reports whether code names one of globals as a whole word.
func usesGlobal(code string, globals []string) bool {
	for _, g := range globals {
		for i := 0; ; {
			j := strings.Index(code[i:], g)
			if j < 0 {
				break
			}
			at := i + j
			end := at + len(g)
			if (at == 0 || !identByte(code[at-1])) && (end == len(code) || !identByte(code[end])) {
				return true
			}
			i = at + 1
		}
	}
	return false
}

func identByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}
