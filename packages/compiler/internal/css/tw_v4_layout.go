package css

import (
	"sort"
	"strconv"
	"strings"
)

// Layout utilities with Tailwind v4 value semantics: spacing, sizing, inset,
// grid lines, aspect ratio, flex shorthand and the static keyword utilities.

// twContainerSizes are the default container widths (v4 --container-*).
var twContainerSizes = map[string]string{
	"3xs": "16rem", "2xs": "18rem", "xs": "20rem", "sm": "24rem", "md": "28rem",
	"lg": "32rem", "xl": "36rem", "2xl": "42rem", "3xl": "48rem", "4xl": "56rem",
	"5xl": "64rem", "6xl": "72rem", "7xl": "80rem",
}

// containerSize resolves a container size name against the theme.
func containerSize(name string, theme TailwindTheme) (string, bool) {
	if v, ok := theme.Containers[name]; ok && v != "" {
		return v, true
	}
	v, ok := twContainerSizes[name]
	return v, ok
}

type spacingRoot struct {
	root  string
	props []string
	auto  bool // accepts auto
	frac  bool // accepts full and fractions
	neg   bool // accepts a leading "-"
}

// spacingRoots are matched in order (longest prefix of a family first).
var spacingRoots = []spacingRoot{
	{"inset-x", []string{"inset-inline"}, true, true, true},
	{"inset-y", []string{"inset-block"}, true, true, true},
	{"inset-s", []string{"inset-inline-start"}, true, true, true},
	{"inset-e", []string{"inset-inline-end"}, true, true, true},
	{"inset", []string{"inset"}, true, true, true},
	{"top", []string{"top"}, true, true, true},
	{"right", []string{"right"}, true, true, true},
	{"bottom", []string{"bottom"}, true, true, true},
	{"left", []string{"left"}, true, true, true},
	{"start", []string{"inset-inline-start"}, true, true, true},
	{"end", []string{"inset-inline-end"}, true, true, true},
	{"mx", []string{"margin-inline"}, true, false, true},
	{"my", []string{"margin-block"}, true, false, true},
	{"mt", []string{"margin-top"}, true, false, true},
	{"mr", []string{"margin-right"}, true, false, true},
	{"mb", []string{"margin-bottom"}, true, false, true},
	{"ml", []string{"margin-left"}, true, false, true},
	{"ms", []string{"margin-inline-start"}, true, false, true},
	{"me", []string{"margin-inline-end"}, true, false, true},
	{"m", []string{"margin"}, true, false, true},
	{"px", []string{"padding-inline"}, false, false, false},
	{"py", []string{"padding-block"}, false, false, false},
	{"pt", []string{"padding-top"}, false, false, false},
	{"pr", []string{"padding-right"}, false, false, false},
	{"pb", []string{"padding-bottom"}, false, false, false},
	{"pl", []string{"padding-left"}, false, false, false},
	{"ps", []string{"padding-inline-start"}, false, false, false},
	{"pe", []string{"padding-inline-end"}, false, false, false},
	{"p", []string{"padding"}, false, false, false},
	{"gap-x", []string{"column-gap"}, false, false, false},
	{"gap-y", []string{"row-gap"}, false, false, false},
	{"gap", []string{"gap"}, false, false, false},
	{"indent", []string{"text-indent"}, false, false, true},
}

// spacingUtility resolves margin, padding, inset, gap and indent utilities.
func spacingUtility(cls string, neg bool, theme TailwindTheme) (string, bool) {
	for _, r := range spacingRoots {
		key, ok := strings.CutPrefix(cls, r.root+"-")
		if !ok || key == "" {
			continue
		}
		if neg && !r.neg {
			return "", false
		}
		var v string
		switch {
		case r.auto && key == "auto":
			v = "auto"
		case r.frac && key == "full":
			v = "100%"
		default:
			if inner, isArb := unwrapArbitrary(key); isArb {
				v = inner
			} else if r.frac {
				if pct, isFrac := fractionPercent(key); isFrac {
					v = pct
				}
			}
			if v == "" {
				v = spacingValue(key, theme)
			}
		}
		if v == "" {
			continue
		}
		if neg {
			v = negateLength(v)
		}
		var b strings.Builder
		for i, p := range r.props {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(p + ": " + v + ";")
		}
		return b.String(), true
	}
	return "", false
}

