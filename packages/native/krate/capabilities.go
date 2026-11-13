package krate

import (
	"errors"
	"sort"
	"sync"
)

// ErrUnsupported is returned (possibly wrapped) by an SDK operation when the
// platform, desktop environment or process cannot do what was asked, for
// example a system tray on a desktop without a StatusNotifier host. Test with
// errors.Is or IsUnsupported. Across the host boundary it travels as the error
// code "unsupported". It also matches the standard library's
// errors.ErrUnsupported.
var ErrUnsupported error = &Error{Code: "unsupported", Message: "not supported on this platform"}

// Is lets errors.Is match any error with the code "unsupported" against
// ErrUnsupported (including one that came back from another process) and
// against errors.ErrUnsupported.
func (e *Error) Is(target error) bool {
	return (target == ErrUnsupported || target == errors.ErrUnsupported) && e.Code == "unsupported"
}

// IsUnsupported reports whether err is, wraps or carries the code of
// ErrUnsupported.
func IsUnsupported(err error) bool {
	if errors.Is(err, ErrUnsupported) || errors.Is(err, errors.ErrUnsupported) {
		return true
	}
	var e *Error
	return errors.As(err, &e) && e.Code == "unsupported"
}

var caps = struct {
	mu     sync.RWMutex
	probes map[string]func() bool
}{probes: map[string]func() bool{}}

// RegisterCapability records a capability under name (for example
// "os.secrets", "tray", "window:alwaysOnTop"). probe reports whether it is
// usable right now; it may be slow the first time and should cache. SDK
// packages call this from init. Registering a name again replaces its probe.
func RegisterCapability(name string, probe func() bool) {
	caps.mu.Lock()
	defer caps.mu.Unlock()
	caps.probes[name] = probe
}

// Supports reports whether the named capability is registered and usable
// right now. An unknown name is false.
func Supports(name string) bool {
	caps.mu.RLock()
	probe := caps.probes[name]
	caps.mu.RUnlock()
	return probe != nil && probe()
}

// Capabilities lists the registered capability names, sorted.
func Capabilities() []string {
	caps.mu.RLock()
	defer caps.mu.RUnlock()
	out := make([]string, 0, len(caps.probes))
	for n := range caps.probes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
