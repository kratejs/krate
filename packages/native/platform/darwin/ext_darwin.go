//go:build darwin

package darwin

// Capability implementations for the SDK (compile-only in this release: they
// are written against AppKit through purego/objc but not exercised by tests).
//
//   - Dialogs: NSOpenPanel, NSSavePanel and NSAlert, run modally on the main
//     thread. Filters become allowed file types from "*.ext" patterns.
//   - Clipboard: NSPasteboard (text, PNG data, file URLs).
//   - Menus: the user's top-level items are inserted after the application
//     menu of the global menu bar; clicks come back through a small
//     Objective-C target class.
//   - Window controls: NSWindow level, fullscreen, zoom, miniaturize, style
//     mask. Unlike GTK there is no window manager variance.
//   - Tray (tray_darwin.go), global shortcuts (hotkey_darwin.go) and
//     notifications (notify_darwin.go) are implemented in their own files.

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.WindowControls = (*Window)(nil)
	_ caps.WindowEvents   = (*App)(nil)
	_ caps.Dialogs        = (*App)(nil)
	_ caps.Clipboard      = (*App)(nil)
	_ caps.Menus          = (*App)(nil)
)

const (
	nsWindowStyleMaskFullScreen = 1 << 14
	nsFloatingWindowLevel       = 3
	nsNormalWindowLevel         = 0
	nsModalResponseOK           = 1
	nsAlertFirstButtonReturn    = 1000
	nsControlStateOn            = 1
	nsControlStateOff           = 0
)

var (
	extMu    sync.Mutex
	nextRef  atomic.Uint64
	winFn    func(caps.WindowEvent)
	menuFn   func(caps.MenuEvent)
	menuTags = map[int]*menuEntry{}
	menuTag  int
	menuTgt  objc.ID
	userTop  []objc.ID // top-level items we inserted
)

type menuEntry struct {
	id    string
	check bool
}

// OnWindowEvent sets the handler for window events.
func (a *App) OnWindowEvent(fn func(caps.WindowEvent)) {
	extMu.Lock()
	winFn = fn
	extMu.Unlock()
}

func emitWindow(w *Window, kind caps.WindowEventKind) {
	extMu.Lock()
	fn := winFn
	extMu.Unlock()
	if fn != nil {
		fn(caps.WindowEvent{Window: w.Ref(), Kind: kind})
	}
}

// Ref identifies the window to the capability interfaces.
func (w *Window) Ref() caps.WindowRef { return caps.WindowRef(w.ref) }

func (w *Window) withWin(fn func()) {
	dispatch(func() {
		if !w.closed {
			withPool(fn)
		}
	})
}

// SetAlwaysOnTop uses the floating window level.
func (w *Window) SetAlwaysOnTop(on bool) {
	w.withWin(func() {
		lvl := nsNormalWindowLevel
		if on {
			lvl = nsFloatingWindowLevel
		}
		w.win.Send(sel("setLevel:"), lvl)
	})
}

func (w *Window) isFullscreen() bool {
	return objc.Send[uint](w.win, sel("styleMask"))&nsWindowStyleMaskFullScreen != 0
}

// SetFullscreen enters or leaves native fullscreen.
func (w *Window) SetFullscreen(on bool) {
	w.withWin(func() {
		if w.isFullscreen() != on {
			w.win.Send(sel("toggleFullScreen:"), objc.ID(0))
		}
	})
}

// SetDecorated(false) removes the title bar (borderless style mask).
func (w *Window) SetDecorated(on bool) {
	w.withWin(func() {
		mask := uint(nsWindowStyleMaskTitled | nsWindowStyleMaskClosable | nsWindowStyleMaskMiniaturizable | nsWindowStyleMaskResizable)
		if !on {
			mask = 0 // NSWindowStyleMaskBorderless
		}
		w.win.Send(sel("setStyleMask:"), mask)
	})
}

// SetPosition moves the window; y counts down from the top of the main screen.
func (w *Window) SetPosition(x, y int) {
	w.withWin(func() {
		wf := objc.Send[nsRect](w.win, sel("frame"))
		w.win.Send(sel("setFrameOrigin:"), nsPoint{X: float64(x), Y: primaryHeight() - float64(y) - wf.Size.Height})
	})
}

// Minimize miniaturizes the window.
func (w *Window) Minimize() {
	w.withWin(func() { w.win.Send(sel("miniaturize:"), objc.ID(0)) })
}

// Maximize zooms the window.
func (w *Window) Maximize() {
	w.withWin(func() {
		if !objc.Send[bool](w.win, sel("isZoomed")) {
			w.win.Send(sel("zoom:"), objc.ID(0))
		}
	})
}

