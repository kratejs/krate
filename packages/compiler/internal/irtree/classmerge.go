package irtree

import (
	"regexp"
	"sort"
	"strings"
)

// This file is a port of twMerge in packages/runtime/src/class-helpers.ts.
// Both sides are checked against packages/runtime/src/testdata/class-merge-vectors.json,
// so change them together.

// mergeTailwindClasses merges conflicting Tailwind utilities: a later class
// wins over an earlier one of the same group and variant chain and takes the
// later position. Unrecognised classes only dedupe on an exact match.
func mergeTailwindClasses(tokens []string) string {
	type entry struct{ key, val string }
	var entries []entry
	remove := func(key string) {
		for i, e := range entries {
			if e.key == key {
				entries = append(entries[:i], entries[i+1:]...)
				return
			}
		}
	}
	for _, t := range tokens {
		for _, token := range strings.Fields(t) {
			scope, group, ok := classifyClass(token)
			if !ok {
				remove("\x00" + token)
				entries = append(entries, entry{"\x00" + token, token})
				continue
			}
			key := scope + group
			remove(key)
			for _, loser := range classConflicts[group] {
				remove(scope + loser)
			}
			entries = append(entries, entry{key, token})
		}
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.val
	}
	return strings.Join(out, " ")
}

// classConflicts lists groups that a later class of the key group also
// overrides when they were seen earlier.
var classConflicts = map[string][]string{
	"p":              {"px", "py", "pt", "pr", "pb", "pl", "ps", "pe"},
	"px":             {"pr", "pl", "ps", "pe"},
	"py":             {"pt", "pb"},
	"m":              {"mx", "my", "mt", "mr", "mb", "ml", "ms", "me"},
	"mx":             {"mr", "ml", "ms", "me"},
	"my":             {"mt", "mb"},
	"inset":          {"inset-x", "inset-y", "top", "right", "bottom", "left", "start", "end"},
	"inset-x":        {"right", "left", "start", "end"},
	"inset-y":        {"top", "bottom"},
	"gap":            {"gap-x", "gap-y"},
	"size":           {"w", "h"},
	"overflow":       {"overflow-x", "overflow-y"},
	"scale":          {"scale-x", "scale-y"},
	"rounded":        {"rounded-t", "rounded-r", "rounded-b", "rounded-l", "rounded-s", "rounded-e", "rounded-tl", "rounded-tr", "rounded-br", "rounded-bl", "rounded-ss", "rounded-se", "rounded-ee", "rounded-es"},
	"rounded-t":      {"rounded-tl", "rounded-tr"},
	"rounded-r":      {"rounded-tr", "rounded-br"},
	"rounded-b":      {"rounded-br", "rounded-bl"},
	"rounded-l":      {"rounded-tl", "rounded-bl"},
	"rounded-s":      {"rounded-ss", "rounded-es"},
	"rounded-e":      {"rounded-se", "rounded-ee"},
	"border-w":       {"border-w-x", "border-w-y", "border-w-t", "border-w-r", "border-w-b", "border-w-l", "border-w-s", "border-w-e"},
	"border-w-x":     {"border-w-r", "border-w-l", "border-w-s", "border-w-e"},
	"border-w-y":     {"border-w-t", "border-w-b"},
	"border-color":   {"border-color-x", "border-color-y", "border-color-t", "border-color-r", "border-color-b", "border-color-l", "border-color-s", "border-color-e"},
	"border-color-x": {"border-color-r", "border-color-l", "border-color-s", "border-color-e"},
	"border-color-y": {"border-color-t", "border-color-b"},
	"font-size":      {"leading"},
}

