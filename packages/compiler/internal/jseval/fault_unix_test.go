//go:build unix

package jseval

import (
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// A fault inside the embedded engine must come back as an error: Go aborts the
// whole process on one unless the goroutine asked for a panic.
func TestGuardFaultsReturnsAnError(t *testing.T) {
	page, err := syscall.Mmap(-1, 0, syscall.Getpagesize(), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		t.Skipf("mmap: %v", err)
	}
	p := unsafe.Pointer(&page[0])
	if err := syscall.Munmap(page); err != nil {
		t.Skipf("munmap: %v", err)
	}
	err = guardFaults(func() error {
		_ = *(*byte)(p)
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "crashed") {
		t.Fatalf("got %v, want a crash error", err)
	}
}
