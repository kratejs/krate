// Package shell is the Krate Native shell as a library: it loads an app
// directory (see README.md), serves the built UI on the app origin, runs the
// app's TypeScript host code in-process on QuickJS, runs its Go host code in a
// separate child process when it has any, and opens the window.
//
// The prebuilt krate-shell binary is a thin main around Run. Release builds
// link everything into one process and use the same pieces.
package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kratejs/krate/packages/native/gohost"
	"github.com/kratejs/krate/packages/native/internal/memory"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/plugin"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk"
	sdkall "github.com/kratejs/krate/packages/native/sdk/all"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	sdkupdater "github.com/kratejs/krate/packages/native/sdk/updater"
)

// RunOptions configures Run.
type RunOptions struct {
	// Browser serves the app on a loopback port behind a per-session token
	// instead of opening a window.
	Browser bool
	// Addr is the browser-mode listen address. Default "127.0.0.1:0".
	Addr string
	// Dev enables developer behavior: panic stacks in errors, mapped JS
	// stacks, webview developer tools.
	Dev bool
	// OnReady runs on its own goroutine once the window is open (or the
	// browser listener is up). The Control stays valid until Run returns.
	OnReady func(ctl *Control)
	// ExtraHandler is consulted before the app handler (and after the token
	// check in browser mode); returning true means it handled the request.
	// Meant for tests and embedders.
	ExtraHandler func(w http.ResponseWriter, r *http.Request) bool
	// Logf receives diagnostics. Default: stderr.
	Logf func(format string, args ...any)
	// Out receives the "url ..." line in browser mode. Default os.Stdout.
	Out io.Writer
	// Context, when done, quits the app.
	Context context.Context
	// GoGlue registers Go host code linked into the process (a release build)
	// on the app registry, instead of starting a Go host child.
	GoGlue func(r *scheme.Registry)
	// Plugins are the native plugins linked into this process. Those the
	// manifest lists under "plugins" are installed; a plugin the manifest
	// lists and the process lacks is an error.
	Plugins []*plugin.Plugin
	// Allowed, when set, replaces the manifest's permission set for the UI
	// half of the SDK (window, dialog, clipboard, ...); the data areas
	// always follow the manifest. Meant for tests. Default: the manifest's
	// "permissions" (or the defaults when it lists none).
	Allowed func(permission string) bool
}

