package shell

import (
	"fmt"
	"sort"
	"strings"
)

type segKind int

// Ranks double as specificity: a lower rank wins.
const (
	segStatic segKind = iota
	segParam
	segRest         // [...name]: one or more segments
	segOptionalRest // [[...name]]: zero or more segments
)

type segment struct {
	kind segKind
	text string // static text
}

type route struct {
	segs []segment
	file string
}

// Matcher resolves URL paths against Krate route patterns.
type Matcher struct {
	routes []route
}

func parsePattern(p string) ([]segment, error) {
	if !strings.HasPrefix(p, "/") {
		return nil, fmt.Errorf("pattern %q must start with /", p)
	}
	var segs []segment
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return nil, nil
	}
	for i, part := range parts {
		last := i == len(parts)-1
		var s segment
		switch {
		case part == "":
			return nil, fmt.Errorf("pattern %q has an empty segment", p)
		case strings.HasPrefix(part, "[[...") && strings.HasSuffix(part, "]]") && len(part) > 7:
			s.kind = segOptionalRest
		case strings.HasPrefix(part, "[...") && strings.HasSuffix(part, "]") && len(part) > 5:
			s.kind = segRest
		case strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") && len(part) > 2 && !strings.ContainsAny(part[1:len(part)-1], "[]."):
			s.kind = segParam
		case strings.ContainsAny(part, "[]"):
			return nil, fmt.Errorf("pattern %q: segment %q is not [param], [...rest] or [[...rest]]", p, part)
		default:
			s.kind = segStatic
			s.text = part
		}
		if (s.kind == segRest || s.kind == segOptionalRest) && !last {
			return nil, fmt.Errorf("pattern %q: a catch-all segment must be last", p)
		}
		segs = append(segs, s)
	}
	return segs, nil
}

// NewMatcher compiles the manifest fallbacks. When several patterns match a
// path, the most specific one wins (static segments before [param] before
// catch-alls); equally specific patterns keep manifest order.
func NewMatcher(fallbacks []Fallback) (*Matcher, error) {
	m := &Matcher{}
	for _, f := range fallbacks {
		segs, err := parsePattern(f.Pattern)
		if err != nil {
			return nil, err
		}
		m.routes = append(m.routes, route{segs: segs, file: f.File})
	}
	sort.SliceStable(m.routes, func(i, j int) bool { return moreSpecific(m.routes[i].segs, m.routes[j].segs) })
	return m, nil
}

func moreSpecific(a, b []segment) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i].kind != b[i].kind {
			return a[i].kind < b[i].kind
		}
	}
	return len(a) > len(b)
}

// Match returns the file for urlPath (cleaned, starting with /).
func (m *Matcher) Match(urlPath string) (string, bool) {
	var parts []string
	if t := strings.Trim(urlPath, "/"); t != "" {
		parts = strings.Split(t, "/")
	}
	for _, r := range m.routes {
		if matchSegs(r.segs, parts) {
			return r.file, true
		}
	}
	return "", false
}

func matchSegs(segs []segment, parts []string) bool {
	for i, s := range segs {
		switch s.kind {
		case segRest:
			return len(parts) > i
		case segOptionalRest:
			return true
		case segParam:
			if i >= len(parts) {
				return false
			}
		case segStatic:
			if i >= len(parts) || parts[i] != s.text {
				return false
			}
		}
	}
	return len(parts) == len(segs)
}
