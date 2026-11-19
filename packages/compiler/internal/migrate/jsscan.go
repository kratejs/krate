package migrate

import (
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/jsscan"
)

var jsIdentRe = regexp.MustCompile(`[A-Za-z_$][\w$]*`)

// identifiers returns the set of identifier-like words in the code of src.
// Property names are included; callers use it to over-approximate references.
func identifiers(src string) map[string]bool {
	out := map[string]bool{}
	for _, m := range jsIdentRe.FindAllString(jsscan.CodeOnly(src), -1) {
		out[m] = true
	}
	return out
}

// splitTop splits the bracketed expression starting at src[open] (one of
// `[`, `{`, `(`) at its top-level commas. It returns the trimmed parts, in
// order; empty parts (a trailing comma) are dropped.
func splitTop(src string, open int) ([]string, bool) {
	if open < 0 || open >= len(src) || strings.IndexByte("[{(", src[open]) < 0 {
		return nil, false
	}
	mask := jsscan.Mask(src)
	depth := 0
	start := open + 1
	var parts []string
	add := func(end int) {
		if p := strings.TrimSpace(src[start:end]); p != "" {
			parts = append(parts, p)
		}
	}
	for i := open; i < len(src); i++ {
		if !mask[i] {
			continue
		}
		switch src[i] {
		case '[', '{', '(':
			depth++
		case ']', '}', ')':
			depth--
			if depth == 0 {
				add(i)
				return parts, true
			}
		case ',':
			if depth == 1 {
				add(i)
				start = i + 1
			}
		}
	}
	return nil, false
}

// dedent removes the indentation shared by the lines after the first, so a
// multi-line snippet can be re-indented where it is placed.
func dedent(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return s
	}
	min := -1
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " \t"))
		if min < 0 || n < min {
			min = n
		}
	}
	if min <= 0 {
		return s
	}
	for i := 1; i < len(lines); i++ {
		if len(lines[i]) >= min {
			lines[i] = lines[i][min:]
		} else {
			lines[i] = strings.TrimLeft(lines[i], " \t")
		}
	}
	return strings.Join(lines, "\n")
}

// indentLines prefixes every line after the first with indent.
func indentLines(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			lines[i] = indent + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}
