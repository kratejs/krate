//go:build windows

package memstat

import (
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"

	xw "golang.org/x/sys/windows"
)

// processMemoryCountersEx is PROCESS_MEMORY_COUNTERS_EX from <psapi.h>.
type processMemoryCountersEx struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
}

var procGetProcessMemoryInfo = xw.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

// Sample reads the memory of each pid with GetProcessMemoryInfo. RSS is the
// working set; PSS and PrivateDirty are both PrivateUsage. A pid that cannot
// be opened is skipped and reported in the error.
func Sample(pids ...int) ([]Proc, error) {
	var out []Proc
	var errs []error
	for _, pid := range pids {
		h, err := xw.OpenProcess(xw.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
		if err != nil {
			errs = append(errs, fmt.Errorf("memstat: pid %d: %w", pid, err))
			continue
		}
		var c processMemoryCountersEx
		c.CB = uint32(unsafe.Sizeof(c))
		r, _, e := procGetProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.CB))
		name := imageName(h)
		_ = xw.CloseHandle(h)
		if r == 0 {
			errs = append(errs, fmt.Errorf("memstat: pid %d: GetProcessMemoryInfo: %w", pid, e))
			continue
		}
		out = append(out, Proc{
			PID: pid, Name: name,
			RSS: uint64(c.WorkingSetSize), PSS: uint64(c.PrivateUsage), PrivateDirty: uint64(c.PrivateUsage),
		})
	}
	return out, errors.Join(errs...)
}

func imageName(h xw.Handle) string {
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if err := xw.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return filepath.Base(syscall.UTF16ToString(buf[:n]))
}

// Name is the executable name of pid, or "".
func Name(pid int) string {
	h, err := xw.OpenProcess(xw.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer xw.CloseHandle(h)
	return imageName(h)
}

// Descendants returns every process below pid, from a Toolhelp32 snapshot.
func Descendants(pid int) ([]int, error) {
	snap, err := xw.CreateToolhelp32Snapshot(xw.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer xw.CloseHandle(snap)
	parent := map[int]int{}
	e := xw.ProcessEntry32{Size: uint32(unsafe.Sizeof(xw.ProcessEntry32{}))}
	for err := xw.Process32First(snap, &e); err == nil; err = xw.Process32Next(snap, &e) {
		parent[int(e.ProcessID)] = int(e.ParentProcessID)
	}
	return descendantsOf(pid, parent), nil
}
