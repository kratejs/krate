package css

import "strings"

// Tailwind v4 style motion utilities: animation, transitions and transforms.
// Transforms use the individual translate/rotate/scale properties so they
// compose with keyframe animations that animate `transform`.

const (
	twTransitionTail = "transition-timing-function: var(--tw-ease, cubic-bezier(0.4, 0, 0.2, 1)); transition-duration: var(--tw-duration, 150ms);"
	twColorProps     = "color, background-color, border-color, outline-color, text-decoration-color, fill, stroke, --tw-gradient-from, --tw-gradient-via, --tw-gradient-to"
	twTransformProps = "transform, translate, scale, rotate"
	twTransitionAll  = twColorProps + ", opacity, box-shadow, " + twTransformProps +
		", filter, -webkit-backdrop-filter, backdrop-filter, display, content-visibility, overlay, pointer-events"
	twTransformVars = "var(--tw-rotate-x,) var(--tw-rotate-y,) var(--tw-rotate-z,) var(--tw-skew-x,) var(--tw-skew-y,)"
)

var easeNames = map[string]string{
	"linear":  "linear",
	"in":      "cubic-bezier(0.4, 0, 1, 1)",
	"out":     "cubic-bezier(0, 0, 0.2, 1)",
	"in-out":  "cubic-bezier(0.4, 0, 0.2, 1)",
	"initial": "initial",
}

// animationUtility resolves animate-*: built-in and theme animations,
// animate-none, animate-[...] and animate-(--var).
func animationUtility(cls string, theme TailwindTheme) (string, bool) {
	rest, ok := strings.CutPrefix(cls, "animate-")
	if !ok || rest == "" {
		return "", false
	}
	if rest == "none" {
		return "animation: none;", true
	}
	if v, isArb := unwrapArbitrary(rest); isArb {
		return "animation: " + v + ";", true
	}
	if v, ok := animationValue(rest, theme); ok {
		return "animation: " + v + ";", true
	}
	return "", false
}

// transitionUtility resolves transition*, duration-*, delay-* and ease-*.
func transitionUtility(cls string) (string, bool) {
	switch cls {
	case "transition":
		return "transition-property: " + twTransitionAll + "; " + twTransitionTail, true
	case "transition-all":
		return "transition-property: all; " + twTransitionTail, true
	case "transition-colors":
		return "transition-property: " + twColorProps + "; " + twTransitionTail, true
	case "transition-opacity":
		return "transition-property: opacity; " + twTransitionTail, true
	case "transition-shadow":
		return "transition-property: box-shadow; " + twTransitionTail, true
	case "transition-transform":
		return "transition-property: " + twTransformProps + "; " + twTransitionTail, true
	case "transition-none":
		return "transition-property: none;", true
	case "transition-normal":
		return "transition-behavior: normal;", true
	case "transition-discrete":
		return "transition-behavior: allow-discrete;", true
	}
	if rest, ok := strings.CutPrefix(cls, "transition-"); ok {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "transition-property: " + v + "; " + twTransitionTail, true
		}
		return "", false
	}
	if rest, ok := strings.CutPrefix(cls, "duration-"); ok {
		switch {
		case rest == "initial":
			return "--tw-duration: initial;", true
		case isDigits(rest):
			return "--tw-duration: " + rest + "ms; transition-duration: " + rest + "ms;", true
		}
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "--tw-duration: " + v + "; transition-duration: " + v + ";", true
		}
		return "", false
	}
	if rest, ok := strings.CutPrefix(cls, "delay-"); ok {
		switch {
		case isDigits(rest):
			return "transition-delay: " + rest + "ms;", true
		}
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "transition-delay: " + v + ";", true
		}
		return "", false
	}
	if rest, ok := strings.CutPrefix(cls, "ease-"); ok {
		if v, ok := easeNames[rest]; ok {
			if v == "initial" {
				return "--tw-ease: initial;", true
			}
			return "--tw-ease: " + v + "; transition-timing-function: " + v + ";", true
		}
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "--tw-ease: " + v + "; transition-timing-function: " + v + ";", true
		}
	}
	return "", false
}

// translateAxisValue resolves a translate value (spacing, fraction, full, px,
// arbitrary). Fractions are percentages; z never takes them.
func translateAxisValue(key string, theme TailwindTheme, allowPercent bool) (string, bool) {
	if v, isArb := unwrapArbitrary(key); isArb {
		return v, true
	}
	if allowPercent {
		if key == "full" {
			return "100%", true
		}
		if pct, ok := fractionPercent(key); ok {
			return pct, true
		}
	}
	if key == "0" {
		return "0px", true
	}
	if v := spacingValue(key, theme); v != "" {
		return v, true
	}
	return "", false
}

