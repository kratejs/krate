package css

import (
	"strconv"
	"strings"
)

// generateExtraCSS resolves the utility families not covered by the core chain
// in tailwind.go: filters, animation, blend modes, additional layout,
// typography, interactivity, 3D transforms, and background gradients. It
// returns "" when the class is not one of these families.
func generateExtraCSS(cls string, theme TailwindTheme) string {
	if css, ok := filterUtility(cls, theme); ok {
		return css
	}
	if css, ok := animationUtility(cls); ok {
		return css
	}
	if css, ok := blendUtility(cls); ok {
		return css
	}
	if css, ok := layoutExtraUtility(cls, theme); ok {
		return css
	}
	if css, ok := typographyExtraUtility(cls, theme); ok {
		return css
	}
	if css, ok := interactivityUtility(cls, theme); ok {
		return css
	}
	if css, ok := transform3DUtility(cls); ok {
		return css
	}
	if css, ok := borderExtraUtility(cls, theme); ok {
		return css
	}
	if css, ok := backgroundExtraUtility(cls, theme); ok {
		return css
	}
	if css, ok := miscUtility(cls, theme); ok {
		return css
	}
	return ""
}

// miscUtility covers remaining standard families not handled elsewhere: SVG
// fill/stroke, text overflow, logical spacing, background clip/origin, scroll
// snap, tables, and forced-colors.
func miscUtility(cls string, theme TailwindTheme) (string, bool) {
	if rest, ok := strings.CutPrefix(cls, "fill-"); ok {
		switch rest {
		case "none":
			return "fill: none;", true
		case "current":
			return "fill: currentColor;", true
		}
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "fill: " + v + ";", true
		}
		if v, ok := colorValue(rest, "", theme); ok {
			return "fill: " + v + ";", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "stroke-"); ok {
		switch rest {
		case "none":
			return "stroke: none;", true
		case "current":
			return "stroke: currentColor;", true
		}
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "stroke: " + v + ";", true
		}
		if n, err := strconv.Atoi(rest); err == nil {
			return "stroke-width: " + strconv.Itoa(n) + ";", true
		}
		if v, ok := colorValue(rest, "", theme); ok {
			return "stroke: " + v + ";", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "placeholder-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "color: " + v + ";", true
		}
		if v, ok := colorValue(rest, "", theme); ok {
			return "color: " + v + ";", true
		}
	}
	if v, ok := logicalSpacing(cls, theme); ok {
		return v, true
	}
	switch cls {
	case "text-ellipsis":
		return "text-overflow: ellipsis;", true
	case "text-clip":
		return "text-overflow: clip;", true
	case "bg-none":
		return "background-image: none;", true
	case "table-auto":
		return "table-layout: auto;", true
	case "table-fixed":
		return "table-layout: fixed;", true
	case "border-collapse":
		return "border-collapse: collapse;", true
	case "border-separate":
		return "border-collapse: separate;", true
	case "caption-top":
		return "caption-side: top;", true
	case "caption-bottom":
		return "caption-side: bottom;", true
	case "forced-color-adjust-auto":
		return "forced-color-adjust: auto;", true
	case "forced-color-adjust-none":
		return "forced-color-adjust: none;", true
	}
	if rest, ok := strings.CutPrefix(cls, "bg-clip-"); ok {
		switch rest {
		case "border":
			return "background-clip: border-box;", true
		case "padding":
			return "background-clip: padding-box;", true
		case "content":
			return "background-clip: content-box;", true
		case "text":
			return "-webkit-background-clip: text; background-clip: text;", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "bg-origin-"); ok {
		switch rest {
		case "border":
			return "background-origin: border-box;", true
		case "padding":
			return "background-origin: padding-box;", true
		case "content":
			return "background-origin: content-box;", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "snap-"); ok {
		switch rest {
		case "none":
			return "scroll-snap-type: none;", true
		case "x":
			return "scroll-snap-type: x var(--tw-scroll-snap-strictness, proximity);", true
		case "y":
			return "scroll-snap-type: y var(--tw-scroll-snap-strictness, proximity);", true
		case "both":
			return "scroll-snap-type: both var(--tw-scroll-snap-strictness, proximity);", true
		case "mandatory":
			return "--tw-scroll-snap-strictness: mandatory;", true
		case "proximity":
			return "--tw-scroll-snap-strictness: proximity;", true
		case "start":
			return "scroll-snap-align: start;", true
		case "end":
			return "scroll-snap-align: end;", true
		case "center":
			return "scroll-snap-align: center;", true
		case "align-none":
			return "scroll-snap-align: none;", true
		case "normal":
			return "scroll-snap-stop: normal;", true
		case "always":
			return "scroll-snap-stop: always;", true
		}
	}
	return "", false
}

// logicalSpacing resolves inline-start/end padding and margin utilities
// (ps/pe/ms/me) to their logical properties.
func logicalSpacing(cls string, theme TailwindTheme) (string, bool) {
	type entry struct{ prefix, prop string }
	for _, e := range []entry{
		{"ps-", "padding-inline-start"}, {"pe-", "padding-inline-end"},
		{"ms-", "margin-inline-start"}, {"me-", "margin-inline-end"},
	} {
		rest, ok := strings.CutPrefix(cls, e.prefix)
		if !ok {
			continue
		}
		if v, isArb := unwrapArbitrary(rest); isArb {
			return e.prop + ": " + v + ";", true
		}
		return e.prop + ": " + spacingValue(rest, theme) + ";", true
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Filters (#29)
// ---------------------------------------------------------------------------

// filterCompose chains filter functions through --tw-* variables so multiple
// filter utilities stack, matching Tailwind's composition model.
func filterVar(name, value string) string {
	return "--tw-" + name + ": " + value + "; filter: var(--tw-blur,) var(--tw-brightness,) var(--tw-contrast,) var(--tw-grayscale,) var(--tw-hue-rotate,) var(--tw-invert,) var(--tw-saturate,) var(--tw-sepia,) var(--tw-drop-shadow,);"
}

func backdropVar(name, value string) string {
	return "--tw-backdrop-" + name + ": " + value + "; -webkit-backdrop-filter: var(--tw-backdrop-blur,) var(--tw-backdrop-brightness,) var(--tw-backdrop-contrast,) var(--tw-backdrop-grayscale,) var(--tw-backdrop-hue-rotate,) var(--tw-backdrop-invert,) var(--tw-backdrop-opacity,) var(--tw-backdrop-saturate,) var(--tw-backdrop-sepia,); backdrop-filter: var(--tw-backdrop-blur,) var(--tw-backdrop-brightness,) var(--tw-backdrop-contrast,) var(--tw-backdrop-grayscale,) var(--tw-backdrop-hue-rotate,) var(--tw-backdrop-invert,) var(--tw-backdrop-opacity,) var(--tw-backdrop-saturate,) var(--tw-backdrop-sepia,);"
}

var filterScales = map[string]map[string]string{
	"blur":       {"": "8px", "none": "", "sm": "4px", "md": "12px", "lg": "16px", "xl": "24px", "2xl": "40px", "3xl": "64px"},
	"brightness": {"": "100%", "50": ".5", "75": ".75", "90": ".9", "95": ".95", "100": "1", "105": "1.05", "110": "1.1", "125": "1.25", "150": "1.5", "200": "2"},
	"contrast":   {"": "100%", "0": "0", "50": ".5", "75": ".75", "100": "1", "125": "1.25", "150": "1.5", "200": "2"},
	"saturate":   {"": "100%", "0": "0", "50": ".5", "100": "1", "150": "1.5", "200": "2"},
}

func filterUtility(cls string, theme TailwindTheme) (string, bool) {
	if cls == "filter" {
		return "filter: var(--tw-blur,) var(--tw-brightness,) var(--tw-contrast,) var(--tw-grayscale,) var(--tw-hue-rotate,) var(--tw-invert,) var(--tw-saturate,) var(--tw-sepia,) var(--tw-drop-shadow,);", true
	}
	if cls == "filter-none" {
		return "filter: none;", true
	}
	if cls == "backdrop-filter" {
		return "-webkit-backdrop-filter: var(--tw-backdrop-blur,) var(--tw-backdrop-brightness,) var(--tw-backdrop-contrast,) var(--tw-backdrop-grayscale,) var(--tw-backdrop-hue-rotate,) var(--tw-backdrop-invert,) var(--tw-backdrop-opacity,) var(--tw-backdrop-saturate,) var(--tw-backdrop-sepia,); backdrop-filter: var(--tw-backdrop-blur,) var(--tw-backdrop-brightness,) var(--tw-backdrop-contrast,) var(--tw-backdrop-grayscale,) var(--tw-backdrop-hue-rotate,) var(--tw-backdrop-invert,) var(--tw-backdrop-opacity,) var(--tw-backdrop-saturate,) var(--tw-backdrop-sepia,);", true
	}
	if cls == "backdrop-filter-none" {
		return "-webkit-backdrop-filter: none; backdrop-filter: none;", true
	}
	// Backdrop variants of the scale-based filters.
	if rest, ok := strings.CutPrefix(cls, "backdrop-"); ok {
		if css, ok := filterScaleValue(rest, theme, true); ok {
			return css, true
		}
		if v, ok := unwrapArbitrary(strings.TrimPrefix(rest, "blur-")); ok && strings.HasPrefix(rest, "blur-") {
			return backdropVar("blur", "blur("+v+")"), true
		}
	}
	// grayscale / invert / sepia (toggles and numeric steps).
	switch cls {
	case "grayscale":
		return filterVar("grayscale", "grayscale(100%)"), true
	case "grayscale-0":
		return filterVar("grayscale", "grayscale(0)"), true
	case "invert":
		return filterVar("invert", "invert(100%)"), true
	case "invert-0":
		return filterVar("invert", "invert(0)"), true
	case "sepia":
		return filterVar("sepia", "sepia(100%)"), true
	case "sepia-0":
		return filterVar("sepia", "sepia(0)"), true
	}
	// grayscale/invert/sepia numeric steps are percentages (grayscale-50 -> 50%).
	for _, name := range []string{"grayscale", "invert", "sepia"} {
		rest, ok := strings.CutPrefix(cls, name+"-")
		if !ok {
			continue
		}
		if v, isArb := unwrapArbitrary(rest); isArb {
			return filterVar(name, name+"("+v+")"), true
		}
		if _, err := strconv.ParseFloat(rest, 64); err == nil {
			return filterVar(name, name+"("+rest+"%)"), true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "hue-rotate-"); ok {
		if v, ok := unwrapArbitrary(rest); ok {
			return filterVar("hue-rotate", "hue-rotate("+v+")"), true
		}
		if _, err := strconv.ParseFloat(rest, 64); err == nil {
			return filterVar("hue-rotate", "hue-rotate("+rest+"deg)"), true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "drop-shadow-"); ok {
		if rest == "none" {
			return filterVar("drop-shadow", "drop-shadow(0 0 #0000)"), true
		}
		if v, ok := unwrapArbitrary(rest); ok {
			return filterVar("drop-shadow", "drop-shadow("+v+")"), true
		}
		switch rest {
		case "sm":
			return filterVar("drop-shadow", "drop-shadow(0 1px 1px rgb(0 0 0 / 0.05))"), true
		case "":
			return filterVar("drop-shadow", "drop-shadow(0 1px 2px rgb(0 0 0 / 0.1)) drop-shadow(0 1px 1px rgb(0 0 0 / 0.06))"), true
		case "md":
			return filterVar("drop-shadow", "drop-shadow(0 4px 3px rgb(0 0 0 / 0.07)) drop-shadow(0 2px 2px rgb(0 0 0 / 0.06))"), true
		case "lg":
			return filterVar("drop-shadow", "drop-shadow(0 10px 8px rgb(0 0 0 / 0.04)) drop-shadow(0 4px 3px rgb(0 0 0 / 0.1))"), true
		case "xl":
			return filterVar("drop-shadow", "drop-shadow(0 20px 13px rgb(0 0 0 / 0.03)) drop-shadow(0 8px 5px rgb(0 0 0 / 0.08))"), true
		case "2xl":
			return filterVar("drop-shadow", "drop-shadow(0 25px 25px rgb(0 0 0 / 0.15))"), true
		}
	}
	return filterScaleValue(cls, theme, false)
}

// filterScaleValue resolves blur-/brightness-/contrast-/saturate- (and their
// backdrop- forms when backdrop is true) against the filter scales.
func filterScaleValue(cls string, theme TailwindTheme, backdrop bool) (string, bool) {
	for name, scale := range filterScales {
		rest, ok := strings.CutPrefix(cls, name+"-")
		if !ok && cls != name {
			continue
		}
		if cls == name {
			rest = ""
		}
		if v, isArb := unwrapArbitrary(rest); isArb {
			return applyFilter(name, filterArbValue(name, v), backdrop), true
		}
		if v, ok := scale[rest]; ok {
			if v == "" {
				// Empty override, not `none`: `none` would cancel the whole
				// `filter:` chain (e.g. `filter: none var(--tw-brightness,)`).
				return applyFilter(name, "", backdrop), true
			}
			return applyFilter(name, filterNamedValue(name, v), backdrop), true
		}
	}
	return "", false
}

func applyFilter(name, fn string, backdrop bool) string {
	if backdrop {
		return backdropVar(name, fn)
	}
	return filterVar(name, fn)
}

// filterNamedValue wraps a scale value in its CSS filter function.
func filterNamedValue(name, v string) string {
	switch name {
	case "blur":
		return "blur(" + v + ")"
	case "brightness":
		return "brightness(" + v + ")"
	case "contrast":
		return "contrast(" + v + ")"
	case "saturate":
		return "saturate(" + v + ")"
	}
	return v
}

// filterArbValue normalizes an arbitrary filter value (blur([...])) to a filter
// function call, preserving a value that already contains the function.
func filterArbValue(name, v string) string {
	if strings.Contains(v, "(") {
		return v
	}
	return name + "(" + v + ")"
}

// ---------------------------------------------------------------------------
// Animation (#30)
// ---------------------------------------------------------------------------

func animationUtility(cls string) (string, bool) {
	if rest, ok := strings.CutPrefix(cls, "animate-"); ok {
		if rest == "none" {
			return "animation: none;", true
		}
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "animation: " + v + ";", true
		}
		switch rest {
		case "spin":
			return "animation: spin 1s linear infinite;", true
		case "ping":
			return "animation: ping 1s cubic-bezier(0, 0, 0.2, 1) infinite;", true
		case "pulse":
			return "animation: pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;", true
		case "bounce":
			return "animation: bounce 1s infinite;", true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Blend modes (#31)
// ---------------------------------------------------------------------------

var blendModes = map[string]bool{
	"normal": true, "multiply": true, "screen": true, "overlay": true,
	"darken": true, "lighten": true, "color-dodge": true, "color-burn": true,
	"hard-light": true, "soft-light": true, "difference": true, "exclusion": true,
	"hue": true, "saturation": true, "color": true, "luminosity": true,
	"plus-darker": true, "plus-lighter": true,
}

func blendUtility(cls string) (string, bool) {
	if rest, ok := strings.CutPrefix(cls, "mix-blend-"); ok {
		if blendModes[rest] {
			return "mix-blend-mode: " + rest + ";", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "bg-blend-"); ok {
		if blendModes[rest] {
			return "background-blend-mode: " + rest + ";", true
		}
	}
	if v, ok := unwrapArbitrary(strings.TrimPrefix(cls, "mix-blend-")); ok && strings.HasPrefix(cls, "mix-blend-") {
		return "mix-blend-mode: " + v + ";", true
	}
	if v, ok := unwrapArbitrary(strings.TrimPrefix(cls, "bg-blend-")); ok && strings.HasPrefix(cls, "bg-blend-") {
		return "background-blend-mode: " + v + ";", true
	}
	return "", false
}

// fontVariantNumeric maps a Tailwind numeric-variant class to the CSS variable
// it sets and the value, so multiple utilities compose.
var fontVariantNumeric = map[string][2]string{
	"ordinal":            {"ordinal", "ordinal"},
	"slashed-zero":       {"slashed-zero", "slashed-zero"},
	"lining-nums":        {"numeric-figure", "lining-nums"},
	"oldstyle-nums":      {"numeric-figure", "oldstyle-nums"},
	"proportional-nums":  {"numeric-spacing", "proportional-nums"},
	"tabular-nums":       {"numeric-spacing", "tabular-nums"},
	"diagonal-fractions": {"numeric-fraction", "diagonal-fractions"},
	"stacked-fractions":  {"numeric-fraction", "stacked-fractions"},
}

// ---------------------------------------------------------------------------
// Layout extras (#32)
// ---------------------------------------------------------------------------

func layoutExtraUtility(cls string, theme TailwindTheme) (string, bool) {
	switch cls {
	case "container":
		return "width: 100%;", true
	case "flow-root":
		return "display: flow-root;", true
	case "inline-table":
		return "display: inline-table;", true
	case "table":
		return "display: table;", true
	case "list-item":
		return "display: list-item;", true
	case "isolate":
		return "isolation: isolate;", true
	case "isolation-auto":
		return "isolation: auto;", true
	case "box-decoration-clone":
		return "box-decoration-break: clone; -webkit-box-decoration-break: clone;", true
	case "box-decoration-slice":
		return "box-decoration-break: slice; -webkit-box-decoration-break: slice;", true
	}
	// Float / clear.
	if rest, ok := strings.CutPrefix(cls, "float-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "float: " + v + ";", true
		}
		switch rest {
		case "right":
			return "float: right;", true
		case "left":
			return "float: left;", true
		case "none":
			return "float: none;", true
		case "start":
			return "float: inline-start;", true
		case "end":
			return "float: inline-end;", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "clear-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "clear: " + v + ";", true
		}
		switch rest {
		case "left", "right", "both", "none", "start", "end":
			if rest == "start" || rest == "end" {
				return "clear: inline-" + rest + ";", true
			}
			return "clear: " + rest + ";", true
		}
	}
	// Overscroll behavior.
	if rest, ok := strings.CutPrefix(cls, "overscroll-"); ok {
		parts := strings.SplitN(rest, "-", 2)
		if len(parts) == 1 {
			switch rest {
			case "auto", "contain", "none":
				return "overscroll-behavior: " + rest + ";", true
			}
		} else {
			axis := parts[0]
			val := parts[1]
			if (axis == "x" || axis == "y") && (val == "auto" || val == "contain" || val == "none") {
				return "overscroll-behavior-" + axis + ": " + val + ";", true
			}
		}
	}
	// Columns.
	if rest, ok := strings.CutPrefix(cls, "columns-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "columns: " + v + ";", true
		}
		switch rest {
		case "auto":
			return "columns: auto;", true
		case "3xs", "2xs", "xs", "sm", "md", "lg", "xl", "2xl", "3xl", "4xl", "5xl", "6xl", "7xl":
			if w, ok := containerScreens[rest]; ok {
				return "columns: " + w + ";", true
			}
		}
		if _, err := strconv.Atoi(rest); err == nil {
			return "columns: " + rest + ";", true
		}
	}
	// Break after/before/inside.
	if rest, ok := strings.CutPrefix(cls, "break-after-"); ok {
		if v, ok := breakValue(rest); ok {
			return "break-after: " + v + ";", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "break-before-"); ok {
		if v, ok := breakValue(rest); ok {
			return "break-before: " + v + ";", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "break-inside-"); ok {
		switch rest {
		case "auto":
			return "break-inside: auto;", true
		case "avoid":
			return "break-inside: avoid;", true
		case "avoid-page":
			return "break-inside: avoid-page;", true
		}
	}
	return "", false
}

func breakValue(rest string) (string, bool) {
	switch rest {
	case "auto", "avoid", "all", "avoid-page", "page", "left", "right", "column":
		return rest, true
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Typography extras (#34)
// ---------------------------------------------------------------------------

func typographyExtraUtility(cls string, theme TailwindTheme) (string, bool) {
	switch cls {
	case "italic":
		return "font-style: italic;", true
	case "not-italic":
		return "font-style: normal;", true
	case "overline":
		return "text-decoration-line: overline;", true
	case "no-underline":
		return "text-decoration-line: none;", true
	case "underline-offset-auto":
		return "text-underline-offset: auto;", true
	case "whitespace-break-spaces":
		return "white-space: break-spaces;", true
	case "break-keep":
		return "word-break: keep-all;", true
	case "break-words":
		return "overflow-wrap: break-word;", true
	case "break-all":
		return "word-break: break-all;", true
	case "break-normal":
		return "overflow-wrap: normal; word-break: normal;", true
	}
	if rest, ok := strings.CutPrefix(cls, "underline-offset-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "text-underline-offset: " + v + ";", true
		}
		if _, err := strconv.Atoi(rest); err == nil {
			if rest == "0" {
				return "text-underline-offset: 0px;", true
			}
			return "text-underline-offset: " + rest + "px;", true
		}
	}
	// Text indent (indent-*).
	if rest, ok := strings.CutPrefix(cls, "indent-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "text-indent: " + v + ";", true
		}
		if _, err := strconv.ParseFloat(rest, 64); err == nil {
			return "text-indent: " + spacingValue(rest, theme) + ";", true
		}
	}
	// Decoration color / style / thickness.
	if rest, ok := strings.CutPrefix(cls, "decoration-"); ok {
		if inner, hint, isArb := arbitraryValue(rest); isArb {
			switch hint {
			case "color":
				return "text-decoration-color: " + inner + ";", true
			case "length":
				return "text-decoration-thickness: " + inner + ";", true
			}
			if looksLikeColor(inner) {
				return "text-decoration-color: " + inner + ";", true
			}
			return "text-decoration-thickness: " + inner + ";", true
		}
		switch rest {
		case "solid", "double", "dotted", "dashed", "wavy":
			return "text-decoration-style: " + rest + ";", true
		}
		if _, err := strconv.Atoi(rest); err == nil {
			return "text-decoration-thickness: " + rest + "px;", true
		}
		if rest == "from-font" {
			return "text-decoration-thickness: from-font;", true
		}
		if rest == "auto" {
			return "text-decoration-thickness: auto;", true
		}
		key, alpha := splitColorModifier(rest)
		if v, ok := colorValue(key, alpha, theme); ok {
			return "text-decoration-color: " + v + ";", true
		}
	}
	// Font stretch.
	if rest, ok := strings.CutPrefix(cls, "font-stretch-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "font-stretch: " + v + ";", true
		}
		switch rest {
		case "ultra-condensed", "extra-condensed", "condensed", "semi-condensed",
			"normal", "semi-expanded", "expanded", "extra-expanded", "ultra-expanded":
			return "font-stretch: " + rest + ";", true
		}
		if strings.HasSuffix(rest, "%") {
			return "font-stretch: " + rest + ";", true
		}
	}
	// Font variant numeric. Composed through variables (Tailwind's model) so
	// e.g. `ordinal tabular-nums` combine instead of overwriting each other.
	if pair, ok := fontVariantNumeric[cls]; ok {
		return "--tw-" + pair[0] + ": " + pair[1] + "; font-variant-numeric: var(--tw-ordinal,) var(--tw-slashed-zero,) var(--tw-numeric-figure,) var(--tw-numeric-spacing,) var(--tw-numeric-fraction,);", true
	}
	// Font feature settings (arbitrary).
	if v, ok := unwrapArbitrary(strings.TrimPrefix(cls, "font-feature-settings-")); ok && strings.HasPrefix(cls, "font-feature-settings-") {
		return "font-feature-settings: " + v + ";", true
	}
	// Hyphens.
	switch cls {
	case "hyphens-none":
		return "hyphens: none;", true
	case "hyphens-manual":
		return "hyphens: manual;", true
	case "hyphens-auto":
		return "hyphens: auto;", true
	}
	// List image.
	if v, ok := unwrapArbitrary(strings.TrimPrefix(cls, "list-image-")); ok && strings.HasPrefix(cls, "list-image-") {
		if v == "none" {
			return "list-style-image: none;", true
		}
		return "list-style-image: " + v + ";", true
	}
	if cls == "list-image-none" {
		return "list-style-image: none;", true
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Interactivity extras (#35)
// ---------------------------------------------------------------------------

func interactivityUtility(cls string, theme TailwindTheme) (string, bool) {
	// Resize.
	switch cls {
	case "resize-none":
		return "resize: none;", true
	case "resize":
		return "resize: both;", true
	case "resize-x":
		return "resize: horizontal;", true
	case "resize-y":
		return "resize: vertical;", true
	}
	// Touch action.
	if rest, ok := strings.CutPrefix(cls, "touch-"); ok {
		switch rest {
		case "auto":
			return "touch-action: auto;", true
		case "none":
			return "touch-action: none;", true
		case "pan-x":
			return "touch-action: pan-x;", true
		case "pan-y":
			return "touch-action: pan-y;", true
		case "pan-left":
			return "touch-action: pan-left;", true
		case "pan-right":
			return "touch-action: pan-right;", true
		case "pan-up":
			return "touch-action: pan-up;", true
		case "pan-down":
			return "touch-action: pan-down;", true
		case "pinch-zoom":
			return "touch-action: pinch-zoom;", true
		case "manipulation":
			return "touch-action: manipulation;", true
		}
	}
	// Will change.
	if rest, ok := strings.CutPrefix(cls, "will-change-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "will-change: " + v + ";", true
		}
		switch rest {
		case "auto":
			return "will-change: auto;", true
		case "scroll":
			return "will-change: scroll-position;", true
		case "contents":
			return "will-change: contents;", true
		case "transform":
			return "will-change: transform;", true
		}
	}
	// Accent color.
	if rest, ok := strings.CutPrefix(cls, "accent-"); ok {
		if inner, _, isArb := arbitraryValue(rest); isArb {
			return "accent-color: " + inner + ";", true
		}
		if rest == "auto" {
			return "accent-color: auto;", true
		}
		key, alpha := splitColorModifier(rest)
		if v, ok := colorValue(key, alpha, theme); ok {
			return "accent-color: " + v + ";", true
		}
	}
	// Caret color.
	if rest, ok := strings.CutPrefix(cls, "caret-"); ok {
		if inner, _, isArb := arbitraryValue(rest); isArb {
			return "caret-color: " + inner + ";", true
		}
		key, alpha := splitColorModifier(rest)
		if v, ok := colorValue(key, alpha, theme); ok {
			return "caret-color: " + v + ";", true
		}
	}
	// Color scheme.
	if rest, ok := strings.CutPrefix(cls, "scheme-"); ok {
		switch rest {
		case "normal", "light", "dark", "light-dark", "only-light", "only-dark":
			return "color-scheme: " + strings.Replace(rest, "-", " ", 1) + ";", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "color-scheme-"); ok {
		switch rest {
		case "normal", "light", "dark":
			return "color-scheme: " + rest + ";", true
		}
	}
	// Field sizing.
	if rest, ok := strings.CutPrefix(cls, "field-sizing-"); ok {
		switch rest {
		case "fixed", "content":
			return "field-sizing: " + rest + ";", true
		}
	}
	// Scrollbar (v4) & scroll-behavior.
	switch cls {
	case "scroll-auto":
		return "scroll-behavior: auto;", true
	case "scroll-smooth":
		return "scroll-behavior: smooth;", true
	}
	// Transition behavior.
	switch cls {
	case "transition-normal":
		return "transition-behavior: normal;", true
	case "transition-discrete":
		return "transition-behavior: allow-discrete;", true
	}
	// Scroll margin / padding (logical and physical).
	if css, ok := scrollSpacingUtility(cls, theme); ok {
		return css, true
	}
	return "", false
}

// scrollSpacingUtility resolves scroll-m*/scroll-p* (including logical
// scroll-mb/scroll-pi variants).
func scrollSpacingUtility(cls string, theme TailwindTheme) (string, bool) {
	prefixes := []struct{ prefix, prop string }{
		{"scroll-mt-", "scroll-margin-top"}, {"scroll-mr-", "scroll-margin-right"},
		{"scroll-mb-", "scroll-margin-bottom"}, {"scroll-ml-", "scroll-margin-left"},
		{"scroll-ms-", "scroll-margin-inline-start"}, {"scroll-me-", "scroll-margin-inline-end"},
		{"scroll-mx-", "scroll-margin-inline"}, {"scroll-my-", "scroll-margin-block"},
		{"scroll-m-", "scroll-margin"},
		{"scroll-pt-", "scroll-padding-top"}, {"scroll-pr-", "scroll-padding-right"},
		{"scroll-pb-", "scroll-padding-bottom"}, {"scroll-pl-", "scroll-padding-left"},
		{"scroll-ps-", "scroll-padding-inline-start"}, {"scroll-pe-", "scroll-padding-inline-end"},
		{"scroll-px-", "scroll-padding-inline"}, {"scroll-py-", "scroll-padding-block"},
		{"scroll-p-", "scroll-padding"},
	}
	// Longest prefixes first so `scroll-mt-` wins over `scroll-m-`.
	for _, p := range prefixes {
		rest, ok := strings.CutPrefix(cls, p.prefix)
		if !ok || rest == "" {
			continue
		}
		if v, isArb := unwrapArbitrary(rest); isArb {
			return p.prop + ": " + v + ";", true
		}
		if _, err := strconv.ParseFloat(rest, 64); err == nil {
			return p.prop + ": " + spacingValue(rest, theme) + ";", true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// 3D transforms (#36)
// ---------------------------------------------------------------------------

func transform3DUtility(cls string) (string, bool) {
	if rest, ok := strings.CutPrefix(cls, "perspective-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "perspective: " + v + ";", true
		}
		switch rest {
		case "dramatic":
			return "perspective: 100px;", true
		case "near":
			return "perspective: 300px;", true
		case "normal":
			return "perspective: 500px;", true
		case "midrange":
			return "perspective: 800px;", true
		case "distant":
			return "perspective: 1200px;", true
		case "none":
			return "perspective: none;", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "perspective-origin-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "perspective-origin: " + v + ";", true
		}
		switch rest {
		case "center", "top", "bottom", "left", "right", "top-left", "top-right", "bottom-left", "bottom-right":
			return "perspective-origin: " + strings.ReplaceAll(rest, "-", " ") + ";", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "transform-3d"); ok && rest == "" {
		return "transform-style: preserve-3d;", true
	}
	if rest, ok := strings.CutPrefix(cls, "transform-style-"); ok {
		switch rest {
		case "3d":
			return "transform-style: preserve-3d;", true
		case "flat":
			return "transform-style: flat;", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "backface-"); ok {
		switch rest {
		case "visible":
			return "backface-visibility: visible;", true
		case "hidden":
			return "backface-visibility: hidden;", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "rotate-x-"); ok {
		if v, ok := angleValue(rest); ok {
			return "--tw-rotate-x: " + v + "; transform: " + transform3DCompose + ";", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "rotate-y-"); ok {
		if v, ok := angleValue(rest); ok {
			return "--tw-rotate-y: " + v + "; transform: " + transform3DCompose + ";", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "rotate-z-"); ok {
		if v, ok := angleValue(rest); ok {
			return "--tw-rotate-z: " + v + "; transform: " + transform3DCompose + ";", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "zoom-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "zoom: " + v + ";", true
		}
		if _, err := strconv.ParseFloat(rest, 64); err == nil {
			return "zoom: " + scaleFactor(rest) + ";", true
		}
	}
	return "", false
}

// transform3DCompose extends the 2D compose with the translateZ/rotateX/Y/Z
// variables used by the 3D transform utilities.
const transform3DCompose = "translate3d(var(--tw-translate-x,0),var(--tw-translate-y,0),var(--tw-translate-z,0)) rotateX(var(--tw-rotate-x,0)) rotateY(var(--tw-rotate-y,0)) rotateZ(var(--tw-rotate-z,0)) skewX(var(--tw-skew-x,0)) skewY(var(--tw-skew-y,0)) scaleX(var(--tw-scale-x,1)) scaleY(var(--tw-scale-y,1)) scaleZ(var(--tw-scale-z,1))"

// ---------------------------------------------------------------------------
// Border extras (#37)
// ---------------------------------------------------------------------------

func borderExtraUtility(cls string, theme TailwindTheme) (string, bool) {
	// Logical borders: border-s / border-e with optional width.
	if rest, ok := strings.CutPrefix(cls, "border-s-"); ok {
		if v, ok := borderSideWidth(rest); ok {
			return "border-inline-start-width: " + v + "; border-style: solid;", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "border-e-"); ok {
		if v, ok := borderSideWidth(rest); ok {
			return "border-inline-end-width: " + v + "; border-style: solid;", true
		}
	}
	// Outline styles and colors (widths are handled in the core chain).
	switch cls {
	case "outline-dashed":
		return "outline-style: dashed;", true
	case "outline-dotted":
		return "outline-style: dotted;", true
	case "outline-double":
		return "outline-style: double;", true
	case "outline-solid":
		return "outline-style: solid;", true
	case "outline-hidden":
		return "outline: 2px solid transparent; outline-offset: 2px;", true
	case "outline-offset-0":
		return "outline-offset: 0px;", true
	}
	if rest, ok := strings.CutPrefix(cls, "outline-offset-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "outline-offset: " + v + ";", true
		}
		if _, err := strconv.Atoi(rest); err == nil {
			return "outline-offset: " + rest + "px;", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "outline-"); ok {
		if inner, hint, isArb := arbitraryValue(rest); isArb {
			switch hint {
			case "color":
				return "outline-color: " + inner + ";", true
			case "length":
				return "outline-width: " + inner + "; outline-style: solid;", true
			}
			if looksLikeColor(inner) {
				return "outline-color: " + inner + ";", true
			}
			if looksLikeLength(inner) {
				return "outline-width: " + inner + "; outline-style: solid;", true
			}
			return "outline: " + inner + " solid;", true
		}
		key, alpha := splitColorModifier(rest)
		if v, ok := colorValue(key, alpha, theme); ok {
			return "outline-color: " + v + ";", true
		}
	}
	// Border spacing. `border-spacing-x`/`-y` are not real CSS properties, so
	// compose through variables (matching Tailwind) rather than emitting them.
	const borderSpacingCompose = " border-spacing: var(--tw-border-spacing-x,0) var(--tw-border-spacing-y,0);"
	if rest, ok := strings.CutPrefix(cls, "border-spacing-x-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "--tw-border-spacing-x: " + v + ";" + borderSpacingCompose, true
		}
		return "--tw-border-spacing-x: " + spacingValue(rest, theme) + ";" + borderSpacingCompose, true
	}
	if rest, ok := strings.CutPrefix(cls, "border-spacing-y-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "--tw-border-spacing-y: " + v + ";" + borderSpacingCompose, true
		}
		return "--tw-border-spacing-y: " + spacingValue(rest, theme) + ";" + borderSpacingCompose, true
	}
	if rest, ok := strings.CutPrefix(cls, "border-spacing-"); ok {
		v := spacingValue(rest, theme)
		if isArb, ok := unwrapArbitrary(rest); ok {
			v = isArb
		}
		return "--tw-border-spacing-x: " + v + "; --tw-border-spacing-y: " + v + ";" + borderSpacingCompose, true
	}
	return "", false
}

// borderSideWidth resolves a border-width suffix (numeric or arbitrary).
func borderSideWidth(rest string) (string, bool) {
	if v, ok := unwrapArbitrary(rest); ok {
		return v, true
	}
	if _, err := strconv.ParseFloat(rest, 64); err == nil {
		if rest == "0" {
			return "0", true
		}
		return rest + "px", true
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Background / gradient extras (#38)
// ---------------------------------------------------------------------------

func backgroundExtraUtility(cls string, theme TailwindTheme) (string, bool) {
	// Tailwind v4 gradient direction names: bg-linear-to-r, bg-linear-45.
	if rest, ok := strings.CutPrefix(cls, "bg-linear-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "background-image: linear-gradient(" + v + ", var(--tw-gradient-stops));", true
		}
		if deg, err := strconv.ParseFloat(rest, 64); err == nil {
			_ = deg
			return "background-image: linear-gradient(" + rest + "deg, var(--tw-gradient-stops));", true
		}
		if dir, ok := gradientDirection(rest); ok {
			return "background-image: linear-gradient(" + dir + ", var(--tw-gradient-stops));", true
		}
	}
	if rest, ok := strings.CutPrefix(cls, "bg-radial-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "background-image: radial-gradient(" + v + ", var(--tw-gradient-stops));", true
		}
		return "background-image: radial-gradient(var(--tw-gradient-stops));", true
	}
	if cls == "bg-radial" {
		return "background-image: radial-gradient(var(--tw-gradient-stops));", true
	}
	if rest, ok := strings.CutPrefix(cls, "bg-conic-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "background-image: conic-gradient(" + v + ", var(--tw-gradient-stops));", true
		}
		return "background-image: conic-gradient(from " + rest + ", var(--tw-gradient-stops));", true
	}
	// Percentage / length gradient stops: from-10%, via-50%, to-90%.
	for _, prefix := range []string{"from", "via", "to"} {
		rest, ok := strings.CutPrefix(cls, prefix+"-")
		if !ok {
			continue
		}
		if strings.HasSuffix(rest, "%") {
			if _, err := strconv.ParseFloat(strings.TrimSuffix(rest, "%"), 64); err == nil {
				return gradientPositionProperty(prefix, rest), true
			}
		}
		if v, isArb := unwrapArbitrary(rest); isArb && looksLikeLength(v) {
			return gradientPositionProperty(prefix, v), true
		}
	}
	return "", false
}

func gradientPositionProperty(prefix, v string) string {
	switch prefix {
	case "from":
		return "--tw-gradient-from-position: " + v + ";"
	case "via":
		return "--tw-gradient-via-position: " + v + ";"
	default:
		return "--tw-gradient-to-position: " + v + ";"
	}
}

func gradientDirection(rest string) (string, bool) {
	switch rest {
	case "to-t":
		return "to top", true
	case "to-tr":
		return "to top right", true
	case "to-r":
		return "to right", true
	case "to-br":
		return "to bottom right", true
	case "to-b":
		return "to bottom", true
	case "to-bl":
		return "to bottom left", true
	case "to-l":
		return "to left", true
	case "to-tl":
		return "to top left", true
	}
	return "", false
}
