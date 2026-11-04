package css

import "strings"

// moreStatic are v4 utilities with a fixed declaration block.
var moreStatic = map[string]string{
	"justify-baseline":          "justify-content: baseline;",
	"place-content-baseline":    "place-content: baseline;",
	"place-items-baseline":      "place-items: baseline;",
	"place-content-center-safe": "place-content: safe center;",
	"place-content-end-safe":    "place-content: safe end;",
	"place-items-center-safe":   "place-items: safe center;",
	"place-items-end-safe":      "place-items: safe end;",
	"place-self-center-safe":    "place-self: safe center;",
	"place-self-end-safe":       "place-self: safe end;",
	"content-center-safe":       "align-content: safe center;",
	"content-end-safe":          "align-content: safe flex-end;",
	"break-inside-avoid-column": "break-inside: avoid-column;",
	"overflow-x-visible":        "overflow-x: visible;",
	"overflow-y-visible":        "overflow-y: visible;",
	"normal-nums":               "font-variant-numeric: normal;",
	"translate-3d":              "translate: var(--tw-translate-x) var(--tw-translate-y) var(--tw-translate-z);",
	"scrollbar-auto":            "scrollbar-width: auto;",
	"scrollbar-thin":            "scrollbar-width: thin;",
	"scrollbar-none":            "scrollbar-width: none;",
	"scrollbar-gutter-auto":     "scrollbar-gutter: auto;",
	"scrollbar-gutter-stable":   "scrollbar-gutter: stable;",
	"scrollbar-gutter-both":     "scrollbar-gutter: stable both-edges;",
	"contain-none":              "contain: none;",
	"contain-content":           "contain: content;",
	"contain-strict":            "contain: strict;",
	"transform-border":          "transform-box: border-box;",
	"transform-content":         "transform-box: content-box;",
	"transform-fill":            "transform-box: fill-box;",
	"transform-stroke":          "transform-box: stroke-box;",
	"transform-view":            "transform-box: view-box;",
	"bg-top-left":               "background-position: left top;",
	"bg-top-right":              "background-position: right top;",
	"bg-bottom-left":            "background-position: left bottom;",
	"bg-bottom-right":           "background-position: right bottom;",
	"object-top-left":           "object-position: left top;",
	"object-top-right":          "object-position: right top;",
	"object-bottom-left":        "object-position: left bottom;",
	"object-bottom-right":       "object-position: right bottom;",
	"via-none":                  "--tw-gradient-via-stops: initial;",
	"shadow-initial":            "--tw-shadow-color: initial;",
	"inset-shadow-initial":      "--tw-inset-shadow-color: initial;",
	"text-shadow-initial":       "--tw-text-shadow-color: initial;",
}

const containCompose = "contain: var(--tw-contain-size,) var(--tw-contain-layout,) var(--tw-contain-paint,) var(--tw-contain-style,);"

// moreUtility resolves the remaining v4 utilities that are not covered by the
// larger families: composed contain-*, tab-size, scrollbar colors and the
// negative underline offset.
func moreUtility(cls string, neg bool, theme TailwindTheme) (string, bool) {
	if neg {
		if rest, ok := strings.CutPrefix(cls, "underline-offset-"); ok {
			v, isArb := unwrapArbitrary(rest)
			if !isArb {
				if !isDigits(rest) {
					return "", false
				}
				v = rest + "px"
			}
			return "text-underline-offset: calc(" + v + " * -1);", true
		}
		return "", false
	}
	if css, ok := moreStatic[cls]; ok {
		return css, true
	}
	switch cls {
	case "contain-size", "contain-layout", "contain-paint", "contain-style", "contain-inline-size":
		kind := strings.TrimPrefix(cls, "contain-")
		name := kind
		if kind == "inline-size" {
			name = "size"
		}
		return "--tw-contain-" + name + ": " + kind + "; " + containCompose, true
	}
	if rest, ok := strings.CutPrefix(cls, "contain-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "contain: " + v + ";", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "tab-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "tab-size: " + v + ";", true
		}
		if isDigits(rest) {
			return "tab-size: " + rest + ";", true
		}
		return "", false
	}
	for _, part := range []string{"thumb", "track"} {
		rest, ok := strings.CutPrefix(cls, "scrollbar-"+part+"-")
		if !ok {
			continue
		}
		c, ok := colorOf(rest, theme)
		if !ok {
			return "", false
		}
		return "--tw-scrollbar-" + part + ": " + c + "; scrollbar-color: var(--tw-scrollbar-thumb) var(--tw-scrollbar-track);", true
	}
	return "", false
}

// splitDecls splits a declaration block on top-level semicolons. Unlike
// topLevelSplit it keeps nested {...} blocks (an at-rule such as the
// forced-colors media block of outline-hidden) in one piece.
func splitDecls(css string) []string {
	var parts []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(css); i++ {
		c := css[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			if depth > 0 {
				depth--
			}
		case c == ';' && depth == 0:
			parts = append(parts, css[start:i])
			start = i + 1
		}
	}
	return append(parts, css[start:])
}

// nestedAtRule splits `@media (...){inner}` into its prelude and inner block.
func nestedAtRule(decl string) (head, inner string, ok bool) {
	decl = strings.TrimSpace(decl)
	if !strings.HasPrefix(decl, "@") || !strings.HasSuffix(decl, "}") {
		return "", "", false
	}
	i := strings.IndexByte(decl, '{')
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(decl[:i]), decl[i+1 : len(decl)-1], true
}
