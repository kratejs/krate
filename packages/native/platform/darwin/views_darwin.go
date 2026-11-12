//go:build darwin

package darwin

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ebitengine/purego/objc"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Several WKWebViews in one NSWindow, laid out as nested NSSplitViews.
// TestGUIViews and the compiler's TestNativeE2EViewsGUI run it in CI on
// macos-latest.
//
// The layout tree and its arithmetic come from caps (pure Go, unit-tested).
// NSSplitView draws the dividers and handles the drag. Minimum pane sizes are
// Auto Layout constraints on each web view (width and height at least the
// minimum, at priority 999 so that a window smaller than the minimums does not
// break the layout). All web views share the one WKProcessPool (see
// newProcessPool), so they share its single web process where WebKit allows.

var (
	_ caps.WindowViews = (*Window)(nil)
	_ caps.ViewEvents  = (*App)(nil)
	_ caps.ViewLookup  = (*App)(nil)
)

const (
	nsSplitViewDividerStyleThin  = 2
	nsLayoutPriorityNearRequired = 999
)

// view is one WKWebView of a window. Fields are main-thread only.
type view struct {
	ref     caps.ViewRef
	w       *Window
	web     objc.ID // 0 while the window is released
	primary bool

	route    string
	state    caps.ViewState
	scrollJS string
	minW     int
	minH     int
	cons     []objc.ID // the minimum-size constraints now active
}

// splitState is one NSSplitView of the layout tree.
type splitState struct {
	sv            objc.ID
	path          string
	axis          caps.Axis
	node          *caps.LayoutNode
	first, second objc.ID
}

func (a *App) registerView(w *Window, web objc.ID, primary bool) *view {
	a.viewMu.Lock()
	a.nextView++
	v := &view{ref: caps.ViewRef(a.nextView), w: w, primary: primary}
	a.viewOwner[v.ref] = w.Ref()
	a.viewMu.Unlock()
	a.setWeb(v, web)
	return v
}

// setWeb changes the WKWebView behind v, keeping the lookup table and the
// window's primary mirror in step.
func (a *App) setWeb(v *view, web objc.ID) {
	if v.web != 0 {
		delete(a.byWeb, v.web)
	}
	v.web = web
	if web != 0 {
		a.byWeb[web] = v
	}
	if v.primary {
		v.w.web = web
	}
}

func (a *App) unregisterView(v *view) {
	a.viewMu.Lock()
	delete(a.viewOwner, v.ref)
	a.viewMu.Unlock()
	if v.web != 0 {
		delete(a.byWeb, v.web)
	}
}

// WindowOfView says which window owns a view. Any goroutine.
func (a *App) WindowOfView(ref caps.ViewRef) (caps.WindowRef, bool) {
	a.viewMu.Lock()
	defer a.viewMu.Unlock()
	w, ok := a.viewOwner[ref]
	return w, ok
}

// OnViewEvent sets the handler for view events.
func (a *App) OnViewEvent(fn func(caps.ViewEvent)) {
	a.viewMu.Lock()
	a.viewHandler = fn
	a.viewMu.Unlock()
}

func emitView(a *App, v *view, kind caps.ViewEventKind, path string) {
	a.viewMu.Lock()
	fn := a.viewHandler
	a.viewMu.Unlock()
	if fn != nil {
		fn(caps.ViewEvent{View: v.ref, Window: v.w.Ref(), Kind: kind, Path: path})
	}
}

func (w *Window) allViews() []*view { return append([]*view{w.pv}, w.extra...) }

func (w *Window) liveViews() []*view {
	var out []*view
	for _, v := range w.allViews() {
		if v.web != 0 {
			out = append(out, v)
		}
	}
	return out
}

func (w *Window) viewByRef(ref caps.ViewRef) *view {
	for _, v := range w.allViews() {
		if v.ref == ref {
			return v
		}
	}
	return nil
}

func (w *Window) currentLayout() *caps.LayoutNode {
	if w.layout == nil {
		return caps.Leaf(w.pv.ref)
	}
	return w.layout
}

// currentRoute is the route the view shows according to WebKit, or the last
// route it was told to load.
func (v *view) currentRoute() string {
	if v.web != 0 {
		if u := v.web.Send(sel("URL")); u != 0 {
			if s := goString(u.Send(sel("absoluteString"))); strings.HasPrefix(s, OriginURL+"/") {
				return s[len(OriginURL):]
			}
		}
	}
	return v.route
}

