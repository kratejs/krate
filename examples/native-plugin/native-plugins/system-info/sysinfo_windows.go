package sysinfo

import (
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	getSystemTimes   = kernel32.NewProc("GetSystemTimes")
	globalMemoryStat = kernel32.NewProc("GlobalMemoryStatusEx")
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func readMemory() (MemoryInfo, error) {
	var m memoryStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	if r, _, err := globalMemoryStat.Call(uintptr(unsafe.Pointer(&m))); r == 0 {
		return MemoryInfo{}, err
	}
	return MemoryInfo{Total: m.TotalPhys, Available: m.AvailPhys}, nil
}

type fileTime struct{ Low, High uint32 }

func (f fileTime) value() uint64 { return uint64(f.High)<<32 | uint64(f.Low) }

func readCPUTimes() (cpuTimes, error) {
	var idle, kernel, user fileTime
	r, _, err := getSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if r == 0 {
		return cpuTimes{}, err
	}
	// Kernel time includes idle time.
	total := kernel.value() + user.value()
	return cpuTimes{busy: total - idle.value(), total: total}, nil
}