// Run loads the app in appDir and runs it until the window closes (or, in
// browser mode, until Quit or Context). In native mode it must be called from
// the main goroutine of a process that locked the main thread
// (runtime.LockOSThread in an init function).
func Run(appDir string, opts RunOptions) error {
	if opts.Logf == nil {
		opts.Logf = func(format string, args ...any) { fmt.Fprintf(os.Stderr, "krate-shell: "+format+"\n", args...) }
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	a, err := load(appDir, opts)
	if err != nil {
		return err
	}
	defer a.close()
	defer a.startMemory()()
	if opts.Browser {
		return a.runBrowser()
	}
	return a.runNative()
}

// app is a loaded app directory.
type app struct {
	dir  string // "" for an embedded app
	fsys fs.FS  // the app directory
	opts RunOptions
	logf func(string, ...any)

	reg     *scheme.Registry
	handler http.Handler

	mu    sync.Mutex // guards man, host, child
	man   *Manifest
	host  jsHost
	child *gohost.Child
	// hostChildren are the running JavaScript host children by entry path.
	hostChildren map[string]*hostChild

	ctl  *Control
	quit func() // set by the run mode

	results queryResults // dev "query" mailbox
	dev     *devTools
	sdk     *sdk.Host
	plug    *plugin.Host                 // nil without plugins
	perms   permission.Set               // from the manifest; read once at load
	glued   bool                         // opts.GoGlue was registered
	plat    atomic.Pointer[platform.App] // native mode only, set before the loop runs
	reaper  *memory.Reaper               // idle trim; nil under the performance profile
}

func load(appDir string, opts RunOptions) (*app, error) {
	dir, err := filepath.Abs(appDir)
	if err != nil {
		return nil, err
	}
	return loadApp(dir, os.DirFS(dir), opts)
}

// loadApp loads the app held in fsys; dir is its directory on disk, or "" for
// an embedded app.
func loadApp(dir string, fsys fs.FS, opts RunOptions) (*app, error) {
	// Plugin permissions must be known before the manifest is parsed.
	if err := plugin.DefinePermissions(opts.Plugins); err != nil {
		return nil, fmt.Errorf("shell: %w", err)
	}
	man, err := ReadManifestFS(fsys)
	if err != nil {
		return nil, pluginHint(fsys, opts, err)
	}
	active, err := activePlugins(man, opts)
	if err != nil {
		return nil, err
	}
	a := &app{dir: dir, fsys: fsys, opts: opts, logf: opts.Logf, man: man, reg: scheme.NewRegistry()}
	a.ctl = &Control{a: a}
	// The data SDK is registered first so that host code cannot shadow its
	// ids. The permission set is parsed once, here; changing app.json needs a
	// restart. The UI half (sdk.Host) is attached to the platform later, by
	// attachSDK, and filtered by the same set.
	opt := sdkall.Options{
		AppID: man.AppID, Name: man.ProductName, Version: man.Version, Permissions: man.Permissions,
		Strict: man.StrictPermissions,
	}
	if man.Updater != nil {
		opt.UpdaterURL, opt.UpdaterPublicKey = man.Updater.URL, man.Updater.PublicKey
	}
	set, err := sdkall.Register(a.reg, opt)
	if err != nil {
		return nil, fmt.Errorf("shell: registering the SDK: %w", err)
	}
	a.perms = set
	// A request from a native view the shell cannot place gets this scope,
	// which allows nothing; see identify.
	if err := set.Scope(unknownView, []string{}); err != nil {
		return nil, fmt.Errorf("shell: %w", err)
	}
	a.sdk = sdk.NewHost()
	if len(active) > 0 {
		a.plug = plugin.NewHost(plugin.HostOptions{
			Registry: a.reg, Perms: set,
			AppID: man.AppID, AppName: man.ProductName, AppVersion: man.Version,
			Dev: opts.Dev, Logf: a.logf,
		})
		for _, p := range active {
			if err := a.plug.Install(p); err != nil {
				a.close()
				return nil, fmt.Errorf("shell: %w", err)
			}
		}
		a.sdk.OnWindow(a.plug.WindowCreated, a.plug.WindowClosed)
	}
	// Host children (JavaScript) reach the data SDK through the private socket.
	a.sdk.ShareWithParent(a.reg)
	if os.Getenv("KRATE_APP_ID") == "" {
		os.Setenv("KRATE_APP_ID", man.AppID)
	}
	if err := a.reloadHostJS(); err != nil {
		a.close()
		return nil, err
	}
	if err := a.restartGoHost(); err != nil {
		a.close()
		return nil, err
	}
	matcher, err := NewMatcher(man.Fallbacks)
	if err != nil {
		a.close()
		return nil, err
	}
	assetsFS, err := fs.Sub(fsys, filepath.ToSlash(filepath.Clean(man.Assets)))
	if err == nil {
		if st, serr := fs.Stat(assetsFS, "."); serr != nil || !st.IsDir() {
			err = fmt.Errorf("not a directory")
		}
	}
	if err != nil {
		a.close()
		return nil, fmt.Errorf("shell: assets directory %q not found in the app (app.json assets=%q)", man.Assets, man.Assets)
	}
	var fallback func(string) (string, bool)
	if len(man.Fallbacks) > 0 {
		fallback = matcher.Match
	}
	so := scheme.Options{
		Assets:   assetsFS,
		Registry: a.reg,
		Dev:      opts.Dev,
		Fallback: fallback,
		Identify: a.identify,
	}
	if opts.Dev {
		a.dev = newDevTools(a.logf)
		a.dev.identify = a.identify
		so.HMR = a.dev.hmr.serve
		so.Observe = a.dev.ring.add
		a.dev.memory = a.serveMemory
	}
	inject := ""
	if opts.Dev {
		inject += devErrorsScript
	}
	if opts.Dev || man.Debug {
		inject += inspectorKeyScript
	}
	app := windowIDHandler(scheme.New(so), inject)
	extra := opts.ExtraHandler
	a.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.reaper.Touch()
		if opts.Dev && a.results.serve(w, r) {
			return
		}
		if extra != nil && extra(w, r) {
			return
		}
		if a.dev != nil && a.dev.handle(w, r) {
			return
		}
		if (opts.Dev || man.Debug) && r.URL.Path == inspectorPath {
			a.serveInspector(w, r)
			return
		}
		app.ServeHTTP(w, r)
	})
	return a, nil
}

