package membudget

import (
	"os"
	"strconv"
)

// ProcessRSS returns the resident memory of process pid, 0 when unknown.
func ProcessRSS(pid int) int64 {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm")
	if err != nil {
		return 0
	}
	return statmRSS(string(b), os.Getpagesize())
}
