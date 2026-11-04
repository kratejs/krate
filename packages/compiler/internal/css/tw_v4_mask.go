package css

import "strings"

// Tailwind v4 mask utilities: static keyword utilities plus the composable
// gradient masks (mask-t-from-*, mask-linear-*, mask-radial-*, mask-conic-*)
// which share the --tw-mask-* variables and mask-image composition.

const maskCompose = "mask-image: var(--tw-mask-linear), var(--tw-mask-radial), var(--tw-mask-conic); mask-composite: intersect;"

var maskStatic = map[string]string{
	"mask-none":                   "mask-image: none;",
	"mask-add":                    "mask-composite: add;",
	"mask-subtract":               "mask-composite: subtract;",
	"mask-intersect":              "mask-composite: intersect;",
	"mask-exclude":                "mask-composite: exclude;",
	"mask-alpha":                  "mask-mode: alpha;",
	"mask-luminance":              "mask-mode: luminance;",
	"mask-match":                  "mask-mode: match-source;",
	"mask-type-alpha":             "mask-type: alpha;",
	"mask-type-luminance":         "mask-type: luminance;",
	"mask-auto":                   "mask-size: auto;",
	"mask-cover":                  "mask-size: cover;",
	"mask-contain":                "mask-size: contain;",
	"mask-no-clip":                "mask-clip: no-clip;",
	"mask-center":                 "mask-position: center;",
	"mask-top":                    "mask-position: top;",
	"mask-right":                  "mask-position: right;",
	"mask-bottom":                 "mask-position: bottom;",
	"mask-left":                   "mask-position: left;",
	"mask-top-left":               "mask-position: left top;",
	"mask-top-right":              "mask-position: right top;",
	"mask-bottom-left":            "mask-position: left bottom;",
	"mask-bottom-right":           "mask-position: right bottom;",
	"mask-repeat":                 "mask-repeat: repeat;",
	"mask-no-repeat":              "mask-repeat: no-repeat;",
	"mask-repeat-x":               "mask-repeat: repeat-x;",
	"mask-repeat-y":               "mask-repeat: repeat-y;",
	"mask-repeat-space":           "mask-repeat: space;",
	"mask-repeat-round":           "mask-repeat: round;",
	"mask-circle":                 "--tw-mask-radial-shape: circle;",
	"mask-ellipse":                "--tw-mask-radial-shape: ellipse;",
	"mask-radial-closest-side":    "--tw-mask-radial-size: closest-side;",
	"mask-radial-closest-corner":  "--tw-mask-radial-size: closest-corner;",
	"mask-radial-farthest-side":   "--tw-mask-radial-size: farthest-side;",
	"mask-radial-farthest-corner": "--tw-mask-radial-size: farthest-corner;",
}

var maskBoxes = map[string]string{
	"border": "border-box", "padding": "padding-box", "content": "content-box",
	"fill": "fill-box", "stroke": "stroke-box", "view": "view-box",
}

var maskRadialPositions = []string{
	"center", "top", "top-right", "right", "bottom-right", "bottom", "bottom-left", "left", "top-left",
}

// maskSides maps the mask-<side> abbreviations to their gradient sides, in
// the order Tailwind declares them.
var maskSides = map[string][]string{
	"t": {"top"}, "r": {"right"}, "b": {"bottom"}, "l": {"left"},
	"x": {"right", "left"}, "y": {"top", "bottom"},
}