// transformUtility resolves translate*, rotate*, skew*, scale*, transform*
// and origin-*. neg is true for a leading "-" on the class.
func transformUtility(cls string, neg bool, theme TailwindTheme) (string, bool) {
	signed := func(v string) string {
		if neg {
			return negateLength(v)
		}
		return v
	}
	switch cls {
	case "transform", "transform-cpu":
		return "transform: " + twTransformVars + ";", !neg
	case "transform-gpu":
		return "transform: translateZ(0) " + twTransformVars + ";", !neg
	case "transform-none":
		return "transform: none;", !neg
	case "translate-none":
		return "translate: none;", !neg
	case "rotate-none":
		return "rotate: none;", !neg
	case "scale-none":
		return "scale: none;", !neg
	case "scale-3d":
		return "scale: var(--tw-scale-x) var(--tw-scale-y) var(--tw-scale-z);", !neg
	}
	if rest, ok := strings.CutPrefix(cls, "transform-"); ok && !neg {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "transform: " + v + ";", true
		}
		return "", false
	}
	if rest, ok := strings.CutPrefix(cls, "translate-"); ok {
		axis, val := "", rest
		for _, a := range []string{"x", "y", "z"} {
			if v, ok := strings.CutPrefix(rest, a+"-"); ok {
				axis, val = a, v
			}
		}
		v, ok := translateAxisValue(val, theme, axis != "z")
		if !ok {
			return "", true
		}
		v = signed(v)
		switch axis {
		case "x":
			return "--tw-translate-x: " + v + "; translate: var(--tw-translate-x) var(--tw-translate-y);", true
		case "y":
			return "--tw-translate-y: " + v + "; translate: var(--tw-translate-x) var(--tw-translate-y);", true
		case "z":
			return "--tw-translate-z: " + v + "; translate: var(--tw-translate-x) var(--tw-translate-y) var(--tw-translate-z);", true
		}
		return "--tw-translate-x: " + v + "; --tw-translate-y: " + v + "; translate: var(--tw-translate-x) var(--tw-translate-y);", true
	}
	if rest, ok := strings.CutPrefix(cls, "rotate-"); ok {
		for _, a := range []string{"x", "y", "z"} {
			if v, ok := strings.CutPrefix(rest, a+"-"); ok {
				ang, ok := angleValue(v)
				if !ok {
					return "", true
				}
				return "--tw-rotate-" + a + ": rotate" + strings.ToUpper(a) + "(" + signed(ang) + "); transform: " + twTransformVars + ";", true
			}
		}
		ang, ok := angleValue(rest)
		if !ok {
			return "", true
		}
		return "rotate: " + signed(ang) + ";", true
	}
	if rest, ok := strings.CutPrefix(cls, "skew-"); ok {
		axes := []string{"x", "y"}
		val := rest
		for _, a := range axes {
			if v, ok := strings.CutPrefix(rest, a+"-"); ok {
				axes, val = []string{a}, v
			}
		}
		ang, ok := angleValue(val)
		if !ok {
			return "", true
		}
		ang = signed(ang)
		var b strings.Builder
		for _, a := range axes {
			b.WriteString("--tw-skew-" + a + ": skew" + strings.ToUpper(a) + "(" + ang + "); ")
		}
		b.WriteString("transform: " + twTransformVars + ";")
		return b.String(), true
	}
	if rest, ok := strings.CutPrefix(cls, "scale-"); ok {
		axes := []string{"x", "y", "z"}
		val := rest
		direct := true
		for _, a := range []string{"x", "y", "z"} {
			if v, ok := strings.CutPrefix(rest, a+"-"); ok {
				axes, val, direct = []string{a}, v, false
			}
		}
		if inner, isArb := unwrapArbitrary(val); isArb && direct {
			return "scale: " + signed(inner) + ";", true
		}
		var v string
		if inner, isArb := unwrapArbitrary(val); isArb {
			v = inner
		} else if plainNumber(val) {
			v = val + "%"
		} else {
			return "", true
		}
		v = signed(v)
		var b strings.Builder
		for _, a := range axes {
			b.WriteString("--tw-scale-" + a + ": " + v + "; ")
		}
		if len(axes) == 1 && axes[0] == "z" {
			b.WriteString("scale: var(--tw-scale-x) var(--tw-scale-y) var(--tw-scale-z);")
		} else {
			b.WriteString("scale: var(--tw-scale-x) var(--tw-scale-y);")
		}
		return b.String(), true
	}
	if rest, ok := strings.CutPrefix(cls, "origin-"); ok && !neg {
		if v, isArb := unwrapArbitrary(rest); isArb {
			return "transform-origin: " + v + ";", true
		}
		if v, ok := originNames[rest]; ok {
			return "transform-origin: " + v + ";", true
		}
		return "", true
	}
	return "", false
}

var originNames = map[string]string{
	"center": "center", "top": "top", "bottom": "bottom", "left": "left", "right": "right",
	"top-left": "0 0", "top-right": "100% 0", "bottom-right": "100% 100%", "bottom-left": "0 100%",
}
