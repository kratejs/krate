//go:build !linux

package notify

import (
	"fmt"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

func newDefaultBackend(publish func(caps.NotifyEvent)) (backend, error) {
	return nil, fmt.Errorf("%w: notifications are not available on this platform build", krate.ErrUnsupported)
}

// Available reports whether notifications can be sent.
func Available(env *bridge.Env) bool {
	_, ok := env.App.(caps.Notifier)
	return ok
}
