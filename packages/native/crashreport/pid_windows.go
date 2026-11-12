//go:build windows

package crashreport

import "syscall"

// processQueryLimitedInformation is PROCESS_QUERY_LIMITED_INFORMATION. The
// standard syscall package is enough here: the Go host imports this package,
// and golang.org/x/sys would have to be in every app's go.sum.
const processQueryLimitedInformation = 0x1000

func pidAlive(pid int) bool {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	const stillActive = 259
	return code == stillActive
}
