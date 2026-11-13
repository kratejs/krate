package bridge

import (
	"fmt"
	"sort"
	"sync"
	"time"

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
	// Scope, when set, is called by window.Open for every window it opens,
	// with the calling window, the new window id and the requested
	// permission list (has reports whether one was given). It reports whether
	// the new window is scoped; an error refuses the window.
	Scope func(caller, window string, perms []string, has bool) (bool, error)
	// FileDrop is the handler the window area registered for dropped files; it
	// is what a platform's own drop handler calls. The dev control channel and
	// tests use it to inject a drop.
	FileDrop func(caps.FileDrop)
	// Unscope, when set, forgets the scope of a window or view that went away.
	Unscope func(id string)
	// Allows, when set, reports whether the window or view id has the
	// permission in its scope. The media area uses it to deny a page's
	// camera, microphone and location requests. Unset means nothing is
	// allowed.
	Allows func(window, permission string) bool
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

	// Views: every webview of every window, by SDK id and by platform ref.
	// The primary view of a window has the window's id, so the id of a view
	// is also the key of its permission scope.
	views    map[string]*ViewRec
	viewRefs map[caps.ViewRef]*ViewRec
	nextView int
	seq      int
	// claimed holds ids handed out by Reserve, ReserveView or Claim whose
	// window or view is not registered yet. An id is a permission scope key,
	// so it may have only one holder from the moment it is chosen.
	claimed map[string]bool
	changed chan struct{} // closed (and replaced) when a view is registered
	held    []caps.ViewEvent
	onView  func(rec ViewRec, held []caps.ViewEvent)
	// created and closed are called (outside the lock) after a window is
	// added or removed.
	created, closed []func(id string)
}

// ViewRec is one registered view.
type ViewRec struct {
	ID      string
	Ref     caps.ViewRef
	Window  string
	Primary bool
	seq     int
}

// Subscribe adds callbacks for windows being added and removed. Either may be nil.
func (w *Windows) Subscribe(created, closed func(id string)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if created != nil {
		w.created = append(w.created, created)
	}
	if closed != nil {
		w.closed = append(w.closed, closed)
	}
}

// NewWindows returns an empty registry.
func NewWindows() *Windows {
	return &Windows{byID: map[string]platform.Window{}, byRef: map[caps.WindowRef]string{},
		views: map[string]*ViewRec{}, viewRefs: map[caps.ViewRef]*ViewRec{}, changed: make(chan struct{})}
}

// ReserveView returns an unused view id "v<N>" and claims it (see Claim).
func (w *Windows) ReserveView() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	for {
		w.nextView++
		id := fmt.Sprintf("v%d", w.nextView)
		if !w.takenLocked(id) {
			w.claimLocked(id)
			return id
		}
	}
}

// IDTaken reports whether id names a window or a view, or is claimed for one.
func (w *Windows) IDTaken(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.takenLocked(id)
}

// Claim takes id for a window or view that is about to be created, and
// reports false when a window, a view or another claim holds it. Check and
// claim are one step: two calls racing for one id cannot both win, so the
// loser cannot undo the permission scope the winner set. A claim ends when
// the id is registered (Add, AddView) or released (Release).
func (w *Windows) Claim(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.takenLocked(id) {
		return false
	}
	w.claimLocked(id)
	return true
}

// Release gives up a claim whose window or view was not created.
func (w *Windows) Release(id string) {
	w.mu.Lock()
	delete(w.claimed, id)
	w.mu.Unlock()
}

func (w *Windows) takenLocked(id string) bool {
	_, v := w.views[id]
	_, win := w.byID[id]
	return v || win || w.claimed[id]
}

func (w *Windows) claimLocked(id string) {
	if w.claimed == nil {
		w.claimed = map[string]bool{}
	}
	w.claimed[id] = true
}

// AddView registers a view of a window under id.
func (w *Windows) AddView(id, window string, ref caps.ViewRef, primary bool) error {
	w.mu.Lock()
	rec, held, err := w.addViewLocked(id, window, ref, primary)
	fn := w.onView
	w.mu.Unlock()
	if err == nil && fn != nil {
		fn(rec, held)
	}
	return err
}

// addViewLocked also takes the events held for the view's ref.
func (w *Windows) addViewLocked(id, window string, ref caps.ViewRef, primary bool) (ViewRec, []caps.ViewEvent, error) {
	if _, dup := w.views[id]; dup {
		return ViewRec{}, nil, fmt.Errorf("view %q already exists", id)
	}
	delete(w.claimed, id)
	w.seq++
	r := &ViewRec{ID: id, Ref: ref, Window: window, Primary: primary, seq: w.seq}
	w.views[id] = r
	w.viewRefs[ref] = r
	close(w.changed)
	w.changed = make(chan struct{})
	var held, rest []caps.ViewEvent
	for _, e := range w.held {
		if e.View == ref {
			held = append(held, e)
		} else {
			rest = append(rest, e)
		}
	}
	w.held = rest
	return *r, held, nil
}

