//go:build !linux

package shortcut

import (
	"errors"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

func newPortalBackend(publish func(string)) (backend, error) {
	return nil, errors.New("no portal")
}

// Available reports whether global shortcuts can work.
func Available(env *bridge.Env) bool {
	_, ok := env.App.(caps.Shortcuts)
	return ok
}
