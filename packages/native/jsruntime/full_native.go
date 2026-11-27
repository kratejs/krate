//go:build !js

package jsruntime

// fullRuntimeUnavailable reports why a full runtime (with the Web API
// prelude) cannot be created. It always can on native targets.
func fullRuntimeUnavailable() error { return nil }
