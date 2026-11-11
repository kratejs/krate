// Package app has the application lifecycle: quit, relaunch, capability
// checks, single-instance enforcement and deep links.
//
// Single instance: SingleInstance claims a per-app unix socket in the user
// runtime directory. The first instance keeps it and receives the command
// line of every later launch through OnSecondInstance; a later launch gets
// first == false and should exit after the call. Deep links reach an app as
// command line arguments such as "myapp://open/42": the operating system
// starts the app (or, with SingleInstance, hands the URL to the running one)
// after the scheme was registered. On Linux registration is a .desktop file
// with MimeType=x-scheme-handler/<scheme>; DesktopEntry generates it at
// packaging time. macOS (CFBundleURLTypes in Info.plist) and Windows (a
// registry key) are packaging steps outside this package.
package app

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// There is no UI permission for the lifecycle: Quit, Relaunch and event
// streams are exposed to the UI under "app".
const Permission = "app"

// Host function ids.
const (
	IDQuit           = "krate/app.quit"
	IDRelaunch       = "krate/app.relaunch"
	IDSupports       = "krate/app.supports"
	IDLaunchArgs     = "krate/app.launchArgs"
	IDSingleInstance = "krate/app.singleInstance"
	IDEvents         = "krate/app.events"
)

// Capabilities are the names Supports answers; init registers each with the
// krate package so krate.Supports sees them in any process.
var Capabilities = []string{
	"dialog", "clipboard", "clipboard:image", "clipboard:files", "menu",
	"tray", "notify", "shortcut", "window", "window:alwaysOnTop",
	"window:fullscreen", "window:frameless", "window:position", "window:release",
}

func init() {
	for _, c := range Capabilities {
		krate.RegisterCapability(c, func() bool { return Supports(c) })
	}
}

// Quit stops the application: the event loop ends and the shell exits.
func Quit() error { return bridge.CallDo(context.Background(), IDQuit, nil) }

// Relaunch starts a new instance of the application with the same arguments
// and then quits this one.
func Relaunch() error { return bridge.CallDo(context.Background(), IDRelaunch, nil) }

// Supports reports whether a capability works right now. Names:
// "dialog", "clipboard", "clipboard:image", "clipboard:files", "menu",
// "tray", "notify", "shortcut", "window", "window:alwaysOnTop",
// "window:fullscreen", "window:frameless", "window:position", "window:release".
func Supports(capability string) bool {
	d, err := bridge.Call(context.Background(), IDSupports, func(e *wire.Encoder) { e.String(capability) })
	if err != nil {
		return false
	}
	ok, err := d.Bool()
	return err == nil && ok
}

// LaunchArgs returns the command line arguments the application was started
// with (without the program name).
func LaunchArgs() ([]string, error) {
	d, err := bridge.Call(context.Background(), IDLaunchArgs, nil)
	if err != nil {
		return nil, err
	}
	return codec.ReadStrings(d)
}

// SecondInstance describes a later launch of the app.
type SecondInstance struct {
	Args []string
	// Dir is the working directory of the later launch.
	Dir string
	// URLs are the arguments that look like deep links (scheme://...).
	URLs []string
}

// SingleInstance claims the single-instance lock for appID, passing this
// launch's args to the running instance when there is one. It reports whether
// this process is the first instance; when it is not, the caller should exit.
func SingleInstance(appID string, args []string) (first bool, err error) {
	d, err := bridge.Call(context.Background(), IDSingleInstance, func(e *wire.Encoder) {
		e.String(appID)
		codec.WriteStrings(e, args)
	})
	if err != nil {
		return false, err
	}
	return d.Bool()
}

var (
	subMu sync.Mutex
	subs  []func(SecondInstance)
	urls  []func(string)
	subC  func()
	subEp int64
)

func ensureSub() error {
	if subC != nil && subEp == bridge.Epoch() {
		return nil
	}
	if subC != nil {
		subC()
		subC = nil
	}
	subEp = bridge.Epoch()
	c, err := bridge.Subscribe(context.Background(), IDEvents, func(d *wire.Decoder) {
		si, err := decodeSecond(d)
		if err != nil {
			return
		}
		subMu.Lock()
		fns := slices.Clone(subs)
		ufns := slices.Clone(urls)
		subMu.Unlock()
		for _, f := range fns {
			f(si)
		}
		for _, u := range si.URLs {
			for _, f := range ufns {
				f(u)
			}
		}
	})
	if err != nil {
		return err
	}
	subC = c
	return nil
}

// OnSecondInstance calls fn each time the app is launched again while
// SingleInstance holds the lock.
func OnSecondInstance(fn func(SecondInstance)) error {
	subMu.Lock()
	defer subMu.Unlock()
	if err := ensureSub(); err != nil {
		return err
	}
	subs = append(subs, fn)
	return nil
}

// OnOpenURL calls fn for every deep link (an argument of the form
// scheme://...) a later launch passes to the running instance.
func OnOpenURL(fn func(url string)) error {
	subMu.Lock()
	defer subMu.Unlock()
	if err := ensureSub(); err != nil {
		return err
	}
	urls = append(urls, fn)
	return nil
}

// URLsFromArgs returns the arguments that are URLs of one of the schemes (any
// scheme://... when none is given).
func URLsFromArgs(args []string, schemes ...string) []string {
	var out []string
	for _, a := range args {
		i := strings.Index(a, "://")
		if i <= 0 {
			continue
		}
		if len(schemes) == 0 {
			out = append(out, a)
			continue
		}
		for _, s := range schemes {
			if strings.EqualFold(a[:i], s) {
				out = append(out, a)
				break
			}
		}
	}
	return out
}

func encodeSecond(e *wire.Encoder, si SecondInstance) {
	e.BeginObject()
	e.Key("args")
	codec.WriteStrings(e, si.Args)
	e.Key("dir")
	e.String(si.Dir)
	e.Key("urls")
	codec.WriteStrings(e, si.URLs)
	e.EndObject()
}

func decodeSecond(d *wire.Decoder) (si SecondInstance, err error) {
	r := &codec.R{D: d, Name: "app event"}
	r.Object(func(k string) bool {
		switch k {
		case "args":
			si.Args = r.Strings()
		case "dir":
			si.Dir = r.String()
		case "urls":
			si.URLs = r.Strings()
		default:
			return false
		}
		return true
	})
	return si, r.Err()
}
