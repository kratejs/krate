package jsruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// MiddlewareRuntime executes the compiled middleware.js in embedded QuickJS.
// It keeps a small pool of VMs (like the API runtime) so concurrent requests
// do not serialize on one VM, supports async middleware, and in hot mode
// reloads the module when the file changes.
type MiddlewareRuntime struct {
	path    string
	workers int

	mu      sync.Mutex
	hot     bool
	env     map[string]string
	idle    []*mwWorker
	tokens  chan struct{}
	matcher bool
	lazy    bool
}

type mwWorker struct {
	rt      *Runtime
	version int64
}

// MiddlewareRequest is the input to the middleware function.
type MiddlewareRequest struct {
	URL        string
	Method     string
	Path       string
	Headers    map[string][]string
	RemoteAddr string
}

// Middleware actions.
const (
	MiddlewareContinue = "continue"
	MiddlewareResponse = "response"
	MiddlewareSkip     = "skip"
	MiddlewareError    = "error"
)

// MiddlewareResult is the output of the middleware function. For
// MiddlewareContinue, Headers are added to the downstream response and
// Rewrite (when set) replaces the request path. For MiddlewareResponse the
// middleware's own response is served.
type MiddlewareResult struct {
	Action     string
	Status     int
	StatusText string
	Headers    [][2]string
	Body       []byte
	Rewrite    string
	Error      string
	Stack      string
}

// NewMiddlewareRuntime loads middleware.js and validates that it exports a
// middleware function.
func NewMiddlewareRuntime(middlewarePath string) (*MiddlewareRuntime, error) {
	n := DefaultAPIWorkers()
	m := &MiddlewareRuntime{path: middlewarePath, workers: n, tokens: make(chan struct{}, n)}
	w, err := m.newWorker()
	if err != nil {
		return nil, err
	}
	m.idle = append(m.idle, w)
	m.matcher = w.hasMatcher()
	return m, nil
}

// NewLazyMiddlewareRuntime returns a runtime that loads middleware.js on first
// use and tolerates a missing or broken file (requests then skip the
// middleware or fail closed with the load error). Dev uses it so edits made
// after startup, including adding the file, take effect.
func NewLazyMiddlewareRuntime(middlewarePath string) *MiddlewareRuntime {
	n := DefaultAPIWorkers()
	m := &MiddlewareRuntime{path: middlewarePath, workers: n, tokens: make(chan struct{}, n), hot: true, lazy: true}
	return m
}

// SetHot makes the runtime re-read middleware.js when it changes on disk.
func (m *MiddlewareRuntime) SetHot(hot bool) {
	m.mu.Lock()
	m.hot = hot
	m.mu.Unlock()
}

// SetEnv sets process.env for the middleware VMs.
func (m *MiddlewareRuntime) SetEnv(env map[string]string) {
	m.mu.Lock()
	m.env = env
	stale := m.idle
	m.idle = nil
	m.mu.Unlock()
	for _, w := range stale {
		_ = w.rt.Close()
	}
}

// HasMatcher reports whether the module declares a matcher (config.matcher);
// without one the host skips static assets by default.
func (m *MiddlewareRuntime) HasMatcher() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.matcher
}

func (w *mwWorker) hasMatcher() bool {
	v, err := w.rt.Execute("__krate_mw_has_matcher()")
	b, _ := v.(bool)
	return err == nil && b
}

func (m *MiddlewareRuntime) newWorker() (*mwWorker, error) {
	code, err := os.ReadFile(m.path)
	if err != nil {
		return nil, fmt.Errorf("failed to read middleware.js: %w", err)
	}
	version := fileVersion(m.path)
	features, known := readMiddlewareFeatures(m.path)
	rt, err := NewWithOptions(runnerOptions(features, known))
	if err != nil {
		return nil, fmt.Errorf("failed to create JS runtime: %w", err)
	}
	fail := func(err error) (*mwWorker, error) {
		_ = rt.Close()
		return nil, err
	}
	if err := rt.registerBodyHost(); err != nil {
		return fail(err)
	}
	m.mu.Lock()
	env := m.env
	m.mu.Unlock()
	if env != nil {
		if err := rt.SetEnv(env); err != nil {
			return fail(err)
		}
	}
	bc, err := apiRunnerBytecode()
	if err != nil {
		return fail(err)
	}
	if _, err := rt.EvalBytecode(bc); err != nil {
		return fail(err)
	}
	if _, err := rt.Execute(stripExports(string(code)) + "\n"); err != nil {
		return fail(fmt.Errorf("failed to execute middleware.js: %w", err))
	}
	v, err := rt.Execute(`__krate_mw_init(
		typeof middleware === 'function' ? middleware : (typeof middleware === 'object' && middleware && typeof middleware.middleware === 'function' ? middleware.middleware : (typeof __default === 'function' ? __default : null)),
		typeof config === 'undefined' ? undefined : config,
		typeof matcher === 'undefined' ? undefined : matcher)`)
	if err != nil {
		return fail(err)
	}
	if msg, _ := v.(string); msg != "" {
		return fail(errors.New(msg))
	}
	return &mwWorker{rt: rt, version: version}, nil
}

func (m *MiddlewareRuntime) acquire() (*mwWorker, error) {
	m.tokens <- struct{}{}
	m.mu.Lock()
	hot := m.hot
	var w *mwWorker
	if n := len(m.idle); n > 0 {
		w = m.idle[n-1]
		m.idle = m.idle[:n-1]
	}
	m.mu.Unlock()
	if w != nil && hot && w.version != fileVersion(m.path) {
		_ = w.rt.Close()
		w = nil
		// Drop the other stale workers too; they reload on demand.
		m.mu.Lock()
		stale := m.idle
		m.idle = nil
		m.mu.Unlock()
		for _, o := range stale {
			_ = o.rt.Close()
		}
	}
	if w == nil {
		nw, err := m.newWorker()
		if err != nil {
			<-m.tokens
			return nil, err
		}
		w = nw
		m.mu.Lock()
		m.matcher = w.hasMatcher()
		m.mu.Unlock()
	}
	return w, nil
}

