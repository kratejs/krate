package css

import (
	"regexp"
	"strings"
)

// Borders, outlines, rings, shadows, gradients, divide and space utilities
// with Tailwind v4 composition (style and ring variables, color layers).

const (
	shadowCompose = "box-shadow: var(--tw-inset-shadow), var(--tw-inset-ring-shadow), var(--tw-ring-offset-shadow), var(--tw-ring-shadow), var(--tw-shadow);"
	gradientStops = "var(--tw-gradient-via-stops, var(--tw-gradient-position), var(--tw-gradient-from) var(--tw-gradient-from-position), var(--tw-gradient-to) var(--tw-gradient-to-position))"
	viaStops      = "var(--tw-gradient-position), var(--tw-gradient-from) var(--tw-gradient-from-position), var(--tw-gradient-via) var(--tw-gradient-via-position), var(--tw-gradient-to) var(--tw-gradient-to-position)"
)

// colorKey resolves a color utility key such as "red-500/50" or
// "[#fff]/[0.4]" to a CSS color.
func colorOf(key string, theme TailwindTheme) (string, bool) {
	base, mod := splitColorModifier(key)
	if inner, hint, isArb := arbitraryValue(base); isArb {
		if hint != "" && hint != "color" {
			return "", false
		}
		if hint == "" && !isVarOnly(inner) && !looksLikeColor(inner) {
			return "", false
		}
		if mod != "" {
			a, ok := resolveAlpha(mod)
			if !ok {
				return "", false
			}
			return withAlpha(inner, a), true
		}
		return inner, true
	}
	return colorValue(base, mod, theme)
}

// lengthOf resolves an arbitrary length value ([3px], [length:var(--w)]).
func lengthOf(key string) (string, bool) {
	inner, hint, isArb := arbitraryValue(key)
	if !isArb {
		return "", false
	}
	if hint == "length" || hint == "line-width" || (hint == "" && looksLikeLength(inner) && !isVarOnly(inner)) {
		return inner, true
	}
	return "", false
}

var borderStyles = map[string]bool{"solid": true, "dashed": true, "dotted": true, "double": true, "hidden": true, "none": true}

var borderSides = map[string][]string{
	"t": {"top"}, "r": {"right"}, "b": {"bottom"}, "l": {"left"},
	"x": {"inline"}, "y": {"block"}, "s": {"inline-start"}, "e": {"inline-end"},
	"bs": {"block-start"}, "be": {"block-end"},
}

// borderUtility resolves border width, color and style utilities.
func borderUtility(cls string, theme TailwindTheme) (string, bool) {
	rest, ok := strings.CutPrefix(cls, "border")
	if !ok {
		return "", false
	}
	if rest == "" {
		return "border-style: var(--tw-border-style); border-width: 1px;", true
	}
	if rest[0] != '-' {
		return "", false
	}
	rest = rest[1:]
	if borderStyles[rest] {
		return "--tw-border-style: " + rest + "; border-style: " + rest + ";", true
	}
	side := ""
	sideProps := []string{""}
	head, tail, hasTail := strings.Cut(rest, "-")
	if props, ok := borderSides[head]; ok {
		side, sideProps, rest = head, props, tail
		if !hasTail {
			rest = ""
		}
	}
	prefix := func(p string) string {
		if p == "" {
			return "border"
		}
		return "border-" + p
	}
	width := func(w string) string {
		var b strings.Builder
		for _, p := range sideProps {
			b.WriteString(prefix(p) + "-style: var(--tw-border-style); " + prefix(p) + "-width: " + w + "; ")
		}
		return strings.TrimSpace(b.String())
	}
	if side == "" {
		// Avoid claiming border-collapse, border-spacing and friends.
		switch {
		case rest == "collapse" || rest == "separate" || strings.HasPrefix(rest, "spacing"):
			return "", false
		}
	}
	if rest == "" {
		return width("1px"), true
	}
	if isDigits(rest) {
		return width(rest + "px"), true
	}
	if w, ok := lengthOf(rest); ok {
		return width(w), true
	}
	if side != "" && borderStyles[rest] {
		return "", false
	}
	if c, ok := colorOf(rest, theme); ok {
		var b strings.Builder
		for _, p := range sideProps {
			b.WriteString(prefix(p) + "-color: " + c + "; ")
		}
		return strings.TrimSpace(b.String()), true
	}
	return "", false
}

