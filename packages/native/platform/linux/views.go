//go:build linux

package linux

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.WindowViews = (*Window)(nil)
	_ caps.ViewEvents  = (*App)(nil)
	_ caps.ViewLookup  = (*App)(nil)
)

// A view is one WebKitWebView of a window. The first one is the primary view;
// the others come from AddView. All of them are created related to a live
// view, so they share the app's single web process.
type view struct {
	ref     caps.ViewRef
	w       *Window
	widget  uintptr // 0 while the window is released
	primary bool

	// Main thread only.
	route    string
	state    caps.ViewState
	scrollJS string
	minW     int
	minH     int
}

// paneState is one GtkPaned of the layout tree. Its position is applied from
// the stored ratio once the paned has a size (see allocateCallback).
type paneState struct {
	id            uintptr
	w             *Window
	widget        uintptr
	path          string
	axis          caps.Axis
	node          *caps.LayoutNode
	first, second uintptr
	ratio         float64
	pending       int // size-allocate rounds left to apply ratio
	skipped       int // rounds seen before the children had a real size
	handle        int
}

// registerView records a view of w. widget may be 0 (a released view being
// recreated later). Main thread.
func (a *App) registerView(w *Window, widget uintptr, primary bool) *view {
	a.mu.Lock()
	a.nextView++
	v := &view{ref: caps.ViewRef(a.nextView), w: w, primary: primary}
	a.views[v.ref] = v
	a.mu.Unlock()
	a.setWidget(v, widget)
	return v
}

// setWidget changes the WebKitWebView behind v, keeping the lookup table and
// the window's primary-widget mirror in step.
func (a *App) setWidget(v *view, widget uintptr) {
	a.mu.Lock()
	if v.widget != 0 {
		delete(a.byWidget, v.widget)
	}
	v.widget = widget
	if widget != 0 {
		a.byWidget[widget] = v
	}
	a.mu.Unlock()
	if v.primary {
		v.w.view = widget
	}
}

func (a *App) unregisterView(v *view) {
	a.mu.Lock()
	delete(a.views, v.ref)
	if v.widget != 0 {
		delete(a.byWidget, v.widget)
	}
	a.mu.Unlock()
}

func (a *App) viewByRef(ref uintptr) *view {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.views[caps.ViewRef(ref)]
}