func (m *MiddlewareRuntime) release(w *mwWorker) {
	m.mu.Lock()
	m.idle = append(m.idle, w)
	m.mu.Unlock()
	<-m.tokens
}

// Execute runs the middleware for one request.
func (m *MiddlewareRuntime) Execute(req MiddlewareRequest) MiddlewareResult {
	return m.ExecuteContext(context.Background(), req)
}

// ExecuteContext runs the middleware, aborting if ctx is cancelled.
func (m *MiddlewareRuntime) ExecuteContext(ctx context.Context, req MiddlewareRequest) MiddlewareResult {
	if m.lazy {
		if _, err := os.Stat(m.path); err != nil {
			return MiddlewareResult{Action: MiddlewareSkip}
		}
	}
	w, err := m.acquire()
	if err != nil {
		return MiddlewareResult{Action: MiddlewareError, Error: err.Error()}
	}
	defer m.release(w)
	rt := w.rt
	defer func() { _, _ = rt.Execute("__krate_finish()") }()

	if req.URL == "" {
		req.URL = "http://localhost" + req.Path
	}
	if req.Method == "" {
		req.Method = "GET"
	}
	env := jsEnvelope{URL: req.URL, Method: req.Method, Path: req.Path, Headers: envelopeHeaders(req.Headers), RemoteAddr: req.RemoteAddr}
	envJSON, _ := json.Marshal(env)

	if err := rt.SetGlobal("__krate_mw_req", string(envJSON)); err != nil {
		return MiddlewareResult{Action: MiddlewareError, Error: err.Error()}
	}
	v, err := rt.Execute("__krate_mw_begin(__krate_mw_req)")
	if err != nil {
		return MiddlewareResult{Action: MiddlewareError, Error: err.Error()}
	}
	if s, _ := v.(string); s == "skip" {
		return MiddlewareResult{Action: MiddlewareSkip}
	}
	phase := func() bool {
		s, _ := rt.evalString("__krate_phase()")
		return s != "pending"
	}
	if err := rt.waitFor(ctx, apiTimeout(), phase); err != nil {
		_, _ = rt.Execute("__krate_abort()")
		return MiddlewareResult{Action: MiddlewareError, Error: "middleware: " + err.Error()}
	}
	raw, err := rt.evalString("__krate_mw_head()")
	if err != nil {
		return MiddlewareResult{Action: MiddlewareError, Error: err.Error()}
	}
	var h struct {
		Kind       string      `json:"kind"`
		Status     int         `json:"status"`
		StatusText string      `json:"statusText"`
		Headers    [][2]string `json:"headers"`
		Body       string      `json:"body"`
		Bin        bool        `json:"bin"`
		Text       string      `json:"text"`
		Rewrite    string      `json:"rewrite"`
		Error      string      `json:"error"`
		Stack      string      `json:"stack"`
	}
	if err := json.Unmarshal([]byte(raw), &h); err != nil {
		return MiddlewareResult{Action: MiddlewareError, Error: "invalid middleware result"}
	}
	switch h.Kind {
	case "error":
		return MiddlewareResult{Action: MiddlewareError, Error: h.Error, Stack: h.Stack}
	case "continue":
		return MiddlewareResult{Action: MiddlewareContinue, Headers: h.Headers, Rewrite: h.Rewrite}
	}
	var body []byte
	if h.Text != "" {
		body = []byte(h.Text)
	} else if h.Body != "" || h.Bin {
		b, ok := rt.outBytes(h.Bin, h.Body)
		if !ok {
			return MiddlewareResult{Action: MiddlewareError, Error: "invalid middleware response body"}
		}
		body = b
	}
	return MiddlewareResult{Action: MiddlewareResponse, Status: h.Status, StatusText: h.StatusText, Headers: h.Headers, Body: body}
}

// readMiddlewareFeatures reads the prelude features the middleware references
// from middleware.json, which the build writes next to middleware.js. A missing
// file or features field means unknown.
func readMiddlewareFeatures(middlewarePath string) ([]string, bool) {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(middlewarePath), "middleware.json"))
	if err != nil {
		return nil, false
	}
	var meta struct {
		Features *[]string `json:"features"`
	}
	if json.Unmarshal(data, &meta) != nil || meta.Features == nil {
		return nil, false
	}
	return *meta.Features, true
}

// Close releases the pooled VMs.
func (m *MiddlewareRuntime) Close() {
	m.mu.Lock()
	idle := m.idle
	m.idle = nil
	m.mu.Unlock()
	for _, w := range idle {
		_ = w.rt.Close()
	}
}

// MiddlewarePath returns the conventional compiled middleware location, or ""
// when the project has none.
func MiddlewarePath(root string) string {
	p := filepath.Join(root, ".krate", "middleware.js")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// LoadMiddlewareRuntime loads the middleware.js and returns a runtime, or nil
// if there is no middleware or it failed to load (the error is reported).
func LoadMiddlewareRuntime(root string) *MiddlewareRuntime {
	p := MiddlewarePath(root)
	if p == "" {
		return nil
	}
	rt, err := NewMiddlewareRuntime(p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  !Middleware load error: %s\n", err)
		return nil
	}
	return rt
}