// Restore undoes minimize, zoom and fullscreen.
func (w *Window) Restore() {
	w.withWin(func() {
		if w.isFullscreen() {
			w.win.Send(sel("toggleFullScreen:"), objc.ID(0))
		}
		if objc.Send[bool](w.win, sel("isMiniaturized")) {
			w.win.Send(sel("deminiaturize:"), objc.ID(0))
		}
		if objc.Send[bool](w.win, sel("isZoomed")) {
			w.win.Send(sel("zoom:"), objc.ID(0))
		}
	})
}

// Focus makes the window key and brings it forward.
func (w *Window) Focus() {
	w.withWin(func() {
		w.revive()
		w.win.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
	})
}

// Show orders the window in. A released window gets its webview back on the
// route and scroll position it had.
func (w *Window) Show() {
	w.withWin(func() {
		w.revive()
		w.win.Send(sel("orderFront:"), objc.ID(0))
	})
}

// Hide orders the window out. WebKit throttles the hidden page by itself; the
// page also gets a krate:memory-pressure event. Release also removes the
// webview.
func (w *Window) Hide() {
	w.withWin(func() {
		w.win.Send(sel("orderOut:"), objc.ID(0))
		w.memoryPressure()
	})
}

// SetPreventClose makes windowShouldClose: refuse and report instead.
func (w *Window) SetPreventClose(on bool) { w.withWin(func() { w.preventClose = on }) }

// State reads the window state.
func (w *Window) State() (st caps.WindowState) {
	runOnMain(func() {
		if w.closed {
			return
		}
		withPool(func() {
			f := objc.Send[nsRect](w.win, sel("frame"))
			st.X, st.Y = int(f.Origin.X), caps.TopLeftY(primaryHeight(), f.Origin.Y, f.Size.Height)
			st.Width, st.Height = int(f.Size.Width), int(f.Size.Height)
			st.Maximized = objc.Send[bool](w.win, sel("isZoomed"))
			st.Minimized = objc.Send[bool](w.win, sel("isMiniaturized"))
			st.Fullscreen = w.isFullscreen()
			st.AlwaysOnTop = objc.Send[int](w.win, sel("level")) >= nsFloatingWindowLevel
			st.Decorated = objc.Send[uint](w.win, sel("styleMask"))&nsWindowStyleMaskTitled != 0
			st.Visible = objc.Send[bool](w.win, sel("isVisible"))
			st.Focused = objc.Send[bool](w.win, sel("isKeyWindow"))
			st.Title = goString(w.win.Send(sel("title")))
		})
	})
	return
}

// FileDialog runs an open or save panel modally.
func (a *App) FileDialog(parent caps.WindowRef, o caps.FileDialogOptions) (paths []string, err error) {
	runOnMain(func() {
		withPool(func() {
			var panel objc.ID
			if o.Mode == caps.SaveFile {
				panel = cls("NSSavePanel").Send(sel("savePanel"))
				if o.Name != "" {
					panel.Send(sel("setNameFieldStringValue:"), nsString(o.Name))
				}
			} else {
				panel = cls("NSOpenPanel").Send(sel("openPanel"))
				panel.Send(sel("setCanChooseFiles:"), o.Mode == caps.OpenFile)
				panel.Send(sel("setCanChooseDirectories:"), o.Mode == caps.OpenFolder)
				panel.Send(sel("setAllowsMultipleSelection:"), o.Multiple)
			}
			if o.Title != "" {
				panel.Send(sel("setMessage:"), nsString(o.Title))
			}
			if o.AcceptLabel != "" {
				panel.Send(sel("setPrompt:"), nsString(o.AcceptLabel))
			}
			if o.Dir != "" {
				panel.Send(sel("setDirectoryURL:"), cls("NSURL").Send(sel("fileURLWithPath:"), nsString(o.Dir)))
			}
			panel.Send(sel("setShowsHiddenFiles:"), o.ShowHidden)
			if exts := extensions(o.Filters); len(exts) > 0 && o.Mode != caps.OpenFolder {
				arr := cls("NSMutableArray").Send(sel("array"))
				for _, e := range exts {
					arr.Send(sel("addObject:"), nsString(e))
				}
				panel.Send(sel("setAllowedFileTypes:"), arr)
			}
			if objc.Send[int](panel, sel("runModal")) != nsModalResponseOK {
				return
			}
			if o.Mode == caps.SaveFile {
				paths = []string{goString(panel.Send(sel("URL")).Send(sel("path")))}
				return
			}
			urls := panel.Send(sel("URLs"))
			n := objc.Send[uint](urls, sel("count"))
			for i := uint(0); i < n; i++ {
				paths = append(paths, goString(urls.Send(sel("objectAtIndex:"), i).Send(sel("path"))))
			}
		})
	})
	return paths, nil
}

