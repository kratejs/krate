package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/scheme"
	sdkos "github.com/kratejs/krate/packages/native/sdk/os"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

// WindowAccess is how the host reads the open windows; sdk.Host implements it.
type WindowAccess interface {
	Window(id string) (platform.Window, string, bool)
	WindowIDs() []string
}

// HostOptions configure a Host.
type HostOptions struct {
	// Registry is the app registry the ops are registered on.
	Registry *scheme.Registry
	// Perms is the app's permission set; the per-window scopes it holds apply
	// to plugin ops.
	Perms permission.Set
	// AppID, AppName and AppVersion come from the manifest.
	AppID, AppName, AppVersion string
	// Dev is true in a development run.
	Dev  bool
	Logf func(format string, args ...any)
}

// DefinePermissions makes the permissions the plugins declare known to the
// permission parser. Call it before parsing the app's permission list.
func DefinePermissions(plugins []*Plugin) error {
	for _, p := range plugins {
		if err := p.Meta.Validate(); err != nil {
			return err
		}
		for _, pi := range p.Meta.Permissions {
			doc := pi.Doc
			if doc == "" {
				doc = "permission of the plugin " + p.Meta.Name
			}
			permission.DefineOnce(permission.Info{Name: PermissionName(p.Meta.Name, pi.Name), Doc: doc + " (plugin " + p.Meta.Name + ")"})
		}
	}
	return nil
}

// Host runs the plugins of one app.
type Host struct {
	opts HostOptions

	mu      sync.Mutex
	plugins []*installed
	names   map[string]bool
	app     platform.App
	wins    WindowAccess
	started bool
	quit    bool
	queue   []func()
}

type installed struct {
	p   *Plugin
	ctx *Context

	onStart   []func() error
	onCreated []func(id string)
	onClosed  []func(id string)
	onQuit    []func()
	// manual ops registered so far.
	manual map[string]bool
}

// NewHost returns a Host with no plugins.
func NewHost(o HostOptions) *Host {
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	return &Host{opts: o, names: map[string]bool{}}
}

// Names lists the installed plugins, sorted.
func (h *Host) Names() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.plugins))
	for _, ip := range h.plugins {
		out = append(out, ip.p.Meta.Name)
	}
	sort.Strings(out)
	return out
}

