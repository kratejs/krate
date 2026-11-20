package build

import (
	"context"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/execx"
	"github.com/kratejs/krate/packages/compiler/internal/safe"
	"github.com/kratejs/krate/packages/native/jsruntime"
)

// scriptcRouteHeader carries the route file the host resolved to the scriptc
// server, which has no router of its own. The host sets it on every request,
// replacing what a client sent.
const scriptcRouteHeader = "X-Krate-Route"

// scriptcSidecar supervises the compiled API server: it runs on a loopback
// port with the host's secret, is restarted when it exits and stops with the
// host. Its lifecycle follows the Node API sidecar's.
type scriptcSidecar struct {
	exe     string
	port    int
	env     []string
	ready   *sidecarReady
	proxy   *httputil.ReverseProxy
	secret  string
	budgets map[string]time.Duration

	mu       sync.Mutex
	cmd      *exec.Cmd
	stopOnce sync.Once
	stopCh   chan struct{}
}

// scriptcCall is the per-request state shared by the proxy hooks: the budget
// timer, which stops once the server has answered.
type scriptcCall struct {
	timer    *time.Timer
	timedOut atomic.Bool
}

type scriptcCallKey struct{}

// newScriptcSidecar prepares the sidecar for exe on port. env is added to the
// process environment of the child.
func newScriptcSidecar(exe string, port int, secret string, env []string, budgets map[string]time.Duration) *scriptcSidecar {
	target, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
	s := &scriptcSidecar{
		exe: exe, port: port, env: env, secret: secret, budgets: budgets,
		ready:  newSidecarReady(sidecarStartTimeout),
		stopCh: make(chan struct{}),
	}
	s.ready.name = "scriptc API server"
	s.ready.hint = "run `krate build` again, or run " + exe + " by hand to see why it fails; routes that are not compiled by scriptc are not affected"
	p := httputil.NewSingleHostReverseProxy(target)
	base := p.Director
	p.Director = func(r *http.Request) {
		base(r)
		r.Header.Set(internalSecretHeader, secret)
	}
	p.Transport = newSidecarTransportTimeout(longestAPIBudget(budgets) + apiTimeoutGrace)
	p.ModifyResponse = func(resp *http.Response) error {
		if c, ok := resp.Request.Context().Value(scriptcCallKey{}).(*scriptcCall); ok {
			c.timer.Stop()
		}
		return nil
	}
	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json")
		if c, ok := r.Context().Value(scriptcCallKey{}).(*scriptcCall); ok && c.timedOut.Load() {
			eprintf("  %s! API route exceeded its request budget: %s %s%s\n", cYellow, r.Method, r.URL.Path, cReset)
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusGatewayTimeout)
			_, _ = io.WriteString(w, `{"error":"Gateway Timeout"}`)
			return
		}
		if r.Context().Err() != nil {
			return
		}
		eprintf("  %sscriptc API proxy error%s %s %s: %v\n", cRed, cReset, r.Method, r.URL.Path, err)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":"Bad Gateway"}`)
	}
	s.proxy = p
	return s
}

// ServeRoute forwards r to the server as the route file. The route's budget
// (maxDuration, else the default) bounds the wait for the response headers,
// as the Node sidecar bounds the handler's Response.
func (s *scriptcSidecar) ServeRoute(w http.ResponseWriter, r *http.Request, file string) {
	if !s.ready.up.Load() && !s.ready.wait(r.Context()) {
		s.ready.unavailable(w, r)
		return
	}
	budget := jsruntime.APITimeout()
	if d, ok := s.budgets[file]; ok {
		budget = d
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	call := &scriptcCall{}
	call.timer = time.AfterFunc(budget, func() {
		call.timedOut.Store(true)
		cancel()
	})
	defer call.timer.Stop()
	r = r.WithContext(context.WithValue(ctx, scriptcCallKey{}, call))
	r.Header.Set(scriptcRouteHeader, file)
	s.proxy.ServeHTTP(newDeadlineWriter(w), r)
}

func (s *scriptcSidecar) makeCmd(token string) *exec.Cmd {
	cmd := exec.Command(s.exe)
	execx.ConfigureTree(cmd)
	cmd.Env = append(os.Environ(), environ.Overlay(environ.Current)...)
	cmd.Env = append(cmd.Env, s.env...)
	cmd.Env = append(cmd.Env,
		"KRATE_API_PORT="+strconv.Itoa(s.port),
		"KRATE_SIDECAR_START="+token,
		"KRATE_PARENT_PID="+strconv.Itoa(os.Getpid()),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = childStderr()
	return cmd
}

// Run starts the server and restarts it when it exits, until Stop. It blocks.
func (s *scriptcSidecar) Run() {
	defer func() {
		if r := recover(); r != nil {
			_ = safe.Recover(r)
		}
	}()
	delay := 100 * time.Millisecond
	for {
		select {
		case <-s.stopCh:
			return
		default:
		}
		token := newSidecarStartToken()
		cmd := s.makeCmd(token)
		started := time.Now()
		s.mu.Lock()
		select {
		case <-s.stopCh:
			s.mu.Unlock()
			return
		default:
		}
		err := cmd.Start()
		if err == nil {
			s.cmd = cmd
		}
		s.mu.Unlock()
		if err == nil {
			gen := s.ready.begin()
			exited := make(chan struct{})
			go s.ready.watch(gen, s.port, token, exited)
			err = cmd.Wait()
			close(exited)
			s.ready.down()
		}
		select {
		case <-s.stopCh:
			return
		default:
		}
		if time.Since(started) > 10*time.Second {
			delay = 100 * time.Millisecond
		}
		eprintf("  %s! scriptc API server exited (%v); restarting in %s%s\n", cYellow, err, delay, cReset)
		select {
		case <-s.stopCh:
			return
		case <-time.After(delay):
		}
		if delay < 5*time.Second {
			delay *= 2
		}
	}
}

// Stop stops the server and its restarts.
func (s *scriptcSidecar) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		execx.KillTree(cmd)
	}
}

// scriptcServePlan is what serve needs of the build's scriptc record.
type scriptcServePlan struct {
	exe    string
	routes map[string]bool
	// warning explains why scriptc routes run on the embedded runtime instead.
	warning string
}

// loadScriptcServePlan reads the manifest of a build made with
// ssr.apiRuntime "scriptc". Without a usable record every route stays on the
// embedded runtime.
func loadScriptcServePlan(root string) scriptcServePlan {
	m, ok := loadScriptcManifest(root)
	if !ok {
		return scriptcServePlan{warning: "ssr.apiRuntime is \"scriptc\" but this build has no .krate/api-scriptc.json: API routes run on the embedded runtime; run `krate build` again"}
	}
	plan := scriptcServePlan{routes: map[string]bool{}}
	for _, r := range m.Routes {
		if r.Runtime == scriptcRouteScriptc {
			plan.routes[r.File] = true
		}
	}
	if len(plan.routes) == 0 {
		return plan
	}
	exe := scriptcServerPath(root)
	if _, err := os.Stat(exe); err != nil || m.Executable == "" {
		return scriptcServePlan{warning: "the scriptc API server " + exe + " is missing: its routes run on the embedded runtime; run `krate build` again"}
	}
	plan.exe = exe
	return plan
}

var scriptcDevNote sync.Once

// scriptcDevNotice says once that dev serves the routes on QuickJS.
func scriptcDevNotice() {
	scriptcDevNote.Do(func() {
		printf("  %s*%s ssr.apiRuntime \"scriptc\" compiles API routes in krate build; krate dev runs them on the embedded QuickJS runtime\n", cCyan, cReset)
	})
}