// extensions turns "*.png" patterns into "png"; a "*" pattern means any file.
func extensions(fs []caps.FileFilter) []string {
	var out []string
	for _, f := range fs {
		for _, p := range f.Patterns {
			if p == "*" || p == "*.*" {
				return nil
			}
			if strings.HasPrefix(p, "*.") && !strings.ContainsAny(p[2:], "*?[") {
				out = append(out, p[2:])
			}
		}
	}
	return out
}

// MessageDialog runs an NSAlert modally.
func (a *App) MessageDialog(parent caps.WindowRef, o caps.MessageDialogOptions) (n int, err error) {
	n = -1
	runOnMain(func() {
		withPool(func() {
			alert := cls("NSAlert").Send(sel("new"))
			defer release(alert)
			msg := o.Message
			if msg == "" {
				msg = o.Title
			}
			alert.Send(sel("setMessageText:"), nsString(msg))
			if o.Detail != "" {
				alert.Send(sel("setInformativeText:"), nsString(o.Detail))
			}
			style := 1 // informational
			switch o.Kind {
			case caps.MessageWarning, caps.MessageQuestion:
				style = 0
			case caps.MessageError:
				style = 2
			}
			alert.Send(sel("setAlertStyle:"), style)
			buttons := o.Buttons
			if len(buttons) == 0 {
				buttons = []string{"OK"}
			}
			for _, b := range buttons {
				alert.Send(sel("addButtonWithTitle:"), nsString(b))
			}
			makeDefaultAlertButton(alert, o.Default)
			r := objc.Send[int](alert, sel("runModal"))
			if i := r - nsAlertFirstButtonReturn; i >= 0 && i < len(buttons) {
				n = i
			}
		})
	})
	return n, nil
}

func pasteboard() objc.ID { return cls("NSPasteboard").Send(sel("generalPasteboard")) }

// ReadText returns the clipboard text.
func (a *App) ReadText() (s string, err error) {
	runOnMain(func() {
		withPool(func() {
			s = goString(pasteboard().Send(sel("stringForType:"), nsString("public.utf8-plain-text")))
		})
	})
	return
}

// WriteText replaces the clipboard with text.
func (a *App) WriteText(s string) error {
	runOnMain(func() {
		withPool(func() {
			pb := pasteboard()
			pb.Send(sel("clearContents"))
			pb.Send(sel("setString:forType:"), nsString(s), nsString("public.utf8-plain-text"))
		})
	})
	return nil
}

// ReadImage returns PNG bytes.
func (a *App) ReadImage() (b []byte, err error) {
	runOnMain(func() {
		withPool(func() { b = dataBytes(pasteboard().Send(sel("dataForType:"), nsString("public.png"))) })
	})
	return
}

// WriteImage puts PNG bytes on the clipboard.
func (a *App) WriteImage(png []byte) error {
	if len(png) == 0 {
		return errors.New("darwin: empty image")
	}
	runOnMain(func() {
		withPool(func() {
			data := cls("NSData").Send(sel("dataWithBytes:length:"), &png[0], uint(len(png)))
			pb := pasteboard()
			pb.Send(sel("clearContents"))
			pb.Send(sel("setData:forType:"), data, nsString("public.png"))
		})
	})
	return nil
}

// ReadFiles returns the file URLs on the clipboard as paths.
func (a *App) ReadFiles() (paths []string, err error) {
	runOnMain(func() {
		withPool(func() {
			classes := cls("NSArray").Send(sel("arrayWithObject:"), cls("NSURL"))
			urls := pasteboard().Send(sel("readObjectsForClasses:options:"), classes, objc.ID(0))
			if urls == 0 {
				return
			}
			n := objc.Send[uint](urls, sel("count"))
			for i := uint(0); i < n; i++ {
				paths = append(paths, goString(urls.Send(sel("objectAtIndex:"), i).Send(sel("path"))))
			}
		})
	})
	return
}

// WriteFiles puts file URLs on the clipboard.
func (a *App) WriteFiles(paths []string) error {
	runOnMain(func() {
		withPool(func() {
			arr := cls("NSMutableArray").Send(sel("array"))
			for _, p := range paths {
				arr.Send(sel("addObject:"), cls("NSURL").Send(sel("fileURLWithPath:"), nsString(p)))
			}
			pb := pasteboard()
			pb.Send(sel("clearContents"))
			pb.Send(sel("writeObjects:"), arr)
		})
	})
	return nil
}