// Install registers the plugin's ops and runs its Init. Everything the glue
// registers is collected first and checked: an id outside the plugin's
// namespace, or an op the manifest does not declare, fails the install with a
// *NamespaceError and nothing of the plugin is registered.
func (h *Host) Install(p *Plugin) error {
	if err := p.Meta.Validate(); err != nil {
		return err
	}
	h.mu.Lock()
	if h.names[p.Meta.Name] {
		h.mu.Unlock()
		return fmt.Errorf("plugin %s is installed twice", p.Meta.Name)
	}
	h.mu.Unlock()

	tmp := scheme.NewRegistry()
	if p.Register != nil {
		if err := protect(func() error { p.Register(tmp); return nil }); err != nil {
			return fmt.Errorf("plugin %s: registering ops: %w", p.Meta.Name, err)
		}
	}
	snap := tmp.Snapshot()
	infos := map[string]OpInfo{}
	check := func(id string) error {
		op, err := p.Meta.opFromID(id)
		if err != nil {
			return err
		}
		info, ok := p.Meta.op(op)
		if !ok || info.Manual {
			return &NamespaceError{Plugin: p.Meta.Name, ID: id, Undeclared: true}
		}
		infos[id] = info
		return nil
	}
	for id := range snap.Funcs {
		if err := check(id); err != nil {
			return err
		}
	}
	for id := range snap.Streams {
		if err := check(id); err != nil {
			return err
		}
	}
	for id := range snap.Signals {
		if err := check(id); err != nil {
			return err
		}
	}

	ip := &installed{p: p, manual: map[string]bool{}}
	ip.ctx = &Context{h: h, ip: ip}
	h.mu.Lock()
	h.names[p.Meta.Name] = true
	h.plugins = append(h.plugins, ip)
	h.mu.Unlock()

	err := protect(func() error {
		for id, f := range snap.Funcs {
			h.registerFunc(p.Meta.Name, infos[id], id, f)
		}
		for id, f := range snap.Streams {
			h.registerStream(p.Meta.Name, infos[id], id, f)
		}
		for id, f := range snap.Signals {
			h.registerSignal(p.Meta.Name, infos[id], id, f)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("plugin %s: %w", p.Meta.Name, err)
	}
	if p.Init != nil {
		if err := protect(func() error { return p.Init(ip.ctx) }); err != nil {
			return fmt.Errorf("plugin %s: Init: %w", p.Meta.Name, err)
		}
	}
	return nil
}

// permOf returns the full permission name of op, "" when ungated.
func permOf(plugin string, op OpInfo) string {
	if op.Permission == "" {
		return ""
	}
	return PermissionName(plugin, op.Permission)
}

func (h *Host) allowed(window, perm string) bool {
	return perm == "" || h.opts.Perms.AllowsFor(window, perm)
}

func (h *Host) registerFunc(plugin string, op OpInfo, id string, f scheme.Func) {
	reg, perm := h.opts.Registry, permOf(plugin, op)
	reg.Trusted(id, f)
	if !h.allowed("", perm) {
		reg.Func(id, func(krate.Ctx, *wire.Decoder, *wire.Encoder) error { return permission.Denied(perm) })
		return
	}
	reg.Func(id, func(ctx krate.Ctx, a *wire.Decoder, out *wire.Encoder) error {
		if !h.allowed(ctx.Window(), perm) {
			return permission.Denied(perm)
		}
		return f(ctx, a, out)
	})
}

func (h *Host) registerStream(plugin string, op OpInfo, id string, f scheme.StreamFunc) {
	reg, perm := h.opts.Registry, permOf(plugin, op)
	reg.TrustedStream(id, f)
	if !h.allowed("", perm) {
		reg.Stream(id, func(krate.Ctx, *wire.Decoder, func(func(*wire.Encoder)) error) error { return permission.Denied(perm) })
		return
	}
	reg.Stream(id, func(ctx krate.Ctx, a *wire.Decoder, emit func(func(*wire.Encoder)) error) error {
		if !h.allowed(ctx.Window(), perm) {
			return permission.Denied(perm)
		}
		return f(ctx, a, emit)
	})
}

func (h *Host) registerSignal(plugin string, op OpInfo, id string, f scheme.SignalFunc) {
	reg, perm := h.opts.Registry, permOf(plugin, op)
	reg.Signal(id, f)
	if perm != "" {
		reg.SignalGate(id, func(window string) error {
			if !h.allowed(window, perm) {
				return permission.Denied(perm)
			}
			return nil
		})
	}
}

// Start tells the plugins the platform is up (app is nil in browser mode):
// queued Dispatch calls run, then the OnStart hooks, in install order. It
// must run on the platform's main thread in native mode. A hook that fails is
// logged and the app keeps running.
func (h *Host) Start(app platform.App, wins WindowAccess) {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return
	}
	h.started, h.app, h.wins = true, app, wins
	queued := h.queue
	h.queue = nil
	plugins := append([]*installed(nil), h.plugins...)
	h.mu.Unlock()
	for _, fn := range queued {
		h.run("queued dispatch", fn)
	}
	for _, ip := range plugins {
		h.mu.Lock()
		hooks := append([]func() error(nil), ip.onStart...)
		h.mu.Unlock()
		for _, fn := range hooks {
			if err := protect(fn); err != nil {
				h.opts.Logf("plugin %s: start hook: %v", ip.p.Meta.Name, err)
			}
		}
	}
}

// WindowCreated forwards a new window to the OnWindowCreated hooks.
func (h *Host) WindowCreated(id string) {
	h.windowEvent(id, func(ip *installed) []func(string) { return ip.onCreated })
}

// WindowClosed forwards a closed window to the OnWindowClosed hooks.
func (h *Host) WindowClosed(id string) {
	h.windowEvent(id, func(ip *installed) []func(string) { return ip.onClosed })
}

func (h *Host) windowEvent(id string, pick func(*installed) []func(string)) {
	h.mu.Lock()
	type job struct {
		name string
		fns  []func(string)
	}
	var jobs []job
	for _, ip := range h.plugins {
		jobs = append(jobs, job{ip.p.Meta.Name, append([]func(string){}, pick(ip)...)})
	}
	h.mu.Unlock()
	for _, j := range jobs {
		for _, fn := range j.fns {
			fn := fn
			h.run("window hook of plugin "+j.name, func() { fn(id) })
		}
	}
}

// Quit runs the OnQuit hooks, last installed plugin first, once.
func (h *Host) Quit() {
	h.mu.Lock()
	if h.quit {
		h.mu.Unlock()
		return
	}
	h.quit = true
	type job struct {
		name string
		fns  []func()
	}
	var jobs []job
	for _, ip := range h.plugins {
		jobs = append(jobs, job{ip.p.Meta.Name, append([]func(){}, ip.onQuit...)})
	}
	h.mu.Unlock()
	for i := len(jobs) - 1; i >= 0; i-- {
		for j := len(jobs[i].fns) - 1; j >= 0; j-- {
			h.run("quit hook of plugin "+jobs[i].name, jobs[i].fns[j])
		}
	}
}

func (h *Host) run(what string, fn func()) {
	if err := protect(func() error { fn(); return nil }); err != nil {
		h.opts.Logf("%s: %v", what, err)
	}
}

func (h *Host) dispatch(fn func()) {
	h.mu.Lock()
	app, started := h.app, h.started
	if !started {
		h.queue = append(h.queue, fn)
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	wrapped := func() { h.run("dispatched function", fn) }
	if app != nil {
		app.Dispatch(wrapped)
		return
	}
	go wrapped()
}

// dataDir is <app data dir>/plugins/<name>, created on first use.
func (h *Host) dataDir(name string) (string, error) {
	base, err := sdkos.AppDataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "plugins", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func protect(f func() error) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("panic: %v", v)
		}
	}()
	return f()
}
