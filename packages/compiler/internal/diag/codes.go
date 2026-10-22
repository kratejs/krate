package diag

import (
	"fmt"
	"sort"
	"sync"
)

// Code is a stable diagnostic identifier ("KR" plus four digits). Codes are
// never reused or renumbered once released, so users can search for them and
// docs can link to them.
//
// Ranges:
//
//	KR1xxx  syntax and parsing
//	KR2xxx  React effects, dependency arrays and hooks
//	KR3xxx  class components and error boundaries
//	KR4xxx  Suspense, lazy, use() and transitions
//	KR5xxx  events, forms and refs
//	KR6xxx  lists and keys
//	KR7xxx  React 19 actions and server actions
//	KR8xxx  islands, compiled packages and general React compatibility
//
// Each range is declared in its own codes_<area>.go file.
type Code string

// CodeInfo documents a registered code.
type CodeInfo struct {
	Code Code
	// Title is a short noun phrase, for example "Dependency array is not an array literal".
	Title string
	// Explanation says why the pattern is reported and what to do instead.
	Explanation string
}

var (
	registryMu sync.RWMutex
	registry   = map[Code]CodeInfo{}
)

// Register adds codes to the registry. It panics on a duplicate code so a
// collision fails every test run instead of shipping.
func Register(infos ...CodeInfo) {
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, info := range infos {
		if _, dup := registry[info.Code]; dup {
			panic(fmt.Sprintf("diag: duplicate code %s", info.Code))
		}
		registry[info.Code] = info
	}
}

// Lookup returns the registry entry for code.
func Lookup(code Code) (CodeInfo, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	info, ok := registry[code]
	return info, ok
}

// Codes returns every registered code, sorted.
func Codes() []CodeInfo {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]CodeInfo, 0, len(registry))
	for _, info := range registry {
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}