// sizeValue resolves a sizing key for width/height style properties.
// kind is one of w, h, min-w, min-h, max-w, max-h, size, basis.
func sizeValue(kind, key string, theme TailwindTheme) (string, bool) {
	if v, isArb := unwrapArbitrary(key); isArb {
		return v, true
	}
	isH := kind == "h" || kind == "min-h" || kind == "max-h"
	switch kind {
	case "max-w":
		if v, ok := theme.MaxWidth[key]; ok && !strings.HasPrefix(key, "screen-") {
			return v, true
		}
	case "min-w":
		if v, ok := theme.MinWidth[key]; ok {
			return v, true
		}
	}
	switch key {
	case "auto":
		if kind == "max-w" || kind == "max-h" {
			return "", false
		}
		return "auto", true
	case "none":
		if kind == "max-w" || kind == "max-h" {
			return "none", true
		}
		return "", false
	case "full":
		return "100%", true
	case "px":
		return "1px", true
	case "min":
		return "min-content", true
	case "max":
		return "max-content", true
	case "fit":
		return "fit-content", true
	case "screen":
		if isH {
			return "100vh", true
		}
		if kind != "basis" {
			return "100vw", true
		}
	case "svh", "lvh", "dvh":
		if isH {
			return "100" + key, true
		}
	case "svw", "lvw", "dvw":
		if !isH && kind != "basis" {
			return "100" + key, true
		}
	case "lh":
		if isH {
			return "1lh", true
		}
	case "prose":
		if kind == "max-w" {
			return "65ch", true
		}
	}
	if kind == "max-w" {
		if bp, ok := strings.CutPrefix(key, "screen-"); ok {
			if w, ok := theme.Screens[bp]; ok {
				return w, true
			}
		}
	}
	if kind == "w" || kind == "min-w" || kind == "max-w" || kind == "basis" || kind == "size" {
		if v, ok := containerSize(key, theme); ok {
			return v, true
		}
	}
	if pct, ok := fractionPercent(key); ok {
		return pct, true
	}
	if v, ok := theme.Sizing[key]; ok && !(plainNumber(key) && hasCustomUnit(theme)) {
		return v, true
	}
	if v, ok := namedSpacing(key, theme); ok {
		return v, true
	}
	if v, ok := spacingMultiple(key, theme); ok {
		return v, true
	}
	return "", false
}

var sizeProps = []struct {
	root  string
	props []string
}{
	{"min-w", []string{"min-width"}},
	{"min-h", []string{"min-height"}},
	{"max-w", []string{"max-width"}},
	{"max-h", []string{"max-height"}},
	{"size", []string{"width", "height"}},
	{"basis", []string{"flex-basis"}},
	{"w", []string{"width"}},
	{"h", []string{"height"}},
}

// sizeUtility resolves w-*, h-*, min-*, max-*, size-* and basis-*.
func sizeUtility(cls string, theme TailwindTheme) (string, bool) {
	for _, p := range sizeProps {
		key, ok := strings.CutPrefix(cls, p.root+"-")
		if !ok || key == "" {
			continue
		}
		v, ok := sizeValue(p.root, key, theme)
		if !ok {
			return "", false
		}
		var b strings.Builder
		for i, prop := range p.props {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(prop + ": " + v + ";")
		}
		return b.String(), true
	}
	return "", false
}

// containerMarker is returned by generateCSS for the `container` utility,
// which expands to one rule per breakpoint (see containerRules).
const containerMarker = "@@container"

// containerRules builds the container utility: full width plus a max-width
// per theme breakpoint, in ascending order.
func containerRules(sel string, ats []variant, theme TailwindTheme) string {
	type bp struct {
		px  float64
		raw string
	}
	var bps []bp
	for _, w := range theme.Screens {
		bps = append(bps, bp{cssLengthPx(w), w})
	}
	sort.Slice(bps, func(i, j int) bool {
		if bps[i].px != bps[j].px {
			return bps[i].px < bps[j].px
		}
		return bps[i].raw < bps[j].raw
	})
	parts := []string{wrapAtRules(sel, "width: 100%;", ats)}
	for _, b := range bps {
		inner := append(append([]variant{}, ats...), variant{kind: "media", at: "(min-width: " + b.raw + ")"})
		parts = append(parts, wrapAtRules(sel, "max-width: "+b.raw+";", inner))
	}
	return strings.Join(parts, "\n")
}

