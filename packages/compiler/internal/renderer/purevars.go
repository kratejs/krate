package renderer

import (
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

var pureVarNameRe = regexp.MustCompile(`^var ([A-Za-z_$][\w$]*)=`)

// dropUnusedPureVars removes build-time data declarations (folded content
// entries, literal tables) that no other hydration code references, and strips
// the markers from the rest. Without this a page that only needs one signal
// would still ship the whole collection entry it looked up at build time.
func dropUnusedPureVars(js string) string {
	if !strings.Contains(js, irtree.PureVarBegin) {
		return js
	}
	type region struct {
		start, end int // [start, end) covers begin marker through end marker
		name       string
	}
	var regions []region
	var rest strings.Builder
	pos := 0
	for {
		i := strings.Index(js[pos:], irtree.PureVarBegin)
		if i < 0 {
			rest.WriteString(js[pos:])
			break
		}
		i += pos
		j := strings.Index(js[i:], irtree.PureVarEnd)
		if j < 0 {
			rest.WriteString(js[pos:])
			regions = nil
			break
		}
		j += i + len(irtree.PureVarEnd)
		rest.WriteString(js[pos:i])
		name := ""
		if m := pureVarNameRe.FindStringSubmatch(js[i+len(irtree.PureVarBegin) : j]); m != nil {
			name = m[1]
		}
		regions = append(regions, region{start: i, end: j, name: name})
		pos = j
	}
	if len(regions) == 0 {
		return strings.ReplaceAll(strings.ReplaceAll(js, irtree.PureVarBegin, ""), irtree.PureVarEnd, "")
	}
	code := rest.String()
	var out strings.Builder
	pos = 0
	for _, r := range regions {
		out.WriteString(js[pos:r.start])
		pos = r.end
		used := r.name == "" || regexp.MustCompile(`(^|[^\w$])`+regexp.QuoteMeta(r.name)+`($|[^\w$])`).MatchString(code)
		if used {
			out.WriteString(js[r.start+len(irtree.PureVarBegin) : r.end-len(irtree.PureVarEnd)])
			continue
		}
		// Drop the declaration and the statement terminator that follows.
		if strings.HasPrefix(js[pos:], ";\n") {
			pos += 2
		} else if strings.HasPrefix(js[pos:], ";") {
			pos++
		}
	}
	out.WriteString(js[pos:])
	return out.String()
}
