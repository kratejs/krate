package membudget

import (
	"encoding/binary"
	"syscall"
)

// readSystem knows the machine's memory on macOS but not what is free: half
// of it stands in for the memory available.
func readSystem() system {
	s, err := syscall.Sysctl("hw.memsize")
	if err != nil {
		return system{}
	}
	b := []byte(s)
	// Sysctl drops a trailing zero byte of the little-endian value.
	for len(b) < 8 {
		b = append(b, 0)
	}
	total := int64(binary.LittleEndian.Uint64(b[:8]))
	return system{total: total, avail: total / 2, guessed: true}
}
