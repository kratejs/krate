package krate

import (
	"errors"
	"sync"
)

// ErrUnsupported is returned (possibly wrapped) by an SDK operation when the
// platform, desktop environment or process cannot do what was asked, for
// example a system tray on a desktop without a StatusNotifier host. Test with
// errors.Is. Across the host boundary it travels as the error code
// "unsupported".
var ErrUnsupported error = &Error{Code: "unsupported", Message: "not supported on this platform"}

// Is lets errors.Is(err, ErrUnsupported) match any error with the code
// "unsupported", including one that came back from another process.
func (e *Error) Is(target error) bool {
	return target == ErrUnsupported && e.Code == "unsupported"
}

// IsUnsupported reports whether err is ErrUnsupported or carries the code
// "unsupported".
func IsUnsupported(err error) bool {
	if errors.Is(err, ErrUnsupported) {
		return true
	}
	var e *Error
	return errors.As(err, &e) && e.Code == "unsupported"
}

var (
	supportMu  sync.RWMutex
	supportFns []func(capability string) bool
)

// RegisterSupport adds a provider consulted by Supports. SDK packages call it
// from init; a provider returns true for the capabilities it can serve.
func RegisterSupport(fn func(capability string) bool) {
	supportMu.Lock()
	supportFns = append(supportFns, fn)
	supportMu.Unlock()
}

// Supports reports whether the named capability (for example "tray",
// "clipboard:image", "window:alwaysOnTop") works in this process right now.
// It is false for capabilities no registered provider knows.
func Supports(capability string) bool {
	supportMu.RLock()
	fns := append([]func(string) bool(nil), supportFns...)
	supportMu.RUnlock()
	for _, f := range fns {
		if f(capability) {
			return true
		}
	}
	return false
}
