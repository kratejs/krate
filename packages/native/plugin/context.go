package plugin

import (
	"fmt"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/scheme"
	sdkos "github.com/kratejs/krate/packages/native/sdk/os"
)

// Context is what a plugin's Init receives: its registrar, lifecycle hooks and
// access to the platform. It is safe for use from any goroutine.
type Context struct {
	h  *Host
	ip *installed
}

// Name is the plugin's name.
func (c *Context) Name() string { return c.ip.p.Meta.Name }

// AppID returns the app's id from its manifest.
func (c *Context) AppID() string { return c.h.opts.AppID }

// AppName returns the app's product name.
func (c *Context) AppName() string { return c.h.opts.AppName }

// AppVersion returns the app's version.
func (c *Context) AppVersion() string { return c.h.opts.AppVersion }

// Dev reports whether this is a development run.
func (c *Context) Dev() bool { return c.h.opts.Dev }

// Logf writes a diagnostic line tagged with the plugin's name.
func (c *Context) Logf(format string, args ...any) {
	c.h.opts.Logf("[plugin %s] %s", c.ip.p.Meta.Name, fmt.Sprintf(format, args...))
}

// AppDataDir is the app's data directory (the same as sdk/os.AppDataDir).
func (c *Context) AppDataDir() (string, error) { return sdkos.AppDataDir() }

// DataDir is a directory private to this plugin inside the app's data
// directory, created on first use.
func (c *Context) DataDir() (string, error) { return c.h.dataDir(c.ip.p.Meta.Name) }

// Dispatch runs fn on the platform's main thread and returns without waiting.
// Before the platform has started it queues fn; in browser mode, where there
// is no main thread, it runs fn on its own goroutine. A panic in fn is logged.
func (c *Context) Dispatch(fn func()) { c.h.dispatch(fn) }

// App returns the platform application, or nil before it started and in
// browser mode. Type-assert it to the optional interfaces of platform/caps.
func (c *Context) App() platform.App {
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	return c.h.app
}

// Windows lists the SDK ids of the open windows.
func (c *Context) Windows() []string {
	c.h.mu.Lock()
	w := c.h.wins
	c.h.mu.Unlock()
	if w == nil {
		return nil
	}
	return w.WindowIDs()
}

// Window returns the platform window with this SDK id ("" = main).
func (c *Context) Window(id string) (platform.Window, bool) {
	c.h.mu.Lock()
	w := c.h.wins
	c.h.mu.Unlock()
	if w == nil {
		return nil, false
	}
	win, _, ok := w.Window(id)
	return win, ok
}

func capOf[T any](c *Context, what, hint string) (T, error) {
	var zero T
	app := c.App()
	if app == nil {
		return zero, fmt.Errorf("%w: %s needs the platform, which is not running (browser mode, or before OnStart)", krate.ErrUnsupported, what)
	}
	v, ok := app.(T)
	if !ok {
		return zero, fmt.Errorf("%w: %s is not available on this platform%s", krate.ErrUnsupported, what, hint)
	}
	return v, nil
}

// Dialogs returns the platform's native dialogs.
func (c *Context) Dialogs() (caps.Dialogs, error) { return capOf[caps.Dialogs](c, "dialogs", "") }

// Menus returns the platform's menus.
func (c *Context) Menus() (caps.Menus, error) { return capOf[caps.Menus](c, "menus", "") }

// Clipboard returns the platform's clipboard.
func (c *Context) Clipboard() (caps.Clipboard, error) {
	return capOf[caps.Clipboard](c, "the clipboard", "")
}

// Tray returns the platform's tray capability. Where the platform application
// has none (Linux draws the tray over D-Bus), use the sdk/tray package.
func (c *Context) Tray() (caps.Tray, error) {
	return capOf[caps.Tray](c, "the tray", "; the sdk/tray package covers the desktops that have one")
}

// Notifier returns the platform's notification capability. Where the platform
// application has none (Linux sends them over D-Bus), use the sdk/notify package.
func (c *Context) Notifier() (caps.Notifier, error) {
	return capOf[caps.Notifier](c, "notifications", "; the sdk/notify package covers the desktops that have one")
}

// WindowControls returns the controls of the window with this SDK id.
func (c *Context) WindowControls(id string) (caps.WindowControls, error) {
	win, ok := c.Window(id)
	if !ok {
		return nil, krate.Errorf("not_found", "no window %q", id)
	}
	wc, ok := win.(caps.WindowControls)
	if !ok {
		return nil, fmt.Errorf("%w: window controls are not available on this platform", krate.ErrUnsupported)
	}
	return wc, nil
}

// OnStart runs fn once the platform is up, on the main thread, before the
// first window opens. A failure is logged.
func (c *Context) OnStart(fn func() error) {
	c.addHook(func() { c.ip.onStart = append(c.ip.onStart, fn) })
}

// OnWindowCreated runs fn for every window the app opens, the main one included.
// Extra views (split panes) added to a window do not count: the id is a window's.
func (c *Context) OnWindowCreated(fn func(id string)) {
	c.addHook(func() { c.ip.onCreated = append(c.ip.onCreated, fn) })
}

// OnWindowClosed runs fn for every window that closed.
func (c *Context) OnWindowClosed(fn func(id string)) {
	c.addHook(func() { c.ip.onClosed = append(c.ip.onClosed, fn) })
}

// OnQuit runs fn when the app quits, last registered first.
func (c *Context) OnQuit(fn func()) { c.addHook(func() { c.ip.onQuit = append(c.ip.onQuit, fn) }) }

func (c *Context) addHook(add func()) {
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	add()
}

// Func registers a hand-written op. The manifest must declare op with
// "manual": true; the permission and the host id (plugin/<name>.<op>) come from
// it, and anything else is refused with a *NamespaceError.
func (c *Context) Func(op string, f scheme.Func) error {
	info, err := c.manualOp(op)
	if err != nil {
		return err
	}
	return protect(func() error { c.h.registerFunc(c.ip.p.Meta.Name, info, OpID(c.ip.p.Meta.Name, op), f); return nil })
}

// Stream registers a hand-written streaming op (see Func).
func (c *Context) Stream(op string, f scheme.StreamFunc) error {
	info, err := c.manualOp(op)
	if err != nil {
		return err
	}
	return protect(func() error { c.h.registerStream(c.ip.p.Meta.Name, info, OpID(c.ip.p.Meta.Name, op), f); return nil })
}

func (c *Context) manualOp(op string) (OpInfo, error) {
	name := c.ip.p.Meta.Name
	info, ok := c.ip.p.Meta.op(op)
	if !ValidOp(op) || !ok || !info.Manual {
		return OpInfo{}, &NamespaceError{Plugin: name, ID: op, Undeclared: ValidOp(op)}
	}
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	if c.ip.manual[op] {
		return OpInfo{}, fmt.Errorf("plugin %s: op %q is registered twice", name, op)
	}
	c.ip.manual[op] = true
	return info, nil
}
