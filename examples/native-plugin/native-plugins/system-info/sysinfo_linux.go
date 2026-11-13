package sysinfo

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
)

func readMemory() (MemoryInfo, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemoryInfo{}, err
	}
	defer f.Close()
	var m MemoryInfo
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		kb, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			m.Total = kb * 1024
		case "MemAvailable":
			m.Available = kb * 1024
		}
	}
	return m, sc.Err()
}

// readCPUTimes reads the aggregate line of /proc/stat.
func readCPUTimes() (cpuTimes, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	return parseProcStat(string(data))
}

func parseProcStat(s string) (cpuTimes, error) {
	line, _, _ := strings.Cut(s, "\n")
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return cpuTimes{}, errors.New("unexpected /proc/stat")
	}
	var total, idle uint64
	for i, v := range f[1:] {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return cpuTimes{}, err
		}
		// user nice system idle iowait irq softirq steal; guest time is
		// already inside user.
		if i >= 8 {
			break
		}
		total += n
		if i == 3 || i == 4 {
			idle += n
		}
	}
	return cpuTimes{busy: total - idle, total: total}, nil
}
