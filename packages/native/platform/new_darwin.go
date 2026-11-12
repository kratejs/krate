//go:build darwin

package platform

import (
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/platform/darwin"
)

// New creates the macOS application (AppKit + WKWebView via purego).
func New(opts Options) (App, error) {
	mem, feat := opts.Memory.Backend()
	a, err := darwin.New(darwin.Options{
		AppID:     opts.AppID,
		Handler:   opts.Handler,
		Debug:     opts.Debug,
		KeepAlive: opts.KeepAlive,
		Memory:    mem,
		Features:  feat,
		Agent:     opts.Agent,
	})
	if err != nil {
		return nil, err
	}
	return darwinApp{a}, nil
}

// darwinApp adapts *darwin.App to App; only NewWindow differs in signature.
// The darwin package cannot import this one (import cycle), so it has its own
// option types.
type darwinApp struct{ *darwin.App }

func (a darwinApp) NewWindow(o WindowOptions) (Window, error) {
	w, err := a.App.NewWindow(darwin.WindowOptions(o))
	if err != nil {
		return nil, err
	}
	return w, nil
}

var (
	_ App                       = darwinApp{}
	_ caps.Tray                 = darwinApp{}
	_ caps.Shortcuts            = darwinApp{}
	_ caps.Notifier             = darwinApp{}
	_ caps.NotifierAvailability = darwinApp{}
	_ caps.NotificationCloser   = darwinApp{}
)
var _ Window = (*darwin.Window)(nil)
