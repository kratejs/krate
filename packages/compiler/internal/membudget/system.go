package membudget

import (
	"bufio"
	"io"
	"path"
	"strconv"
	"strings"
)

// parseMeminfo returns MemTotal and MemAvailable of a /proc/meminfo, in
// bytes (0 when missing).
func parseMeminfo(r io.Reader) (total, avail int64) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		n, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			continue
		}
		if len(f) > 2 && strings.EqualFold(f[2], "kB") {
			n *= kib
		}
		switch f[0] {
		case "MemTotal:":
			total = n
		case "MemAvailable:":
			avail = n
		}
	}
	return total, avail
}

// cgroupPath returns the cgroup v2 path of a /proc/self/cgroup ("0::/a/b"),
// or "" when the process is not in a v2 hierarchy.
func cgroupPath(selfCgroup string) string {
	for _, line := range strings.Split(selfCgroup, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "0::"); ok {
			return p
		}
	}
	return ""
}

// parseCgroupMax parses memory.max: a byte count, or "max" for no limit
// (ok false).
func parseCgroupMax(s string) (n int64, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "max" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// statValue returns key's value in a memory.stat.
func statValue(stat, key string) int64 {
	for _, line := range strings.Split(stat, "\n") {
		k, v, ok := strings.Cut(line, " ")
		if ok && k == key {
			n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return n
		}
	}
	return 0
}

// cgroupLimits walks a cgroup v2 hierarchy from the process's group up to the
// root and returns the tightest memory limit and the headroom left under it
// (the limit less the memory in use that cannot be reclaimed). ok is false
// when no group sets a limit.
func cgroupLimits(read func(name string) (string, error), group string) (limit, headroom int64, ok bool) {
	for dir := path.Clean("/" + group); ; dir = path.Dir(dir) {
		if s, err := read(path.Join(dir, "memory.max")); err == nil {
			if lim, has := parseCgroupMax(s); has {
				cur := int64(0)
				if c, err := read(path.Join(dir, "memory.current")); err == nil {
					cur, _ = strconv.ParseInt(strings.TrimSpace(c), 10, 64)
				}
				if st, err := read(path.Join(dir, "memory.stat")); err == nil {
					cur -= statValue(st, "inactive_file")
				}
				room := max(lim-max(cur, 0), 0)
				if !ok || lim < limit {
					limit = lim
				}
				if !ok || room < headroom {
					headroom = room
				}
				ok = true
			}
		}
		if dir == "/" {
			return limit, headroom, ok
		}
	}
}

// linuxSystem reads /proc/meminfo and the cgroup v2 limits under root
// ("/sys/fs/cgroup").
func linuxSystem(readFile func(string) ([]byte, error), root string) system {
	var sys system
	if data, err := readFile("/proc/meminfo"); err == nil {
		sys.total, sys.avail = parseMeminfo(strings.NewReader(string(data)))
	}
	self, err := readFile("/proc/self/cgroup")
	if err != nil {
		return sys
	}
	group := cgroupPath(string(self))
	if group == "" {
		return sys
	}
	read := func(name string) (string, error) {
		b, err := readFile(path.Join(root, name))
		return string(b), err
	}
	limit, room, ok := cgroupLimits(read, group)
	if !ok {
		return sys
	}
	if sys.total <= 0 || limit < sys.total {
		sys.total = limit
		sys.cgroup = true
	}
	if sys.avail <= 0 || room < sys.avail {
		sys.avail = room
		sys.cgroup = true
	}
	return sys
}

// statmRSS returns the resident bytes of a /proc/<pid>/statm.
func statmRSS(statm string, pageSize int) int64 {
	f := strings.Fields(statm)
	if len(f) < 2 {
		return 0
	}
	n, err := strconv.ParseInt(f[1], 10, 64)
	if err != nil {
		return 0
	}
	return n * int64(pageSize)
}
