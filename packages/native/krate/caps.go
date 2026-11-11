package krate

import (
	"errors"
	"sort"
	"sync"
)

// ErrUnsupported is returned by SDK operations that the current platform or
// environment cannot perform (for example secrets without a Secret Service).
// It is the standard library's errors.ErrUnsupported, so errors.Is works with
// either name.
var ErrUnsupported = errors.ErrUnsupported

var caps = struct {
	mu     sync.RWMutex
	probes map[string]func() bool
}{probes: map[string]func() bool{}}

// RegisterCapability records a platform capability under name (for example
// "os.secrets"). probe reports whether it is usable right now; it may be slow
// the first time and should cache. SDK packages call this from init.
func RegisterCapability(name string, probe func() bool) {
	caps.mu.Lock()
	defer caps.mu.Unlock()
	caps.probes[name] = probe
}

// Supports reports whether the named capability is registered and usable. An
// unknown name is false.
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
