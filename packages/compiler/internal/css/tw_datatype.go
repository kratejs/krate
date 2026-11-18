package css

import (
	"regexp"
	"strings"
)

// Data type inference for arbitrary values handed to a custom utility's
// --value([type]) and --modifier([type]), following Tailwind v4's rules.

const twNumberRe = `[+-]?\d*\.?\d+(?:[eE][+-]?\d+)?`

var (
	twIsNumber     = regexp.MustCompile(`^` + twNumberRe + `$`)
	twIsPercentage = regexp.MustCompile(`^` + twNumberRe + `%$`)
	twIsFraction   = regexp.MustCompile(`^` + twNumberRe + `\s*/\s*` + twNumberRe + `$`)
	twIsLength     = regexp.MustCompile(`^` + twNumberRe + `(?:cm|mm|Q|in|pc|pt|px|em|ex|ch|cap|ic|rem|rex|rch|rcap|ric|lh|rlh|vw|vh|vmin|vmax|vb|vi|svw|svh|svi|svb|svmin|svmax|lvw|lvh|lvi|lvb|lvmin|lvmax|dvw|dvh|dvi|dvb|dvmin|dvmax|cqw|cqh|cqi|cqb|cqmin|cqmax)$`)
	twIsAngle      = regexp.MustCompile(`^` + twNumberRe + `(?:deg|rad|grad|turn)$`)
	twIsVector     = regexp.MustCompile(`^` + twNumberRe + ` +` + twNumberRe + ` +` + twNumberRe + `$`)
	twSpacingFn    = regexp.MustCompile(`(?i)^--spacing\(`)
	twColorFn      = regexp.MustCompile(`(?i)^(?:rgba?|hsla?|hwb|color|(?:ok)?(?:lab|lch)|light-dark|color-mix|--alpha)\(`)
	twURL          = regexp.MustCompile(`^url\(.*\)$`)
	twGradient     = regexp.MustCompile(`^(?:repeating-)?(?:conic|linear|radial)-gradient\(`)
	twImageFn      = regexp.MustCompile(`^(?:element|image|cross-fade|image-set)\(`)
	twCanonNumber  = regexp.MustCompile(`^(?:0|[1-9]\d*)(?:\.\d*[1-9])?$`)
	twCanonInt     = regexp.MustCompile(`^(?:0|[1-9]\d*)$`)
)

// twDataTypes are the types a [type] argument or a `type:value` hint may name.
var twDataTypes = map[string]bool{
	"color": true, "length": true, "percentage": true, "ratio": true, "number": true,
	"integer": true, "url": true, "position": true, "bg-size": true, "line-width": true,
	"image": true, "family-name": true, "generic-name": true, "absolute-size": true,
	"relative-size": true, "angle": true, "vector": true,
}

// twMathFns are the CSS math functions Tailwind recognizes in a value.
var twMathFns = []string{"calc", "min", "max", "clamp", "mod", "rem", "sin", "cos", "tan", "asin",
	"acos", "atan", "atan2", "pow", "sqrt", "hypot", "log", "exp", "round"}

// twHasMathFn reports whether v calls a CSS math function anywhere.
func twHasMathFn(v string) bool {
	if !strings.Contains(v, "(") {
		return false
	}
	for _, fn := range twMathFns {
		if strings.Contains(v, fn+"(") {
			return true
		}
	}
	return false
}

// twInfer returns the first of types that the arbitrary value v can be read
// as, or "" when none matches. A var() value has no knowable type, so it
// matches nothing. This is Tailwind v4's inferDataType.
func twInfer(v string, types ...string) string {
	for _, typ := range types {
		if inferDataType(v, typ) {
			return typ
		}
	}
	return ""
}

// inferDataType reports whether the arbitrary value v can be read as typ.
// A var() value has no knowable type, so it matches nothing.
func inferDataType(v, typ string) bool {
	if strings.HasPrefix(v, "var(") {
		return false
	}
	return twIsType(v, typ)
}

