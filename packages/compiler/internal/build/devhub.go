package build

import (
	"sort"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// DevEvent is a single dev-server notification broadcast to every connected
// browser tab over SSE. Type selects the SSE event name: "reload",
// "build-error", or "client-error".
type DevEvent struct {
	Type        string            `json:"type"`
	Routes      []string          `json:"routes,omitempty"`
	Diagnostics []diag.Diagnostic `json:"diagnostics,omitempty"`
	BuildOK     bool              `json:"buildOk"`
	ClientError *ClientError      `json:"clientError,omitempty"`
	Page        string            `json:"page,omitempty"`
	// CSSOnly marks a rebuild caused solely by stylesheet edits, so the
	// browser can swap stylesheets in place instead of reloading.
	CSSOnly bool `json:"cssOnly,omitempty"`
}

// ClientError is a runtime error reported by a browser client via
// POST /__krate/client-error. It is surfaced in the terminal and echoed to all
// connected tabs so multi-tab sessions stay in sync.
type ClientError struct {
	Message string `json:"message"`
	Stack   string `json:"stack,omitempty"`
	URL     string `json:"url,omitempty"`
	Line    int    `json:"line,omitempty"`
	Col     int    `json:"col,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

// DevHub fans dev events out to all connected SSE clients. The previous
// single-consumer channel delivered each event to only one browser tab; the hub
// broadcasts to every subscriber and retains the latest build state so a newly
// connected tab is caught up immediately (important when the initial build
// failed before any browser connected).
type DevHub struct {
	mu   sync.Mutex
	subs map[chan DevEvent]struct{}
	last *DevEvent

	// hook runs once per published rebuild event, before the fan-out, so
	// server-side state (the SSR renderer cache) is invalidated exactly once
	// whether zero, one or many tabs are connected.
	hookMu sync.Mutex
	hook   func(DevEvent)

	restartOnce sync.Once
	restart     chan struct{}

	// clientErrs keeps the most recent browser errors and lastEventAt the time
	// of the latest build event, for the /__krate/state endpoint (MCP).
	clientErrs  []ClientErrorEntry
	lastEventAt time.Time
	rebuilds    int

	// serverErrs are failed server-side parts (sidecars, middleware) that have
	// not recovered yet, by source, so a tab opened later still shows them.
	serverErrs    map[string]ClientError
	lastServerErr map[string]time.Time
}

// NewDevHub creates an empty hub.
func NewDevHub() *DevHub {
	return &DevHub{subs: make(map[chan DevEvent]struct{}), restart: make(chan struct{})}
}

// SetReloadHook installs fn to run once for every "reload" event published.
func (h *DevHub) SetReloadHook(fn func(DevEvent)) {
	h.hookMu.Lock()
	h.hook = fn
	h.hookMu.Unlock()
}

// RequestRestart asks the running dev server to shut down so the caller can
// start a fresh one (config or env changed). Safe to call more than once.
func (h *DevHub) RequestRestart() {
	h.restartOnce.Do(func() { close(h.restart) })
}

// RestartRequested is closed once RequestRestart has been called.
func (h *DevHub) RestartRequested() <-chan struct{} { return h.restart }

// Subscribe registers a listener. Every Publish is delivered to the returned
// channel (dropped if the listener is too slow). Call cancel to unsubscribe.
func (h *DevHub) Subscribe() (<-chan DevEvent, func()) {
	ch := make(chan DevEvent, 32)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	cancel := func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
	return ch, cancel
}

// Publish broadcasts an event to every subscriber and records it as the current
// build state. BuildOK is derived from the diagnostics so callers cannot forget
// to set it.
func (h *DevHub) Publish(ev DevEvent) {
	if ev.Type == "reload" {
		h.hookMu.Lock()
		hook := h.hook
		h.hookMu.Unlock()
		if hook != nil {
			hook(ev)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if ev.Type == "client-error" && ev.ClientError != nil {
		h.clientErrs = append(h.clientErrs, ClientErrorEntry{ClientError: *ev.ClientError, Time: time.Now()})
		if len(h.clientErrs) > maxClientErrors {
			h.clientErrs = append([]ClientErrorEntry(nil), h.clientErrs[len(h.clientErrs)-maxClientErrors:]...)
		}
		for ch := range h.subs {
			select {
			case ch <- ev:
			default:
			}
		}
		return
	}
	ev.BuildOK = len(ev.Diagnostics) == 0
	h.last = &ev
	h.lastEventAt = time.Now()
	h.rebuilds++
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Current returns the most recently published event, if any. Used to catch up
// newly connected tabs.
func (h *DevHub) Current() (DevEvent, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.last == nil {
		return DevEvent{}, false
	}
	return *h.last, true
}

// PublishServerError shows a failed server-side part in every tab and keeps it
// until ClearServerError(source). The same report within a few seconds is
// ignored, so a request that fails over and over does not flood the overlay.
func (h *DevHub) PublishServerError(source string, ce ClientError) {
	h.mu.Lock()
	if h.serverErrs == nil {
		h.serverErrs = map[string]ClientError{}
		h.lastServerErr = map[string]time.Time{}
	}
	prev, seen := h.serverErrs[source]
	repeat := seen && prev.Message == ce.Message && time.Since(h.lastServerErr[source]) < crashRepeatWindow
	h.serverErrs[source] = ce
	h.lastServerErr[source] = time.Now()
	h.mu.Unlock()
	if !repeat {
		h.Publish(DevEvent{Type: "client-error", ClientError: &ce})
	}
}

// ClearServerError forgets the error a source reported.
func (h *DevHub) ClearServerError(source string) {
	h.mu.Lock()
	delete(h.serverErrs, source)
	delete(h.lastServerErr, source)
	h.mu.Unlock()
}

// ServerErrors returns the server-side failures that have not recovered, in a
// stable order.
func (h *DevHub) ServerErrors() []ClientError {
	h.mu.Lock()
	defer h.mu.Unlock()
	keys := make([]string, 0, len(h.serverErrs))
	for k := range h.serverErrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]ClientError, 0, len(keys))
	for _, k := range keys {
		out = append(out, h.serverErrs[k])
	}
	return out
}
