package mcp

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// findingTags maps a check that names an element to the source tag it is about.
var findingTags = map[string]*regexp.Regexp{
	"a11y/img-alt":     regexp.MustCompile(`<(?:img|Image)\b`),
	"perf/image-dims":  regexp.MustCompile(`<(?:img|Image)\b`),
	"a11y/form-label":  regexp.MustCompile(`<(?:input|select|textarea)\b`),
	"a11y/button-name": regexp.MustCompile(`<button\b`),
	"a11y/link-name":   regexp.MustCompile(`<(?:a|Link)\b`),
}

// findingLocator finds a source position for checks, which run on built HTML.
// The nth finding of a rule in a file maps to the nth matching tag there; a tag
// rendered by a child component has no match and stays without a position.
type findingLocator struct {
	s    *Service
	seen map[string]int
}

func (s *Service) newFindingLocator() *findingLocator {
	return &findingLocator{s: s, seen: map[string]int{}}
}

func (l *findingLocator) next(rule, file string) (line, col int, ok bool) {
	re := findingTags[rule]
	if re == nil || file == "" {
		return 0, 0, false
	}
	key := rule + "\x00" + file
	n := l.seen[key]
	l.seen[key] = n + 1
	data, err := os.ReadFile(filepath.Join(l.s.root, filepath.FromSlash(file)))
	if err != nil {
		return 0, 0, false
	}
	text := string(data)
	matches := re.FindAllStringIndex(text, -1)
	if n >= len(matches) {
		return 0, 0, false
	}
	at := matches[n][0]
	line = strings.Count(text[:at], "\n") + 1
	col = at - strings.LastIndexByte(text[:at], '\n')
	return line, col, true
}