// OnView sets the function called when a view is registered, with the events
// that were held for it (see Hold).
func (w *Windows) OnView(fn func(rec ViewRec, held []caps.ViewEvent)) {
	w.mu.Lock()
	w.onView = fn
	w.mu.Unlock()
}

// Hold keeps an event of a view that is not registered yet, to be delivered
// when it is. The platform reports events as soon as a view exists, which can
// be before the code that created it has registered it.
func (w *Windows) Hold(e caps.ViewEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.viewRefs[e.View]; ok {
		return
	}
	w.held = append(w.held, e)
	if len(w.held) > 64 {
		w.held = w.held[len(w.held)-64:]
	}
}

// RemoveView forgets a view.
func (w *Windows) RemoveView(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if r, ok := w.views[id]; ok {
		delete(w.viewRefs, r.Ref)
		delete(w.views, id)
	}
}

// View returns the view with this id.
func (w *Windows) View(id string) (ViewRec, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if r, ok := w.views[id]; ok {
		return *r, true
	}
	return ViewRec{}, false
}

// ViewByRef returns the view for a platform reference.
func (w *Windows) ViewByRef(ref caps.ViewRef) (ViewRec, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if r, ok := w.viewRefs[ref]; ok {
		return *r, true
	}
	return ViewRec{}, false
}

// ViewsOf lists the views of a window ("" = all windows) in creation order.
func (w *Windows) ViewsOf(window string) []ViewRec {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []ViewRec
	for _, r := range w.views {
		if window == "" || r.Window == window {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out
}

// WaitView returns the view for ref, waiting up to d for it to be registered:
// a page can send its first request before the code that created its view has
// returned.
func (w *Windows) WaitView(ref caps.ViewRef, d time.Duration) (ViewRec, bool) {
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	for {
		w.mu.Lock()
		r, ok := w.viewRefs[ref]
		ch := w.changed
		var rec ViewRec
		if ok {
			rec = *r
		}
		w.mu.Unlock()
		if ok {
			return rec, true
		}
		select {
		case <-ch:
		case <-deadline.C:
			return ViewRec{}, false
		}
	}
}

// Reserve returns the id the next window with an empty id would get, and
// claims it (see Claim).
func (w *Windows) Reserve() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	id := w.nextWindowLocked()
	w.claimLocked(id)
	return id
}

// nextWindowLocked returns an unused window id "w<N>". A page may name its
// views (window.addView with an id), so an id is skipped when a view, a window
// or a claim already holds it: two holders would share one permission scope,
// and closing the window would drop the scope the view still relies on.
func (w *Windows) nextWindowLocked() string {
	for {
		w.next++
		id := fmt.Sprintf("w%d", w.next)
		if !w.takenLocked(id) {
			return id
		}
	}
}

// Add registers w under id; an empty id gets "w<N>". The first window added
// is the main window.
func (w *Windows) Add(id string, win platform.Window) string {
	id, hooks := w.add(id, win)
	for _, f := range hooks {
		f(id)
	}
	return id
}

func (w *Windows) add(id string, win platform.Window) (string, []func(string)) {
	w.mu.Lock()
	if id == "" {
		id = w.nextWindowLocked()
	}
	delete(w.claimed, id)
	w.byID[id] = win
	if c, ok := win.(caps.WindowControls); ok {
		w.byRef[c.Ref()] = id
	}
	if w.main == "" {
		w.main = id
	}
	var rec ViewRec
	var held []caps.ViewEvent
	if v, ok := win.(caps.WindowViews); ok {
		rec, held, _ = w.addViewLocked(id, id, v.PrimaryView(), true)
	}
	fn := w.onView
	hooks := append([]func(string){}, w.created...)
	w.mu.Unlock()
	if rec.ID != "" && fn != nil {
		fn(rec, held)
	}
	return id, hooks
}

// Remove forgets a window.
func (w *Windows) Remove(id string) {
	hooks := w.remove(id)
	for _, f := range hooks {
		f(id)
	}
}

func (w *Windows) remove(id string) []func(string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, known := w.byID[id]
	if win, ok := w.byID[id]; ok {
		if c, ok := win.(caps.WindowControls); ok {
			delete(w.byRef, c.Ref())
		}
	}
	delete(w.byID, id)
	for vid, r := range w.views {
		if r.Window == id {
			delete(w.viewRefs, r.Ref)
			delete(w.views, vid)
		}
	}
	if w.main == id {
		w.main = ""
		for k := range w.byID {
			w.main = k
			break
		}
	}
	if !known {
		return nil
	}
	return append([]func(string){}, w.closed...)
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

// InjectKey is the Providers key under which an area leaves its test-event
// injector (a func(payload []byte) error); see sdk.Host.Inject.
func InjectKey(area string) string { return "inject." + area }