// outlineUtility resolves outline width, style, color and offset.
func outlineUtility(cls string, neg bool, theme TailwindTheme) (string, bool) {
	if cls == "outline" && !neg {
		return "outline-style: var(--tw-outline-style); outline-width: 1px;", true
	}
	rest, ok := strings.CutPrefix(cls, "outline-")
	if !ok {
		return "", false
	}
	if off, ok := strings.CutPrefix(rest, "offset-"); ok {
		var v string
		if isDigits(off) {
			v = off + "px"
		} else if inner, isArb := unwrapArbitrary(off); isArb {
			v = inner
		} else {
			return "", false
		}
		if neg {
			v = negateLength(v)
		}
		return "outline-offset: " + v + ";", true
	}
	if neg {
		return "", false
	}
	switch rest {
	case "solid", "dashed", "dotted", "double":
		return "--tw-outline-style: " + rest + "; outline-style: " + rest + ";", true
	case "none":
		return "--tw-outline-style: none; outline-style: none;", true
	case "hidden":
		return "--tw-outline-style: none; outline-style: none; @media (forced-colors: active){outline: 2px solid transparent; outline-offset: 2px;}", true
	}
	if isDigits(rest) {
		return "outline-style: var(--tw-outline-style); outline-width: " + rest + "px;", true
	}
	if w, ok := lengthOf(rest); ok {
		return "outline-style: var(--tw-outline-style); outline-width: " + w + ";", true
	}
	if c, ok := colorOf(rest, theme); ok {
		return "outline-color: " + c + ";", true
	}
	return "", false
}

// ringUtility resolves ring-*, inset-ring-* and ring-offset-*.
func ringUtility(cls string, theme TailwindTheme) (string, bool) {
	ringShadow := func(prop, inset, w, color string) string {
		return prop + ": " + inset + "0 0 0 " + w + " var(" + color + ", currentcolor); " + shadowCompose
	}
	if cls == "ring" || strings.HasPrefix(cls, "ring-") && !strings.HasPrefix(cls, "ring-offset-") {
		rest := strings.TrimPrefix(strings.TrimPrefix(cls, "ring"), "-")
		w := ""
		switch {
		case rest == "":
			w = "1px"
		case rest == "inset":
			return "--tw-ring-inset: inset;", true
		case isDigits(rest):
			w = rest + "px"
		default:
			if lw, ok := lengthOf(rest); ok {
				w = lw
			}
		}
		if w != "" {
			return "--tw-ring-shadow: var(--tw-ring-inset,) 0 0 0 calc(" + w + " + var(--tw-ring-offset-width)) var(--tw-ring-color, currentcolor); " + shadowCompose, true
		}
		if c, ok := colorOf(rest, theme); ok {
			return "--tw-ring-color: " + c + ";", true
		}
		return "", false
	}
	if rest, ok := strings.CutPrefix(cls, "ring-offset-"); ok {
		w := ""
		if isDigits(rest) {
			w = rest + "px"
		} else if lw, ok := lengthOf(rest); ok {
			w = lw
		}
		if w != "" {
			return "--tw-ring-offset-width: " + w + "; --tw-ring-offset-shadow: var(--tw-ring-inset,) 0 0 0 var(--tw-ring-offset-width) var(--tw-ring-offset-color);", true
		}
		if c, ok := colorOf(rest, theme); ok {
			return "--tw-ring-offset-color: " + c + ";", true
		}
		return "", false
	}
	if cls == "inset-ring" || strings.HasPrefix(cls, "inset-ring-") {
		rest := strings.TrimPrefix(strings.TrimPrefix(cls, "inset-ring"), "-")
		w := ""
		switch {
		case rest == "":
			w = "1px"
		case isDigits(rest):
			w = rest + "px"
		default:
			if lw, ok := lengthOf(rest); ok {
				w = lw
			}
		}
		if w != "" {
			return ringShadow("--tw-inset-ring-shadow", "inset ", w, "--tw-inset-ring-color"), true
		}
		if c, ok := colorOf(rest, theme); ok {
			return "--tw-inset-ring-color: " + c + ";", true
		}
	}
	return "", false
}

