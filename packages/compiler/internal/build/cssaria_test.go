package build

import (
	"regexp"
	"strings"
)

// bootScriptHolds reports whether an inline <script data-krate-boot> in html
// contains marker.
func bootScriptHolds(html, marker string) bool {
	for _, m := range regexp.MustCompile(`(?s)<script[^>]*data-krate-boot[^>]*>(.*?)</script>`).FindAllStringSubmatch(html, -1) {
		if strings.Contains(m[1], marker) {
			return true
		}
	}
	return false
}
