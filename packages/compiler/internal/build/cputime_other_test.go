//go:build !unix

package build

import "time"

// processCPU is not measured on this platform.
func processCPU() time.Duration { return 0 }
