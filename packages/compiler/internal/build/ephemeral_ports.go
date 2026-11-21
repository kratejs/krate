package build

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// ephemeralPortRange is the port range the OS hands out to bind(0) and to the
// local end of outgoing connections. Linux reads it from procfs; macOS and
// Windows default to the IANA range.
var ephemeralPortRange = sync.OnceValues(func() (int, int) {
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range"); err == nil {
			if f := strings.Fields(string(data)); len(f) == 2 {
				lo, err1 := strconv.Atoi(f[0])
				hi, err2 := strconv.Atoi(f[1])
				if err1 == nil && err2 == nil && 0 < lo && lo <= hi && hi <= 65535 {
					return lo, hi
				}
			}
		}
		return 32768, 60999
	}
	return 49152, 65535
})

// inEphemeralRange reports whether the OS may hand port to another socket.
func inEphemeralRange(port int) bool {
	lo, hi := ephemeralPortRange()
	return port >= lo && port <= hi
}
