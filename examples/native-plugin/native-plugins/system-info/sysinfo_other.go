//go:build !linux && !darwin && !windows

package sysinfo

import "errors"

func readMemory() (MemoryInfo, error) {
	return MemoryInfo{}, errors.New("sysinfo: not supported on this platform")
}

func readCPUTimes() (cpuTimes, error) {
	return cpuTimes{}, errors.New("sysinfo: not supported on this platform")
}