// load starts a navigation to path on the app origin (main thread).
func (v *view) load(path string) {
	v.route = caps.CleanRoute(path)
	v.state = caps.ViewState{Route: v.route}
	if v.web == 0 {
		return
	}
	u := cls("NSURL").Send(sel("URLWithString:"), nsString(appURL(path)))
	if u == 0 {
		return
	}
	req := cls("NSURLRequest").Send(sel("requestWithURL:"), u)
	v.web.Send(sel("loadRequest:"), req)
}

// applyMinSize replaces the minimum-size constraints of the view.
func (v *view) applyMinSize() {
	for _, c := range v.cons {
		c.Send(sel("setActive:"), false)
		release(c)
	}
	v.cons = nil
	if v.web == 0 {
		return
	}
	add := func(anchorSel string, min int) {
		if min <= 0 {
			return
		}
		anchor := v.web.Send(sel(anchorSel))
		if anchor == 0 {
			return
		}
		c := anchor.Send(sel("constraintGreaterThanOrEqualToConstant:"), float64(min))
		if c == 0 {
			return
		}
		retain(c)
		c.Send(sel("setPriority:"), float32(nsLayoutPriorityNearRequired))
		c.Send(sel("setActive:"), true)
		v.cons = append(v.cons, c)
	}
	add("widthAnchor", v.minW)
	add("heightAnchor", v.minH)
}

// PrimaryView is the view the window was created with.
func (w *Window) PrimaryView() caps.ViewRef { return w.pv.ref }

// Views lists the views of the window.
func (w *Window) Views() (out []caps.ViewInfo) {
	runOnMain(func() {
		for _, v := range w.allViews() {
			out = append(out, caps.ViewInfo{Ref: v.ref, Path: v.currentRoute(), Primary: v.primary})
		}
	})
	return
}

var errReleased = errors.New("darwin: the window is released; Show it first")

// AddView creates a view and splits it into the layout.
func (w *Window) AddView(o caps.ViewOptions) (ref caps.ViewRef, err error) {
	runOnMain(func() { withPool(func() { ref, err = w.addView(o) }) })
	return
}

func (w *Window) addView(o caps.ViewOptions) (caps.ViewRef, error) {
	a := w.app
	if w.closed {
		return 0, errors.New("darwin: the window is closed")
	}
	if w.released || w.releasing {
		return 0, errReleased
	}
	if o.MinWidth < 0 || o.MinHeight < 0 {
		return 0, errors.New("darwin: view minimum size cannot be negative")
	}
	beside := o.Beside
	if beside == 0 {
		beside = w.pv.ref
	}
	if w.viewByRef(beside) == nil {
		return 0, fmt.Errorf("darwin: view %d is not in this window", beside)
	}
	web := a.newWebView()
	if web == 0 {
		return 0, errors.New("darwin: WKWebView creation failed")
	}
	v := a.registerView(w, web, false)
	next, err := caps.SplitLeaf(w.currentLayout(), beside, v.ref, o)
	if err != nil {
		a.unregisterView(v)
		a.setWeb(v, 0)
		release(web)
		return 0, err
	}
	v.minW, v.minH = o.MinWidth, o.MinHeight
	w.snapshotRatios()
	w.extra = append(w.extra, v)
	w.layout = next
	v.applyMinSize() // before the dividers are positioned: a later constraint resets them
	w.applyLayout()
	v.load(o.Path)
	emitView(a, v, caps.ViewAdded, v.route)
	return v.ref, nil
}

// RemoveView destroys a view of the window; the sibling takes its space.
func (w *Window) RemoveView(ref caps.ViewRef) (err error) {
	runOnMain(func() { withPool(func() { err = w.removeView(ref, caps.ViewRemoved) }) })
	return
}

