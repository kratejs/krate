// Package safe turns panics into ordinary errors so a bug in one unit of work
// (a page, a plugin hook, an embedded-JS evaluation) is reported as a normal
// diagnostic instead of crashing the whole compiler. Set KRATE_DEBUG=1 to let
// panics propagate with their original stack for debugging.
package safe

import (
	"fmt"
	"os"
	"runtime/debug"
)

// PanicError wraps a recovered panic value together with the stack at the point
// it was recovered.
type PanicError struct {
	Value any
	Stack []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("internal error: %v", e.Value)
}

// debugEnabled reports whether panics should be re-thrown instead of recovered.
func debugEnabled() bool { return os.Getenv("KRATE_DEBUG") != "" }

// Recover converts a recover() value into an error. It always prints the value
// and stack to stderr so a contained panic is never silent, regardless of
// KRATE_DEBUG. When KRATE_DEBUG is set the panic is re-thrown unchanged.
func Recover(v any) error {
	if debugEnabled() {
		panic(v)
	}
	stack := debug.Stack()
	fmt.Fprintf(os.Stderr, "krate: recovered internal panic: %v\n%s\n", v, stack)
	return &PanicError{Value: v, Stack: stack}
}

// Run executes fn, converting a panic into an error returned to the caller.
func Run(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = Recover(r)
		}
	}()
	return fn()
}

// Guard runs fn and recovers any panic, reporting it to onPanic (which may be
// nil). Intended for fire-and-forget goroutines whose return value is discarded.
func Guard(fn func(), onPanic func(error)) {
	defer func() {
		if r := recover(); r != nil {
			if onPanic != nil {
				onPanic(Recover(r))
			}
		}
	}()
	fn()
}
