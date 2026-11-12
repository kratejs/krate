// Package sdk assembles the UI half of the Krate Native SDK for the process
// that owns the platform (the shell): window, menu, tray, shortcut, dialog,
// clipboard, notify and app lifecycle.
//
// The areas live in their own packages (sdk/window, sdk/dialog, ...) and are
// plain Go functions for host code. Every operation is a serializable
// message: in the shell it runs against the platform; in the Go host child
// of a dev run it is forwarded to the shell over the socket named by
// KRATE_GOHOST_PARENT. A Host registers the operations three ways:
//
//   - local dispatch for host code linked into the shell process;
//   - ServeParent, a private unix socket serving every operation to the
//     trusted Go host child;
//   - RegisterUI, the permission-filtered subset the page may call, as host
//     ids "krate/<area>.<Name>" plus event streams "krate/<area>.events".
package sdk

import (
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/internal/hosttransport"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

// ParentEnv is the environment variable that tells a Go host child where to
// reach the shell's SDK operations.
const ParentEnv = bridge.ParentEnv

// Host is the SDK of one process that owns the platform.
type Host struct {
	mu        sync.Mutex
	tcpSrv    *http.Server
	tcpAddr   string
	table     *bridge.Table
	env       *bridge.Env
	parentReg *scheme.Registry
	srv       *http.Server
	dir       string
	attached  bool
	// winCreated and winClosed are window callbacks given before Attach.
	winCreated, winClosed []func(id string)
}

// OnWindow registers callbacks for windows the SDK learns about (AddWindow,
// window.open) and for windows that closed. Either may be nil. It may be
// called before or after Attach.
func (h *Host) OnWindow(created, closed func(id string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.env != nil {
		h.env.Windows.Subscribe(created, closed)
		return
	}
	if created != nil {
		h.winCreated = append(h.winCreated, created)
	}
	if closed != nil {
		h.winClosed = append(h.winClosed, closed)
	}
}

// Window returns the platform window with this SDK id ("" = main).
func (h *Host) Window(id string) (platform.Window, string, bool) {
	h.mu.Lock()
	env := h.env
	h.mu.Unlock()
	if env == nil {
		return nil, "", false
	}
	return env.Windows.Get(id)
}

// WindowIDs lists the SDK ids of the open windows.
func (h *Host) WindowIDs() []string {
	h.mu.Lock()
	env := h.env
	h.mu.Unlock()
	if env == nil {
		return nil
	}
	return env.Windows.List()
}

// NewHost returns a Host that is not attached to a platform yet.
func NewHost() *Host {
	return &Host{parentReg: scheme.NewRegistry()}
}

// ServeParent starts the private socket for the Go host child and returns
// the value for ParentEnv. Operations answer "unavailable" until Attach.
func (h *Host) ServeParent() (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.srv != nil {
		return "unix:" + filepath.Join(h.dir, "sdk.sock"), nil
	}
	var dir string
	if c, err := os.UserCacheDir(); err == nil {
		d := filepath.Join(c, "krate", "native", strconv.Itoa(os.Getpid()))
		if os.MkdirAll(d, 0o700) == nil && len(filepath.Join(d, "sdk.sock")) <= 100 {
			dir = d
		}
	}
	if dir == "" {
		d, err := os.MkdirTemp("", "krate-sdk")
		if err != nil {
			return "", err
		}
		dir = d
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	sock := filepath.Join(dir, "sdk.sock")
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return "", err
	}
	h.dir = dir
	srv := &http.Server{Handler: scheme.New(scheme.Options{Registry: h.parentReg}), ReadHeaderTimeout: 10 * time.Second}
	h.srv = srv
	go func() { _ = srv.Serve(ln) }() // ends with ErrServerClosed on shutdown
	return "unix:" + sock, nil
}

// ServeParentTCP is ServeParent over loopback TCP with a random token, for the
// JavaScript host children of Windows (Node, Bun and Deno cannot use AF_UNIX
// there). It returns "tcp:127.0.0.1:<port>?token=<hex>" for ParentEnv. Every
// request must carry the token in the X-Krate-Token header.
func (h *Host) ServeParentTCP() (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.tcpSrv != nil {
		return h.tcpAddr, nil
	}
	ln, ep, err := hosttransport.ListenTCP()
	if err != nil {
		return "", err
	}
	tcp := &http.Server{
		Handler:           hosttransport.Guard(ep, scheme.New(scheme.Options{Registry: h.parentReg})),
		ReadHeaderTimeout: 10 * time.Second,
	}
	h.tcpSrv = tcp
	h.tcpAddr = ep.String()
	go func() { _ = tcp.Serve(ln) }() // ends with ErrServerClosed on shutdown
	return h.tcpAddr, nil
}

// ShareWithParent adds the trusted form of the "krate/" ids registered on src
// (the data areas: fs, db, kv, net, updater) to the private socket, so a
// JavaScript host child can call them. The UI half is added by Attach.
func (h *Host) ShareWithParent(src *scheme.Registry) {
	src.CopyTrusted(h.parentReg, "krate/")
}

// Attach connects the SDK to the platform app and makes it the process's
// local dispatch target. Pass nil for a process without a platform (browser
// mode): every operation then answers krate.ErrUnsupported.
func (h *Host) Attach(app platform.App) {
	if app == nil {
		app = noApp{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.attached {
		return
	}
	h.attached = true
	h.env = bridge.NewEnv(app)
	for _, f := range h.winCreated {
		h.env.Windows.Subscribe(f, nil)
	}
	for _, f := range h.winClosed {
		h.env.Windows.Subscribe(nil, f)
	}
	h.winCreated, h.winClosed = nil, nil
	h.table = bridge.NewTable()
	for _, a := range areas {
		a.attach(h)
	}
	h.table.RegisterAll(h.parentReg)
	bridge.SetLocal(h.table)
}

// RegisterUI registers on reg the operations the page may call. Those whose
// permission allowed accepts work; the others fail with denied(permission)
// (sdk/all.RegisterUI passes permission.Set.Allows and permission.Denied).
// Call it after Attach.
func (h *Host) RegisterUI(reg *scheme.Registry, allowed func(permission string) bool, denied func(permission string) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.table != nil {
		h.table.RegisterUI(reg, allowed, denied)
	}
}

// RegisterUIFor is RegisterUI with a per-window check: allowed("", p) says
// whether the app grants p, and every call is checked again with the id of the
// calling window (see permission.Set.AllowsFor).
func (h *Host) RegisterUIFor(reg *scheme.Registry, allowed func(window, permission string) bool, denied func(permission string) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.table != nil {
		h.table.RegisterUIFor(reg, allowed, denied)
	}
}

// SetWindowScope installs the function the window area calls for every window
// it opens, to narrow that window's permissions (window.Options.Permissions,
// or the caller's scope, which a new window inherits). It reports whether the
// window is scoped, or an error when the list names something the caller does
// not have. sdk/all sets it from the permission Set.
func (h *Host) SetWindowScope(f func(caller, window string, permissions []string, has bool) (bool, error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.env != nil {
		h.env.Scope = f
	}
}

// SetWindowUnscope installs the function the window area calls when a window
// or view goes away, to drop its permission scope. sdk/all sets it from the
// permission Set.
func (h *Host) SetWindowUnscope(f func(id string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.env != nil {
		h.env.Unscope = f
	}
}

// SetPermissionCheck installs the function the media area asks whether a
// window or view has a permission in its scope (permission.Set.AllowsFor).
// Without it every webview permission request is denied. sdk/all sets it.
func (h *Host) SetPermissionCheck(f func(window, permission string) bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.env != nil {
		h.env.Allows = f
	}
}

// ViewWindow maps the view a request came from, as the platform reported it,
// to the id the SDK knows it by, which is also the key of its permission
// scope: the window id for a window's primary view, the view id for the
// others. A view the platform has but the SDK has not registered yet (its page
// asked before the code that created it returned) is waited for briefly. ok is
// false for a view nobody knows.
func (h *Host) ViewWindow(ref caps.ViewRef) (id string, ok bool) {
	h.mu.Lock()
	env := h.env
	h.mu.Unlock()
	if env == nil || ref == 0 {
		return "", false
	}
	if r, found := env.Windows.ViewByRef(ref); found {
		return r.ID, true
	}
	if l, can := env.App.(caps.ViewLookup); can {
		if _, known := l.WindowOfView(ref); !known {
			return "", false
		}
		if r, found := env.Windows.WaitView(ref, viewWait); found {
			return r.ID, true
		}
	}
	return "", false
}

// viewWait is how long a request waits for its view to be registered.
const viewWait = 3 * time.Second

// AddWindow registers a platform window under id ("main" for the first).
func (h *Host) AddWindow(id string, w platform.Window) string {
	h.mu.Lock()
	env := h.env
	h.mu.Unlock()
	if env == nil {
		return ""
	}
	return env.Windows.Add(id, w)
}

// Close stops the parent socket and detaches local dispatch.
func (h *Host) Close() {
	h.mu.Lock()
	srv, dir, tcp := h.srv, h.dir, h.tcpSrv
	h.srv, h.dir, h.tcpSrv, h.tcpAddr = nil, "", nil, ""
	attached := h.attached
	h.mu.Unlock()
	if attached && bridge.Local() == h.table {
		bridge.SetLocal(nil)
	}
	if srv != nil {
		srv.Close()
	}
	if tcp != nil {
		tcp.Close()
	}
	if dir != "" {
		os.RemoveAll(dir)
	}
}

// IDs lists every operation id with the permission the UI needs ("" = not
// exposed to the UI). Useful for docs and tests.
func (h *Host) IDs() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.table == nil {
		return nil
	}
	return h.table.IDs()
}

func (h *Host) supports(c string) bool {
	h.mu.Lock()
	env := h.env
	h.mu.Unlock()
	if env == nil {
		return false
	}
	for _, a := range areas {
		if a.supports == nil {
			continue
		}
		if ok, handled := a.supports(env, c); handled {
			return ok
		}
	}
	return false
}

// noApp is the platform of a process that has none; every capability
// assertion on it fails.
type noApp struct{}

func (noApp) Run(func()) error   { return errors.New("sdk: no platform") }
func (noApp) Dispatch(fn func()) { fn() }
func (noApp) NewWindow(platform.WindowOptions) (platform.Window, error) {
	return nil, krate.ErrUnsupported
}
func (noApp) Origin() string { return "" }
func (noApp) Quit()          {}

// Subscribers is the number of open event streams on a topic ("menu",
// "window", "tray", "shortcut", "notify", "app"); for tests.
func (h *Host) Subscribers(topic string) int {
	h.mu.Lock()
	env := h.env
	h.mu.Unlock()
	if env == nil {
		return 0
	}
	return env.Topic(topic).Subscribers()
}

// SupportsForTest answers the same question as app.Supports for the host.
func (h *Host) SupportsForTest(capability string) bool { return h.supports(capability) }

// WindowRef returns the platform reference of the window with this SDK id
// ("" = main), or 0. For tests.
func (h *Host) WindowRef(id string) caps.WindowRef {
	h.mu.Lock()
	env := h.env
	h.mu.Unlock()
	if env == nil {
		return 0
	}
	return env.Windows.Ref(id)
}

// InjectFileDrop reports a drop of files onto the first view of a window ("" =
// the main window) as the platform would. It is for the dev control channel
// and tests; it reports whether the window exists and the SDK handles drops.
func (h *Host) InjectFileDrop(window string, paths []string, x, y int) bool {
	h.mu.Lock()
	env := h.env
	h.mu.Unlock()
	if env == nil || env.FileDrop == nil {
		return false
	}
	win, _, ok := env.Windows.Get(window)
	if !ok {
		return false
	}
	v, ok := win.(caps.WindowViews)
	if !ok {
		return false
	}
	var wref caps.WindowRef
	if c, ok := win.(caps.WindowControls); ok {
		wref = c.Ref()
	}
	env.FileDrop(caps.FileDrop{View: v.PrimaryView(), Window: wref, Paths: paths, X: x, Y: y})
	return true
}
