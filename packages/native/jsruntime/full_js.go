//go:build js

package jsruntime

import "errors"

// fullRuntimeUnavailable reports why a full runtime cannot be created: on
// js/wasm every VM shares the host's global object, and the Web API prelude
// would replace the host's own fetch, URL, timers and performance, which the
// Go runtime itself calls. Only NewBare, which leaves them alone, works.
func fullRuntimeUnavailable() error {
	return errors.New("the JavaScript runtime with Web APIs is not available in the in-browser build; " +
		"build-time evaluation is limited to expression folding")
}