// maskUtility resolves mask-* utilities.
func maskUtility(cls string, neg bool, theme TailwindTheme) (string, bool) {
	if !strings.HasPrefix(cls, "mask-") {
		return "", false
	}
	if neg {
		return maskAngle(cls, true)
	}
	if css, ok := maskStatic[cls]; ok {
		return css, true
	}
	rest := cls[len("mask-"):]
	if css, ok := maskAngle(cls, false); ok {
		return css, true
	}
	if k, ok := strings.CutPrefix(rest, "origin-"); ok {
		if box, ok := maskBoxes[k]; ok {
			return "mask-origin: " + box + ";", true
		}
		return "", false
	}
	if k, ok := strings.CutPrefix(rest, "clip-"); ok {
		if box, ok := maskBoxes[k]; ok {
			return "mask-clip: " + box + ";", true
		}
		return "", false
	}
	if k, ok := strings.CutPrefix(rest, "size-"); ok {
		if v, isArb := unwrapArbitrary(k); isArb {
			return "mask-size: " + v + ";", true
		}
		return "", false
	}
	if k, ok := strings.CutPrefix(rest, "position-"); ok {
		if v, isArb := unwrapArbitrary(k); isArb {
			return "mask-position: " + v + ";", true
		}
		return "", false
	}
	if k, ok := strings.CutPrefix(rest, "radial-at-"); ok {
		for _, p := range maskRadialPositions {
			if k == p {
				return "--tw-mask-radial-position: " + strings.ReplaceAll(p, "-", " ") + ";", true
			}
		}
		return "", false
	}
	if k, ok := strings.CutPrefix(rest, "radial-"); ok {
		if v, isArb := unwrapArbitrary(k); isArb {
			return maskCompose + " --tw-mask-radial: radial-gradient(var(--tw-mask-radial-stops, var(--tw-mask-radial-size))); --tw-mask-radial-size: " + v + ";", true
		}
	}
	if k, ok := strings.CutPrefix(rest, "linear-"); ok {
		if v, isArb := unwrapArbitrary(k); isArb {
			return maskCompose + " --tw-mask-linear: linear-gradient(var(--tw-mask-linear-stops, var(--tw-mask-linear-position))); --tw-mask-linear-position: " + v + ";", true
		}
	}
	if k, ok := strings.CutPrefix(rest, "conic-"); ok {
		if v, isArb := unwrapArbitrary(k); isArb {
			return maskCompose + " --tw-mask-conic: conic-gradient(var(--tw-mask-conic-stops, var(--tw-mask-conic-position))); --tw-mask-conic-position: " + v + ";", true
		}
	}
	if css, ok := maskStopUtility(rest, theme); ok {
		return css, true
	}
	// mask-[<image>] and mask-(--var)
	if v, hint, isArb := arbitraryValue(rest); isArb && (hint == "" || hint == "image" || hint == "url") {
		return "mask-image: " + v + ";", true
	}
	return "", false
}

// maskAngle resolves mask-linear-<deg> and mask-conic-<deg> (negatable).
func maskAngle(cls string, neg bool) (string, bool) {
	for _, kind := range []string{"linear", "conic"} {
		key, ok := strings.CutPrefix(cls, "mask-"+kind+"-")
		if !ok || !plainNumber(key) {
			continue
		}
		pos := key + "deg"
		if neg {
			pos = "-" + pos
		}
		fn := kind + "-gradient"
		return maskCompose + " --tw-mask-" + kind + ": " + fn + "(var(--tw-mask-" + kind + "-stops, var(--tw-mask-" + kind + "-position))); --tw-mask-" + kind + "-position: " + pos + ";", true
	}
	return "", false
}

