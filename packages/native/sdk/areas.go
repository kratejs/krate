package sdk

import "github.com/kratejs/krate/packages/native/sdk/internal/bridge"

// An area is one part of the UI half of the SDK. Each area lives in its own
// file (area_<name>.go) that registers it from init, behind the build tag
// !krate_no_<name>. A release build passes -tags krate_no_<name> for every area
// the app cannot reach, so the platform glue of that area is not linked.
type area struct {
	name string
	// attach registers the platform-side operations of the area.
	attach func(h *Host)
	// supports answers app.Supports for the capabilities the area owns;
	// handled is false for a capability it does not own.
	supports func(env *bridge.Env, capability string) (ok, handled bool)
}

var areas []area

// Areas lists the names of the UI areas linked into this binary.
func Areas() []string {
	out := make([]string, len(areas))
	for i, a := range areas {
		out[i] = a.name
	}
	return out
}
