package bridge

import (
	"fmt"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Env is what the platform-side operations of every SDK area share.
type Env struct {
	App     platform.App
	Windows *Windows

	mu     sync.Mutex
	topics map[string]*Topic
	// Providers fill in capabilities the App itself lacks, for example the
	// D-Bus notification and tray backends on Linux. Keys: "notify", "tray",
	// "shortcut".
	Providers map[string]any
}

// NewEnv returns an Env for app.
func NewEnv(app platform.App) *Env {
	return &Env{App: app, Windows: NewWindows(), topics: map[string]*Topic{}, Providers: map[string]any{}}
}

// Topic returns the named event topic, creating it on first use.
func (e *Env) Topic(name string) *Topic {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.topics[name]
	if t == nil {
		t = &Topic{}
		e.topics[name] = t
	}
	return t
}

// Windows maps SDK window ids to platform windows.
type Windows struct {
	mu    sync.Mutex
	byID  map[string]platform.Window
	byRef map[caps.WindowRef]string
	next  int
	main  string
}

// NewWindows returns an empty registry.
func NewWindows() *Windows {
	return &Windows{byID: map[string]platform.Window{}, byRef: map[caps.WindowRef]string{}}
}

// Add registers w under id; an empty id gets "w<N>". The first window added
// is the main window.
func (w *Windows) Add(id string, win platform.Window) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if id == "" {
		w.next++
		id = fmt.Sprintf("w%d", w.next)
	}
	w.byID[id] = win
	if c, ok := win.(caps.WindowControls); ok {
		w.byRef[c.Ref()] = id
	}
	if w.main == "" {
		w.main = id
	}
	return id
}

// Remove forgets a window.
func (w *Windows) Remove(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if win, ok := w.byID[id]; ok {
		if c, ok := win.(caps.WindowControls); ok {
			delete(w.byRef, c.Ref())
		}
	}
	delete(w.byID, id)
	if w.main == id {
		w.main = ""
		for k := range w.byID {
			w.main = k
			break
		}
	}
}

// Get returns the window for id; "" means the main window.
func (w *Windows) Get(id string) (platform.Window, string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if id == "" {
		id = w.main
	}
	win, ok := w.byID[id]
	return win, id, ok
}

// ID returns the id for a platform window reference, or "".
func (w *Windows) ID(ref caps.WindowRef) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.byRef[ref]
}

// Ref returns the reference for id ("" = main), or 0.
func (w *Windows) Ref(id string) caps.WindowRef {
	win, _, ok := w.Get(id)
	if !ok {
		return 0
	}
	if c, ok := win.(caps.WindowControls); ok {
		return c.Ref()
	}
	return 0
}

// List returns all ids.
func (w *Windows) List() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, 0, len(w.byID))
	for id := range w.byID {
		out = append(out, id)
	}
	return out
}

// Cap returns app as T (one of the capability interfaces), or
// krate.ErrUnsupported with a message naming the missing capability.
func Cap[T any](app platform.App, what string) (T, error) {
	v, ok := app.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("%w: %s is not available on this platform", krate.ErrUnsupported, what)
	}
	return v, nil
}

// Controls returns the window controls of the window with this SDK id
// ("" = main).
func (e *Env) Controls(id string) (caps.WindowControls, platform.Window, string, error) {
	win, wid, ok := e.Windows.Get(id)
	if !ok {
		return nil, nil, "", krate.Errorf("not_found", "no window %q", id)
	}
	c, ok := win.(caps.WindowControls)
	if !ok {
		return nil, win, wid, fmt.Errorf("%w: window controls are not available on this platform", krate.ErrUnsupported)
	}
	return c, win, wid, nil
}
