//go:build darwin || linux

package sdkos

import "strings"

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
