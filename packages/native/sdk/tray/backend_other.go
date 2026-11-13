//go:build !linux

package tray

import (
	"fmt"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

func newDefaultBackend(app any, publish func(caps.TrayEvent)) (backend, error) {
	return nil, fmt.Errorf("%w: the tray is not available on this platform build", krate.ErrUnsupported)
}

// Available reports whether a tray icon can be shown.
func Available(env *bridge.Env) bool {
	_, ok := env.App.(caps.Tray)
	return ok
}
