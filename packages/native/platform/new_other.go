//go:build !linux && !darwin && !windows

package platform

import (
	"fmt"
	"runtime"
)

// New is not supported on this OS.
func New(opts Options) (App, error) {
	return nil, fmt.Errorf("krate: desktop windows are not supported on %s", runtime.GOOS)
}