// Clear empties the clipboard.
func (a *App) Clear() error {
	runOnMain(func() { pasteboard().Send(sel("clearContents")) })
	return nil
}

// OnMenu sets the handler for menu clicks.
func (a *App) OnMenu(fn func(caps.MenuEvent)) {
	extMu.Lock()
	menuFn = fn
	extMu.Unlock()
}

// menuClicked is the action of every item we create.
func (a *App) menuClicked(item objc.ID) {
	tag := objc.Send[int](item, sel("tag"))
	extMu.Lock()
	e := menuTags[tag]
	fn := menuFn
	extMu.Unlock()
	if e == nil {
		return
	}
	checked := false
	if e.check {
		checked = objc.Send[int](item, sel("state")) != nsControlStateOn
		st := nsControlStateOff
		if checked {
			st = nsControlStateOn
		}
		item.Send(sel("setState:"), st)
	}
	if fn != nil {
		fn(caps.MenuEvent{ID: e.id, Checked: checked})
	}
}

func buildNSMenu(title string, items []caps.MenuItem) objc.ID {
	m := newMenu(title)
	// Items are enabled by this flag alone, not by the responder chain.
	m.Send(sel("setAutoenablesItems:"), false)
	for _, it := range items {
		if it.Kind == caps.MenuItemSeparator {
			m.Send(sel("addItem:"), cls("NSMenuItem").Send(sel("separatorItem")))
			continue
		}
		label := menuLabel(it.Label)
		key, mask := accelKeyEquivalent(it.Accelerator)
		item := menuItem(label, sel("krateMenu:"), key, mask)
		if len(it.Submenu) > 0 || it.Kind == caps.MenuItemSubmenu {
			item.Send(sel("setSubmenu:"), buildNSMenu(label, it.Submenu))
		} else {
			extMu.Lock()
			menuTag++
			tag := menuTag
			menuTags[tag] = &menuEntry{id: it.ID, check: it.Kind == caps.MenuItemCheck || it.Kind == caps.MenuItemRadio}
			extMu.Unlock()
			item.Send(sel("setTag:"), tag)
			item.Send(sel("setTarget:"), menuTgt)
			if it.Checked {
				item.Send(sel("setState:"), nsControlStateOn)
			}
		}
		if it.Disabled {
			item.Send(sel("setEnabled:"), false)
		}
		m.Send(sel("addItem:"), item)
	}
	return m
}

func (a *App) ensureMenuTarget() {
	if menuTgt == 0 {
		menuTgt = cls("KrateMenuTarget").Send(sel("new"))
	}
}

// SetAppMenu inserts the items after the application menu.
func (a *App) SetAppMenu(items []caps.MenuItem) error {
	runOnMain(func() {
		withPool(func() {
			a.ensureMenuTarget()
			bar := a.nsApp.Send(sel("mainMenu"))
			for _, it := range userTop {
				bar.Send(sel("removeItem:"), it)
				release(it)
			}
			userTop = nil
			for i, it := range items {
				label := menuLabel(it.Label)
				// The menu bar item carries the label too: VoiceOver reads the
				// item's title, and an empty one would leave the menu unnamed.
				top := menuItem(label, 0, "", 0)
				top.Send(sel("setSubmenu:"), buildNSMenu(label, it.Submenu))
				retain(top)
				bar.Send(sel("insertItem:atIndex:"), top, 1+i)
				userTop = append(userTop, top)
			}
		})
	})
	return nil
}

// PopupMenu shows a context menu at the pointer (x and y are ignored).
func (a *App) PopupMenu(parent caps.WindowRef, items []caps.MenuItem, x, y int) error {
	dispatch(func() {
		withPool(func() {
			a.ensureMenuTarget()
			m := buildNSMenu("", items)
			loc := objc.Send[nsPoint](cls("NSEvent"), sel("mouseLocation"))
			m.Send(sel("popUpMenuPositioningItem:atLocation:inView:"), objc.ID(0), loc, objc.ID(0))
		})
	})
	return nil
}

// mainCall runs fn on the main thread inside an autorelease pool and returns
// its error. Before the app is running, off the main thread, there is no loop
// to run fn, so it fails instead of blocking forever.
func (a *App) mainCall(fn func() error) error {
	if !a.ready.Load() && !isMainThread() {
		return errors.New("darwin: the app is not running yet; call this after Run's onReady callback")
	}
	var err error
	runOnMain(func() { withPool(func() { err = fn() }) })
	return err
}