// shadowTokenRe matches color components of a shadow so they can be wrapped in
// a var() that a shadow color utility overrides.
var shadowTokenRe = regexp.MustCompile(`rgba?\([^)]*\)|hsla?\([^)]*\)|oklch\([^)]*\)|oklab\([^)]*\)|#[0-9a-fA-F]{3,8}\b`)

// colorizeShadow wraps each color in a shadow value with var(<colorVar>, color).
func colorizeShadow(val, colorVar string) string {
	val = colorizeNamed(val, colorVar)
	return shadowTokenRe.ReplaceAllStringFunc(val, func(c string) string {
		return "var(" + colorVar + ", " + c + ")"
	})
}

var boxShadowScale = map[string]string{
	"2xs":   "0 1px rgb(0 0 0 / 0.05)",
	"xs":    "0 1px 2px 0 rgb(0 0 0 / 0.05)",
	"sm":    "0 1px 3px 0 rgb(0 0 0 / 0.1), 0 1px 2px -1px rgb(0 0 0 / 0.1)",
	"":      "0 1px 3px 0 rgb(0 0 0 / 0.1), 0 1px 2px -1px rgb(0 0 0 / 0.1)",
	"md":    "0 4px 6px -1px rgb(0 0 0 / 0.1), 0 2px 4px -2px rgb(0 0 0 / 0.1)",
	"lg":    "0 10px 15px -3px rgb(0 0 0 / 0.1), 0 4px 6px -4px rgb(0 0 0 / 0.1)",
	"xl":    "0 20px 25px -5px rgb(0 0 0 / 0.1), 0 8px 10px -6px rgb(0 0 0 / 0.1)",
	"2xl":   "0 25px 50px -12px rgb(0 0 0 / 0.25)",
	"inner": "inset 0 2px 4px 0 rgb(0 0 0 / 0.05)",
}

var insetShadowScale = map[string]string{
	"2xs": "inset 0 1px rgb(0 0 0 / 0.05)",
	"xs":  "inset 0 1px 1px rgb(0 0 0 / 0.05)",
	"sm":  "inset 0 2px 4px rgb(0 0 0 / 0.05)",
	"":    "inset 0 2px 4px rgb(0 0 0 / 0.05)",
}

var textShadowScale = map[string]string{
	"2xs": "0px 1px 0px rgb(0 0 0 / 0.15)",
	"xs":  "0px 1px 1px rgb(0 0 0 / 0.2)",
	"sm":  "0px 1px 0px rgb(0 0 0 / 0.075), 0px 1px 1px rgb(0 0 0 / 0.075), 0px 2px 2px rgb(0 0 0 / 0.075)",
	"md":  "0px 1px 1px rgb(0 0 0 / 0.1), 0px 1px 2px rgb(0 0 0 / 0.1), 0px 2px 4px rgb(0 0 0 / 0.1)",
	"lg":  "0px 1px 2px rgb(0 0 0 / 0.1), 0px 3px 2px rgb(0 0 0 / 0.1), 0px 4px 8px rgb(0 0 0 / 0.1)",
}

var dropShadowScale = map[string]string{
	"xs":  "drop-shadow(0 1px 1px rgb(0 0 0 / 0.05))",
	"sm":  "drop-shadow(0 1px 2px rgb(0 0 0 / 0.15))",
	"":    "drop-shadow(0 1px 2px rgb(0 0 0 / 0.1)) drop-shadow(0 1px 1px rgb(0 0 0 / 0.06))",
	"md":  "drop-shadow(0 3px 3px rgb(0 0 0 / 0.12))",
	"lg":  "drop-shadow(0 4px 4px rgb(0 0 0 / 0.15))",
	"xl":  "drop-shadow(0 9px 7px rgb(0 0 0 / 0.1))",
	"2xl": "drop-shadow(0 25px 25px rgb(0 0 0 / 0.15))",
}

