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
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/scheme"
	appmod "github.com/kratejs/krate/packages/native/sdk/app"
	"github.com/kratejs/krate/packages/native/sdk/clipboard"
	"github.com/kratejs/krate/packages/native/sdk/dialog"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/menu"
	"github.com/kratejs/krate/packages/native/sdk/notify"
	"github.com/kratejs/krate/packages/native/sdk/shortcut"
	"github.com/kratejs/krate/packages/native/sdk/tray"
	"github.com/kratejs/krate/packages/native/sdk/window"
)

// ParentEnv is the environment variable that tells a Go host child where to
// reach the shell's SDK operations.
const ParentEnv = bridge.ParentEnv

// Host is the SDK of one process that owns the platform.
type Host struct {
	mu        sync.Mutex
	table     *bridge.Table
	env       *bridge.Env
	parentReg *scheme.Registry
	srv       *http.Server
	dir       string
	attached  bool
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
	os.Chmod(dir, 0o700)
	sock := filepath.Join(dir, "sdk.sock")
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return "", err
	}
	h.dir = dir
	h.srv = &http.Server{Handler: scheme.New(scheme.Options{Registry: h.parentReg}), ReadHeaderTimeout: 10 * time.Second}
	go h.srv.Serve(ln)
	return "unix:" + sock, nil
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
	h.table = bridge.NewTable()
	dialog.Register(h.table, h.env)
	clipboard.Register(h.table, h.env)
	menu.Register(h.table, h.env)
	tray.Register(h.table, h.env)
	notify.Register(h.table, h.env)
	shortcut.RegisterOps(h.table, h.env)
	window.Register(h.table, h.env)
	appmod.Register(h.table, h.env, h.supports)
	h.table.RegisterAll(h.parentReg)
	bridge.SetLocal(h.table)
}

// RegisterUI registers on reg the operations the page may call: the ones
// whose permission allowed accepts. Call it after Attach.
func (h *Host) RegisterUI(reg *scheme.Registry, allowed func(permission string) bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.table != nil {
		h.table.RegisterUI(reg, allowed)
	}
}

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
	srv, dir := h.srv, h.dir
	h.srv, h.dir = nil, ""
	attached := h.attached
	h.mu.Unlock()
	if attached && bridge.Local() == h.table {
		bridge.SetLocal(nil)
	}
	if srv != nil {
		srv.Close()
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
	_, _, _, cerr := env.Controls("")
	controls := cerr == nil
	wayland := runtime.GOOS == "linux" && os.Getenv("WAYLAND_DISPLAY") != ""
	switch c {
	case "dialog":
		_, ok := env.App.(caps.Dialogs)
		return ok
	case "clipboard", "clipboard:image", "clipboard:files":
		_, ok := env.App.(caps.Clipboard)
		return ok
	case "menu":
		_, ok := env.App.(caps.Menus)
		return ok
	case "tray":
		return tray.Available(env)
	case "notify":
		return notify.Available(env)
	case "shortcut":
		return shortcut.Available(env)
	case "window":
		return true
	case "window:fullscreen", "window:frameless":
		return controls
	case "window:alwaysOnTop", "window:position":
		return controls && !wayland
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
