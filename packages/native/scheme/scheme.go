package scheme

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
)

// DefaultCSP is the policy used when Options.CSP is empty. Prerendered pages
// carry inline module scripts and inline styles, so script-src and style-src
// allow 'unsafe-inline' for now; everything else is limited to the app origin.
// Tightening this needs per-page hashes from the compiler.
const DefaultCSP = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"connect-src 'self'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self' data:; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// Options configures the handler returned by New.
type Options struct {
	// Assets is the built dist.
	Assets fs.FS
	// Registry holds the generated wrappers. Nil means an empty registry.
	Registry *Registry
	// Dev puts panic stacks in error messages and enables the HMR endpoint.
	Dev bool
	// CSP overrides DefaultCSP.
	CSP string
	// Fallback maps a URL path with no file to a dynamic route shell, for
	// example /tasks/123 to tasks/[id]/index.html.
	Fallback func(urlPath string) (file string, ok bool)
	// HMR serves GET /__krate/hmr when Dev is set.
	HMR func(w http.ResponseWriter, r *http.Request)
	// Observe, when set, receives host traffic events (calls, streams,
	// signals) for the dev inspector. It runs on the serving goroutines, so
	// it must be quick and safe for concurrent use.
	Observe func(Event)
	// Identify, when set, tells which window a request really came from.
	// The platform layer attaches the originating webview to every request
	// it serves (see caps.ViewOf) and the shell maps it to a window id. When
	// it reports native, the page's own x-krate-window header is ignored:
	// page script can set that header to anything. Without Identify, or when
	// it reports native=false (browser mode has no native views), the header
	// is the identity.
	Identify func(r *http.Request) (window string, native bool)
}

// windowOf returns the window a request belongs to: the native identity when
// there is one, otherwise what the page claims.
func (s *server) windowOf(r *http.Request) string {
	if s.o.Identify != nil {
		if w, native := s.o.Identify(r); native {
			return w
		}
	}
	return r.Header.Get(windowHdr)
}

// Event describes one piece of host traffic.
type Event struct {
	// Kind is "call", "stream-open", "stream-end", "signal-open" or "signal".
	Kind string `json:"kind"`
	// ID is the call id (calls and stream-open), 0 otherwise.
	ID int64 `json:"id,omitempty"`
	// Fn is the function, stream or signal name.
	Fn     string    `json:"fn"`
	Window string    `json:"window,omitempty"`
	Time   time.Time `json:"time"`
	// Dur is how long a call ran, or how long a stream was open.
	Dur time.Duration `json:"durNs,omitempty"`
	// Code and Message describe the error, if any.
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func (s *server) observe(ev Event) {
	if s.o.Observe == nil {
		return
	}
	ev.Time = time.Now()
	s.o.Observe(ev)
}

// errCodeMsg returns the code and message encodeError would send, without the
// dev stack.
func errCodeMsg(err error) (code, msg string) {
	var pe *panicError
	var ke *krate.Error
	switch {
	case errors.As(err, &pe):
		return "panic", fmt.Sprint(pe.val)
	case errors.As(err, &ke):
		return ke.Code, ke.Message
	case errors.Is(err, context.Canceled):
		return "canceled", err.Error()
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded", err.Error()
	}
	return "internal", err.Error()
}

const (
	pathPrefix  = "/__krate/"
	framesType  = "application/x-krate-frames"
	windowHdr   = "x-krate-window"
	streamTTL   = 30 * time.Second
	streamPath  = pathPrefix + "stream/"
	emptyArgs   = "\x00\x00\x00\x02[]\x00\x00"
	maxSigNames = 256
)

type callKey struct {
	window string
	id     int64
}

type server struct {
	o      Options
	reg    *Registry
	csp    string
	expiry time.Duration
	sem    chan struct{}

	mu      sync.Mutex
	calls   map[callKey]context.CancelFunc
	streams map[string]*stream
	nextID  atomic.Int64
}

// New returns the handler for the app origin.
func New(o Options) http.Handler {
	return newServer(o, streamTTL)
}

func newServer(o Options, expiry time.Duration) *server {
	if o.Registry == nil {
		o.Registry = NewRegistry()
	}
	csp := o.CSP
	if csp == "" {
		csp = DefaultCSP
	}
	return &server{
		o:       o,
		reg:     o.Registry,
		csp:     csp,
		expiry:  expiry,
		sem:     make(chan struct{}, runtime.GOMAXPROCS(0)*4),
		calls:   make(map[callKey]context.CancelFunc),
		streams: make(map[string]*stream),
	}
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", s.csp)
	h.Set("X-Content-Type-Options", "nosniff")

	p := r.URL.Path
	switch {
	case p == pathPrefix+"call":
		s.method(w, r, http.MethodPost, s.handleCall)
	case p == pathPrefix+"cancel":
		s.method(w, r, http.MethodPost, s.handleCancel)
	case strings.HasPrefix(p, streamPath):
		s.method(w, r, http.MethodGet, s.handleStream)
	case p == pathPrefix+"signals":
		s.method(w, r, http.MethodGet, s.handleSignals)
	case p == pathPrefix+"hmr":
		if !s.o.Dev || s.o.HMR == nil {
			http.NotFound(w, r)
			return
		}
		s.method(w, r, http.MethodGet, s.o.HMR)
	case strings.HasPrefix(p, pathPrefix):
		http.NotFound(w, r)
	default:
		s.handleStatic(w, r)
	}
}

func (s *server) method(w http.ResponseWriter, r *http.Request, want string, h http.HandlerFunc) {
	if r.Method != want {
		w.Header().Set("Allow", want)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h(w, r)
}
