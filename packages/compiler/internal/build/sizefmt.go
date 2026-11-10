package build

import "fmt"

// FormatBytes renders a size for humans: "512 B", "1.5 kB", "3.20 MB".
// Units are decimal (1 kB = 1000 B), matching what browsers and hosts report.
func FormatBytes(n int64) string {
	switch {
	case n < 0:
		return "-" + FormatBytes(-n)
	case n < 1000:
		return fmt.Sprintf("%d B", n)
	case n < 1000*1000:
		return fmt.Sprintf("%.1f kB", float64(n)/1000)
	case n < 1000*1000*1000:
		return fmt.Sprintf("%.2f MB", float64(n)/1e6)
	}
	return fmt.Sprintf("%.2f GB", float64(n)/1e9)
}
