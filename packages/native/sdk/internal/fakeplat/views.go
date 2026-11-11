package fakeplat

import (
	"errors"
	"fmt"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.WindowViews = (*Window)(nil)
	_ caps.ViewEvents  = (*App)(nil)
	_ caps.ViewLookup  = (*App)(nil)
)

// viewRec is a view of a fake window.
type viewRec struct {
	ref  caps.ViewRef
	path string
	win  *Window
	// Evals are the scripts run in the view.
	evals []string
}

func (a *App) newView(w *Window, path string) *viewRec {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextView++
	v := &viewRec{ref: a.nextView, path: path, win: w}
	if a.viewRecs == nil {
		a.viewRecs = map[caps.ViewRef]*viewRec{}
	}
	a.viewRecs[v.ref] = v
	return v
}

// OnViewEvent implements caps.ViewEvents.
func (a *App) OnViewEvent(fn func(caps.ViewEvent)) { a.mu.Lock(); a.viewFn = fn; a.mu.Unlock() }

// EmitView delivers a view event as the platform would.
func (a *App) EmitView(e caps.ViewEvent) {
	a.mu.Lock()
	fn := a.viewFn
	a.mu.Unlock()
	if fn != nil {
		fn(e)
	}
}

// WindowOfView implements caps.ViewLookup.
func (a *App) WindowOfView(v caps.ViewRef) (caps.WindowRef, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r := a.viewRecs[v]; r != nil {
		return r.win.ref, true
	}
	return 0, false
}

// Evals lists the scripts run in a view with EvalView.
func (a *App) Evals(v caps.ViewRef) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r := a.viewRecs[v]; r != nil {
		return append([]string(nil), r.evals...)
	}
	return nil
}

func (w *Window) primary() *viewRec {
	w.mu.Lock()
	if w.pv == nil {
		w.mu.Unlock()
		pv := w.app.newView(w, w.Opts.Path)
		w.mu.Lock()
		if w.pv == nil {
			w.pv = pv
		}
	}
	defer w.mu.Unlock()
	return w.pv
}

func (w *Window) lookup(ref caps.ViewRef) *viewRec {
	if p := w.primary(); p.ref == ref {
		return p
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, v := range w.extra {
		if v.ref == ref {
			return v
		}
	}
	return nil
}

func (w *Window) PrimaryView() caps.ViewRef { return w.primary().ref }

func (w *Window) Views() []caps.ViewInfo {
	p := w.primary()
	out := []caps.ViewInfo{{Ref: p.ref, Path: p.path, Primary: true}}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, v := range w.extra {
		out = append(out, caps.ViewInfo{Ref: v.ref, Path: v.path})
	}
	return out
}

func (w *Window) AddView(o caps.ViewOptions) (caps.ViewRef, error) {
	p := w.primary()
	beside := o.Beside
	if beside == 0 {
		beside = p.ref
	}
	if w.lookup(beside) == nil {
		return 0, fmt.Errorf("view %d is not in this window", beside)
	}
	v := w.app.newView(w, o.Path)
	w.mu.Lock()
	next, err := caps.SplitLeaf(w.currentLayoutLocked(p), beside, v.ref, o)
	if err != nil {
		w.mu.Unlock()
		return 0, err
	}
	w.layout = next
	w.extra = append(w.extra, v)
	w.mu.Unlock()
	w.rec("addview " + o.Path)
	// Like a real platform, the event is sent before AddView returns.
	w.app.EmitView(caps.ViewEvent{View: v.ref, Window: w.ref, Kind: caps.ViewAdded, Path: o.Path})
	return v.ref, nil
}

func (w *Window) currentLayoutLocked(p *viewRec) *caps.LayoutNode {
	if w.layout == nil {
		return caps.Leaf(p.ref)
	}
	return w.layout
}

func (w *Window) RemoveView(ref caps.ViewRef) error {
	p := w.primary()
	if ref == p.ref {
		return errors.New("the primary view cannot be removed")
	}
	v := w.lookup(ref)
	if v == nil {
		return fmt.Errorf("view %d is not in this window", ref)
	}
	w.mu.Lock()
	next, err := caps.RemoveLeaf(w.currentLayoutLocked(p), ref)
	if err != nil {
		w.mu.Unlock()
		return err
	}
	w.layout = next
	for i, e := range w.extra {
		if e == v {
			w.extra = append(w.extra[:i:i], w.extra[i+1:]...)
			break
		}
	}
	w.mu.Unlock()
	w.app.EmitView(caps.ViewEvent{View: ref, Window: w.ref, Kind: caps.ViewRemoved})
	w.app.mu.Lock()
	delete(w.app.viewRecs, ref)
	w.app.mu.Unlock()
	return nil
}

func (w *Window) SetLayout(root *caps.LayoutNode) error {
	var refs []caps.ViewRef
	for _, v := range w.Views() {
		refs = append(refs, v.Ref)
	}
	if err := root.Validate(refs); err != nil {
		return err
	}
	w.mu.Lock()
	w.layout = root.Clone()
	w.mu.Unlock()
	return nil
}

func (w *Window) Layout() *caps.LayoutNode {
	p := w.primary()
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.currentLayoutLocked(p).Clone()
}

func (w *Window) FocusView(ref caps.ViewRef) error {
	if w.lookup(ref) == nil {
		return fmt.Errorf("view %d is not in this window", ref)
	}
	w.rec(fmt.Sprintf("focusview %d", ref))
	return nil
}

func (w *Window) NavigateView(ref caps.ViewRef, path string) error {
	v := w.lookup(ref)
	if v == nil {
		return fmt.Errorf("view %d is not in this window", ref)
	}
	w.app.mu.Lock()
	v.path = path
	w.app.mu.Unlock()
	return nil
}

func (w *Window) EvalView(ref caps.ViewRef, js string) error {
	v := w.lookup(ref)
	if v == nil {
		return fmt.Errorf("view %d is not in this window", ref)
	}
	w.app.mu.Lock()
	v.evals = append(v.evals, js)
	w.app.mu.Unlock()
	return nil
}
