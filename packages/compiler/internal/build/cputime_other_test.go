//go:build !unix

package build

import "time"

// processCPU is not measured on this platform.
func processCPU() time.Duration { return 0 }

// childCPU is not measured on this platform.
func childCPU() time.Duration { return 0 }
