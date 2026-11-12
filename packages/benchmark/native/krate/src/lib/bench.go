package lib

import (
	"os"
	"runtime"
)

// Ping is the host call behind the button.
func Ping(n int) string {
	return "pong " + runtime.GOOS + " " + itoa(n+1)
}

// Report hands the first-paint record to the harness: the file named by
// BENCH_REPORT gets one JSON line.
func Report(record string) error {
	path := os.Getenv("BENCH_REPORT")
	if path == "" {
		return nil
	}
	return os.WriteFile(path, []byte(record+"\n"), 0o644)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
