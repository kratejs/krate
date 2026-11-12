//go:build darwin

package darwin

// Window chrome on AppKit: full-size content with hidden or transparent title
// bars, traffic light placement, vibrancy through an NSVisualEffectView behind
// a transparent WKWebView, document state, sheets, native tabs, the share
// picker, and the activation policy (menu-bar-only apps). Compiled for macOS
// and covered by GUI tests that CI runs; it has not run on a Mac here.

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.WindowChrome          = (*Window)(nil)
	_ caps.TitleBarDoubleClicker = (*Window)(nil)
	_ caps.WindowDocument        = (*Window)(nil)
	_ caps.WindowSheets          = (*Window)(nil)
	_ caps.WindowTabs            = (*Window)(nil)
	_ caps.AppPresence           = (*App)(nil)
	_ caps.Sharer                = (*App)(nil)
	_ caps.ChromeEvents          = (*App)(nil)
)

// AppKit constants.
const (
	nsWindowStyleMaskFullSizeContentView = 1 << 15
	nsWindowTitleVisible                 = 0
	nsWindowTitleHidden                  = 1
	nsWindowBelow                        = -1
	nsVisualEffectBlendBehindWindow      = 0
	nsVisualEffectStateActive            = 1
	nsEventTypeLeftMouseDown             = 1
	nsEventTypeLeftMouseDragged          = 6
	nsMinYEdge                           = 1
	nsWindowCloseButton                  = 0
	nsWindowMiniaturizeButton            = 1
	nsWindowZoomButton                   = 2
)

// chromeState is a window's chrome (main thread only).
type chromeState struct {
	titleBar    string
	transparent bool
	vibrancy    string
	fx          objc.ID // the NSVisualEffectView, 0 when none
	tl          *caps.Point
	tabMode     string
	sheetOf     objc.ID // the parent window while this one is a sheet
	// buttonSpacing is the distance between traffic lights, measured before
	// they are moved.
	buttonSpacing float64
}

var chromeMu sync.Mutex
var chromeFn func(caps.ChromeEvent)

// OnChromeEvent sets the handler for chrome events.
func (a *App) OnChromeEvent(fn func(caps.ChromeEvent)) {
	chromeMu.Lock()
	chromeFn = fn
	chromeMu.Unlock()
}

func emitChrome(w *Window, kind, id string) {
	chromeMu.Lock()
	fn := chromeFn
	chromeMu.Unlock()
	if fn != nil && w != nil {
		fn(caps.ChromeEvent{Window: w.Ref(), Kind: kind, ID: id})
	}
}

func unsupported(format string, args ...any) error {
	return fmt.Errorf("%w: %s", krate.ErrUnsupported, fmt.Sprintf(format, args...))
}

// applyChrome sets what the options ask for on a new window, before it is
// shown. The style mask is built by the caller (see styleFor).
func (w *Window) applyChrome(o caps.ChromeOptions) {
	w.chrome.titleBar = o.TitleBar
	if w.chrome.titleBar == "" {
		w.chrome.titleBar = caps.TitleBarNative
	}
	if o.IsZero() {
		return // an ordinary window: leave AppKit's defaults alone
	}
	w.applyTitleBar()
	if o.Transparent {
		w.setTransparent(true)
	}
	if o.Vibrancy != "" {
		_ = w.setVibrancy(o.Vibrancy) // an unsupported material leaves the window plain
	}
	if o.TrafficLight != nil {
		p := *o.TrafficLight
		w.chrome.tl = &p
		w.applyTrafficLights()
	}
	if o.TabbingID != "" || o.TabbingMode != "" {
		w.setTabbing(o.TabbingID, o.TabbingMode)
	}
}

// styleFor adds the style mask bits a title bar style needs.
func styleFor(mask uint, titleBar string) uint {
	if titleBar == caps.TitleBarHidden || titleBar == caps.TitleBarNone {
		mask |= nsWindowStyleMaskFullSizeContentView
	}
	return mask
}

// applyTitleBar makes the window look like its title bar style.
func (w *Window) applyTitleBar() {
	win := w.win
	mask := objc.Send[uint](win, sel("styleMask"))
	hidden := w.chrome.titleBar != caps.TitleBarNative
	if hidden {
		mask |= nsWindowStyleMaskFullSizeContentView
	} else {
		mask &^= nsWindowStyleMaskFullSizeContentView
	}
	win.Send(sel("setStyleMask:"), mask)
	win.Send(sel("setTitlebarAppearsTransparent:"), hidden)
	vis := nsWindowTitleVisible
	if hidden {
		vis = nsWindowTitleHidden
	}
	win.Send(sel("setTitleVisibility:"), vis)
	// "none" also hides the traffic lights; "hidden" keeps them.
	for _, b := range []int{nsWindowCloseButton, nsWindowMiniaturizeButton, nsWindowZoomButton} {
		if btn := win.Send(sel("standardWindowButton:"), b); btn != 0 {
			btn.Send(sel("setHidden:"), w.chrome.titleBar == caps.TitleBarNone)
		}
	}
	w.applyTrafficLights()
}

