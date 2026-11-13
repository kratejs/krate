package fakeplat

import (
	"errors"
	"sync"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// The fake platform implements every chrome interface so the SDK layer above
// it can be tested without a window system. A call is recorded in the
// window's Calls, and state is kept the way a platform keeps it.

var (
	_ caps.WindowChrome   = (*Window)(nil)
	_ caps.WindowDocument = (*Window)(nil)
	_ caps.WindowSheets   = (*Window)(nil)
	_ caps.WindowTabs     = (*Window)(nil)
	_ caps.WindowThumbBar = (*Window)(nil)
	_ caps.JumpLister     = (*App)(nil)
	_ caps.AppPresence    = (*App)(nil)
	_ caps.Sharer         = (*App)(nil)
	_ caps.ChromeEvents   = (*App)(nil)
)

type chromeRec struct {
	mu    sync.Mutex
	st    caps.ChromeState
	sheet caps.WindowRef
	thumb []caps.ThumbButton
}

func (c *chromeRec) init(o caps.ChromeOptions) {
	o, _ = o.Validate()
	c.st = caps.ChromeState{
		TitleBar: o.TitleBar, Transparent: o.Transparent, Backdrop: o.Backdrop, Vibrancy: o.Vibrancy,
		Overlay: o.Overlay, TrafficLight: o.TrafficLight, TabbingID: o.TabbingID, TabbingMode: o.TabbingMode,
		Tabs: 1, Resizable: true, Decorated: true,
		Features: []string{
			caps.FeatureTitleBar, caps.FeatureTransparent, caps.FeatureBackdrop, caps.FeatureVibrancy,
			caps.FeatureTitleBarOverlay, caps.FeatureTrafficLights, caps.FeatureDrag, caps.FeatureResize,
			caps.FeatureDocument, caps.FeatureSheets, caps.FeatureTabs, caps.FeatureThumbButtons,
		},
	}
}

func (w *Window) with(call string, f func(c *caps.ChromeState)) {
	w.rec(call)
	w.chrome.mu.Lock()
	f(&w.chrome.st)
	w.chrome.mu.Unlock()
}

// ChromeState returns the recorded chrome.
func (w *Window) ChromeState() caps.ChromeState {
	w.chrome.mu.Lock()
	defer w.chrome.mu.Unlock()
	st := w.chrome.st
	st.ThumbButtons = len(w.chrome.thumb)
	st.Sheet = w.chrome.sheet != 0
	return st
}

func (w *Window) SetTitleBar(s string) error {
	w.with("titlebar "+s, func(c *caps.ChromeState) { c.TitleBar = s })
	return nil
}
func (w *Window) SetBackdrop(n string) error {
	w.with("backdrop "+n, func(c *caps.ChromeState) { c.Backdrop = n })
	return nil
}
func (w *Window) SetVibrancy(m string) error {
	w.with("vibrancy "+m, func(c *caps.ChromeState) { c.Vibrancy = m })
	return nil
}
func (w *Window) SetTitleBarOverlay(o *caps.TitleBarOverlay) error {
	w.with("overlay", func(c *caps.ChromeState) { c.Overlay = o })
	return nil
}
func (w *Window) SetTrafficLightPosition(p caps.Point) error {
	w.with("trafficlights", func(c *caps.ChromeState) { c.TrafficLight = &p })
	return nil
}
func (w *Window) StartDrag() error { w.rec("startdrag"); return nil }
func (w *Window) StartResize(e string) error {
	if caps.EdgeIndex(e) < 0 {
		return errors.New("bad edge")
	}
	w.rec("startresize " + e)
	return nil
}
func (w *Window) ToggleMaximize() error {
	w.rec("togglemaximize")
	w.mu.Lock()
	w.St.Maximized = !w.St.Maximized
	w.mu.Unlock()
	return nil
}
func (w *Window) SetDocumentEdited(on bool) error {
	w.with("documentedited", func(c *caps.ChromeState) { c.DocumentEdited = on })
	return nil
}
func (w *Window) SetRepresentedFile(p string) error {
	w.with("representedfile "+p, func(c *caps.ChromeState) { c.RepresentedFile = p })
	return nil
}
func (w *Window) AttachSheet(child caps.WindowRef) error {
	w.rec("attachsheet")
	w.chrome.mu.Lock()
	w.chrome.sheet = child
	w.chrome.mu.Unlock()
	return nil
}
func (w *Window) EndSheet() error {
	w.rec("endsheet")
	w.chrome.mu.Lock()
	w.chrome.sheet = 0
	w.chrome.mu.Unlock()
	return nil
}
func (w *Window) SetTabbing(id, mode string) error {
	w.with("tabbing "+id+" "+mode, func(c *caps.ChromeState) { c.TabbingID, c.TabbingMode = id, mode })
	return nil
}
func (w *Window) MergeAllWindows() error {
	w.rec("mergeall")
	w.app.mu.Lock()
	defer w.app.mu.Unlock()
	for _, o := range w.app.Windows {
		o.chrome.mu.Lock()
		o.chrome.st.Tabs = len(w.app.Windows)
		o.chrome.mu.Unlock()
	}
	return nil
}
func (w *Window) MoveTabToNewWindow() error {
	w.with("movetab", func(c *caps.ChromeState) { c.Tabs = 1 })
	return nil
}
func (w *Window) Tabs() []caps.WindowRef {
	w.app.mu.Lock()
	defer w.app.mu.Unlock()
	if w.chrome.st.Tabs <= 1 {
		return []caps.WindowRef{w.ref}
	}
	var out []caps.WindowRef
	for _, o := range w.app.Windows {
		out = append(out, o.ref)
	}
	return out
}
func (w *Window) SetThumbButtons(bs []caps.ThumbButton) error {
	w.rec("thumbbuttons")
	w.chrome.mu.Lock()
	w.chrome.thumb = append([]caps.ThumbButton(nil), bs...)
	w.chrome.mu.Unlock()
	return nil
}

// ThumbButtons returns the buttons last set.
func (w *Window) ThumbButtons() []caps.ThumbButton {
	w.chrome.mu.Lock()
	defer w.chrome.mu.Unlock()
	return append([]caps.ThumbButton(nil), w.chrome.thumb...)
}

func (a *App) SetJumpList(ts []caps.JumpTask) error {
	a.mu.Lock()
	a.Jump = append([]caps.JumpTask(nil), ts...)
	a.mu.Unlock()
	return nil
}
func (a *App) ClearJumpList() error { return a.SetJumpList(nil) }

func (a *App) SetActivationPolicy(p string) error {
	a.mu.Lock()
	a.Policy = p
	a.mu.Unlock()
	return nil
}
func (a *App) ActivationPolicy() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Policy == "" {
		return caps.PolicyRegular
	}
	return a.Policy
}
func (a *App) SetDockVisible(on bool) error {
	if on {
		return a.SetActivationPolicy(caps.PolicyRegular)
	}
	return a.SetActivationPolicy(caps.PolicyAccessory)
}
func (a *App) Share(parent caps.WindowRef, items caps.ShareItems) error {
	a.mu.Lock()
	a.Shared = append(a.Shared, items)
	a.mu.Unlock()
	return nil
}

// OnChromeEvent implements caps.ChromeEvents.
func (a *App) OnChromeEvent(fn func(caps.ChromeEvent)) {
	a.mu.Lock()
	a.chromeFn = fn
	a.mu.Unlock()
}

// EmitChrome delivers a chrome event as the platform would.
func (a *App) EmitChrome(w *Window, kind, id string) {
	a.mu.Lock()
	fn := a.chromeFn
	a.mu.Unlock()
	if fn != nil {
		fn(caps.ChromeEvent{Window: w.ref, Kind: kind, ID: id})
	}
}
