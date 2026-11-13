package shell

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

const (
	devEventsPath      = "/__krate/dev/events"
	devClientErrorPath = "/__krate/dev/client-error"
	eventRingSize      = 500
	hmrKeepAlive       = 25 * time.Second
	maxClientError     = 64 * 1024
)

// devTools is the dev-only part of the shell: the HMR fan-out the driving
// process feeds through the control channel, and the host traffic recorder.
type devTools struct {
	logf func(string, ...any)
	hmr  *hmrHub
	ring *eventRing
	// memory serves devMemoryPath; the app sets it.
	memory http.HandlerFunc
	// identify names the window or view a request came from.
	identify func(*http.Request) (string, bool)
}

func newDevTools(logf func(string, ...any)) *devTools {
	return &devTools{logf: logf, hmr: newHMRHub(), ring: newEventRing(eventRingSize)}
}

// handle serves the dev endpoints that live outside the scheme package. It
// reports whether it handled r.
func (d *devTools) handle(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case devEventsPath:
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return true
		}
		d.ring.serve(w, r)
		return true
	case devMemoryPath:
		if d.memory == nil {
			return false
		}
		d.memory(w, r)
		return true
	case devClientErrorPath:
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return true
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, maxClientError))
		var ce clientError
		if json.Unmarshal(body, &ce) == nil && ce.Message != "" {
			win := ce.Window
			if d.identify != nil {
				// The shell's own view-to-window mapping beats the id the
				// page claims.
				if id, ok := d.identify(r); ok && id != unknownView {
					win = id
				}
			}
			for _, line := range ce.logLines(win) {
				d.logf("%s", line)
			}
		}
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

// hmrFrame encodes {"hmr": msg}. msg must be one JSON value.
func hmrFrame(msg []byte) ([]byte, error) {
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("hmr")
	e.Raw(msg)
	e.EndObject()
	return e.Finish()
}

// hmrHub fans the driver's HMR messages out to every open /__krate/hmr
// stream. It keeps the latest build-error so a window opened (or reloaded)
// after a failed build still shows it.
type hmrHub struct {
	mu      sync.Mutex
	subs    map[chan []byte]struct{}
	lastErr []byte // frame of the current build-error, nil when the build is fine
}

func newHMRHub() *hmrHub { return &hmrHub{subs: map[chan []byte]struct{}{}} }

// publish validates msg (a JSON object), frames it and delivers it. A slow
// subscriber drops frames rather than blocking the build.
func (h *hmrHub) publish(msg []byte) error {
	var compact bytes.Buffer
	if err := json.Compact(&compact, bytes.TrimSpace(msg)); err != nil {
		return fmt.Errorf("hmr message is not JSON: %w", err)
	}
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(compact.Bytes(), &probe); err != nil {
		return fmt.Errorf("hmr message must be a JSON object: %w", err)
	}
	fr, err := hmrFrame(compact.Bytes())
	if err != nil {
		return err
	}
	h.mu.Lock()
	switch probe.Type {
	case "build-error":
		h.lastErr = fr
	case "reload":
		h.lastErr = nil
	}
	for ch := range h.subs {
		select {
		case ch <- fr:
		default:
		}
	}
	h.mu.Unlock()
	return nil
}