// unknownView is the scope key of a request whose native view the shell cannot
// place in a window. Its scope is empty, so such a request can use no SDK id.
const unknownView = "?unknown-view"

// identify binds a request to the window or view it came from. The platform
// layer tags every request it serves with the webview that made it (see
// caps.ViewOf), and the SDK maps that view to the id its permission scope is
// kept under. Page script cannot influence either step, so the page's own
// x-krate-window header is ignored whenever there is a native identity. Only
// without one (browser mode has no native views) does the header count.
func (a *app) identify(r *http.Request) (string, bool) {
	ref, ok := caps.ViewOf(r)
	if !ok {
		return "", false
	}
	if id, ok := a.sdk.ViewWindow(ref); ok {
		return id, true
	}
	return unknownView, true
}

// attachSDK connects the UI half of the SDK to the platform (nil in browser
// mode) and exposes it to the page under the manifest's permissions. An id
// whose permission is missing fails with permission_denied.
func (a *app) attachSDK(p platform.App) {
	a.sdk.Attach(p)
	if p != nil {
		sdkupdater.SetQuit(p.Quit)
	}
	sdkall.RegisterUI(a.reg, a.sdk, a.perms, a.opts.Allowed)
}

// startMemory applies the Go side of the memory profile and starts the idle
// reaper. The returned function stops it.
func (a *app) startMemory() func() {
	p := a.man.MemoryProfile()
	memory.ApplyGo(p)
	r := memory.StartReaper(p, func() {
		a.mu.Lock()
		h := a.host
		a.mu.Unlock()
		if h != nil {
			h.GC()
		}
	})
	a.reaper = r
	return r.Stop
}

func (a *app) close() {
	if a.plug != nil {
		a.plug.Quit()
	}
	a.sdk.Close()
	a.mu.Lock()
	host, child := a.host, a.child
	a.host, a.child = nil, nil
	hcs := a.hostChildren
	a.hostChildren = nil
	a.mu.Unlock()
	for _, hc := range hcs {
		if err := hc.child.Stop(); err != nil {
			a.logf("stopping a host child: %v", err)
		}
	}
	if child != nil {
		if err := child.Stop(); err != nil {
			a.logf("stopping the Go child: %v", err)
		}
	}
	if host != nil {
		host.Close()
	}
}

// guard turns a registry panic (duplicate id) into an error.
func guard(what string, f func()) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("shell: %s: %v", what, v)
		}
	}()
	f()
	return nil
}

// reloadHostJS loads or reloads the hostjs bundle named by the manifest on
// disk and registers what it exports, and restarts the JavaScript host
// children, which have no state worth keeping across a code change.
func (a *app) reloadHostJS() error {
	if err := a.reloadQuickJS(); err != nil {
		return err
	}
	return a.restartHostChildren()
}

func (a *app) reloadQuickJS() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	man, err := ReadManifestFS(a.fsys)
	if err != nil {
		return err
	}
	a.man = man
	spec := man.HostJS
	if spec == nil {
		return nil
	}
	bundle, err := fs.ReadFile(a.fsys, spec.Bundle)
	if err != nil {
		return fmt.Errorf("shell: reading the host bundle: %w", err)
	}
	var srcMap []byte
	if spec.Map != "" {
		srcMap, err = fs.ReadFile(a.fsys, spec.Map)
		if err != nil {
			return fmt.Errorf("shell: reading the host source map: %w", err)
		}
	}
	if a.host == nil {
		env := map[string]string{"NODE_ENV": "production", "KRATE_APP_DIR": a.dir}
		if a.opts.Dev {
			env["NODE_ENV"] = "development"
		}
		mb := spec.MemoryMB
		if mb == 0 {
			mb = man.MemoryProfile().HostMemoryMB()
		}
		h, err := newJSHost(a.opts.Dev, mb, env, a.logf)
		if err != nil {
			return err
		}
		if err := h.Load(bundle, srcMap, spec.Bundle); err != nil {
			h.Close()
			return err
		}
		if err := guard("registering host functions", func() { h.Register(a.reg) }); err != nil {
			h.Close()
			return err
		}
		a.host = h
		return nil
	}
	if err := a.host.Reload(bundle, srcMap, spec.Bundle); err != nil {
		return err
	}
	return guard("registering host functions", func() { a.host.Register(a.reg) })
}