// twIsType is inferDataType without the var() guard, applied to the parts of
// a compound value too.
func twIsType(v, typ string) bool {
	switch typ {
	case "color":
		if strings.HasPrefix(v, "#") {
			return true
		}
		return twNamedColors[strings.ToLower(v)] || twColorFn.MatchString(v)
	case "length":
		return twIsLength.MatchString(v) || twSpacingFn.MatchString(v) || twHasMathFn(v)
	case "percentage":
		return twIsPercentage.MatchString(v) || twHasMathFn(v)
	case "ratio":
		return twIsFraction.MatchString(v) || twHasMathFn(v)
	case "number":
		return twIsNumber.MatchString(v) || twHasMathFn(v)
	case "integer":
		return twCanonInt.MatchString(v)
	case "url":
		return twURL.MatchString(v)
	case "line-width":
		for _, part := range topLevelSplit(v, ' ') {
			if !twIsType(part, "length") && !twIsType(part, "number") && part != "thin" && part != "medium" && part != "thick" {
				return false
			}
		}
		return true
	case "image":
		n := 0
		for _, part := range topLevelSplit(v, ',') {
			switch {
			case strings.HasPrefix(part, "var("):
			case twURL.MatchString(part), twGradient.MatchString(part), twImageFn.MatchString(part):
				n++
			default:
				return false
			}
		}
		return n > 0
	case "family-name":
		n := 0
		for _, part := range topLevelSplit(v, ',') {
			if part != "" && part[0] >= '0' && part[0] <= '9' {
				return false
			}
			if !strings.HasPrefix(part, "var(") {
				n++
			}
		}
		return n > 0
	case "generic-name":
		switch v {
		case "serif", "sans-serif", "monospace", "cursive", "fantasy", "system-ui", "ui-serif",
			"ui-sans-serif", "ui-monospace", "ui-rounded", "math", "emoji", "fangsong":
			return true
		}
	case "absolute-size":
		switch v {
		case "xx-small", "x-small", "small", "medium", "large", "x-large", "xx-large", "xxx-large":
			return true
		}
	case "relative-size":
		return v == "larger" || v == "smaller"
	case "angle":
		return twIsAngle.MatchString(v)
	case "vector":
		return twIsVector.MatchString(v)
	case "position":
		n := 0
		for _, part := range topLevelSplit(v, ' ') {
			switch {
			case part == "center" || part == "top" || part == "right" || part == "bottom" || part == "left":
				n++
			case strings.HasPrefix(part, "var("):
			case twIsType(part, "length") || twIsType(part, "percentage"):
				n++
			default:
				return false
			}
		}
		return n > 0
	case "bg-size":
		n := 0
		for _, part := range topLevelSplit(v, ',') {
			if part == "cover" || part == "contain" {
				n++
				continue
			}
			sizes := topLevelSplit(part, ' ')
			if len(sizes) != 1 && len(sizes) != 2 {
				return false
			}
			ok := true
			for _, sz := range sizes {
				if sz != "auto" && !twIsType(sz, "length") && !twIsType(sz, "percentage") {
					ok = false
				}
			}
			if ok {
				n++
			}
		}
		return n > 0
	}
	return false
}

// twNamedColors are the CSS named and system colors Tailwind recognizes,
// lower-cased.
var twNamedColors = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range strings.Fields(`transparent currentcolor aliceblue antiquewhite aqua aquamarine azure beige bisque
black blanchedalmond blue blueviolet brown burlywood cadetblue chartreuse chocolate coral cornflowerblue cornsilk
crimson cyan darkblue darkcyan darkgoldenrod darkgray darkgreen darkgrey darkkhaki darkmagenta darkolivegreen
darkorange darkorchid darkred darksalmon darkseagreen darkslateblue darkslategray darkslategrey darkturquoise
darkviolet deeppink deepskyblue dimgray dimgrey dodgerblue firebrick floralwhite forestgreen fuchsia gainsboro
ghostwhite gold goldenrod gray green greenyellow grey honeydew hotpink indianred indigo ivory khaki lavender
lavenderblush lawngreen lemonchiffon lightblue lightcoral lightcyan lightgoldenrodyellow lightgray lightgreen
lightgrey lightpink lightsalmon lightseagreen lightskyblue lightslategray lightslategrey lightsteelblue lightyellow
lime limegreen linen magenta maroon mediumaquamarine mediumblue mediumorchid mediumpurple mediumseagreen
mediumslateblue mediumspringgreen mediumturquoise mediumvioletred midnightblue mintcream mistyrose moccasin
navajowhite navy oldlace olive olivedrab orange orangered orchid palegoldenrod palegreen paleturquoise
palevioletred papayawhip peachpuff peru pink plum powderblue purple rebeccapurple red rosybrown royalblue
saddlebrown salmon sandybrown seagreen seashell sienna silver skyblue slateblue slategray slategrey snow
springgreen steelblue tan teal thistle tomato turquoise violet wheat white whitesmoke yellow yellowgreen
canvas canvastext linktext visitedtext activetext buttonface buttontext buttonborder field fieldtext highlight
highlighttext selecteditem selecteditemtext mark marktext graytext accentcolor accentcolortext`) {
		m[n] = true
	}
	return m
}()
