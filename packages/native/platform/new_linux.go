//go:build linux

package platform

import "github.com/kratejs/krate/packages/native/platform/linux"

// New creates the WebKitGTK application. Call it from the main goroutine.
func New(opts Options) (App, error) {
	a, err := linux.New(linux.Options(opts))
	if err != nil {
		return nil, err
	}
	return linuxApp{a}, nil
}

// linuxApp adapts *linux.App, whose NewWindow returns a concrete type, to App.
type linuxApp struct{ *linux.App }

// Unwrap returns the underlying implementation, for tests.
func (a linuxApp) Unwrap() *linux.App { return a.App }

func (a linuxApp) NewWindow(opts WindowOptions) (Window, error) {
	w, err := a.App.NewWindow(linux.WindowOptions(opts))
	if err != nil {
		return nil, err
	}
	return w, nil
}
