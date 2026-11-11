package pkg

import (
	"fmt"
	"regexp"
	"strings"
)

var semverRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

type semver struct {
	Major, Minor, Patch string
	Pre                 string
}

func splitVersion(v string) (semver, bool) {
	m := semverRe.FindStringSubmatch(v)
	if m == nil {
		return semver{}, false
	}
	return semver{m[1], m[2], m[3], m[4]}, true
}

// core is "x.y.z" without prerelease and build parts.
func (s semver) core() string { return s.Major + "." + s.Minor + "." + s.Patch }

// msix is the four-part version MSIX wants; the last part must be 0 for the
// Microsoft Store.
func (s semver) msix() string { return s.core() + ".0" }

// debian turns a prerelease into a "~" suffix so it sorts before the release.
func (s semver) debian() string {
	if s.Pre != "" {
		return s.core() + "~" + s.Pre
	}
	return s.core()
}

// fileVersion is the four numbers of a Windows version resource.
func (s semver) fileVersion() [4]uint16 {
	var out [4]uint16
	for i, p := range []string{s.Major, s.Minor, s.Patch} {
		var n int
		fmt.Sscanf(p, "%d", &n)
		if n > 65535 {
			n = 65535
		}
		out[i] = uint16(n)
	}
	return out
}

func macCategory(c string) string {
	c = strings.TrimSpace(c)
	switch {
	case c == "":
		return "public.app-category.utilities"
	case strings.Contains(c, "."):
		return c
	}
	return "public.app-category." + strings.ToLower(c)
}
