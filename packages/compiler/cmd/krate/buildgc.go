package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/kratejs/krate/packages/compiler/internal/jsworker"
	"github.com/kratejs/krate/packages/compiler/internal/membudget"
)

// workerReserve is the memory set aside for each build-time JavaScript
// worker below the Go heap limit.
const workerReserve = 256 << 20

// tuneBuildGC sets the Go soft memory limit from the build's memory budget
// (KRATE_MAX_MEMORY, or three quarters of the memory available at start; see
// membudget.GoLimit), so the collector works harder as the heap nears it
// instead of the machine swapping. A one-shot build also gets more heap
// between collections below that limit (the collector otherwise takes an
// eighth of the build's CPU). GOGC and GOMEMLIMIT set by the user win.
func tuneBuildGC(oneShot bool) {
	b, err := membudget.Current()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sWarning:%s %v; using a budget of %s\n", cYellow, cReset, err, membudget.Format(b.Bytes))
	}
	membudget.ApplyGoLimit(int64(jsworker.DefaultSize()) * workerReserve)
	if oneShot && os.Getenv("GOGC") == "" {
		debug.SetGCPercent(200)
	}
}
