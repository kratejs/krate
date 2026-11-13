//go:build linux

package linux

import (
	"sync"

	"github.com/ebitengine/purego"
)

var (
	trimOnce sync.Once
	trimFn   func(pad uintptr) int32
)

// mallocTrim asks glibc to give freed heap pages back to the kernel. GTK,
// GLib and the WebKit client code allocate with malloc, and the arenas keep
// freed memory after a page loads; trimming returns it (typically 2-8 MB in
// the UI process). It does nothing where malloc_trim does not exist (musl).
func mallocTrim() {
	trimOnce.Do(func() {
		h, err := purego.Dlopen("libc.so.6", rtldNow|rtldGlobal)
		if err != nil {
			return
		}
		if _, err := purego.Dlsym(h, "malloc_trim"); err != nil {
			return
		}
		purego.RegisterLibFunc(&trimFn, h, "malloc_trim")
	})
	if trimFn != nil {
		trimFn(0)
	}
}
