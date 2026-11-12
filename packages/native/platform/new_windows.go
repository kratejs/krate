//go:build windows

package platform

import (
	"errors"
	"os"

	winplat "github.com/kratejs/krate/packages/native/platform/windows"
)

// New creates the Windows application (Win32 + WebView2). It must be called
// on the main goroutine.
func New(opts Options) (App, error) {
	mem, feat := opts.Memory.Backend()
	a, err := winplat.New(winplat.Config{
		AppID:     opts.AppID,
		AppName:   opts.AppName,
		IconPath:  opts.IconPath,
		Handler:   opts.Handler,
		Debug:     opts.Debug,
		KeepAlive: opts.KeepAlive,
		Memory:    mem,
		Features:  feat,
	})
	if errors.Is(err, winplat.ErrActivationForwarded) {
		// A notification click started this process and the running instance
		// has it; there is nothing left to do.
		os.Exit(0)
	}
	if err != nil {
		return nil, err
	}
	return winApp{a}, nil
}

// winApp embeds the implementation so that its optional capability methods
// (dialogs, clipboard, menus, window events) are promoted.
type winApp struct{ *winplat.App }

func (w winApp) NewWindow(o WindowOptions) (Window, error) {
	win, err := w.App.NewWindow(winplat.WindowConfig{
		Title:     o.Title,
		Width:     o.Width,
		Height:    o.Height,
		Resizable: o.Resizable,
		Path:      o.Path,
	})
	if err != nil {
		return nil, err
	}
	return win, nil
}
