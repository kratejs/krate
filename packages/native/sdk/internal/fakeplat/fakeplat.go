// Package fakeplat is an in-memory platform.App with every optional
// capability, for tests of the SDK layers above the platform.
package fakeplat

import (
	"errors"
	"sync"

	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// App records what the SDK asks of it.
type App struct {
	mu sync.Mutex

	Windows  []*Window
	nextRef  caps.WindowRef
	nextView caps.ViewRef
	viewRecs map[caps.ViewRef]*viewRec
	viewFn   func(caps.ViewEvent)
	openFn   func([]string)
	dropFn   func(caps.FileDrop)
	urlFn    func([]string)
	Quit_    int

	// Dialogs
	FileResult   []string
	FileOpts     []caps.FileDialogOptions
	MessageOpts  []caps.MessageDialogOptions
	MessageReply int

	// Clipboard
	Text  string
	Image []byte
	Files []string

	// Menus
	AppMenu    []caps.MenuItem
	Popups     [][]caps.MenuItem
	menuFn     func(caps.MenuEvent)
	winFn      func(caps.WindowEvent)
	shortcutFn func(string)
	Shortcuts  map[string]caps.Accelerator
}

var (
	_ platform.App        = (*App)(nil)
	_ caps.Dialogs        = (*App)(nil)
	_ caps.Clipboard      = (*App)(nil)
	_ caps.Menus          = (*App)(nil)
	_ caps.WindowEvents   = (*App)(nil)
	_ caps.Shortcuts      = (*App)(nil)
	_ caps.WindowControls = (*Window)(nil)
)

// New returns an App.
func New() *App { return &App{Shortcuts: map[string]caps.Accelerator{}} }

func (a *App) Run(onReady func()) error { onReady(); return nil }
func (a *App) Dispatch(fn func())       { fn() }
func (a *App) Origin() string           { return "krate://app" }
func (a *App) Quit() {
	a.mu.Lock()
	a.Quit_++
	a.mu.Unlock()
}

// QuitCalls is how often Quit ran.
func (a *App) QuitCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Quit_
}

// NewWindow creates a Window.
func (a *App) NewWindow(o platform.WindowOptions) (platform.Window, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextRef++
	w := &Window{app: a, ref: a.nextRef, Opts: o, St: caps.WindowState{Width: o.Width, Height: o.Height, Title: o.Title, Decorated: true, Visible: true}}
	a.Windows = append(a.Windows, w)
	return w, nil
}

// EmitWindow delivers a window event as the platform would.
func (a *App) EmitWindow(w *Window, kind caps.WindowEventKind) {
	a.mu.Lock()
	fn := a.winFn
	if kind == caps.WindowClosed {
		// A closed window's views are gone, as on a real platform.
		for ref, v := range a.viewRecs {
			if v.win == w {
				delete(a.viewRecs, ref)
			}
		}
	}
	a.mu.Unlock()
	if fn != nil {
		fn(caps.WindowEvent{Window: w.ref, Kind: kind})
	}
}

// EmitMenu delivers a menu click.
func (a *App) EmitMenu(ev caps.MenuEvent) {
	a.mu.Lock()
	fn := a.menuFn
	a.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// EmitShortcut delivers a shortcut press.
func (a *App) EmitShortcut(handle string) {
	a.mu.Lock()
	fn := a.shortcutFn
	a.mu.Unlock()
	if fn != nil {
		fn(handle)
	}
}

func (a *App) OnWindowEvent(fn func(caps.WindowEvent)) { a.mu.Lock(); a.winFn = fn; a.mu.Unlock() }
func (a *App) OnMenu(fn func(caps.MenuEvent))          { a.mu.Lock(); a.menuFn = fn; a.mu.Unlock() }
func (a *App) OnShortcut(fn func(string))              { a.mu.Lock(); a.shortcutFn = fn; a.mu.Unlock() }

func (a *App) FileDialog(parent caps.WindowRef, o caps.FileDialogOptions) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.FileOpts = append(a.FileOpts, o)
	return a.FileResult, nil
}

func (a *App) MessageDialog(parent caps.WindowRef, o caps.MessageDialogOptions) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.MessageOpts = append(a.MessageOpts, o)
	return a.MessageReply, nil
}

