//go:build !linux && !darwin && !windows

package memstat

// Sample is not implemented on this OS.
func Sample(pids ...int) ([]Proc, error) { return nil, ErrUnsupported }

// Descendants is not implemented on this OS.
func Descendants(pid int) ([]int, error) { return nil, ErrUnsupported }

// Name is not implemented on this OS.
func Name(pid int) string { return "" }
