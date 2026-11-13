//go:build darwin

package memstat

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// rusageInfoV4 is the front of struct rusage_info_v4 from <sys/resource.h>
// (RUSAGE_INFO_V4 = 4); the tail is padding so the kernel never writes past
// the buffer. Only ri_resident_size and ri_phys_footprint are read.
type rusageInfoV4 struct {
	UUID             [16]byte
	UserTime         uint64
	SystemTime       uint64
	PkgIdleWkups     uint64
	InterruptWkups   uint64
	Pageins          uint64
	WiredSize        uint64
	ResidentSize     uint64
	PhysFootprint    uint64
	ProcStartAbstime uint64
	ProcExitAbstime  uint64
	_                [64]uint64
}

const rusageInfoV4Flavor = 4

var (
	libprocOnce sync.Once
	libprocErr  error

	procPidRusage func(pid int32, flavor int32, buf unsafe.Pointer) int32
	procName      func(pid int32, buf unsafe.Pointer, size uint32) int32
	listChildPids func(ppid int32, buf unsafe.Pointer, size int32) int32
)

func loadLibproc() error {
	libprocOnce.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libproc.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			libprocErr = fmt.Errorf("memstat: libproc: %w", err)
			return
		}
		purego.RegisterLibFunc(&procPidRusage, lib, "proc_pid_rusage")
		purego.RegisterLibFunc(&procName, lib, "proc_name")
		purego.RegisterLibFunc(&listChildPids, lib, "proc_listchildpids")
	})
	return libprocErr
}

// Sample reads the memory of each pid with proc_pid_rusage. PSS and
// PrivateDirty both hold ri_phys_footprint. A pid that cannot be read (it
// exited, or belongs to another user) is skipped and reported in the error.
func Sample(pids ...int) ([]Proc, error) {
	if err := loadLibproc(); err != nil {
		return nil, err
	}
	var out []Proc
	var firstErr error
	for _, pid := range pids {
		var ri rusageInfoV4
		if r := procPidRusage(int32(pid), rusageInfoV4Flavor, unsafe.Pointer(&ri)); r != 0 {
			if firstErr == nil {
				firstErr = fmt.Errorf("memstat: proc_pid_rusage(%d) failed (%d)", pid, r)
			}
			continue
		}
		out = append(out, Proc{
			PID: pid, Name: Name(pid),
			RSS: ri.ResidentSize, PSS: ri.PhysFootprint, PrivateDirty: ri.PhysFootprint,
		})
	}
	return out, firstErr
}

// Name is the process name of pid, or "".
func Name(pid int) string {
	if loadLibproc() != nil {
		return ""
	}
	buf := make([]byte, 256)
	n := procName(int32(pid), unsafe.Pointer(&buf[0]), uint32(len(buf)))
	if n <= 0 {
		return ""
	}
	return string(buf[:n])
}

// Descendants returns every process below pid. WebKit's own processes are
// XPC services and are not among them.
func Descendants(pid int) ([]int, error) {
	if err := loadLibproc(); err != nil {
		return nil, err
	}
	var out []int
	queue := []int{pid}
	seen := map[int]bool{pid: true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		buf := make([]int32, 1024)
		listChildPids(int32(cur), unsafe.Pointer(&buf[0]), int32(len(buf)*4))
		// Whether the call returns bytes or a count differs between OS
		// releases; the buffer is zeroed, so read until the first 0.
		for _, c := range buf {
			if c <= 0 {
				break
			}
			if seen[int(c)] {
				continue
			}
			seen[int(c)] = true
			out = append(out, int(c))
			queue = append(queue, int(c))
		}
	}
	return out, nil
}