// splitClassTop splits s on sep outside [] and () so arbitrary values stay intact.
func splitClassTop(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '[' || c == '(':
			depth++
		case c == ']' || c == ')':
			if depth > 0 {
				depth--
			}
		case c == sep && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

var (
	lenHintRe     = regexp.MustCompile(`^(length|size|percentage|number|position|image|url|family|weight|line-width|bg-size)$`)
	kindHintRe    = regexp.MustCompile(`^([a-z-]+):(.*)$`)
	colorFuncRe   = regexp.MustCompile(`(?i)^(#|rgb|hsl|hwb|lab|lch|oklab|oklch|color\()`)
	mathFuncRe    = regexp.MustCompile(`^(calc|min|max|clamp)\(`)
	dimensionRe   = regexp.MustCompile(`(?i)^[+-]?(\d+\.?\d*|\.\d+)(%|[a-z]+)$`)
	plainNumberRe = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)$`)
	fractionRe    = regexp.MustCompile(`^\d+/\d+$`)
	textSizeRe    = regexp.MustCompile(`^(xs|sm|base|lg|xl|\d+xl)$`)
	arbPropRe     = regexp.MustCompile(`^\[([a-z-]+):`)
	sideRe        = regexp.MustCompile(`^(x|y|t|r|b|l|s|e)(?:-(.*))?$`)
	bgRepeatRe    = regexp.MustCompile(`^(repeat|no-repeat)`)
	bgImageRe     = regexp.MustCompile(`^(gradient-to|linear|radial|conic)`)
	bgPositionArb = regexp.MustCompile(`^\[(position|percentage):`)
	bgImageArb    = regexp.MustCompile(`^\[(image|url):`)
	shadowSizeRe  = regexp.MustCompile(`^(2xs|xs|sm|md|lg|xl|2xl|none|inner)$`)
)

// valueKind is the kind of an arbitrary or variable value: "length", "color",
// "number", "other", "unknown", or "" when the value is not arbitrary.
func arbitraryKind(value string) string {
	var inner string
	switch {
	case strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]"):
		inner = value[1 : len(value)-1]
	case strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")"):
		inner = value[1 : len(value)-1]
	default:
		return ""
	}
	if m := kindHintRe.FindStringSubmatch(inner); m != nil && !strings.HasPrefix(m[2], ":") {
		hint := m[1]
		switch {
		case hint == "color":
			return "color"
		case hint == "number" || hint == "weight":
			return "number"
		case lenHintRe.MatchString(hint):
			if hint == "family" || hint == "image" || hint == "url" {
				return "other"
			}
			return "length"
		}
	}
	if strings.HasPrefix(value, "(") {
		return "unknown"
	}
	v := strings.ReplaceAll(inner, "_", " ")
	switch {
	case colorFuncRe.MatchString(v):
		return "color"
	case mathFuncRe.MatchString(v) || dimensionRe.MatchString(v):
		return "length"
	case plainNumberRe.MatchString(v):
		return "number"
	}
	return "unknown"
}

func isNumericValue(v string) bool {
	return plainNumberRe.MatchString(v) || fractionRe.MatchString(v)
}

func stringSet(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

// simpleGroups are utility heads whose every value collapses into one group.
var simpleGroups = stringSet(
	"p", "px", "py", "pt", "pr", "pb", "pl", "ps", "pe",
	"m", "mx", "my", "mt", "mr", "mb", "ml", "ms", "me",
	"gap", "gap-x", "gap-y", "space-x", "space-y",
	"inset", "inset-x", "inset-y", "top", "right", "bottom", "left", "start", "end",
	"z", "order", "opacity", "duration", "delay", "ease", "animate",
	"basis", "grow", "shrink", "w", "h", "size", "min-w", "max-w", "min-h", "max-h",
	"grid-cols", "grid-rows", "col-span", "col-start", "col-end", "row-span", "row-start", "row-end",
	"leading", "tracking", "line-clamp", "columns", "aspect",
	"blur", "brightness", "contrast", "grayscale", "hue-rotate", "invert", "saturate", "sepia",
	"backdrop-blur", "backdrop-brightness", "backdrop-contrast", "backdrop-grayscale",
	"backdrop-hue-rotate", "backdrop-invert", "backdrop-opacity", "backdrop-saturate", "backdrop-sepia",
	"scale", "scale-x", "scale-y", "rotate", "translate-x", "translate-y", "skew-x", "skew-y",
	"origin", "cursor", "select", "underline-offset", "indent", "perspective", "will-change",
	"overflow", "overflow-x", "overflow-y", "overscroll", "pointer-events", "resize", "whitespace",
	"rounded", "rounded-t", "rounded-r", "rounded-b", "rounded-l", "rounded-s", "rounded-e",
	"rounded-tl", "rounded-tr", "rounded-br", "rounded-bl", "rounded-ss", "rounded-se", "rounded-ee", "rounded-es",
	"transition",
)

var (
	fontWeights  = stringSet("thin", "extralight", "light", "normal", "medium", "semibold", "bold", "extrabold", "black")
	borderStyles = stringSet("solid", "dashed", "dotted", "double", "hidden", "none")
	bgPositions  = stringSet("bottom", "center", "left", "left-bottom", "left-top", "right", "right-bottom", "right-top", "top")
	textAligns   = stringSet("left", "center", "right", "justify", "start", "end")
	textWraps    = stringSet("wrap", "nowrap", "balance", "pretty")
	textOverflow = stringSet("ellipsis", "clip")
	bgSizes      = stringSet("auto", "cover", "contain")
	bgAttach     = stringSet("fixed", "local", "scroll")
	decoStyles   = stringSet("solid", "double", "dotted", "dashed", "wavy")
	objectFits   = stringSet("contain", "cover", "fill", "none", "scale-down")
)

// standaloneGroups maps value-less utilities to a group.
var standaloneGroups = func() map[string]string {
	m := map[string]string{"truncate": "truncate"}
	add := func(group string, names ...string) {
		for _, n := range names {
			m[n] = group
		}
	}
	add("display", "block", "inline-block", "inline", "flex", "inline-flex", "table", "inline-table", "table-caption", "table-cell", "table-column", "table-column-group", "table-footer-group", "table-header-group", "table-row-group", "table-row", "flow-root", "grid", "inline-grid", "contents", "list-item", "hidden")
	add("position", "static", "fixed", "absolute", "relative", "sticky")
	add("visibility", "visible", "invisible", "collapse")
	add("font-style", "italic", "not-italic")
	add("text-transform", "uppercase", "lowercase", "capitalize", "normal-case")
	add("text-decoration", "underline", "overline", "line-through", "no-underline")
	add("flex-direction", "flex-row", "flex-row-reverse", "flex-col", "flex-col-reverse")
	add("flex-wrap", "flex-wrap", "flex-nowrap", "flex-wrap-reverse")
	return m
}()

// classifyClass returns the variant/important scope and utility group of a
// class. ok is false when the class has no known group.
func classifyClass(token string) (scope, group string, ok bool) {
	parts := splitClassTop(token, ':')
	base := parts[len(parts)-1]
	variants := append([]string(nil), parts[:len(parts)-1]...)
	sort.Strings(variants)
	important := ""
	if strings.HasPrefix(base, "!") {
		important = "!"
		base = base[1:]
	}
	if strings.HasSuffix(base, "!") {
		important = "!"
		base = base[:len(base)-1]
	}
	scope = strings.Join(variants, ":") + ":" + important
	base = strings.TrimPrefix(base, "-")
	if strings.HasPrefix(base, "[") {
		if m := arbPropRe.FindStringSubmatch(base); m != nil {
			return scope, "arb:" + m[1], true
		}
		return scope, "", false
	}
	if slash := splitClassTop(base, '/'); len(slash) > 1 {
		base = strings.Join(slash[:len(slash)-1], "/")
	}
	group, ok = utilityGroup(base)
	return scope, group, ok
}

// splitHead finds the longest dash-separated head of base (up to 3 segments) in set.
func splitHead(base string, set map[string]bool) (string, bool) {
	segs := splitClassTop(base, '-')
	for n := min(len(segs), 3); n >= 1; n-- {
		head := strings.Join(segs[:n], "-")
		if set[head] {
			return head, true
		}
	}
	return "", false
}

func utilityGroup(base string) (string, bool) {
	if g, ok := standaloneGroups[base]; ok {
		return g, true
	}
	segs := splitClassTop(base, '-')
	head := segs[0]
	rest := strings.Join(segs[1:], "-")

	switch head {
	case "text":
		switch {
		case rest == "":
			return "", false
		case textSizeRe.MatchString(rest):
			return "font-size", true
		case textAligns[rest]:
			return "text-align", true
		case textWraps[rest]:
			return "text-wrap", true
		case textOverflow[rest]:
			return "text-overflow", true
		case arbitraryKind(rest) == "length":
			return "font-size", true
		}
		return "text-color", true
	case "font":
		k := arbitraryKind(rest)
		switch {
		case fontWeights[rest] || k == "number":
			return "font-weight", true
		case strings.HasPrefix(rest, "stretch"):
			return "font-stretch", true
		}
		return "font-family", true
	case "bg":
		switch {
		case rest == "":
			return "", false
		case bgSizes[rest]:
			return "bg-size", true
		case bgPositions[rest]:
			return "bg-position", true
		case bgRepeatRe.MatchString(rest):
			return "bg-repeat", true
		case bgAttach[rest]:
			return "bg-attachment", true
		case strings.HasPrefix(rest, "clip-"):
			return "bg-clip", true
		case strings.HasPrefix(rest, "origin-"):
			return "bg-origin", true
		case strings.HasPrefix(rest, "blend-"):
			return "bg-blend", true
		case rest == "none" || bgImageRe.MatchString(rest):
			return "bg-image", true
		case arbitraryKind(rest) == "length":
			return "bg-size", true
		case strings.HasPrefix(rest, "[") && bgPositionArb.MatchString(rest):
			return "bg-position", true
		case strings.HasPrefix(rest, "[") && (bgImageArb.MatchString(rest) || strings.HasPrefix(rest, "[url(")):
			return "bg-image", true
		}
		return "bg-color", true
	case "border":
		return widthColorStyle(rest, "border")
	case "divide":
		switch {
		case rest == "x" || rest == "y" || strings.HasPrefix(rest, "x-") || strings.HasPrefix(rest, "y-"):
			return "divide-" + rest[:1], true
		case rest == "reverse" || strings.HasSuffix(rest, "-reverse"):
			return "divide-reverse", true
		case borderStyles[rest]:
			return "divide-style", true
		}
		return "divide-color", true
	case "outline":
		if strings.HasPrefix(rest, "offset") {
			return "outline-offset", true
		}
		return widthColorStyle(rest, "outline")
	case "ring":
		if strings.HasPrefix(rest, "offset-") {
			r := strings.TrimPrefix(rest, "offset-")
			if k := arbitraryKind(r); isNumericValue(r) || k == "length" || k == "number" {
				return "ring-offset-w", true
			}
			return "ring-offset-color", true
		}
		if rest == "inset" {
			return "ring-inset", true
		}
		if rest == "" {
			return "ring-w", true
		}
		if k := arbitraryKind(rest); isNumericValue(rest) || k == "length" || k == "number" {
			return "ring-w", true
		}
		return "ring-color", true
	case "shadow":
		if rest == "" {
			return "shadow", true
		}
		k := arbitraryKind(rest)
		switch {
		case k == "color":
			return "shadow-color", true
		case k == "length" || k == "number" || k == "other" || (k == "unknown" && strings.HasPrefix(rest, "[")):
			return "shadow", true
		case shadowSizeRe.MatchString(rest):
			return "shadow", true
		}
		return "shadow-color", true
	case "stroke":
		if k := arbitraryKind(rest); isNumericValue(rest) || k == "length" || k == "number" {
			return "stroke-w", true
		}
		return "stroke-color", true
	case "fill":
		return "fill", true
	case "decoration":
		if decoStyles[rest] {
			return "decoration-style", true
		}
		if k := arbitraryKind(rest); isNumericValue(rest) || k == "length" || k == "number" || rest == "from-font" || rest == "auto" {
			return "decoration-thickness", true
		}
		return "decoration-color", true
	case "object":
		if objectFits[rest] {
			return "object-fit", true
		}
		return "object-position", true
	case "list":
		switch {
		case rest == "inside" || rest == "outside":
			return "list-position", true
		case strings.HasPrefix(rest, "image"):
			return "list-image", true
		}
		return "list-style-type", true
	case "flex":
		if rest == "" {
			return "", false
		}
		return "flex", true
	case "grid":
		if strings.HasPrefix(rest, "flow") {
			return "grid-flow", true
		}
	case "shrink", "grow":
		return head, true
	case "transition":
		return "transition", true
	}

	return splitHead(base, simpleGroups)
}

// widthColorStyle returns the group for border/outline: width, style or
// colour, with an optional side.
func widthColorStyle(rest, name string) (string, bool) {
	side, value := "", rest
	if m := sideRe.FindStringSubmatch(rest); name == "border" && m != nil {
		side = "-" + m[1]
		value = m[2]
	}
	if name == "border" && (rest == "collapse" || rest == "separate") {
		return "border-collapse", true
	}
	if name == "border" && strings.HasPrefix(rest, "spacing") {
		return "border-spacing", true
	}
	if value == "" {
		if name == "border" {
			return "border-w" + side, true
		}
		return name + "-w", true
	}
	if borderStyles[value] {
		if name == "outline" && value != "hidden" {
			return "outline-style", true
		}
		return name + "-style", true
	}
	if k := arbitraryKind(value); isNumericValue(value) || k == "length" || k == "number" {
		if name == "border" {
			return "border-w" + side, true
		}
		return name + "-w", true
	}
	if name == "border" {
		return "border-color" + side, true
	}
	return name + "-color", true
}