func (a *App) ReadText() (string, error) { a.mu.Lock(); defer a.mu.Unlock(); return a.Text, nil }
func (a *App) WriteText(s string) error  { a.mu.Lock(); a.Text = s; a.mu.Unlock(); return nil }
func (a *App) ReadImage() ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Image, nil
}
func (a *App) WriteImage(p []byte) error { a.mu.Lock(); a.Image = p; a.mu.Unlock(); return nil }
func (a *App) ReadFiles() ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Files, nil
}
func (a *App) WriteFiles(p []string) error { a.mu.Lock(); a.Files = p; a.mu.Unlock(); return nil }
func (a *App) Clear() error {
	a.mu.Lock()
	a.Text, a.Image, a.Files = "", nil, nil
	a.mu.Unlock()
	return nil
}

func (a *App) SetAppMenu(items []caps.MenuItem) error {
	a.mu.Lock()
	a.AppMenu = items
	a.mu.Unlock()
	return nil
}
func (a *App) PopupMenu(parent caps.WindowRef, items []caps.MenuItem, x, y int) error {
	a.mu.Lock()
	a.Popups = append(a.Popups, items)
	a.mu.Unlock()
	return nil
}

func (a *App) RegisterShortcut(h string, acc caps.Accelerator) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, dup := a.Shortcuts[h]; dup {
		return errors.New("duplicate")
	}
	a.Shortcuts[h] = acc
	return nil
}
func (a *App) UnregisterShortcut(h string) error {
	a.mu.Lock()
	delete(a.Shortcuts, h)
	a.mu.Unlock()
	return nil
}

// Window is a recorded window.
type Window struct {
	app  *App
	ref  caps.WindowRef
	Opts platform.WindowOptions

	mu       sync.Mutex
	St       caps.WindowState
	Calls    []string
	released bool
	Path     string

	pv     *viewRec
	extra  []*viewRec
	layout *caps.LayoutNode
}

func (w *Window) rec(s string) { w.mu.Lock(); w.Calls = append(w.Calls, s); w.mu.Unlock() }

// Called lists the recorded calls.
func (w *Window) Called() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.Calls...)
}

func (w *Window) Navigate(p string) { w.rec("navigate " + p); w.mu.Lock(); w.Path = p; w.mu.Unlock() }
func (w *Window) Eval(js string)    { w.rec("eval") }
func (w *Window) SetTitle(t string) { w.rec("title " + t); w.mu.Lock(); w.St.Title = t; w.mu.Unlock() }
func (w *Window) SetSize(a, b int) {
	w.rec("size")
	w.mu.Lock()
	w.St.Width, w.St.Height = a, b
	w.mu.Unlock()
}
func (w *Window) Close() { w.rec("close") }

func (w *Window) Ref() caps.WindowRef { return w.ref }
func (w *Window) SetAlwaysOnTop(on bool) {
	w.rec("above")
	w.mu.Lock()
	w.St.AlwaysOnTop = on
	w.mu.Unlock()
}
func (w *Window) SetFullscreen(on bool) {
	w.rec("fullscreen")
	w.mu.Lock()
	w.St.Fullscreen = on
	w.mu.Unlock()
}
func (w *Window) SetDecorated(on bool) {
	w.rec("decorated")
	w.mu.Lock()
	w.St.Decorated = on
	w.mu.Unlock()
}
func (w *Window) SetPosition(x, y int) {
	w.rec("position")
	w.mu.Lock()
	w.St.X, w.St.Y = x, y
	w.mu.Unlock()
}
func (w *Window) Minimize() { w.rec("minimize") }
func (w *Window) Maximize() {
	w.rec("maximize")
	w.mu.Lock()
	w.St.Maximized = true
	w.mu.Unlock()
}
func (w *Window) Restore() {
	w.rec("restore")
	w.mu.Lock()
	w.St.Maximized, w.St.Fullscreen = false, false
	w.mu.Unlock()
}
func (w *Window) Focus() { w.rec("focus") }
func (w *Window) Show()  { w.rec("show") }
func (w *Window) Hide()  { w.rec("hide") }

// Release and Released implement caps.WindowRelease.
func (w *Window) Release() {
	w.rec("release")
	w.mu.Lock()
	w.released = true
	w.mu.Unlock()
}
func (w *Window) Released() bool { w.mu.Lock(); defer w.mu.Unlock(); return w.released }
func (w *Window) SetPreventClose(on bool) {
	w.rec("preventclose")
}
func (w *Window) State() caps.WindowState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.St
}

// ShortcutHandles lists the registered shortcut handles.
func (a *App) ShortcutHandles() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for h := range a.Shortcuts {
		out = append(out, h)
	}
	return out
}
