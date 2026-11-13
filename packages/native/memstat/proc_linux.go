//go:build linux

package memstat

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// Sample reads the memory of each pid. A pid that no longer exists is skipped.
// Other errors (for example a process of another user) are joined into the
// returned error together with the processes that could be read.
func Sample(pids ...int) ([]Proc, error) {
	var out []Proc
	var errs []error
	for _, pid := range pids {
		p, err := sampleOne(pid)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
			continue
		}
		out = append(out, p)
	}
	return out, errors.Join(errs...)
}

func sampleOne(pid int) (Proc, error) {
	dir := "/proc/" + strconv.Itoa(pid)
	p, err := readSmapsFile(dir + "/smaps_rollup")
	if err != nil {
		// Kernels before 4.14 have no smaps_rollup; a process of another
		// user denies both.
		var err2 error
		p, err2 = readSmapsFile(dir + "/smaps")
		if err2 != nil {
			if errors.Is(err, fs.ErrNotExist) && errors.Is(err2, fs.ErrNotExist) {
				return Proc{}, fs.ErrNotExist
			}
			return Proc{}, fmt.Errorf("memstat: pid %d: %w", pid, err2)
		}
	}
	p.PID = pid
	p.Name = procName(pid)
	return p, nil
}

func readSmapsFile(path string) (Proc, error) {
	f, err := os.Open(path)
	if err != nil {
		return Proc{}, err
	}
	defer f.Close()
	return parseSmaps(f)
}

// procName is the executable's base name, or the kernel's (truncated) command
// name when the link is unreadable.
func procName(pid int) string {
	dir := "/proc/" + strconv.Itoa(pid)
	if exe, err := os.Readlink(dir + "/exe"); err == nil {
		return filepath.Base(trimDeleted(exe))
	}
	if b, err := os.ReadFile(dir + "/comm"); err == nil {
		for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == 0) {
			b = b[:len(b)-1]
		}
		return string(b)
	}
	return ""
}

func trimDeleted(s string) string {
	const suffix = " (deleted)"
	if len(s) > len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}

// Descendants returns every process below pid (children, grandchildren, ...).
func Descendants(pid int) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	parent := map[int]int{}
	for _, e := range entries {
		n, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		if pp, ok := parseStatPPID(string(b)); ok {
			parent[n] = pp
		}
	}
	return descendantsOf(pid, parent), nil
}

// Name is the executable name of pid, or "".
func Name(pid int) string { return procName(pid) }