func (w *Window) removeView(ref caps.ViewRef, kind caps.ViewEventKind) error {
	a := w.app
	v := w.viewByRef(ref)
	switch {
	case w.closed:
		return errors.New("darwin: the window is closed")
	case v == nil:
		return fmt.Errorf("darwin: view %d is not in this window", ref)
	case v.primary:
		return errors.New("darwin: the primary view cannot be removed; close the window instead")
	case w.released || w.releasing:
		return errReleased
	}
	w.snapshotRatios()
	next, err := caps.RemoveLeaf(w.currentLayout(), ref)
	if err != nil {
		return err
	}
	for i, e := range w.extra {
		if e == v {
			w.extra = append(w.extra[:i:i], w.extra[i+1:]...)
			break
		}
	}
	w.layout = next
	web := v.web
	w.applyLayout() // the removed view is not in the new tree
	for _, c := range v.cons {
		c.Send(sel("setActive:"), false)
		release(c)
	}
	v.cons = nil
	web.Send(sel("stopLoading"))
	web.Send(sel("setNavigationDelegate:"), objc.ID(0))
	web.Send(sel("removeFromSuperview"))
	a.unregisterView(v)
	v.web = 0
	release(web)
	emitView(a, v, kind, "")
	return nil
}

// SetLayout replaces the layout of the window.
func (w *Window) SetLayout(root *caps.LayoutNode) (err error) {
	runOnMain(func() { withPool(func() { err = w.setLayout(root) }) })
	return
}

func (w *Window) setLayout(root *caps.LayoutNode) error {
	if w.closed {
		return errors.New("darwin: the window is closed")
	}
	if w.released || w.releasing {
		return errReleased
	}
	var refs []caps.ViewRef
	for _, v := range w.allViews() {
		refs = append(refs, v.ref)
	}
	if err := root.Validate(refs); err != nil {
		return err
	}
	root = root.Clone()
	var adopt func(n *caps.LayoutNode)
	adopt = func(n *caps.LayoutNode) {
		if n == nil {
			return
		}
		if n.IsLeaf() {
			if v := w.viewByRef(n.View); v != nil {
				v.minW, v.minH = n.MinWidth, n.MinHeight
			}
			return
		}
		adopt(n.First)
		adopt(n.Second)
	}
	adopt(root)
	w.layout = root
	for _, v := range w.liveViews() {
		v.applyMinSize()
	}
	w.applyLayout()
	return nil
}

// Layout returns the layout with the ratios the dividers have now.
func (w *Window) Layout() (out *caps.LayoutNode) {
	runOnMain(func() {
		w.snapshotRatios()
		out = w.currentLayout().Clone()
	})
	return
}

// FocusView gives keyboard focus to a view.
func (w *Window) FocusView(ref caps.ViewRef) (err error) {
	runOnMain(func() {
		v := w.viewByRef(ref)
		switch {
		case w.closed || v == nil:
			err = fmt.Errorf("darwin: view %d is not in this window", ref)
		case v.web == 0:
			err = errReleased
		default:
			w.win.Send(sel("makeFirstResponder:"), v.web)
		}
	})
	return
}

// NavigateView loads a route in a view.
func (w *Window) NavigateView(ref caps.ViewRef, path string) (err error) {
	runOnMain(func() {
		v := w.viewByRef(ref)
		if w.closed || v == nil {
			err = fmt.Errorf("darwin: view %d is not in this window", ref)
			return
		}
		v.load(path)
	})
	return
}

// EvalView runs JavaScript in a view without waiting for the result.
func (w *Window) EvalView(ref caps.ViewRef, js string) (err error) {
	runOnMain(func() {
		v := w.viewByRef(ref)
		switch {
		case w.closed || v == nil:
			err = fmt.Errorf("darwin: view %d is not in this window", ref)
		case v.web == 0:
			err = errReleased
		default:
			v.web.Send(sel("evaluateJavaScript:completionHandler:"), nsString(js), objc.ID(0))
		}
	})
	return
}

// applyLayout rebuilds the split view tree from w.layout. The web views are
// taken out of their superviews first (each keeps the reference this package
// owns), then added to freshly built NSSplitViews, and the result becomes the
// window's content view. Main thread.
func (w *Window) applyLayout() {
	live := w.liveViews()
	for _, v := range live {
		v.web.Send(sel("removeFromSuperview"))
	}
	var frame nsRect
	if old := w.win.Send(sel("contentView")); old != 0 {
		frame = objc.Send[nsRect](old, sel("frame"))
	}
	w.splits = nil
	root := w.build(w.currentLayout(), "", len(w.currentLayout().Leaves()) > 1)
	w.root = root
	root.Send(sel("setFrame:"), frame)
	w.win.Send(sel("setContentView:"), root) // retains root, releases the old tree
	if !w.currentLayout().IsLeaf() {
		release(root) // the window holds it now; build returned an owned reference
	}
	w.applyRatios()
	w.win.Send(sel("makeFirstResponder:"), w.pv.web)
	w.deferRatios()
}

