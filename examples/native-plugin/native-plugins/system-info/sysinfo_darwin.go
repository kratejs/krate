package sysinfo

import (
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func readMemory() (MemoryInfo, error) {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return MemoryInfo{}, err
	}
	total, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	return MemoryInfo{Total: total}, err
}

// readCPUTimes approximates the busy time from ps: the sum of the per-process
// CPU percentages over the number of CPUs, accumulated as one second per call.
func readCPUTimes() (cpuTimes, error) {
	out, err := exec.Command("ps", "-A", "-o", "%cpu=").Output()
	if err != nil {
		return cpuTimes{}, err
	}
	sum := 0.0
	for _, l := range strings.Fields(string(out)) {
		if v, err := strconv.ParseFloat(l, 64); err == nil {
			sum += v
		}
	}
	if runtime.NumCPU() == 0 {
		return cpuTimes{}, errors.New("no CPUs")
	}
	load := sum / float64(runtime.NumCPU())
	if load > 100 {
		load = 100
	}
	// A reading is an instantaneous percentage, so every call is its own window.
	darwinTicks += 1000
	darwinBusy += uint64(load * 10)
	return cpuTimes{busy: darwinBusy, total: darwinTicks}, nil
}

var darwinTicks, darwinBusy uint64