// restartGoHost starts the Go host child, or restarts it with the binary the
// manifest names now.
func (a *app) restartGoHost() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	man, err := ReadManifestFS(a.fsys)
	if err != nil {
		return err
	}
	a.man = man
	if g := a.opts.GoGlue; g != nil {
		if a.glued {
			return nil
		}
		a.glued = true
		return guard("registering Go host functions", func() { g(a.reg) })
	}
	spec := man.GoHost
	if spec == nil {
		return nil
	}
	if a.dir == "" {
		return fmt.Errorf("shell: an embedded app cannot start a Go host child; link the Go host code with RunOptions.GoGlue")
	}
	binary := filepath.Join(a.dir, filepath.FromSlash(spec.Binary))
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("shell: the Go host binary: %w", err)
	}
	if a.child == nil {
		c := gohost.NewChild(gohost.ChildOptions{
			Logf:   func(format string, args ...any) { a.logf(format, args...) },
			Prefix: "[gohost] ",
		})
		env := []string{
			"KRATE_APP_DIR=" + a.dir,
			"KRATE_APP_ID=" + man.AppID,
			"KRATE_APP_NAME=" + man.ProductName,
			"KRATE_APP_VERSION=" + man.Version,
		}
		parent, err := a.sdk.ServeParent()
		if err != nil {
			return err
		}
		env = append(env, gohost.ParentEnv+"="+parent)
		if a.opts.Dev {
			env = append(env, "KRATE_DEV=1")
		}
		env = append(env, memory.EnvProfile+"="+string(man.MemoryProfile()))
		if err := c.Start(context.Background(), binary, env); err != nil {
			return err
		}
		a.child = c
	} else if err := a.child.Restart(binary); err != nil {
		return err
	}
	return guard("registering Go host functions", func() {
		a.child.Remote().RegisterInto(a.reg, spec.Funcs, spec.Streams, spec.Signals)
	})
}

// windows tracks the windows this app opened.
type windows struct {
	mu   sync.Mutex
	list []platform.Window
}

func (w *windows) add(win platform.Window) {
	w.mu.Lock()
	w.list = append(w.list, win)
	w.mu.Unlock()
}

func (w *windows) all() []platform.Window {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]platform.Window(nil), w.list...)
}

func (a *app) runNative() error {
	man := a.man
	platApp, err := platform.New(platform.Options{
		AppID:   man.AppID,
		AppName: man.ProductName,
		Handler: a.handler,
		Debug:   a.opts.Dev || man.Debug,
		Memory:  platformMemory(man),
		// The manifest is read once, at load, like the permissions.
		KeepAlive: man.Window.KeepAlive,
	})
	if err != nil {
		return err
	}
	a.attachSDK(platApp)
	a.plat.Store(&platApp)
	wins := &windows{}
	a.ctl.wins = wins
	a.ctl.origin = platApp.Origin()
	a.quit = platApp.Quit
	if ctx := a.opts.Context; ctx != nil {
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			select {
			case <-ctx.Done():
				platApp.Quit()
			case <-stop:
			}
		}()
	}
	var openErr error
	runErr := platApp.Run(func() {
		if a.plug != nil {
			a.plug.Start(platApp, a.sdk)
		}
		path := man.Window.Path
		if wp := man.Window.Permissions; wp != nil {
			if err := a.perms.Scope("main", *wp); err != nil {
				openErr = fmt.Errorf("shell: window.permissions: %w", err)
				platApp.Quit()
				return
			}
			path = krate.ScopedPath(path, "main")
		}
		win, err := platApp.NewWindow(platform.WindowOptions{
			Title:     man.Window.Title,
			Width:     man.Window.Width,
			Height:    man.Window.Height,
			Resizable: man.Window.IsResizable(),
			Path:      path,
		})
		if err != nil {
			openErr = fmt.Errorf("shell: opening the window: %w", err)
			platApp.Quit()
			return
		}
		wins.add(win)
		a.sdk.AddWindow("main", win)
		if a.opts.OnReady != nil {
			go a.opts.OnReady(a.ctl)
		}
	})
	if runErr != nil {
		return runErr
	}
	return openErr
}

