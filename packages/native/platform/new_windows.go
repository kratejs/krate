//go:build windows

package platform

import (
	winplat "github.com/kratejs/krate/packages/native/platform/windows"
)

// New creates the Windows application (Win32 + WebView2). It must be called
// on the main goroutine.
func New(opts Options) (App, error) {
	a, err := winplat.New(winplat.Config{
		AppID:   opts.AppID,
		Handler: opts.Handler,
		Debug:   opts.Debug,
	})
	if err != nil {
		return nil, err
	}
	return winApp{a}, nil
}

type winApp struct{ a *winplat.App }

func (w winApp) Run(onReady func()) error { return w.a.Run(onReady) }
func (w winApp) Dispatch(fn func())       { w.a.Dispatch(fn) }
func (w winApp) Origin() string           { return w.a.Origin() }
func (w winApp) Quit()                    { w.a.Quit() }

func (w winApp) NewWindow(o WindowOptions) (Window, error) {
	win, err := w.a.NewWindow(winplat.WindowConfig{
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
