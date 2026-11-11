package sdkos

import (
	"os"
	"runtime"
	"strings"
)

// Info describes the machine and the app.
type Info struct {
	// OS is runtime.GOOS: "linux", "darwin" or "windows".
	OS string
	// Arch is runtime.GOARCH.
	Arch string
	// OSVersion is a human-readable OS version when cheaply available, such
	// as "Debian GNU/Linux 13 (trixie)", "14.5" or "10.0.22631"; else empty.
	OSVersion string
	Hostname  string
	// Locale is a BCP 47 tag such as "en-US"; "en-US" when unknown.
	Locale     string
	AppID      string
	AppVersion string
}

// GetInfo collects Info. Missing pieces are empty strings, not errors.
func GetInfo() Info {
	a, _ := Current()
	host, _ := os.Hostname()
	loc := systemLocale()
	if loc == "" {
		loc = "en-US"
	}
	return Info{
		OS: runtime.GOOS, Arch: runtime.GOARCH,
		OSVersion: osVersion(), Hostname: host, Locale: loc,
		AppID: a.ID, AppVersion: a.Version,
	}
}

// localeFromEnv turns POSIX locale variables into a BCP 47 tag. "C" and
// "POSIX" count as unset.
func localeFromEnv(getenv func(string) string) string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := getenv(k)
		if v == "" || v == "C" || v == "POSIX" {
			continue
		}
		if i := strings.IndexAny(v, ".@"); i >= 0 {
			v = v[:i]
		}
		if v == "" || v == "C" || v == "POSIX" {
			continue
		}
		return strings.ReplaceAll(v, "_", "-")
	}
	return ""
}
