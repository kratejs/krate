//go:build linux

package linux

import (
	"os"
)

// guiMemory is the profile the GUI tests run under: KRATE_TEST_MEMORY
// (default "balanced") with every optional feature off.
func guiMemory() Memory {
	p := os.Getenv("KRATE_TEST_MEMORY")
	if p == "" {
		p = "balanced"
	}
	return Memory{Profile: p}
}
