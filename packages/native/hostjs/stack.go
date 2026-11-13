package hostjs

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// QuickJS prints frames as "    at name (<eval>:LINE:COL)" or "    at <eval>:LINE:COL".
var frameRe = regexp.MustCompile(`^(\s*at )(?:(.*?) \()?<eval>:(\d+):(\d+)\)?$`)

// mapStack rewrites the frames that belong to the host bundle (those below
// lineBase are the web API prelude and host.js) to original source positions,
// and drops the frames of host.js's own plumbing.
func (h *Host) mapStack(stack string) string {
	if stack == "" {
		return ""
	}
	var out []string
	for _, line := range strings.Split(strings.TrimRight(stack, "\n"), "\n") {
		m := frameRe.FindStringSubmatch(line)
		if m == nil {
			out = append(out, line)
			continue
		}
		ln, _ := strconv.Atoi(m[3])
		col, _ := strconv.Atoi(m[4])
		if ln <= lineBase {
			if strings.Contains(m[2], "__kh_") {
				continue
			}
			out = append(out, line)
			continue
		}
		ln -= lineBase
		file, oline, ocol := h.filename, ln, col
		name := m[2]
		if p, ok := h.sm.lookup(ln, col); ok {
			file, oline, ocol = p.Source, p.Line, p.Col
		}
		if name != "" {
			out = append(out, fmt.Sprintf("%s%s (%s:%d:%d)", m[1], name, file, oline, ocol))
		} else {
			out = append(out, fmt.Sprintf("%s%s:%d:%d", m[1], file, oline, ocol))
		}
	}
	return strings.Join(out, "\n")
}

var frameLoc = regexp.MustCompile(`\(?([^()\s]+:\d+:\d+)\)?$`)

// firstFrame returns the location of the first frame of a mapped stack that is
// not part of the web API prelude.
func firstFrame(stack string) string {
	for _, line := range strings.Split(stack, "\n") {
		if strings.Contains(line, "<eval>") || strings.Contains(line, "(native)") {
			continue
		}
		if m := frameLoc.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			return m[1]
		}
	}
	return ""
}