// setTransparent makes the window and its webview see-through.
func (w *Window) setTransparent(on bool) {
	w.chrome.transparent = on
	win := w.win
	win.Send(sel("setOpaque:"), !on)
	if on {
		win.Send(sel("setBackgroundColor:"), cls("NSColor").Send(sel("clearColor")))
	} else {
		win.Send(sel("setBackgroundColor:"), cls("NSColor").Send(sel("windowBackgroundColor")))
	}
	for _, v := range w.allViews() {
		w.clearWebBackground(v.web, on)
	}
}

// clearWebBackground stops a WKWebView from painting its white background.
func (w *Window) clearWebBackground(web objc.ID, clear bool) {
	if web == 0 {
		return
	}
	if s := sel("_setDrawsBackground:"); respondsTo(web, s) {
		web.Send(s, !clear)
	}
	if s := sel("setUnderPageBackgroundColor:"); respondsTo(web, s) {
		c := cls("NSColor").Send(sel("windowBackgroundColor"))
		if clear {
			c = cls("NSColor").Send(sel("clearColor"))
		}
		web.Send(s, c)
	}
}

// setVibrancy puts an NSVisualEffectView with the material behind the page; ""
// removes it.
func (w *Window) setVibrancy(material string) error {
	if material == "" {
		if w.chrome.fx != 0 {
			w.chrome.fx.Send(sel("removeFromSuperview"))
			release(w.chrome.fx)
			w.chrome.fx = 0
		}
		w.chrome.vibrancy = ""
		w.setTransparent(false)
		return nil
	}
	m, ok := caps.VibrancyMaterial(material)
	if !ok {
		return fmt.Errorf("unknown vibrancy material %q", material)
	}
	content := w.win.Send(sel("contentView"))
	frame := content.Send(sel("superview"))
	if frame == 0 {
		return unsupported("the window has no frame view to attach vibrancy to")
	}
	if w.chrome.fx == 0 {
		fx := cls("NSVisualEffectView").Send(sel("alloc")).Send(sel("initWithFrame:"), objc.Send[nsRect](frame, sel("bounds")))
		if fx == 0 {
			return unsupported("NSVisualEffectView is not available")
		}
		fx.Send(sel("setBlendingMode:"), nsVisualEffectBlendBehindWindow)
		fx.Send(sel("setState:"), nsVisualEffectStateActive)
		fx.Send(sel("setAutoresizingMask:"), nsViewWidthSizable|nsViewHeightSizable)
		frame.Send(sel("addSubview:positioned:relativeTo:"), fx, nsWindowBelow, content)
		w.chrome.fx = fx
	}
	w.chrome.fx.Send(sel("setMaterial:"), m)
	w.chrome.vibrancy = material
	w.setTransparent(true)
	return nil
}

// applyTrafficLights moves the three window buttons to the requested place.
func (w *Window) applyTrafficLights() {
	if w.chrome.tl == nil {
		return
	}
	win := w.win
	closeB := win.Send(sel("standardWindowButton:"), nsWindowCloseButton)
	miniB := win.Send(sel("standardWindowButton:"), nsWindowMiniaturizeButton)
	zoomB := win.Send(sel("standardWindowButton:"), nsWindowZoomButton)
	if closeB == 0 || miniB == 0 || zoomB == 0 {
		return
	}
	titlebar := closeB.Send(sel("superview"))
	container := titlebar.Send(sel("superview"))
	if titlebar == 0 || container == 0 {
		return
	}
	if w.chrome.buttonSpacing == 0 {
		cf := objc.Send[nsRect](closeB, sel("frame"))
		mf := objc.Send[nsRect](miniB, sel("frame"))
		w.chrome.buttonSpacing = mf.Origin.X - cf.Origin.X
		if w.chrome.buttonSpacing <= 0 {
			w.chrome.buttonSpacing = 20
		}
	}
	bh := objc.Send[nsRect](closeB, sel("frame")).Size.Height
	ch := bh + 2*float64(w.chrome.tl.Y)
	wf := objc.Send[nsRect](win.Send(sel("contentView")).Send(sel("superview")), sel("frame"))
	cfr := objc.Send[nsRect](container, sel("frame"))
	cfr.Size.Height = ch
	cfr.Origin.Y = wf.Size.Height - ch
	container.Send(sel("setFrame:"), cfr)
	for i, b := range []objc.ID{closeB, miniB, zoomB} {
		f := objc.Send[nsRect](b, sel("frame"))
		f.Origin.X = float64(w.chrome.tl.X) + float64(i)*w.chrome.buttonSpacing
		f.Origin.Y = float64(w.chrome.tl.Y)
		b.Send(sel("setFrame:"), f)
	}
}

