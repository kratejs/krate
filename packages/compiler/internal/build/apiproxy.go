package build

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// errSidecarFallthrough signals that the API sidecar returned 404 (or was
// unreachable) and the request should be handled by Krate's built-in API
// handlers instead.
var errSidecarFallthrough = errors.New("api sidecar: fall through")

// apiSidecar is a user-provided HTTP service that owns some/all API routes.
//
// Two modes:
//   - supervised: SidecarConfig.Command is set; Krate starts the process (with
//     Cwd/Env/Args) and restarts it if it exits.
//   - proxy-only: only Target/Port is set; the user runs the process themselves.
//
// Requests under the configured Prefix (default "/api") are always forwarded
// first. A 404 (or an unreachable sidecar) falls through to Krate's own Go/TS/
// QuickJS API routes, so the sidecar can implement exactly the routes it wants
// — including dynamic segments like `/users/[id]` — with no route declarations.
type apiSidecar struct {
	config *config.SidecarConfig
	prefix string
	root   string

	proxy *httputil.ReverseProxy

	mu        sync.Mutex
	cmd       *exec.Cmd
	stopOnce  sync.Once
	stopCh    chan struct{}
	startedAt time.Time
}

// sidecarPrefix normalizes the configured prefix (default "/api", no trailing
// slash).
func sidecarPrefix(sc *config.SidecarConfig) string {
	p := strings.TrimSpace(sc.Prefix)
	if p == "" {
		return "/api"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimRight(p, "/")
}

// newAPISidecar builds a proxy for the configured sidecar. Returns nil when no
// sidecar is configured.
func newAPISidecar(root string, cfg *config.Config) (*apiSidecar, error) {
	sc := cfg.API.Sidecar
	if sc == nil {
		return nil, nil
	}

	rawTarget := sc.Target
	if rawTarget == "" {
		port := sc.Port
		if port == 0 {
			return nil, fmt.Errorf("api.sidecar: a port or target is required")
		}
		rawTarget = fmt.Sprintf("http://127.0.0.1:%d", port)
	}
	target, err := url.Parse(rawTarget)
	if err != nil {
		return nil, fmt.Errorf("api.sidecar target %q: %w", rawTarget, err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	// Stream, don't buffer, and cap how long we wait for response headers so a
	// hung sidecar cannot pin a request forever.
	proxy.Transport = newSidecarTransport()
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if errors.Is(err, errSidecarFallthrough) {
			return // caller lets the built-in handlers try
		}
		// Unreachable sidecar: also fall through (built-ins or a clean 404).
	}
	proxy.ModifyResponse = func(res *http.Response) error {
		if res.StatusCode == http.StatusNotFound {
			return errSidecarFallthrough
		}
		return nil
	}
	proxy.FlushInterval = 100 * time.Millisecond

	// Preserve the original path; add forwarded headers.
	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		origDirector(req)
		req.Host = target.Host
		if req.Header.Get("X-Forwarded-Host") == "" {
			req.Header.Set("X-Forwarded-Host", req.URL.Host)
		}
	}

	return &apiSidecar{
		config: sc,
		prefix: sidecarPrefix(sc),
		root:   root,
		proxy:  proxy,
	}, nil
}

// Prefix returns the normalized path prefix the sidecar owns.
func (s *apiSidecar) Prefix() string { return s.prefix }

// Matches reports whether the request path is under the sidecar's prefix.
func (s *apiSidecar) Matches(path string) bool {
	return path == s.prefix || strings.HasPrefix(path, s.prefix+"/")
}

// Start launches the supervised process (if any). Returns immediately; the
// process is restarted on unexpected exit until Stop.
func (s *apiSidecar) Start() error {
	if s.config.Command == "" {
		return nil
	}
	s.stopCh = make(chan struct{})
	s.startedAt = time.Now()
	go s.supervise()
	return nil
}

func (s *apiSidecar) supervise() {
	for {
		cmd := s.newCmd()
		s.mu.Lock()
		s.cmd = cmd
		s.mu.Unlock()

		err := cmd.Run()

		select {
		case <-s.stopCh:
			return
		default:
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s⚠ API sidecar exited:%s %v (restarting in 1s)\n", cYellow, cReset, err)
		}
		time.Sleep(time.Second)
	}
}

func (s *apiSidecar) newCmd() *exec.Cmd {
	args := append([]string{}, s.config.Args...)
	cmd := exec.Command(s.config.Command, args...)
	cwd := s.config.Cwd
	if cwd == "" {
		cwd = s.root
	}
	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(s.root, cwd)
	}
	cmd.Dir = cwd

	env := os.Environ()
	if s.config.Port != 0 {
		env = append(env, fmt.Sprintf("PORT=%d", s.config.Port), fmt.Sprintf("KRATE_API_SIDECAR_PORT=%d", s.config.Port))
	}
	for k, v := range s.config.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// Stop terminates the supervised process (no-op in proxy-only mode).
func (s *apiSidecar) Stop() {
	if s.config.Command == "" {
		return
	}
	s.stopOnce.Do(func() {
		if s.stopCh != nil {
			close(s.stopCh)
		}
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

// statusWriter tracks whether the wrapped ResponseWriter had any status/body
// written, so Handle can tell "sidecar handled it" from "sidecar 404'd/unreachable".
type statusWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *statusWriter) WriteHeader(code int) {
	w.wrote = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Handle forwards the request to the sidecar. It returns true when the sidecar
// produced a response; false when it returned 404 / was unreachable, in which
// case the caller should fall through to the built-in API handlers.
func (s *apiSidecar) Handle(w http.ResponseWriter, r *http.Request) bool {
	sw := &statusWriter{ResponseWriter: w}
	s.proxy.ServeHTTP(sw, r)
	return sw.wrote
}
