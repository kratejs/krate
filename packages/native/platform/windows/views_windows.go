//go:build windows

package windows

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Several WebView2 controllers in one top-level window. TestGUIViews and the
// compiler's TestNativeE2EViewsGUI run it in CI on windows-latest.
//
// Every controller is created from the app's one ICoreWebView2Environment,
// parented to the window's HWND, and given a rectangle by put_Bounds. The
// rectangles come from caps.Arrange (through splitter, which has the DPI
// scaling and the divider drag and is unit-tested on every OS). The gaps
// between controllers are the dividers: the mouse messages over a gap reach
// the window procedure, which hit-tests them (mouseDown, mouseMove, mouseUp,
// setCursor) and moves the ratio of the split being dragged.

var (
	_ caps.WindowViews = (*Window)(nil)
	_ caps.ViewEvents  = (*App)(nil)
	_ caps.ViewLookup  = (*App)(nil)
)

func (a *App) registerView(w *Window, primary bool) *view {
	a.viewMu.Lock()
	a.nextView++
	v := &view{w: w, ref: caps.ViewRef(a.nextView), primary: primary, ready: make(chan struct{})}
	a.viewOwner[v.ref] = w.Ref()
	a.viewMu.Unlock()
	return v
}

func (a *App) unregisterView(v *view) {
	a.viewMu.Lock()
	delete(a.viewOwner, v.ref)
	a.viewMu.Unlock()
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

// placeViews gives every controller its rectangle. Main thread.
func (w *Window) placeViews() {
	var r rect
	_, _, _ = pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	w.sp.layout = w.currentLayout()
	w.sp.bounds = caps.Rect{W: int(r.right - r.left), H: int(r.bottom - r.top)}
	w.sp.dpi = windowDPI(w.hwnd)
	for _, p := range w.sp.arrange().Panes {
		v := w.viewByRef(p.View)
		if v == nil || v.controller == nil {
			continue
		}
		b := rect{left: int32(p.Rect.X), top: int32(p.Rect.Y), right: int32(p.Rect.X + p.Rect.W), bottom: int32(p.Rect.Y + p.Rect.H)}
		putBounds(v.controller, &b)
	}
}

// mouseDown starts a divider drag. It reports whether the press was on a
// divider.
func (w *Window) mouseDown(lParam uintptr) bool {
	if len(w.allViews()) < 2 || w.released {
		return false
	}
	w.placeSplitter()
	x, y := mousePos(lParam)
	if !w.sp.press(x, y) {
		return false
	}
	_, _, _ = pSetCapture.Call(w.hwnd)
	return true
}

func (w *Window) mouseMove(lParam uintptr) {
	if !w.sp.dragging {
		return
	}
	x, y := mousePos(lParam)
	if w.sp.move(x, y) {
		w.placeViews()
	}
}

func (w *Window) mouseUp() {
	if w.sp.dragging {
		w.sp.release()
		_, _, _ = pReleaseCapture.Call()
	}
}

// setCursor shows a resize cursor over a divider. It reports whether it did.
func (w *Window) setCursor() bool {
	if len(w.allViews()) < 2 || w.released {
		return false
	}
	var pt struct{ x, y int32 }
	_, _, _ = pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	_, _, _ = pScreenToClient.Call(w.hwnd, uintptr(unsafe.Pointer(&pt)))
	w.placeSplitter()
	var id uintptr
	switch w.sp.cursor(int(pt.x), int(pt.y)) {
	case cursorSizeWE:
		id = idcSizeWE
	case cursorSizeNS:
		id = idcSizeNS
	default:
		return false
	}
	c, _, _ := pLoadCursorW.Call(0, id)
	_, _, _ = pSetCursor.Call(c)
	return true
}

// placeSplitter refreshes the splitter's bounds and layout without moving
// controllers.
func (w *Window) placeSplitter() {
	var r rect
	_, _, _ = pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	w.sp.layout = w.currentLayout()
	w.sp.bounds = caps.Rect{W: int(r.right - r.left), H: int(r.bottom - r.top)}
	w.sp.dpi = windowDPI(w.hwnd)
}

// mousePos reads the client coordinates of a mouse message (signed 16 bit).
func mousePos(lParam uintptr) (x, y int) {
	return int(int16(lParam & 0xFFFF)), int(int16((lParam >> 16) & 0xFFFF))
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

var errReleased = errors.New("windows: the window is released; Show it first")

// AddView creates a view and splits it into the layout. Called from another
// goroutine it returns once the view's webview is ready.
func (w *Window) AddView(o caps.ViewOptions) (caps.ViewRef, error) {
	var v *view
	var err error
	w.app.callMain(func() { v, err = w.addView(o) })
	if err != nil {
		return 0, err
	}
	if !w.app.onMain() {
		select {
		case <-v.ready:
		case <-w.app.exited:
			return 0, errors.New("windows: app stopped before the view was ready")
		}
		if v.err != nil {
			return 0, v.err
		}
	}
	return v.ref, nil
}

func (w *Window) addView(o caps.ViewOptions) (*view, error) {
	a := w.app
	switch {
	case w.destroyed:
		return nil, errors.New("windows: the window is closed")
	case w.released || w.releasing:
		return nil, errReleased
	case o.MinWidth < 0 || o.MinHeight < 0:
		return nil, errors.New("windows: view minimum size cannot be negative")
	}
	beside := o.Beside
	if beside == 0 {
		beside = w.pv.ref
	}
	if w.viewByRef(beside) == nil {
		return nil, fmt.Errorf("windows: view %d is not in this window", beside)
	}
	v := a.registerView(w, false)
	next, err := caps.SplitLeaf(w.currentLayout(), beside, v.ref, o)
	if err != nil {
		a.unregisterView(v)
		return nil, err
	}
	v.minW, v.minH = o.MinWidth, o.MinHeight
	v.path = o.Path
	w.extra = append(w.extra, v)
	w.layout = next
	w.placeViews() // the existing controllers shrink to make room
	v.createController()
	emitView(a, v, caps.ViewAdded, v.path)
	return v, nil
}

// RemoveView closes a view's controller; the sibling takes its space.
func (w *Window) RemoveView(ref caps.ViewRef) (err error) {
	w.app.callMain(func() { err = w.removeView(ref, caps.ViewRemoved) })
	return
}

func (w *Window) removeView(ref caps.ViewRef, kind caps.ViewEventKind) error {
	v := w.viewByRef(ref)
	switch {
	case w.destroyed:
		return errors.New("windows: the window is closed")
	case v == nil:
		return fmt.Errorf("windows: view %d is not in this window", ref)
	case v.primary:
		return errors.New("windows: the primary view cannot be removed; close the window instead")
	case w.released || w.releasing:
		return errReleased
	}
	w.dropView(v, kind)
	return nil
}

// dropView takes a non-primary view out of the window and the layout. Main
// thread.
func (w *Window) dropView(v *view, kind caps.ViewEventKind) {
	next, err := caps.RemoveLeaf(w.currentLayout(), v.ref)
	if err == nil {
		w.layout = next
	}
	for i, e := range w.extra {
		if e == v {
			w.extra = append(w.extra[:i:i], w.extra[i+1:]...)
			break
		}
	}
	emitView(w.app, v, kind, "")
	v.closeController()
	v.pending = nil
	v.finish(errors.New("windows: view removed"))
	w.app.unregisterView(v)
	w.placeViews()
}

// SetLayout replaces the layout of the window.
func (w *Window) SetLayout(root *caps.LayoutNode) (err error) {
	w.app.callMain(func() { err = w.setLayout(root) })
	return
}

func (w *Window) setLayout(root *caps.LayoutNode) error {
	switch {
	case w.destroyed:
		return errors.New("windows: the window is closed")
	case w.released || w.releasing:
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
	w.placeViews()
	return nil
}

// Layout returns the layout. The ratios are kept in the tree as the user
// drags the dividers, so there is nothing to read back from the controllers.
func (w *Window) Layout() (out *caps.LayoutNode) {
	w.app.callMain(func() { out = w.currentLayout().Clone() })
	return
}

// FocusView gives keyboard focus to a view.
func (w *Window) FocusView(ref caps.ViewRef) (err error) {
	w.app.callMain(func() {
		v := w.viewByRef(ref)
		switch {
		case w.destroyed || v == nil:
			err = fmt.Errorf("windows: view %d is not in this window", ref)
		case w.released:
			err = errReleased
		default:
			// WebView2 creates the controller asynchronously; until then the
			// call waits in the view's queue.
			v.do(v.focus)
		}
	})
	return
}

// NavigateView loads a route in a view.
func (w *Window) NavigateView(ref caps.ViewRef, path string) (err error) {
	w.app.callMain(func() {
		v := w.viewByRef(ref)
		if w.destroyed || v == nil {
			err = fmt.Errorf("windows: view %d is not in this window", ref)
			return
		}
		v.path = path
		if v.webview != nil {
			vcall(v.webview, wvNavigate, uintptr(unsafe.Pointer(wstr(navURL(path)))))
		}
	})
	return
}

// EvalView runs JavaScript in a view without waiting for the result.
func (w *Window) EvalView(ref caps.ViewRef, js string) (err error) {
	w.app.callMain(func() {
		v := w.viewByRef(ref)
		switch {
		case w.destroyed || v == nil:
			err = fmt.Errorf("windows: view %d is not in this window", ref)
		case w.released:
			err = errReleased
		default:
			// A view whose webview WebView2 is still creating runs the script
			// once it exists, as Window.Eval does.
			v.do(func() { vcall(v.webview, wvExecuteScript, uintptr(unsafe.Pointer(wstr(js))), 0) })
		}
	})
	return
}