// shadowFamily describes one of the shadow utility families.
type shadowFamily struct {
	root     string // utility root, e.g. "shadow"
	shadowV  string // custom property holding the shadow
	colorV   string // custom property holding the color override
	scale    map[string]string
	themeMap func(TailwindTheme) map[string]string
	emit     func(shadow string) string // declarations that apply the shadow
	none     string
}

var shadowFamilies = []shadowFamily{
	{"inset-shadow", "--tw-inset-shadow", "--tw-inset-shadow-color", insetShadowScale, nil,
		func(v string) string { return "--tw-inset-shadow: " + v + "; " + shadowCompose }, "inset 0 0 #0000"},
	{"text-shadow", "", "--tw-text-shadow-color", textShadowScale, nil,
		func(v string) string { return "text-shadow: " + v + ";" }, "none"},
	{"drop-shadow", "--tw-drop-shadow", "--tw-drop-shadow-color", dropShadowScale, nil,
		func(v string) string { return "--tw-drop-shadow: " + v + "; filter: " + filterCompose + ";" }, ""},
	{"shadow", "--tw-shadow", "--tw-shadow-color", boxShadowScale, func(t TailwindTheme) map[string]string { return t.Shadows },
		func(v string) string { return "--tw-shadow: " + v + "; " + shadowCompose }, "0 0 #0000"},
}

const filterCompose = "var(--tw-blur,) var(--tw-brightness,) var(--tw-contrast,) var(--tw-grayscale,) var(--tw-hue-rotate,) var(--tw-invert,) var(--tw-opacity,) var(--tw-saturate,) var(--tw-sepia,) var(--tw-drop-shadow,)"

// shadowUtility resolves shadow, inset-shadow, text-shadow and drop-shadow.
func shadowUtility(cls string, theme TailwindTheme) (string, bool) {
	for _, f := range shadowFamilies {
		var key string
		if cls == f.root {
			key = ""
		} else if k, ok := strings.CutPrefix(cls, f.root+"-"); ok && k != "" {
			key = k
		} else {
			continue
		}
		if key == "none" {
			if f.root == "drop-shadow" {
				return "--tw-drop-shadow: initial; filter: " + filterCompose + ";", true
			}
			return f.emit(f.none), true
		}
		if f.themeMap != nil {
			if v, ok := f.themeMap(theme)[key]; ok {
				if v == "none" {
					return f.emit(f.none), true
				}
				return f.emit(colorizeShadow(v, f.colorV)), true
			}
		}
		if v, ok := f.scale[key]; ok {
			return f.emit(colorizeShadow(v, f.colorV)), true
		}
		if key == "" {
			return "", false
		}
		if inner, hint, isArb := arbitraryValue(key); isArb {
			switch {
			case hint == "color":
				return f.colorDecl(inner, "")
			case hint == "" && looksLikeColor(inner) && !isVarOnly(inner):
				return f.colorDecl(inner, "")
			}
			val := inner
			if f.root == "inset-shadow" && !strings.HasPrefix(val, "inset") && !isVarOnly(val) {
				val = "inset " + val
			}
			if f.root == "drop-shadow" {
				val = "drop-shadow(" + colorizeShadow(val, f.colorV) + ")"
				return f.emit(val), true
			}
			if isVarOnly(val) {
				return f.emit(val), true
			}
			return f.emit(colorizeShadow(val, f.colorV)), true
		}
		if c, ok := colorOf(key, theme); ok {
			return f.colorDecl(c, "")
		}
		return "", false
	}
	return "", false
}

func (f shadowFamily) colorDecl(color, _ string) (string, bool) {
	return f.colorV + ": " + color + ";", true
}

// gradientInterp maps the /modifier of bg-linear-*, bg-radial and bg-conic.
func gradientInterp(mod string) (string, bool) {
	switch mod {
	case "":
		return "oklab", true
	case "srgb", "hsl", "hwb", "lab", "lch", "oklab", "oklch":
		return mod, true
	case "longer", "shorter", "increasing", "decreasing":
		return "oklch " + mod + " hue", true
	}
	return "", false
}

func gradientImage(fn, position, interp string) string {
	return "--tw-gradient-position: " + position + " in " + interp + "; background-image: " + fn + "-gradient(var(--tw-gradient-stops));"
}