func (h *hmrHub) subscribe() (chan []byte, []byte, func()) {
	ch := make(chan []byte, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	replay := h.lastErr
	h.mu.Unlock()
	return ch, replay, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

// serve is the GET /__krate/hmr handler.
func (h *hmrHub) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-krate-frames")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	flush := func() {
		if fl != nil {
			fl.Flush()
		}
	}
	ch, replay, cancel := h.subscribe()
	defer cancel()
	write := func(fr []byte) bool {
		if _, err := w.Write(fr); err != nil {
			return false
		}
		flush()
		return true
	}
	hello, _ := hmrFrame([]byte(`{"type":"connected"}`))
	if !write(hello) {
		return
	}
	if replay != nil && !write(replay) {
		return
	}
	tick := time.NewTicker(hmrKeepAlive)
	defer tick.Stop()
	ping, _ := hmrFrame([]byte(`{"type":"ping"}`))
	for {
		select {
		case fr := <-ch:
			if !write(fr) {
				return
			}
		case <-tick.C:
			if !write(ping) {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// loggedEvent is one recorded host event with its sequence number.
type loggedEvent struct {
	Seq int64 `json:"seq"`
	scheme.Event
}

// eventRing keeps the newest host events.
type eventRing struct {
	mu    sync.Mutex
	buf   []loggedEvent
	next  int64 // sequence of the next event, starting at 1
	subs  map[chan struct{}]struct{}
	limit int
}

func newEventRing(limit int) *eventRing {
	return &eventRing{limit: limit, next: 1, subs: map[chan struct{}]struct{}{}}
}

func (r *eventRing) add(ev scheme.Event) {
	r.mu.Lock()
	r.buf = append(r.buf, loggedEvent{Seq: r.next, Event: ev})
	r.next++
	if len(r.buf) > r.limit {
		r.buf = append([]loggedEvent(nil), r.buf[len(r.buf)-r.limit:]...)
	}
	for ch := range r.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	r.mu.Unlock()
}

// since returns the events with a sequence above seq.
func (r *eventRing) since(seq int64) []loggedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []loggedEvent
	for _, ev := range r.buf {
		if ev.Seq > seq {
			out = append(out, ev)
		}
	}
	return out
}

func eventFrame(ev loggedEvent) ([]byte, error) {
	raw, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("ev")
	e.Raw(raw)
	e.EndObject()
	return e.Finish()
}

// serve answers GET /__krate/dev/events?since=<seq>&follow=1: one frame
// {"ev": {...}} per recorded event newer than since, then, with follow, every
// later event until the client goes away.
func (r *eventRing) serve(w http.ResponseWriter, req *http.Request) {
	since, _ := strconv.ParseInt(req.URL.Query().Get("since"), 10, 64)
	follow := req.URL.Query().Get("follow") == "1"
	w.Header().Set("Content-Type", "application/x-krate-frames")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	send := func() bool {
		for _, ev := range r.since(since) {
			fr, err := eventFrame(ev)
			if err != nil {
				continue
			}
			if _, err := w.Write(fr); err != nil {
				return false
			}
			since = ev.Seq
		}
		if fl != nil {
			fl.Flush()
		}
		return true
	}
	if !follow {
		send()
		return
	}
	wake := make(chan struct{}, 1)
	r.mu.Lock()
	r.subs[wake] = struct{}{}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.subs, wake)
		r.mu.Unlock()
	}()
	for {
		if !send() {
			return
		}
		select {
		case <-wake:
		case <-req.Context().Done():
			return
		}
	}
}

// clientError is what the dev overlay posts when the page threw, rejected a
// promise, logged console.error or failed to load a script.
type clientError struct {
	Message string `json:"message"`
	Stack   string `json:"stack"`
	URL     string `json:"url"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Kind    string `json:"kind"`
	Window  string `json:"window"`
}

// maxStackLines caps the stack lines printed for one error.
const maxStackLines = 12

// logLines renders the error for the terminal: one tagged line with the
// window or view id and the message, then the stack. win falls back to "main"
// (the first window carries no id of its own).
func (ce clientError) logLines(win string) []string {
	if win == "" {
		win = "main"
	}
	msg := ce.Message
	switch ce.Kind {
	case "unhandledrejection":
		msg = "unhandled rejection: " + msg
	case "console":
		msg = "console.error: " + msg
	}
	head := strings.SplitN(msg, "\n", 2)[0]
	loc := ce.URL
	if ce.Line > 0 {
		loc = fmt.Sprintf("%s:%d:%d", ce.URL, ce.Line, ce.Col)
	}
	first := "[webview] " + win + ": " + head
	if loc != "" && strings.TrimSpace(ce.Stack) == "" {
		first += " (" + loc + ")"
	}
	out := []string{first}
	n := 0
	for _, l := range strings.Split(strings.TrimSpace(ce.Stack), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || l == head {
			continue
		}
		if n == maxStackLines {
			out = append(out, "[webview]     ...")
			break
		}
		out = append(out, "[webview]     "+l)
		n++
	}
	return out
}