// maskStopUtility resolves <axis>-from-*/to-* where axis is a side (t, r, b,
// l, x, y) or a gradient kind (linear, radial, conic).
func maskStopUtility(rest string, theme TailwindTheme) (string, bool) {
	axis, tail, ok := strings.Cut(rest, "-")
	if !ok {
		return "", false
	}
	end, key, ok := strings.Cut(tail, "-")
	if !ok || (end != "from" && end != "to") {
		return "", false
	}
	sides, isSide := maskSides[axis]
	if !isSide && axis != "linear" && axis != "radial" && axis != "conic" {
		return "", false
	}
	prop, ok := maskStopValue(key, theme)
	if !ok {
		return "", false
	}
	var b strings.Builder
	b.WriteString(maskCompose)
	if isSide {
		b.WriteString(" --tw-mask-linear: var(--tw-mask-left), var(--tw-mask-right), var(--tw-mask-bottom), var(--tw-mask-top);")
		for _, s := range sides {
			b.WriteString(" --tw-mask-" + s + ": linear-gradient(to " + s + ", var(--tw-mask-" + s + "-from-color) var(--tw-mask-" + s + "-from-position), var(--tw-mask-" + s + "-to-color) var(--tw-mask-" + s + "-to-position));")
			b.WriteString(" --tw-mask-" + s + "-" + end + "-" + prop.kind + ": " + prop.value + ";")
		}
		return b.String(), true
	}
	stops := "var(--tw-mask-" + axis + "-position), "
	switch axis {
	case "radial":
		stops = "var(--tw-mask-radial-shape) var(--tw-mask-radial-size) at var(--tw-mask-radial-position), "
	case "conic":
		stops = "from var(--tw-mask-conic-position), "
	}
	stops += "var(--tw-mask-" + axis + "-from-color) var(--tw-mask-" + axis + "-from-position), var(--tw-mask-" + axis + "-to-color) var(--tw-mask-" + axis + "-to-position)"
	b.WriteString(" --tw-mask-" + axis + "-stops: " + stops + ";")
	b.WriteString(" --tw-mask-" + axis + ": " + axis + "-gradient(var(--tw-mask-" + axis + "-stops));")
	b.WriteString(" --tw-mask-" + axis + "-" + end + "-" + prop.kind + ": " + prop.value + ";")
	return b.String(), true
}

type maskStop struct{ kind, value string }

// maskStopValue classifies a from-/to- key as a position or a color.
func maskStopValue(key string, theme TailwindTheme) (maskStop, bool) {
	if key == "" {
		return maskStop{}, false
	}
	if strings.HasSuffix(key, "%") && plainNumber(strings.TrimSuffix(key, "%")) {
		return maskStop{"position", key}, true
	}
	if plainNumber(key) {
		if v := spacingValue(key, theme); v != "" {
			if v == "0" {
				v = "0px"
			}
			return maskStop{"position", v}, true
		}
	}
	if inner, hint, isArb := arbitraryValue(key); isArb && (hint == "length" || hint == "percentage" || (hint == "" && (looksLikeLength(inner) || isVarOnly(inner)))) {
		return maskStop{"position", inner}, true
	}
	if c, ok := colorOf(key, theme); ok {
		return maskStop{"color", c}, true
	}
	return maskStop{}, false
}

// maskProperties registers the --tw-mask-* variables so their neutral
// initial values apply and they do not inherit.
func maskProperties() []twProperty {
	props := []twProperty{
		tpi("--tw-mask-linear", "linear-gradient(#fff, #fff)"),
		tpi("--tw-mask-radial", "linear-gradient(#fff, #fff)"),
		tpi("--tw-mask-conic", "linear-gradient(#fff, #fff)"),
		tpi("--tw-mask-linear-position", "0deg"),
		tpi("--tw-mask-radial-shape", "ellipse"),
		tpi("--tw-mask-radial-size", "farthest-corner"),
		tpi("--tw-mask-radial-position", "center"),
		tpi("--tw-mask-conic-position", "0deg"),
	}
	for _, s := range []string{"top", "right", "bottom", "left"} {
		props = append(props, tpi("--tw-mask-"+s, "linear-gradient(#fff, #fff)"))
	}
	for _, a := range []string{"top", "right", "bottom", "left", "linear", "radial", "conic"} {
		props = append(props,
			tpi("--tw-mask-"+a+"-from-position", "0%"),
			tpi("--tw-mask-"+a+"-to-position", "100%"),
			tpi("--tw-mask-"+a+"-from-color", "black"),
			tpi("--tw-mask-"+a+"-to-color", "transparent"))
	}
	return props
}