// Control drives a running app: the dev loop uses it to reload host code
// without restarting the process.
type Control struct {
	a      *app
	wins   *windows
	origin string
	url    string
}

// ReloadHostJS re-reads app.json and the host bundle from disk and swaps it
// into the running VM (see hostjs.Host.Reload for what survives).
func (c *Control) ReloadHostJS() error { return c.a.reloadHostJS() }

// RestartGoHost restarts only the Go host child, with the binary app.json
// names now. Windows stay open and signals reconnect on their own.
func (c *Control) RestartGoHost() error { return c.a.restartGoHost() }

// HMR delivers one dev-hub message (a JSON object with a "type") to every
// window's /__krate/hmr stream. It needs a shell started with Dev.
func (c *Control) HMR(msg string) error {
	if c.a.dev == nil {
		return errors.New("hmr needs dev mode")
	}
	return c.a.dev.hmr.publish([]byte(msg))
}

// Eval runs JavaScript in every view of every open window. It does not wait
// for a result.
func (c *Control) Eval(js string) error {
	if c.wins == nil {
		return errors.New("browser mode has no native windows")
	}
	list := c.wins.all()
	if len(list) == 0 {
		return errors.New("no open window")
	}
	for _, w := range list {
		if wv, ok := w.(platform.WindowViews); ok {
			for _, v := range wv.Views() {
				if err := wv.EvalView(v.Ref, js); err != nil {
					return err
				}
			}
			continue
		}
		w.Eval(js)
	}
	return nil
}

// Quit stops the app; Run returns.
func (c *Control) Quit() {
	if q := c.a.quit; q != nil {
		q()
	}
}

// URL is the browser-mode address including the session token, or "".
func (c *Control) URL() string { return c.url }

// Origin is the native app origin ("krate://app" or "https://app.localhost"),
// or "" in browser mode.
func (c *Control) Origin() string { return c.origin }

// platformMemory is the webview side of the manifest's memory profile. An
// app.json without a features block was not scanned, so every optional
// feature stays on for it.
func platformMemory(m *Manifest) platform.Memory {
	out := platform.Memory{Profile: string(m.MemoryProfile())}
	if f := m.Features; f != nil {
		out.Features = platform.Features{Media: f.Media, WebAudio: f.WebAudio, WebGL: f.WebGL, WebRTC: f.WebRTC, SpellCheck: f.SpellCheck}
	} else {
		out.Features = platform.Features{Media: true, WebAudio: true, WebGL: true, WebRTC: true, SpellCheck: true}
	}
	return out
}

// activePlugins returns the linked plugins the manifest lists, in the
// manifest's order.
func activePlugins(man *Manifest, opts RunOptions) ([]*plugin.Plugin, error) {
	var out []*plugin.Plugin
	for _, name := range man.Plugins {
		var found *plugin.Plugin
		for _, p := range opts.Plugins {
			if p.Meta.Name == name {
				found = p
			}
		}
		if found == nil {
			return nil, fmt.Errorf("shell: the app uses the plugin %q, which this shell does not link; run it with the shell that krate native dev or krate native build makes for the app (the prebuilt krate-shell has no plugins)", name)
		}
		out = append(out, found)
	}
	return out, nil
}

// pluginHint explains a manifest error that comes from plugin permissions a
// shell without the plugin cannot know.
func pluginHint(fsys fs.FS, opts RunOptions, err error) error {
	if len(opts.Plugins) > 0 {
		return err
	}
	b, rerr := fs.ReadFile(fsys, ManifestFile)
	if rerr != nil {
		return err
	}
	var m struct {
		Plugins []string `json:"plugins"`
	}
	if json.Unmarshal(b, &m) == nil && len(m.Plugins) > 0 {
		return fmt.Errorf("%w\n  the app uses the plugins %s, which this shell does not link; run it with the shell that krate native dev or krate native build makes for the app", err, strings.Join(m.Plugins, ", "))
	}
	return err
}