// deferRatios positions the dividers a second time on the next turn of the
// main queue. A constraint added after the first pass (the minimum sizes) or
// the window's own first layout can undo a divider position that was set
// inside the same turn; setting it again once the layout has settled holds.
func (w *Window) deferRatios() {
	w.layoutGen++
	w.ratiosSettled = false
	gen := w.layoutGen
	dispatch(func() {
		if w.layoutGen != gen {
			return
		}
		if !w.closed && !w.released && !w.releasing {
			w.applyRatios()
		}
		w.ratiosSettled = true
	})
}

// build makes the NSView for a layout node. A split view is returned with a
// reference the caller owns; a web view is returned as is.
func (w *Window) build(n *caps.LayoutNode, path string, inSplit bool) objc.ID {
	if n.IsLeaf() {
		v := w.viewByRef(n.View)
		if v == nil || v.web == 0 {
			return cls("NSView").Send(sel("new"))
		}
		// A web view that is the whole content view keeps its autoresizing
		// mask; one in a split view is sized by the split view's constraints.
		v.web.Send(sel("setTranslatesAutoresizingMaskIntoConstraints:"), !inSplit)
		return v.web
	}
	sv := cls("NSSplitView").Send(sel("new"))
	sv.Send(sel("setVertical:"), n.Axis == caps.Horizontal) // vertical dividers: panes side by side
	sv.Send(sel("setDividerStyle:"), nsSplitViewDividerStyleThin)
	first, second := w.build(n.First, path+"0", true), w.build(n.Second, path+"1", true)
	sv.Send(sel("addArrangedSubview:"), first)
	sv.Send(sel("addArrangedSubview:"), second)
	if !n.First.IsLeaf() {
		release(first)
	}
	if !n.Second.IsLeaf() {
		release(second)
	}
	w.splits = append(w.splits, &splitState{sv: sv, path: path, axis: n.Axis, node: n, first: first, second: second})
	return sv
}

// applyRatios positions every divider from the ratios in the tree, outer
// splits first because the inner ones size themselves from them.
func (w *Window) applyRatios() {
	if len(w.splits) == 0 || w.root == 0 {
		return
	}
	w.root.Send(sel("layoutSubtreeIfNeeded"))
	for _, s := range w.splits {
		f := objc.Send[nsRect](s.sv, sel("frame"))
		thick := int(objc.Send[float64](s.sv, sel("dividerThickness")))
		total := int(f.Size.Width)
		if s.axis == caps.Vertical {
			total = int(f.Size.Height)
		}
		fw, fh := caps.MinSize(s.node.First, thick)
		sw, sh := caps.MinSize(s.node.Second, thick)
		minF, minS := fw, sw
		if s.axis == caps.Vertical {
			minF, minS = fh, sh
		}
		ratio := s.node.Ratio
		if ratio <= 0 || ratio >= 1 {
			ratio = 0.5
		}
		first, _ := caps.SplitSizes(total, thick, ratio, minF, minS)
		s.sv.Send(sel("setPosition:ofDividerAtIndex:"), float64(first), 0)
		s.sv.Send(sel("layoutSubtreeIfNeeded"))
	}
}

// snapshotRatios copies the live divider positions into w.layout.
func (w *Window) snapshotRatios() {
	if w.layout == nil || w.released || !w.ratiosSettled {
		return
	}
	for _, s := range w.splits {
		fr := objc.Send[nsRect](s.first, sel("frame"))
		sr := objc.Send[nsRect](s.second, sel("frame"))
		fs, ss := fr.Size.Width, sr.Size.Width
		if s.axis == caps.Vertical {
			fs, ss = fr.Size.Height, sr.Size.Height
		}
		if fs <= 0 || ss <= 0 {
			continue
		}
		if n := w.layout.At(s.path); n != nil && !n.IsLeaf() {
			n.Ratio = fs / (fs + ss)
		}
	}
}