// cssLengthPx converts a px/rem/em length to pixels for ordering only.
func cssLengthPx(v string) float64 {
	v = strings.TrimSpace(v)
	mult := 1.0
	switch {
	case strings.HasSuffix(v, "rem"):
		v, mult = strings.TrimSuffix(v, "rem"), 16
	case strings.HasSuffix(v, "em"):
		v, mult = strings.TrimSuffix(v, "em"), 16
	case strings.HasSuffix(v, "px"):
		v = strings.TrimSuffix(v, "px")
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	return f * mult
}

// gridLineUtility resolves col-*/row-* span, start, end and shorthand values.
func gridLineUtility(cls string, neg bool) (string, bool) {
	axis, rest := "", ""
	switch {
	case strings.HasPrefix(cls, "col-"):
		axis, rest = "column", cls[4:]
	case strings.HasPrefix(cls, "row-"):
		axis, rest = "row", cls[4:]
	default:
		return "", false
	}
	prop := "grid-" + axis
	num := func(v string) (string, bool) {
		if inner, isArb := unwrapArbitrary(v); isArb {
			return inner, true
		}
		if isDigits(v) {
			return v, true
		}
		return "", false
	}
	switch {
	case rest == "auto" && !neg:
		return prop + ": auto;", true
	case rest == "span-full" && !neg:
		return prop + ": 1 / -1;", true
	case strings.HasPrefix(rest, "span-") && !neg:
		if v, ok := num(rest[5:]); ok {
			return prop + ": span " + v + " / span " + v + ";", true
		}
	case strings.HasPrefix(rest, "start-"), strings.HasPrefix(rest, "end-"):
		side, val, _ := strings.Cut(rest, "-")
		if val == "auto" && !neg {
			return prop + "-" + side + ": auto;", true
		}
		if v, ok := num(val); ok {
			if neg {
				v = negateLength(v)
			}
			return prop + "-" + side + ": " + v + ";", true
		}
	default:
		if v, isArb := unwrapArbitrary(rest); isArb && !neg {
			return prop + ": " + v + ";", true
		}
	}
	return "", false
}

// flexUtility resolves flex-*, grow-* and shrink-* values beyond the keywords.
func flexUtility(cls string) (string, bool) {
	if key, ok := strings.CutPrefix(cls, "flex-"); ok {
		if isDigits(key) {
			return "flex: " + key + ";", true
		}
		if v, isArb := unwrapArbitrary(key); isArb {
			return "flex: " + v + ";", true
		}
		if pct, ok := fractionPercent(key); ok {
			a, b, _ := strings.Cut(key, "/")
			_ = pct
			return "flex: calc(" + a + "/" + b + " * 100%);", true
		}
	}
	for _, p := range []struct{ root, prop string }{{"grow", "flex-grow"}, {"shrink", "flex-shrink"}} {
		if key, ok := strings.CutPrefix(cls, p.root+"-"); ok {
			if isDigits(key) {
				return p.prop + ": " + key + ";", true
			}
			if v, isArb := unwrapArbitrary(key); isArb {
				return p.prop + ": " + v + ";", true
			}
		}
	}
	return "", false
}

// aspectUtility resolves aspect-W/H ratios and arbitrary values.
func aspectUtility(cls string) (string, bool) {
	key, ok := strings.CutPrefix(cls, "aspect-")
	if !ok {
		return "", false
	}
	switch key {
	case "auto":
		return "aspect-ratio: auto;", true
	case "square":
		return "aspect-ratio: 1 / 1;", true
	case "video":
		return "aspect-ratio: 16 / 9;", true
	}
	if v, isArb := unwrapArbitrary(key); isArb {
		return "aspect-ratio: " + v + ";", true
	}
	if a, b, ok := strings.Cut(key, "/"); ok && plainNumber(a) && plainNumber(b) {
		return "aspect-ratio: " + a + "/" + b + ";", true
	}
	return "", false
}

// gridAutoUtility resolves auto-cols-* and auto-rows-*.
func gridAutoUtility(cls string) (string, bool) {
	for _, p := range []struct{ root, prop string }{
		{"auto-cols-", "grid-auto-columns"}, {"auto-rows-", "grid-auto-rows"},
	} {
		key, ok := strings.CutPrefix(cls, p.root)
		if !ok {
			continue
		}
		switch key {
		case "auto":
			return p.prop + ": auto;", true
		case "min":
			return p.prop + ": min-content;", true
		case "max":
			return p.prop + ": max-content;", true
		case "fr":
			return p.prop + ": minmax(0, 1fr);", true
		}
		if v, isArb := unwrapArbitrary(key); isArb {
			return p.prop + ": " + v + ";", true
		}
	}
	return "", false
}

// gridTemplateUtility resolves grid-cols-* / grid-rows-* beyond plain counts.
func gridTemplateUtility(cls string) (string, bool) {
	for _, p := range []struct{ root, prop string }{
		{"grid-cols-", "grid-template-columns"}, {"grid-rows-", "grid-template-rows"},
	} {
		key, ok := strings.CutPrefix(cls, p.root)
		if !ok {
			continue
		}
		switch {
		case key == "none" || key == "subgrid":
			return p.prop + ": " + key + ";", true
		case isDigits(key):
			return p.prop + ": repeat(" + key + ", minmax(0, 1fr));", true
		}
		if v, isArb := unwrapArbitrary(key); isArb {
			return p.prop + ": " + v + ";", true
		}
	}
	return "", false
}

// staticUtilities are keyword utilities that need no value parsing.
var staticUtilities = map[string]string{
	"collapse":             "visibility: collapse;",
	"underline":            "text-decoration-line: underline;",
	"line-through":         "text-decoration-line: line-through;",
	"appearance-auto":      "appearance: auto;",
	"align-sub":            "vertical-align: sub;",
	"align-super":          "vertical-align: super;",
	"antialiased":          "-webkit-font-smoothing: antialiased; -moz-osx-font-smoothing: grayscale;",
	"subpixel-antialiased": "-webkit-font-smoothing: auto; -moz-osx-font-smoothing: auto;",
	"text-balance":         "text-wrap: balance;",
	"text-pretty":          "text-wrap: pretty;",
	"text-wrap":            "text-wrap: wrap;",
	"text-nowrap":          "text-wrap: nowrap;",
	"text-start":           "text-align: start;",
	"text-end":             "text-align: end;",
	"overflow-clip":        "overflow: clip;",
	"overflow-x-clip":      "overflow-x: clip;",
	"overflow-y-clip":      "overflow-y: clip;",
	"table-caption":        "display: table-caption;",
	"table-cell":           "display: table-cell;",
	"table-column":         "display: table-column;",
	"table-column-group":   "display: table-column-group;",
	"table-footer-group":   "display: table-footer-group;",
	"table-header-group":   "display: table-header-group;",
	"table-row":            "display: table-row;",
	"table-row-group":      "display: table-row-group;",
	"wrap-anywhere":        "overflow-wrap: anywhere;",
	"wrap-break-word":      "overflow-wrap: break-word;",
	"wrap-normal":          "overflow-wrap: normal;",
	"content-normal":       "align-content: normal;",
	"content-none":         "--tw-content: none; content: none;",
	"justify-normal":       "justify-content: normal;",
	"justify-stretch":      "justify-content: stretch;",
	"justify-items-normal": "justify-items: normal;",
	"line-clamp-none":      "overflow: visible; display: block; -webkit-box-orient: horizontal; -webkit-line-clamp: unset;",
	"list-none":            "list-style-type: none;",
	"list-disc":            "list-style-type: disc;",
	"list-decimal":         "list-style-type: decimal;",
	"select-none":          "-webkit-user-select: none; user-select: none;",
	"select-text":          "-webkit-user-select: text; user-select: text;",
	"select-all":           "-webkit-user-select: all; user-select: all;",
	"select-auto":          "-webkit-user-select: auto; user-select: auto;",
	"sr-only":              "position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; border-width: 0;",
	"not-sr-only":          "position: static; width: auto; height: auto; padding: 0; margin: 0; overflow: visible; clip-path: none; white-space: normal;",
	"touch-pan-x":          "--tw-pan-x: pan-x; touch-action: var(--tw-pan-x,) var(--tw-pan-y,) var(--tw-pinch-zoom,);",
	"touch-pan-left":       "--tw-pan-x: pan-left; touch-action: var(--tw-pan-x,) var(--tw-pan-y,) var(--tw-pinch-zoom,);",
	"touch-pan-right":      "--tw-pan-x: pan-right; touch-action: var(--tw-pan-x,) var(--tw-pan-y,) var(--tw-pinch-zoom,);",
	"touch-pan-y":          "--tw-pan-y: pan-y; touch-action: var(--tw-pan-x,) var(--tw-pan-y,) var(--tw-pinch-zoom,);",
	"touch-pan-up":         "--tw-pan-y: pan-up; touch-action: var(--tw-pan-x,) var(--tw-pan-y,) var(--tw-pinch-zoom,);",
	"touch-pan-down":       "--tw-pan-y: pan-down; touch-action: var(--tw-pan-x,) var(--tw-pan-y,) var(--tw-pinch-zoom,);",
	"touch-pinch-zoom":     "--tw-pinch-zoom: pinch-zoom; touch-action: var(--tw-pan-x,) var(--tw-pan-y,) var(--tw-pinch-zoom,);",
}

// alignmentKeywords adds the v4 safe/last-baseline alignment keywords.
func alignmentUtility(cls string) (string, bool) {
	tbl := map[string]string{
		"items-center-safe": "align-items: safe center;", "items-end-safe": "align-items: safe flex-end;",
		"items-baseline-last": "align-items: last baseline;",
		"self-center-safe":    "align-self: safe center;", "self-end-safe": "align-self: safe flex-end;",
		"self-baseline-last":        "align-self: last baseline;",
		"justify-center-safe":       "justify-content: safe center;",
		"justify-end-safe":          "justify-content: safe flex-end;",
		"justify-items-center-safe": "justify-items: safe center;",
		"justify-items-end-safe":    "justify-items: safe end;",
		"justify-self-center-safe":  "justify-self: safe center;",
		"justify-self-end-safe":     "justify-self: safe end;",
		"content-baseline-last":     "align-content: last baseline;",
	}
	v, ok := tbl[cls]
	return v, ok
}

var cursorNames = map[string]bool{
	"auto": true, "default": true, "pointer": true, "wait": true, "text": true, "move": true,
	"help": true, "not-allowed": true, "none": true, "context-menu": true, "progress": true,
	"cell": true, "crosshair": true, "vertical-text": true, "alias": true, "copy": true,
	"no-drop": true, "grab": true, "grabbing": true, "all-scroll": true, "col-resize": true,
	"row-resize": true, "n-resize": true, "e-resize": true, "s-resize": true, "w-resize": true,
	"ne-resize": true, "nw-resize": true, "se-resize": true, "sw-resize": true, "ew-resize": true,
	"ns-resize": true, "nesw-resize": true, "nwse-resize": true, "zoom-in": true, "zoom-out": true,
}

// miscValueUtility resolves arbitrary/variable values for single-property
// utilities: cursor, align, line-clamp, list, object, order, z, opacity,
// stroke width and friends.
func miscValueUtility(cls string, neg bool) (string, bool) {
	if key, ok := strings.CutPrefix(cls, "cursor-"); ok && !neg {
		if cursorNames[key] {
			return "cursor: " + key + ";", true
		}
		if v, isArb := unwrapArbitrary(key); isArb {
			return "cursor: " + v + ";", true
		}
		return "", false
	}
	if key, ok := strings.CutPrefix(cls, "line-clamp-"); ok && !neg {
		if isDigits(key) {
			return "overflow: hidden; display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: " + key + ";", true
		}
		if v, isArb := unwrapArbitrary(key); isArb {
			return "overflow: hidden; display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: " + v + ";", true
		}
		return "", false
	}
	if key, ok := strings.CutPrefix(cls, "list-"); ok && !neg {
		switch key {
		case "inside":
			return "list-style-position: inside;", true
		case "outside":
			return "list-style-position: outside;", true
		}
		if strings.HasPrefix(key, "image-") {
			return "", false
		}
		if v, isArb := unwrapArbitrary(key); isArb {
			return "list-style-type: " + v + ";", true
		}
		return "", false
	}
	if key, ok := strings.CutPrefix(cls, "align-"); ok && !neg {
		if v, isArb := unwrapArbitrary(key); isArb {
			return "vertical-align: " + v + ";", true
		}
		return "", false
	}
	if key, ok := strings.CutPrefix(cls, "order-"); ok {
		var v string
		switch {
		case isDigits(key):
			v = key
		case key == "first" && !neg:
			return "order: -9999;", true
		case key == "last" && !neg:
			return "order: 9999;", true
		case key == "none" && !neg:
			return "order: 0;", true
		default:
			if inner, isArb := unwrapArbitrary(key); isArb {
				v = inner
			}
		}
		if v == "" {
			return "", false
		}
		if neg {
			v = negateLength(v)
		}
		return "order: " + v + ";", true
	}
	if key, ok := strings.CutPrefix(cls, "z-"); ok {
		var v string
		switch {
		case isDigits(key):
			v = key
		case key == "auto" && !neg:
			return "z-index: auto;", true
		default:
			if inner, isArb := unwrapArbitrary(key); isArb {
				v = inner
			}
		}
		if v == "" {
			return "", false
		}
		if neg {
			v = negateLength(v)
		}
		return "z-index: " + v + ";", true
	}
	if key, ok := strings.CutPrefix(cls, "opacity-"); ok && !neg {
		if isDigits(key) {
			n, _ := strconv.Atoi(key)
			if n <= 100 {
				return "opacity: " + formatNum(float64(n)/100) + ";", true
			}
			return "", false
		}
		if v, isArb := unwrapArbitrary(key); isArb {
			return "opacity: " + v + ";", true
		}
		return "", false
	}
	return "", false
}

func hasCustomUnit(theme TailwindTheme) bool {
	u := strings.TrimSpace(theme.SpacingUnit)
	return u != "" && u != "0.25rem"
}
