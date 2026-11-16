package main

import (
	"bufio"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// tuneBuildGC gives a one-shot build more heap between collections (the
// collector otherwise takes an eighth of the build's CPU) under a soft limit
// of 60% of physical memory, so the heap never pushes the machine into swap.
// GOGC and GOMEMLIMIT set by the user win; without a known memory size the
// runtime defaults stay.
func tuneBuildGC() {
	total := physicalMemory()
	if total <= 0 {
		return
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(total / 10 * 6)
	}
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(200)
	}
}

// physicalMemory returns the machine's memory in bytes from /proc/meminfo,
// or 0 where that is not available.
func physicalMemory() int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0
			}
			return kb * 1024
		}
	}
	return 0
}