// chromeAdded keeps new webviews of a transparent window see-through.
func (w *Window) chromeAdded(web objc.ID) {
	if w.chrome.transparent {
		w.clearWebBackground(web, true)
	}
}

// ChromeState reports the window's chrome.
func (w *Window) ChromeState() (st caps.ChromeState) {
	runOnMain(func() {
		if w.closed {
			return
		}
		withPool(func() {
			st.TitleBar = w.chrome.titleBar
			if st.TitleBar == "" {
				st.TitleBar = caps.TitleBarNative
			}
			st.Transparent = w.chrome.transparent
			st.Vibrancy = w.chrome.vibrancy
			if w.chrome.tl != nil {
				p := *w.chrome.tl
				st.TrafficLight = &p
			}
			st.DocumentEdited = objc.Send[bool](w.win, sel("isDocumentEdited"))
			st.RepresentedFile = goString(w.win.Send(sel("representedFilename")))
			st.TabbingID = goString(w.win.Send(sel("tabbingIdentifier")))
			st.TabbingMode = w.chrome.tabMode
			st.Tabs = len(w.tabGroup())
			st.Sheet = w.chrome.sheetOf != 0
			st.Resizable = objc.Send[uint](w.win, sel("styleMask"))&nsWindowStyleMaskResizable != 0
			st.Decorated = objc.Send[uint](w.win, sel("styleMask"))&nsWindowStyleMaskTitled != 0
			st.Features = []string{
				caps.FeatureTitleBar, caps.FeatureTransparent, caps.FeatureVibrancy, caps.FeatureTrafficLights,
				caps.FeatureDrag, caps.FeatureDocument, caps.FeatureSheets, caps.FeatureTabs,
			}
		})
	})
	return
}

// SetTitleBar switches the title bar style.
func (w *Window) SetTitleBar(style string) error {
	style, ok := caps.NormalizeTitleBar(style)
	if !ok {
		return fmt.Errorf("titleBar must be native, hidden or none")
	}
	w.withWin(func() {
		w.chrome.titleBar = style
		w.applyTitleBar()
	})
	return nil
}

// SetBackdrop is a Windows feature.
func (w *Window) SetBackdrop(name string) error {
	if name == "" || name == caps.BackdropNone {
		return nil
	}
	return unsupported("window backdrops (Mica, Acrylic) are a Windows 11 feature; use vibrancy on macOS")
}

// SetVibrancy sets the vibrancy material behind the page.
func (w *Window) SetVibrancy(material string) (err error) {
	if _, ok := caps.VibrancyMaterial(material); !ok && material != "" {
		return fmt.Errorf("unknown vibrancy material %q", material)
	}
	runOnMain(func() {
		if w.closed {
			return
		}
		withPool(func() { err = w.setVibrancy(material) })
	})
	return
}

// SetTitleBarOverlay is a Windows feature.
func (w *Window) SetTitleBarOverlay(o *caps.TitleBarOverlay) error {
	if o == nil {
		return nil
	}
	return unsupported("a title bar overlay is a Windows feature; macOS keeps the traffic lights over a hidden title bar (titleBar: \"hidden\")")
}

// SetTrafficLightPosition moves the window buttons.
func (w *Window) SetTrafficLightPosition(p caps.Point) error {
	w.withWin(func() {
		w.chrome.tl = &p
		w.applyTrafficLights()
	})
	return nil
}

// StartDrag starts a window move from the mouse event being processed.
func (w *Window) StartDrag() (err error) {
	runOnMain(func() {
		if w.closed {
			err = fmt.Errorf("window closed")
			return
		}
		withPool(func() {
			ev := w.app.nsApp.Send(sel("currentEvent"))
			if ev == 0 {
				err = fmt.Errorf("no mouse event to drag from")
				return
			}
			switch objc.Send[uint](ev, sel("type")) {
			case nsEventTypeLeftMouseDown, nsEventTypeLeftMouseDragged:
				w.win.Send(sel("performWindowDragWithEvent:"), ev)
			default:
				// The button is already up: nothing to drag.
			}
		})
	})
	return
}