// viewOfRequest is the view a scheme request came from, 0 when WebKit does not
// say or the view is unknown.
func (a *App) viewOfRequest(request uintptr) caps.ViewRef {
	if a.v.reqView == nil {
		return 0
	}
	p := a.v.reqView(request)
	if p == 0 {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if v := a.byWidget[p]; v != nil {
		return v.ref
	}
	return 0
}

// WindowOfView says which window owns a view. It may be called from any
// goroutine.
func (a *App) WindowOfView(ref caps.ViewRef) (caps.WindowRef, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v := a.views[ref]; v != nil {
		return v.w.Ref(), true
	}
	return 0, false
}

// OnViewEvent sets the handler for view events.
func (a *App) OnViewEvent(fn func(caps.ViewEvent)) {
	a.mu.Lock()
	a.viewHandler = fn
	a.mu.Unlock()
}

func (a *App) emitView(v *view, kind caps.ViewEventKind, path string) {
	a.mu.Lock()
	fn := a.viewHandler
	a.mu.Unlock()
	if fn != nil {
		fn(caps.ViewEvent{View: v.ref, Window: v.w.Ref(), Kind: kind, Path: path})
	}
}

// liveViews lists the views of w that hold a webview, primary first.
func (w *Window) liveViews() []*view {
	out := make([]*view, 0, 1+len(w.extra))
	for _, v := range w.allViews() {
		if v.widget != 0 {
			out = append(out, v)
		}
	}
	return out
}

func (w *Window) allViews() []*view {
	return append([]*view{w.pv}, w.extra...)
}

func (w *Window) viewByRef(ref caps.ViewRef) *view {
	for _, v := range w.allViews() {
		if v.ref == ref {
			return v
		}
	}
	return nil
}

func (v *view) currentRoute() string {
	if v.widget != 0 && v.w.app.v.getURI != nil {
		if uri := goString(v.w.app.v.getURI(v.widget)); len(uri) > len(Origin) && uri[:len(Origin)+1] == Origin+"/" {
			return uri[len(Origin):]
		}
	}
	return v.route
}

func (v *view) load(path string) {
	v.route = cleanPath(path)
	v.state = caps.ViewState{Route: v.route}
	if v.widget != 0 {
		v.w.app.api.wkViewLoadURI(v.widget, Origin+v.route)
	}
}

// connectView installs the per-view signal handlers; the user data is the view
// ref, not the window id.
func (a *App) connectView(v *view) {
	api := a.api
	api.gSignalConnectData(v.widget, "decide-policy", cbPolicy, uintptr(v.ref), 0, 0)
	api.gSignalConnectData(v.widget, "load-changed", cbLoad, uintptr(v.ref), 0, 0)
	api.gSignalConnectData(v.widget, "close", cbClose, uintptr(v.ref), 0, 0)
	a.connectCrash(v)
	a.connectDrops(v.widget, v.ref)
}

// PrimaryView is the view the window was created with.
func (w *Window) PrimaryView() caps.ViewRef { return w.pv.ref }

// Views lists the views of the window.
func (w *Window) Views() (out []caps.ViewInfo) {
	w.app.callMain(func() {
		for _, v := range w.allViews() {
			out = append(out, caps.ViewInfo{Ref: v.ref, Path: v.currentRoute(), Primary: v.primary})
		}
	})
	return
}

// currentLayout is the layout tree, a single leaf when nothing was split.
func (w *Window) currentLayout() *caps.LayoutNode {
	if w.layout == nil {
		return caps.Leaf(w.pv.ref)
	}
	return w.layout
}

var errReleased = errors.New("krate: the window is released; Show it first")

// AddView creates a view on o.Path and splits it into the layout.
func (w *Window) AddView(o caps.ViewOptions) (ref caps.ViewRef, err error) {
	if !w.app.callMain(func() { ref, err = w.addView(o) }) {
		return 0, errors.New("krate: application is not running")
	}
	return
}

func (w *Window) addView(o caps.ViewOptions) (caps.ViewRef, error) {
	a := w.app
	if !w.alive {
		return 0, errors.New("krate: the window is closed")
	}
	if w.released || w.releasing {
		return 0, errReleased
	}
	if o.MinWidth < 0 || o.MinHeight < 0 {
		return 0, errors.New("krate: view minimum size cannot be negative")
	}
	beside := o.Beside
	if beside == 0 {
		beside = w.pv.ref
	}
	if bv := w.viewByRef(beside); bv == nil {
		return 0, fmt.Errorf("krate: view %d is not in this window", beside)
	}
	widget := a.newView()
	if widget == 0 {
		return 0, errors.New("krate: webkit_web_view_new failed")
	}
	v := a.registerView(w, widget, false)
	v.minW, v.minH = o.MinWidth, o.MinHeight
	next, err := caps.SplitLeaf(w.currentLayout(), beside, v.ref, o)
	if err != nil {
		a.api.gtkWidgetDestroy(widget)
		a.unregisterView(v)
		return 0, err
	}
	a.applyViewSettings(widget)
	a.connectView(v)
	if v.minW > 0 || v.minH > 0 {
		a.api.gtkWidgetSetSizeReq(widget, int32(v.minW), int32(v.minH))
	}
	w.extra = append(w.extra, v)
	w.layout = next
	w.applyLayout()
	v.load(o.Path)
	a.emitView(v, caps.ViewAdded, v.route)
	return v.ref, nil
}

// RemoveView destroys a view of the window; the sibling takes its space.
func (w *Window) RemoveView(ref caps.ViewRef) (err error) {
	if !w.app.callMain(func() { err = w.removeView(ref, caps.ViewRemoved) }) {
		return errors.New("krate: application is not running")
	}
	return
}

func (w *Window) removeView(ref caps.ViewRef, kind caps.ViewEventKind) error {
	a := w.app
	v := w.viewByRef(ref)
	switch {
	case !w.alive:
		return errors.New("krate: the window is closed")
	case v == nil:
		return fmt.Errorf("krate: view %d is not in this window", ref)
	case v.primary:
		return errors.New("krate: the primary view cannot be removed; close the window instead")
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
	// The widget is destroyed with the old pane tree by applyLayout.
	a.unregisterView(v)
	v.widget = 0
	w.applyLayout()
	a.emitView(v, kind, "")
	return nil
}

// SetLayout replaces the layout of the window.
func (w *Window) SetLayout(root *caps.LayoutNode) (err error) {
	if !w.app.callMain(func() { err = w.setLayout(root) }) {
		return errors.New("krate: application is not running")
	}
	return
}

func (w *Window) setLayout(root *caps.LayoutNode) error {
	if !w.alive {
		return errors.New("krate: the window is closed")
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
	for _, v := range w.allViews() {
		// A leaf's minimum size is the pane's, wherever the leaf sits.
		var min *caps.LayoutNode
		var find func(n *caps.LayoutNode)
		find = func(n *caps.LayoutNode) {
			if n == nil || min != nil {
				return
			}
			if n.IsLeaf() {
				if n.View == v.ref {
					min = n
				}
				return
			}
			find(n.First)
			find(n.Second)
		}
		find(root)
		if min != nil {
			v.minW, v.minH = min.MinWidth, min.MinHeight
			w.app.api.gtkWidgetSetSizeReq(v.widget, int32(v.minW), int32(v.minH))
		}
	}
	w.layout = root
	w.applyLayout()
	return nil
}

// Layout returns the layout with the ratios the dividers have now.
func (w *Window) Layout() (out *caps.LayoutNode) {
	w.app.callMain(func() {
		w.snapshotRatios()
		out = w.currentLayout().Clone()
	})
	return
}

// FocusView gives keyboard focus to a view.
func (w *Window) FocusView(ref caps.ViewRef) (err error) {
	w.app.callMain(func() {
		v := w.viewByRef(ref)
		switch {
		case !w.alive || v == nil:
			err = fmt.Errorf("krate: view %d is not in this window", ref)
		case v.widget == 0:
			err = errReleased
		default:
			if w.app.x.gtkWidgetGrabFocus != nil {
				w.app.x.gtkWidgetGrabFocus(v.widget)
			}
		}
	})
	return
}

// NavigateView loads a route in a view.
func (w *Window) NavigateView(ref caps.ViewRef, path string) (err error) {
	w.app.callMain(func() {
		v := w.viewByRef(ref)
		if !w.alive || v == nil {
			err = fmt.Errorf("krate: view %d is not in this window", ref)
			return
		}
		v.load(path)
	})
	return
}

// EvalView runs JavaScript in a view without waiting for the result.
func (w *Window) EvalView(ref caps.ViewRef, js string) (err error) {
	w.app.callMain(func() {
		v := w.viewByRef(ref)
		switch {
		case !w.alive || v == nil:
			err = fmt.Errorf("krate: view %d is not in this window", ref)
		case v.widget == 0:
			err = errReleased
		default:
			w.app.api.wkViewEval(v.widget, js, -1, 0, 0, 0, 0, 0)
		}
	})
	return
}

// applyLayout rebuilds the pane tree from w.layout: every webview is taken out
// of its container (keeping it alive), the old panes are destroyed, and the new
// tree is packed into the window. Main thread.
func (w *Window) applyLayout() {
	a := w.app
	x := a.x
	if x.gtkPanedNew == nil {
		return
	}
	api := a.api
	live := w.liveViews()
	for _, v := range live {
		api.gObjectRef(v.widget)
		if p := x.gtkWidgetGetParent(v.widget); p != 0 {
			x.gtkContainerRemove(p, v.widget)
		}
	}
	for _, p := range w.panes {
		delete(a.panes, p.id)
	}
	w.panes = nil
	// The webviews are out of the old tree; destroying its panes removes
	// them from the box. A root that is itself a webview was detached above.
	if w.root != 0 && !w.rootIsView(live) {
		api.gtkWidgetDestroy(w.root)
	}
	w.root = 0
	layout := w.currentLayout()
	root := w.build(layout, "")
	w.root = root
	if w.box != 0 {
		x.gtkBoxPackStart(w.box, root, 1, 1, 0)
		if w.menubar != 0 {
			x.gtkBoxReorderChild(w.box, w.menubar, 0)
		}
	} else {
		api.gtkContainerAdd(w.win, root)
	}
	// The containers own the webviews again.
	for _, v := range live {
		api.gObjectUnref(v.widget)
	}
	api.gtkWidgetShowAll(w.win)
	w.layoutNow()
}

// layoutNow runs the pending resize passes of the window right away instead of
// waiting for the frame clock. GTK on Wayland only runs them when the
// compositor has answered the previous frame; weston can take long to do so
// for a surface that just changed, and until then the new panes have no size,
// so the ratios could not be applied and Layout could not read them. Main
// thread.
func (w *Window) layoutNow() {
	x := w.app.x
	if x.gtkCheckResize == nil || x.gtkWidgetGetVisible == nil || x.gtkWidgetGetVisible(w.win) == 0 {
		return
	}
	for i := 0; i < 6; i++ {
		x.gtkCheckResize(w.win)
		settled := true
		for _, p := range w.panes {
			if p.pending > 0 {
				settled = false
			}
		}
		if settled {
			return
		}
	}
}

func (w *Window) rootIsView(live []*view) bool {
	for _, v := range live {
		if v.widget == w.root {
			return true
		}
	}
	return false
}

// build makes the widget for a layout node.
func (w *Window) build(n *caps.LayoutNode, path string) uintptr {
	a := w.app
	x := a.x
	if n.IsLeaf() {
		if v := w.viewByRef(n.View); v != nil && v.widget != 0 {
			return v.widget
		}
		return x.gtkBoxNew(0, 0) // a placeholder; Validate keeps this from happening
	}
	orient := int32(0)
	if n.Axis == caps.Vertical {
		orient = 1
	}
	p := x.gtkPanedNew(orient)
	first, second := w.build(n.First, path+"0"), w.build(n.Second, path+"1")
	// Neither half shrinks below its size request, which carries the minimum
	// size of its panes; both grow with the window.
	x.gtkPanedPack1(p, first, 1, 0)
	x.gtkPanedPack2(p, second, 1, 0)
	a.nextPaned++
	ps := &paneState{id: a.nextPaned, w: w, widget: p, path: path, axis: n.Axis, node: n, first: first, second: second,
		ratio: n.Ratio, pending: 3}
	if ps.ratio <= 0 || ps.ratio >= 1 {
		ps.ratio = 0.5
	}
	a.panes[ps.id] = ps
	w.panes = append(w.panes, ps)
	a.api.gSignalConnectData(p, "size-allocate", cbPaneAlloc, ps.id, 0, 0)
	return p
}

// allocateCallback applies a pane's ratio once GTK has given it a size, which
// takes a couple of rounds because the divider thickness is only known from
// the children's allocations.
func allocateCallback(widget, alloc, id uintptr) uintptr {
	a := theApp
	ps := a.panes[id]
	if ps == nil || ps.pending <= 0 || alloc == 0 {
		return 0
	}
	x := a.x
	base := *(*unsafe.Pointer)(unsafe.Pointer(&alloc))
	width, height := int(*(*int32)(unsafe.Add(base, 8))), int(*(*int32)(unsafe.Add(base, 12)))
	total := width
	fs, ss := int(x.gtkWidgetAllocW(ps.first)), int(x.gtkWidgetAllocW(ps.second))
	if ps.axis == caps.Vertical {
		total = height
		fs, ss = int(x.gtkWidgetAllocH(ps.first)), int(x.gtkWidgetAllocH(ps.second))
	}
	if total <= 1 {
		return 0
	}
	// A child that was never allocated reports 1x1 (GTK's default), which is
	// not a size: measuring the divider from it would give a handle as wide as
	// the paned. Under a compositor that configures surfaces asynchronously
	// (Wayland) the first round often sees exactly that, so such a round only
	// positions the divider with the last known handle and is not counted.
	if fs > 1 && ss > 1 {
		if h := total - fs - ss; h > 0 {
			ps.handle = h
		}
		ps.pending--
	} else if ps.skipped++; ps.skipped > 16 {
		ps.pending--
	}
	fw, fh := caps.MinSize(ps.node.First, ps.handle)
	sw, sh := caps.MinSize(ps.node.Second, ps.handle)
	minF, minS := fw, sw
	if ps.axis == caps.Vertical {
		minF, minS = fh, sh
	}
	first, _ := caps.SplitSizes(total, ps.handle, ps.ratio, minF, minS)
	x.gtkPanedSetPos(widget, int32(first))
	return 0
}

// snapshotRatios copies the live divider positions into w.layout.
func (w *Window) snapshotRatios() {
	if w.layout == nil || w.released {
		return
	}
	x := w.app.x
	for _, p := range w.panes {
		fs, ss := int(x.gtkWidgetAllocW(p.first)), int(x.gtkWidgetAllocW(p.second))
		if p.axis == caps.Vertical {
			fs, ss = int(x.gtkWidgetAllocH(p.first)), int(x.gtkWidgetAllocH(p.second))
		}
		if fs <= 1 || ss <= 1 || p.pending > 0 {
			continue
		}
		if n := w.layout.At(p.path); n != nil && !n.IsLeaf() {
			n.Ratio = float64(fs) / float64(fs+ss)
		}
	}
}

// SetDividerPosition moves the divider of the split at path (see
// caps.Divider.Path) to pos pixels from the start of its pane, the way a drag
// of the divider handle does: it goes through gtk_paned_set_position. It is for
// tests, which cannot move the pointer.
func (w *Window) SetDividerPosition(path string, pos int) (err error) {
	w.app.callMain(func() {
		for _, p := range w.panes {
			if p.path == path {
				p.pending = 0
				w.app.x.gtkPanedSetPos(p.widget, int32(pos))
				w.layoutNow()
				return
			}
		}
		err = fmt.Errorf("krate: no split at %q", path)
	})
	return
}

// viewWidget returns the WebKitWebView of a view, for tests.
func (w *Window) viewWidget(ref caps.ViewRef) (widget uintptr) {
	w.app.callMain(func() {
		if v := w.viewByRef(ref); v != nil {
			widget = v.widget
		}
	})
	return
}

// DragDivider moves a divider of the window with this platform reference the
// way a drag does (see Window.SetDividerPosition). app is a platform.App or
// *App. For tests.
func DragDivider(app any, ref caps.WindowRef, path string, pos int) error {
	a := appOf(app)
	if a == nil {
		return errors.New("krate: not a Linux app")
	}
	w := a.windowByID(uintptr(ref))
	if w == nil {
		return errors.New("krate: no such window")
	}
	return w.SetDividerPosition(path, pos)
}
