//go:build !linux

package membudget

// ProcessRSS returns the resident memory of process pid, 0 when unknown.
func ProcessRSS(pid int) int64 { return 0 }