// StartResize is not needed: the window keeps AppKit's resize edges.
func (w *Window) StartResize(edge string) error {
	return unsupported("macOS windows keep their own resize edges")
}

// ToggleMaximize zooms the window or restores it.
func (w *Window) ToggleMaximize() error {
	w.withWin(func() { w.win.Send(sel("zoom:"), objc.ID(0)) })
	return nil
}

// TitleBarDoubleClick does what the user's setting says a double click on a
// title bar does: zoom (the default), minimize or nothing.
func (w *Window) TitleBarDoubleClick() error {
	w.withWin(func() {
		action := goString(cls("NSUserDefaults").Send(sel("standardUserDefaults")).Send(sel("stringForKey:"), nsString("AppleActionOnDoubleClick")))
		switch action {
		case "None":
		case "Minimize":
			w.win.Send(sel("performMiniaturize:"), objc.ID(0))
		default:
			w.win.Send(sel("zoom:"), objc.ID(0))
		}
	})
	return nil
}

// SetDocumentEdited marks the document as having unsaved changes.
func (w *Window) SetDocumentEdited(on bool) error {
	w.withWin(func() { w.win.Send(sel("setDocumentEdited:"), on) })
	return nil
}

// SetRepresentedFile shows the proxy icon of a file in the title bar.
func (w *Window) SetRepresentedFile(path string) error {
	w.withWin(func() { w.win.Send(sel("setRepresentedFilename:"), nsString(path)) })
	return nil
}

// AttachSheet shows the window with this ref as a sheet of w.
func (w *Window) AttachSheet(child caps.WindowRef) (err error) {
	runOnMain(func() {
		withPool(func() {
			c := w.app.windowByRef(child)
			switch {
			case w.closed:
				err = fmt.Errorf("window closed")
			case c == nil:
				err = fmt.Errorf("no window with that ref")
			case c == w:
				err = fmt.Errorf("a window cannot be a sheet of itself")
			case c.chrome.sheetOf != 0:
				err = fmt.Errorf("the window is already a sheet")
			default:
				c.win.Send(sel("orderOut:"), objc.ID(0))
				c.chrome.sheetOf = w.win
				w.win.Send(sel("beginSheet:completionHandler:"), c.win, objc.ID(0))
			}
		})
	})
	return
}

// EndSheet ends the sheet the window is showing as.
func (w *Window) EndSheet() (err error) {
	runOnMain(func() {
		withPool(func() {
			if w.closed || w.chrome.sheetOf == 0 {
				return
			}
			parent := w.chrome.sheetOf
			w.chrome.sheetOf = 0
			parent.Send(sel("endSheet:"), w.win)
		})
	})
	return
}

// windowDidEndSheet runs when a window closed a sheet.
func (a *App) windowDidEndSheet(parent objc.ID) {
	w := a.windows[parent]
	for _, c := range a.windows {
		if c.chrome.sheetOf == parent {
			c.chrome.sheetOf = 0
		}
	}
	emitChrome(w, caps.ChromeSheetEnded, "")
}

// windowByRef finds a window by its capability reference (main thread).
func (a *App) windowByRef(ref caps.WindowRef) *Window {
	for _, w := range a.windows {
		if w.Ref() == ref {
			return w
		}
	}
	return nil
}

// setTabbing sets the tabbing identifier and mode.
func (w *Window) setTabbing(id, mode string) {
	if id != "" {
		w.win.Send(sel("setTabbingIdentifier:"), nsString(id))
	}
	if v, ok := caps.TabbingModeValue(mode); ok {
		w.win.Send(sel("setTabbingMode:"), v)
	}
	if mode == "" {
		mode = caps.TabbingAuto
	}
	w.chrome.tabMode = mode
}

// SetTabbing sets the window's tabbing identifier and mode.
func (w *Window) SetTabbing(id, mode string) error {
	if _, ok := caps.TabbingModeValue(mode); !ok {
		return fmt.Errorf("tabbing mode must be auto, preferred or disallowed")
	}
	w.withWin(func() { w.setTabbing(id, mode) })
	return nil
}

// MergeAllWindows moves all windows with the same tabbing identifier into
// one tab group.
func (w *Window) MergeAllWindows() error {
	w.withWin(func() {
		w.win.Send(sel("mergeAllWindows:"), objc.ID(0))
		emitChrome(w, caps.ChromeTabsChanged, "")
	})
	return nil
}