var linearDirections = map[string]string{
	"to-t": "to top", "to-tr": "to top right", "to-r": "to right", "to-br": "to bottom right",
	"to-b": "to bottom", "to-bl": "to bottom left", "to-l": "to left", "to-tl": "to top left",
}

// backgroundGradientUtility resolves bg-linear-*, bg-gradient-to-*, bg-radial
// and bg-conic.
func backgroundGradientUtility(cls string, neg bool) (string, bool) {
	key, ok := strings.CutPrefix(cls, "bg-")
	if !ok {
		return "", false
	}
	kind, rest := "", ""
	switch {
	case strings.HasPrefix(key, "linear-"):
		kind, rest = "linear", key[len("linear-"):]
	case strings.HasPrefix(key, "gradient-to-"):
		kind, rest = "linear", key[len("gradient-"):]
	case key == "radial" || strings.HasPrefix(key, "radial-") || strings.HasPrefix(key, "radial/"):
		kind, rest = "radial", strings.TrimPrefix(strings.TrimPrefix(key, "radial"), "-")
	case key == "conic" || strings.HasPrefix(key, "conic-") || strings.HasPrefix(key, "conic/"):
		kind, rest = "conic", strings.TrimPrefix(strings.TrimPrefix(key, "conic"), "-")
	default:
		return "", false
	}
	base, mod := splitColorModifier(rest)
	if strings.HasPrefix(base, "/") {
		base, mod = "", base[1:]
	}
	if inner, isArb := unwrapArbitrary(base); isArb {
		if neg {
			return "", false
		}
		return "--tw-gradient-position: " + inner + "; background-image: " + kind + "-gradient(var(--tw-gradient-stops," + inner + "));", true
	}
	interp, ok := gradientInterp(mod)
	if !ok {
		return "", false
	}
	switch kind {
	case "linear":
		if dir, ok := linearDirections[base]; ok && !neg {
			return gradientImage("linear", dir, interp), true
		}
		if plainNumber(base) {
			ang := base + "deg"
			if neg {
				ang = "-" + ang
			}
			return gradientImage("linear", ang, interp), true
		}
	case "radial":
		if base == "" && !neg {
			return "--tw-gradient-position: in " + interp + "; background-image: radial-gradient(var(--tw-gradient-stops));", true
		}
	case "conic":
		if neg && plainNumber(base) {
			return gradientImage("conic", "from -"+base+"deg", interp), true
		}
		if base == "" && !neg {
			return "--tw-gradient-position: in " + interp + "; background-image: conic-gradient(var(--tw-gradient-stops));", true
		}
		if plainNumber(base) {
			return gradientImage("conic", "from "+base+"deg", interp), true
		}
	}
	return "", false
}

// gradientStopUtility resolves from-*, via-* and to-* colors and positions.
func gradientStopUtility(cls string, theme TailwindTheme) (string, bool) {
	for _, root := range []string{"from", "via", "to"} {
		key, ok := strings.CutPrefix(cls, root+"-")
		if !ok || key == "" {
			continue
		}
		if strings.HasSuffix(key, "%") && plainNumber(strings.TrimSuffix(key, "%")) {
			return "--tw-gradient-" + root + "-position: " + key + ";", true
		}
		if inner, hint, isArb := arbitraryValue(key); isArb && (hint == "length" || hint == "percentage" || (hint == "" && looksLikeLength(inner) && !isVarOnly(inner))) {
			return "--tw-gradient-" + root + "-position: " + inner + ";", true
		}
		c, ok := colorOf(key, theme)
		if !ok {
			if inner, hint, isArb := arbitraryValue(key); isArb && hint == "" && !looksLikeLength(inner) {
				c, ok = inner, true
			}
		}
		if !ok {
			return "", false
		}
		switch root {
		case "from":
			return "--tw-gradient-from: " + c + "; --tw-gradient-stops: " + gradientStops + ";", true
		case "via":
			return "--tw-gradient-via: " + c + "; --tw-gradient-via-stops: " + viaStops + "; --tw-gradient-stops: var(--tw-gradient-via-stops);", true
		default:
			return "--tw-gradient-to: " + c + "; --tw-gradient-stops: " + gradientStops + ";", true
		}
	}
	return "", false
}

