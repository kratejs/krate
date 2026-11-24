package build

import (
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/membudget"
)

// devRebuildGCPercent is the collector's target while a dev rebuild runs. A
// rebuild allocates a lot in a few seconds (every page parses and rewrites its
// module graph) and a collector sized for the live heap spent a fifth of the
// rebuild's CPU marking it again and again.
const devRebuildGCPercent = 400

// boostDevGC raises the garbage collector's target for the length of a dev
// rebuild and returns the function that puts it back. The memory budget the
// builds already honour (KRATE_MAX_MEMORY, the cgroup limit, free memory)
// becomes the collector's soft limit, so the larger target never takes the
// process past it. A GOGC set by the user is left alone.
func boostDevGC() (restore func()) {
	if os.Getenv("GOGC") != "" {
		return func() {}
	}
	oldPercent := debug.SetGCPercent(devRebuildGCPercent)
	oldLimit := debug.SetMemoryLimit(-1)
	if b, _ := membudget.Current(); b.Bytes > 0 {
		debug.SetMemoryLimit(b.Bytes)
	}
	return func() {
		debug.SetGCPercent(oldPercent)
		debug.SetMemoryLimit(oldLimit)
		scheduleFreeOSMemory()
	}
}

// freeMemoryAfter is how long the dev server stays idle before it hands the
// memory a rebuild's heap grew into back to the system. The first request
// after a rebuild is not made to wait for the collection this needs.
const freeMemoryAfter = 10 * time.Second

var (
	freeMu    sync.Mutex
	freeTimer *time.Timer
)

func scheduleFreeOSMemory() {
	freeMu.Lock()
	defer freeMu.Unlock()
	if freeTimer == nil {
		freeTimer = time.AfterFunc(freeMemoryAfter, debug.FreeOSMemory)
		return
	}
	freeTimer.Reset(freeMemoryAfter)
}