// MoveTabToNewWindow takes the window out of its tab group.
func (w *Window) MoveTabToNewWindow() error {
	w.withWin(func() {
		w.win.Send(sel("moveTabToNewWindow:"), objc.ID(0))
		emitChrome(w, caps.ChromeTabsChanged, "")
	})
	return nil
}

// tabGroup lists the windows in w's tab group, in tab order; w alone when it
// has no tabs (main thread).
func (w *Window) tabGroup() []*Window {
	arr := w.win.Send(sel("tabbedWindows"))
	if arr == 0 {
		return []*Window{w}
	}
	n := objc.Send[uint](arr, sel("count"))
	out := make([]*Window, 0, n)
	for i := uint(0); i < n; i++ {
		if tw := w.app.windows[arr.Send(sel("objectAtIndex:"), i)]; tw != nil {
			out = append(out, tw)
		}
	}
	if len(out) == 0 {
		return []*Window{w}
	}
	return out
}

// Tabs lists the windows in the window's tab group.
func (w *Window) Tabs() (out []caps.WindowRef) {
	runOnMain(func() {
		if w.closed {
			return
		}
		withPool(func() {
			for _, tw := range w.tabGroup() {
				out = append(out, tw.Ref())
			}
		})
	})
	return
}

// newWindowForTab runs when the user clicks the "+" button of a tab bar.
func (a *App) newWindowForTab(sender objc.ID) {
	if key := a.nsApp.Send(sel("keyWindow")); key != 0 {
		if w := a.windows[key]; w != nil {
			emitChrome(w, caps.ChromeNewTab, "")
		}
	}
}

// SetActivationPolicy sets how the app appears: regular, accessory (no dock
// icon, no menu bar) or prohibited.
func (a *App) SetActivationPolicy(policy string) error {
	v, ok := caps.ActivationPolicyValue(policy)
	if !ok {
		return fmt.Errorf("activation policy must be regular, accessory or prohibited")
	}
	return a.mainCall(func() error {
		if !objc.Send[bool](a.nsApp, sel("setActivationPolicy:"), v) {
			return fmt.Errorf("macOS refused the activation policy %q", policy)
		}
		if policy == caps.PolicyRegular {
			a.nsApp.Send(sel("activateIgnoringOtherApps:"), true)
		}
		return nil
	})
}

// ActivationPolicy reports the current policy.
func (a *App) ActivationPolicy() (p string) {
	_ = a.mainCall(func() error {
		switch objc.Send[int](a.nsApp, sel("activationPolicy")) {
		case 1:
			p = caps.PolicyAccessory
		case 2:
			p = caps.PolicyProhibited
		default:
			p = caps.PolicyRegular
		}
		return nil
	})
	return
}

// SetDockVisible shows or hides the dock icon.
func (a *App) SetDockVisible(on bool) error {
	if on {
		return a.SetActivationPolicy(caps.PolicyRegular)
	}
	return a.SetActivationPolicy(caps.PolicyAccessory)
}

var sharePicker objc.ID

// Share shows the share picker anchored on the window's content view.
func (a *App) Share(parent caps.WindowRef, items caps.ShareItems) error {
	if err := items.Validate(); err != nil {
		return err
	}
	return a.mainCall(func() error {
		w := a.windowByRef(parent)
		if w == nil {
			for _, cand := range a.windows {
				w = cand
				break
			}
		}
		if w == nil {
			return fmt.Errorf("no window to anchor the share menu on")
		}
		arr := cls("NSMutableArray").Send(sel("array"))
		if items.Text != "" {
			arr.Send(sel("addObject:"), nsString(items.Text))
		}
		for _, u := range items.URLs {
			if url := cls("NSURL").Send(sel("URLWithString:"), nsString(u)); url != 0 {
				arr.Send(sel("addObject:"), url)
			}
		}
		for _, f := range items.Files {
			arr.Send(sel("addObject:"), cls("NSURL").Send(sel("fileURLWithPath:"), nsString(f)))
		}
		if sharePicker != 0 {
			release(sharePicker)
		}
		sharePicker = cls("NSSharingServicePicker").Send(sel("alloc")).Send(sel("initWithItems:"), arr)
		if sharePicker == 0 {
			return unsupported("NSSharingServicePicker is not available")
		}
		content := w.win.Send(sel("contentView"))
		sharePicker.Send(sel("showRelativeToRect:ofView:preferredEdge:"), objc.Send[nsRect](content, sel("bounds")), content, nsMinYEdge)
		return nil
	})
}
