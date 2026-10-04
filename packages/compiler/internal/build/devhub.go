package build

import (
	"sync"

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
}

// NewDevHub creates an empty hub.
func NewDevHub() *DevHub {
	return &DevHub{subs: make(map[chan DevEvent]struct{})}
}

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
	h.mu.Lock()
	defer h.mu.Unlock()
	ev.BuildOK = len(ev.Diagnostics) == 0
	h.last = &ev
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