// spaceUtility resolves space-x/y-* between children (the rule is wrapped in
// :where(& > :not(:last-child)) by classToRule). neg is a leading "-".
func spaceUtility(cls string, neg bool, theme TailwindTheme) (string, bool) {
	axis, rest := "", ""
	switch {
	case strings.HasPrefix(cls, "space-x-"):
		axis, rest = "x", cls[len("space-x-"):]
	case strings.HasPrefix(cls, "space-y-"):
		axis, rest = "y", cls[len("space-y-"):]
	default:
		return "", false
	}
	rv := "--tw-space-" + axis + "-reverse"
	if rest == "reverse" {
		if neg {
			return "", false
		}
		return rv + ": 1;", true
	}
	v := spacingValue(rest, theme)
	if v == "" {
		return "", false
	}
	if neg {
		v = negateLength(v)
	}
	start, end := "margin-inline-start", "margin-inline-end"
	if axis == "y" {
		start, end = "margin-block-start", "margin-block-end"
	}
	return rv + ": 0; " + start + ": calc(" + v + " * var(" + rv + ")); " + end + ": calc(" + v + " * calc(1 - var(" + rv + ")));", true
}

// divideUtility resolves divide-x/y width, reverse, color and style.
func divideUtility(cls string, theme TailwindTheme) (string, bool) {
	rest, ok := strings.CutPrefix(cls, "divide-")
	if !ok {
		return "", false
	}
	if borderStyles[rest] {
		return "--tw-border-style: " + rest + "; border-style: " + rest + ";", true
	}
	for _, axis := range []string{"x", "y"} {
		if rest != axis && !strings.HasPrefix(rest, axis+"-") {
			continue
		}
		val := strings.TrimPrefix(strings.TrimPrefix(rest, axis), "-")
		rv := "--tw-divide-" + axis + "-reverse"
		if val == "reverse" {
			return rv + ": 1;", true
		}
		w := "1px"
		switch {
		case val == "":
		case isDigits(val):
			w = val + "px"
		default:
			lw, ok := lengthOf(val)
			if !ok {
				return "", false
			}
			w = lw
		}
		if axis == "x" {
			return rv + ": 0; border-inline-style: var(--tw-border-style); border-inline-start-width: calc(" + w + " * var(" + rv + ")); border-inline-end-width: calc(" + w + " * calc(1 - var(" + rv + ")));", true
		}
		return rv + ": 0; border-bottom-style: var(--tw-border-style); border-top-style: var(--tw-border-style); border-top-width: calc(" + w + " * var(" + rv + ")); border-bottom-width: calc(" + w + " * calc(1 - var(" + rv + ")));", true
	}
	if c, ok := colorOf(rest, theme); ok {
		return "border-color: " + c + ";", true
	}
	return "", false
}

// isSiblingUtility reports whether the rule applies to the children of the
// element carrying the class (space-* and divide-*).
func isSiblingUtility(base string) bool {
	base = strings.TrimPrefix(base, "-")
	return strings.HasPrefix(base, "space-x-") || strings.HasPrefix(base, "space-y-") ||
		strings.HasPrefix(base, "divide-")
}

var namedShadowColors = map[string]bool{
	"red": true, "blue": true, "green": true, "black": true, "white": true, "gray": true,
	"grey": true, "yellow": true, "orange": true, "purple": true, "pink": true, "cyan": true,
	"magenta": true, "currentcolor": true, "transparent": true, "teal": true, "navy": true,
}

// colorizeNamed wraps a trailing named color of each comma-separated shadow
// in var(colorVar, name).
func colorizeNamed(val, colorVar string) string {
	parts := splitParenCommas(val)
	for i, p := range parts {
		t := strings.TrimSpace(p)
		if strings.Contains(t, "var(") {
			continue
		}
		fields := strings.Fields(t)
		if len(fields) == 0 {
			continue
		}
		last := fields[len(fields)-1]
		if namedShadowColors[strings.ToLower(last)] {
			fields[len(fields)-1] = "var(" + colorVar + ", " + last + ")"
			parts[i] = strings.Join(fields, " ")
		}
	}
	return strings.Join(parts, ", ")
}

func splitParenCommas(s string) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}
