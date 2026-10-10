package css

import (
	"fmt"
	"strconv"
	"strings"
)

// variant is one parsed modifier applied to a utility. Exactly one of the
// selector or at-rule fields is set, per kind.
type variant struct {
	// kind is "pseudo" (suffix the utility selector), "ancestor" (wrap it with
	// a PLACEHOLDER), "media", or "supports".
	kind string
	// sel is the selector fragment for pseudo/ancestor kinds.
	sel string
	// at is the condition for media/supports kinds, e.g. "(min-width: 640px)".
	at string
}

// parseVariants splits a full class name into its stacked variant tokens (left
// to right) and the base utility. It respects `[...]` brackets (which may
// contain `:`) and leaves `/` opacity modifiers untouched, so
// `sm:hover:bg-red-500/50` yields (["sm","hover"], "bg-red-500/50").
func parseVariants(cls string) ([]string, string) {
	var variants []string
	depth := 0
	start := 0
	for i := 0; i < len(cls); i++ {
		switch cls[i] {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				variants = append(variants, cls[start:i])
				start = i + 1
			}
		}
	}
	base := cls[start:]
	base = strings.TrimPrefix(base, "!")
	return variants, base
}

// resolveVariant maps a variant token to its CSS form using the theme (for
// breakpoints and dark-mode strategy). ok=false means "unknown variant" so the
// caller can skip the class rather than emit it unconditionally.
func resolveVariant(v string, theme TailwindTheme) (variant, bool) {
	// Arbitrary variant: [&:nth-child(3)], [@media(min-width:900px)], [dir=rtl].
	if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
		inner := strings.ReplaceAll(v[1:len(v)-1], "_", " ")
		if strings.HasPrefix(inner, "@") {
			if i := strings.IndexByte(inner, '('); i > 0 {
				kw := inner[1:i]
				kind := "@media"
				if kw == "supports" {
					kind = "@supports"
				}
				return variant{kind: strings.TrimPrefix(kind, "@"), at: inner[i:]}, true
			}
			return variant{}, false
		}
		if inner == "&" {
			return variant{kind: "pseudo"}, true
		}
		if strings.Contains(inner, "&") {
			return variant{kind: "ancestor", sel: strings.ReplaceAll(inner, "&", "PLACEHOLDER")}, true
		}
		// Bare selector fragment, e.g. [dir=rtl] -> ancestor.
		return variant{kind: "ancestor", sel: inner + " PLACEHOLDER"}, true
	}

	// Responsive breakpoints from the theme (mobile-first min-width).
	if bp, ok := theme.Screens[v]; ok {
		return variant{kind: "media", at: "(min-width: " + bp + ")"}, true
	}
	if rest, ok := strings.CutPrefix(v, "min-"); ok {
		if bp, ok := theme.Screens[rest]; ok {
			return variant{kind: "media", at: "(min-width: " + bp + ")"}, true
		}
	}
	if rest, ok := strings.CutPrefix(v, "max-"); ok {
		if bp, ok := theme.Screens[rest]; ok {
			return variant{kind: "media", at: "(max-width: calc(" + bp + " - 0.02px))"}, true
		}
	}

	// Container-query variants: @sm:, @md: (min-width), @max-sm:, @container.
	if strings.HasPrefix(v, "@") {
		return resolveContainerVariant(v, theme)
	}

	// not-* variant negates a pseudo/media/supports variant.
	if rest, ok := strings.CutPrefix(v, "not-"); ok {
		if inner, ok := resolveVariant(rest, theme); ok {
			switch inner.kind {
			case "pseudo":
				return variant{kind: "pseudo", sel: ":not(" + inner.sel + ")"}, true
			case "media":
				return variant{kind: "media", at: "not " + inner.at}, true
			case "supports":
				return variant{kind: "supports", at: "not " + inner.at}, true
			}
		}
	}

	// Child variants (Tailwind v4): `*:` selects direct children, `**:` all.
	if v == "*" {
		return variant{kind: "ancestor", sel: "PLACEHOLDER > *"}, true
	}
	if v == "**" {
		return variant{kind: "ancestor", sel: "PLACEHOLDER *"}, true
	}

	// Dark mode: strategy from config (default media).
	if v == "dark" {
		switch theme.DarkMode {
		case "class", "selector":
			return variant{kind: "ancestor", sel: ".dark PLACEHOLDER"}, true
		default:
			return variant{kind: "media", at: "(prefers-color-scheme: dark)"}, true
		}
	}

	// Direction.
	if v == "rtl" {
		return variant{kind: "ancestor", sel: "[dir='rtl'] PLACEHOLDER"}, true
	}
	if v == "ltr" {
		return variant{kind: "ancestor", sel: "[dir='ltr'] PLACEHOLDER"}, true
	}

	// Group / peer (optionally named: group/item-hover). Group uses a
	// descendant combinator; peer uses the general-sibling combinator (`~`),
	// since only a preceding sibling can observe the peer's state.
	if kind, name, state, ok := parseGroupPeer(v); ok {
		base := "." + EscapeClass(kind)
		if name != "" {
			base += "\\/" + EscapeClass(name)
		}
		stateSel, ok := groupPeerStateSelector(state)
		if !ok {
			return variant{}, false
		}
		combinator := " "
		if kind == "peer" {
			combinator = " ~ "
		}
		return variant{kind: "ancestor", sel: base + stateSel + combinator + "PLACEHOLDER"}, true
	}

	// data-* / aria-* / has-*.
	if sel, ok := parseAttrVariant(v); ok {
		return variant{kind: "ancestor", sel: sel + " PLACEHOLDER"}, true
	}

	if sel, ok := pseudoVariants[v]; ok {
		return variant{kind: "pseudo", sel: sel}, true
	}
	if sel, ok := nthVariants(v); ok {
		return variant{kind: "pseudo", sel: sel}, true
	}
	if at, ok := mediaVariants[v]; ok {
		return variant{kind: "media", at: at}, true
	}
	// inert: / starting: attribute-style variants.
	if v == "inert" {
		return variant{kind: "ancestor", sel: "[inert] PLACEHOLDER"}, true
	}
	if v == "starting" {
		return variant{kind: "ancestor", sel: "[data-starting] PLACEHOLDER"}, true
	}
	// Generic supports-[...].
	if rest, ok := strings.CutPrefix(v, "supports-"); ok {
		if inner, ok := unwrapArbitrary(rest); ok {
			return variant{kind: "supports", at: "(" + inner + ")"}, true
		}
	}
	if at, ok := supportsVariants[v]; ok {
		return variant{kind: "supports", at: at}, true
	}
	return variant{}, false
}

// nthVariants resolves the positional pseudo-class variants (nth-*, first-line,
// first-letter) to their CSS selector form.
func nthVariants(v string) (string, bool) {
	switch v {
	case "first-line":
		return "::first-line", true
	case "first-letter":
		return "::first-letter", true
	}
	type nthKind struct{ prefix, fn string }
	for _, k := range []nthKind{
		{"nth-of-type-", ":nth-of-type"},
		{"nth-last-of-type-", ":nth-last-of-type"},
		{"nth-last-", ":nth-last-child"},
		{"nth-", ":nth-child"},
	} {
		if rest, ok := strings.CutPrefix(v, k.prefix); ok && rest != "" {
			return k.fn + "(" + rest + ")", true
		}
	}
	return "", false
}

// containerScreens are container-query widths keyed by the `@<name>` suffix.
var containerScreens = map[string]string{
	"3xs": "16rem", "2xs": "18rem", "xs": "20rem", "sm": "24rem", "md": "28rem",
	"lg": "32rem", "xl": "36rem", "2xl": "42rem", "3xl": "48rem", "4xl": "56rem",
	"5xl": "64rem", "6xl": "72rem", "7xl": "80rem",
}

// resolveContainerVariant resolves Tailwind v4 container-query variants:
// `@sm`, `@max-sm`, and arbitrary `@[400px]`.
func resolveContainerVariant(v string, theme TailwindTheme) (variant, bool) {
	rest := v[1:] // drop '@'
	if rest == "" {
		return variant{}, false
	}
	minMax := "min"
	if after, ok := strings.CutPrefix(rest, "max-"); ok {
		minMax = "max"
		rest = after
	}
	var width string
	if inner, ok := unwrapArbitrary(rest); ok {
		width = inner
	} else if w, ok := containerScreens[rest]; ok {
		width = w
	} else if w, ok := theme.Screens[rest]; ok {
		width = w
	}
	if width == "" {
		return variant{}, false
	}
	if minMax == "max" {
		return variant{kind: "media", at: "(max-width: calc(" + width + " - 0.1px))"}, true
	}
	return variant{kind: "media", at: "(min-width: " + width + ")"}, true
}

// groupPeerStateSelector converts a group/peer state token into a selector
// fragment. `checked` -> `:checked`; `aria-checked` -> `[aria-checked='true']`;
// `data-open` -> `[data-open]`; `has-[...]` -> `:has(...)`.
func groupPeerStateSelector(state string) (string, bool) {
	if sel, ok := parseAttrVariant(state); ok {
		return sel, true
	}
	if sel, ok := pseudoVariants[state]; ok {
		return sel, true
	}
	if sel, ok := nthVariants(state); ok {
		return sel, true
	}
	if state == "" {
		return "", false
	}
	return ":" + state, true
}

// parseGroupPeer recognizes group-<state>/peer-<state> and the named forms
// group/<name>-<state> / peer/<name>-<state>.
func parseGroupPeer(v string) (kind, name, state string, ok bool) {
	for _, k := range []string{"group", "peer"} {
		rest, isK := strings.CutPrefix(v, k)
		if !isK {
			continue
		}
		if named, after, hasName := strings.Cut(rest, "/"); hasName {
			if i := strings.IndexByte(after, '-'); i > 0 {
				state = after[i+1:]
				if state != "" {
					return k, named, state, true
				}
			}
			continue
		}
		if strings.HasPrefix(rest, "-") {
			state = rest[1:]
			if state != "" {
				return k, "", state, true
			}
		}
	}
	return "", "", "", false
}

// parseAttrVariant handles data-[...], aria-[...]/aria-*, and has-[...].
func parseAttrVariant(v string) (string, bool) {
	switch {
	case strings.HasPrefix(v, "data-"):
		rest := v[len("data-"):]
		if strings.HasPrefix(rest, "[") && strings.HasSuffix(rest, "]") {
			return "[data-" + strings.ReplaceAll(rest[1:len(rest)-1], "_", " ") + "]", true
		}
		return "[data-" + rest + "]", true
	case strings.HasPrefix(v, "aria-"):
		rest := v[len("aria-"):]
		if strings.HasPrefix(rest, "[") && strings.HasSuffix(rest, "]") {
			return "[aria-" + strings.ReplaceAll(rest[1:len(rest)-1], "_", " ") + "]", true
		}
		return "[aria-" + rest + "='true']", true
	case strings.HasPrefix(v, "has-"):
		rest := v[len("has-"):]
		if strings.HasPrefix(rest, "[") && strings.HasSuffix(rest, "]") {
			return ":has(" + strings.ReplaceAll(rest[1:len(rest)-1], "_", " ") + ")", true
		}
	}
	return "", false
}

// pseudoVariants maps state variants to a selector fragment appended to the
// utility selector (pseudo-classes and pseudo-elements alike).
var pseudoVariants = map[string]string{
	"hover": ":hover", "focus": ":focus", "focus-visible": ":focus-visible",
	"focus-within": ":focus-within", "active": ":active", "visited": ":visited",
	"target": ":target", "disabled": ":disabled", "enabled": ":enabled",
	"checked": ":checked", "indeterminate": ":indeterminate", "default": ":default",
	"required": ":required", "valid": ":valid", "invalid": ":invalid",
	"in-range": ":in-range", "out-of-range": ":out-of-range",
	"placeholder-shown": ":placeholder-shown", "autofill": ":autofill",
	"read-only": ":read-only", "empty": ":empty", "open": "[open]",
	"closed": ":not([open])", "first": ":first-child", "last": ":last-child",
	"only": ":only-child", "odd": ":nth-child(odd)", "even": ":nth-child(even)",
	"first-of-type": ":first-of-type", "last-of-type": ":last-of-type",
	"only-of-type": ":only-of-type", "placeholder": "::placeholder",
	"before": "::before", "after": "::after", "marker": "::marker",
	"selection": "::selection", "file": "::file-selector-button",
	"backdrop": "::backdrop",
}

// mediaVariants maps mode variants to their media condition.
var mediaVariants = map[string]string{
	"motion-safe":        "(prefers-reduced-motion: no-preference)",
	"motion-reduce":      "(prefers-reduced-motion: reduce)",
	"contrast-more":      "(prefers-contrast: more)",
	"contrast-less":      "(prefers-contrast: less)",
	"print":              "print",
	"portrait":           "(orientation: portrait)",
	"landscape":          "(orientation: landscape)",
	"forced-colors":      "(forced-colors: active)",
	"pointer-fine":       "(pointer: fine)",
	"pointer-coarse":     "(pointer: coarse)",
	"pointer-none":       "(pointer: none)",
	"any-pointer-fine":   "(any-pointer: fine)",
	"any-pointer-coarse": "(any-pointer: coarse)",
	"any-pointer-none":   "(any-pointer: none)",
	"noscript":           "scripting: none",
	"inverted-colors":    "(inverted-colors: inverted)",
}

// supportsVariants maps a variant to a @supports condition.
var supportsVariants = map[string]string{
	"supports-grid":     "(display: grid)",
	"supports-backdrop": "(backdrop-filter: blur(0))",
}

// applyVariants builds the final selector and the wrapping at-rules for a base
// utility selector. Pseudo variants suffix the selector; ancestor variants wrap
// it; media/supports conditions nest outward. `baseSel` is the already-escaped
// class name (without a leading dot); the returned selector includes the dot.
// Returns the selector and at-rules (outermost first).
func applyVariants(baseSel string, vs []variant) (string, []variant) {
	sel := "." + baseSel
	var ats []variant
	for i := len(vs) - 1; i >= 0; i-- {
		switch vs[i].kind {
		case "pseudo":
			sel += vs[i].sel
		case "ancestor":
			sel = strings.Replace(vs[i].sel, "PLACEHOLDER", sel, 1)
		case "media", "supports":
			if vs[i].at != "" {
				ats = append(ats, vs[i])
			}
		}
	}
	// ats are innermost-first (right-to-left); reverse to outermost-first.
	for i, j := 0, len(ats)-1; i < j; i, j = i+1, j-1 {
		ats[i], ats[j] = ats[j], ats[i]
	}
	return sel, ats
}

// variantOrderKey returns a sort key reproducing Tailwind's cascade order:
// base utilities first, then state/pseudo variants, then dark, supports, and
// finally responsive breakpoints in ascending width. Within a category the
// variant tokens and class name break ties deterministically, so output is
// byte-stable. Prefixing the rank (zero-padded) is what fixes the previous
// inversion where every @media rule sorted before every base rule.
func variantOrderKey(cls string, variants []variant, ats []variant) string {
	rank, sub := 0, ""
	if len(ats) > 0 {
		rank, sub = atRuleOrder(ats[0])
	} else if len(variants) > 0 {
		rank = 1
	}
	return fmt.Sprintf("%02d:%s|%s|%s", rank, sub, variantTokensKey(variants), EscapeClass(cls))
}

// atRuleOrder ranks an at-rule variant: dark (2) < supports (3) < min-width
// breakpoints ascending (4) < max-width (5) < feature/orientation queries (6) <
// other (7).
func atRuleOrder(a variant) (int, string) {
	if a.kind == "supports" {
		return 3, a.at
	}
	at := a.at
	switch {
	case strings.Contains(at, "min-width"):
		return 4, fmt.Sprintf("%012d", firstPx(at))
	case strings.Contains(at, "max-width"):
		return 5, fmt.Sprintf("%012d", firstPx(at))
	case strings.Contains(at, "prefers-color-scheme"):
		return 2, at
	case strings.Contains(at, "hover:"), strings.Contains(at, "pointer:"),
		strings.Contains(at, "orientation:"), strings.Contains(at, "any-hover:"),
		strings.Contains(at, "any-pointer:"), strings.Contains(at, "prefers-contrast:"):
		return 6, at
	default:
		return 7, at
	}
}

// firstPx extracts the leading integer pixel value from a media condition
// (e.g. "(min-width: 640px)" -> 640). Returns 0 when none is found.
func firstPx(at string) int {
	start := -1
	for i := 0; i < len(at); i++ {
		if at[i] >= '0' && at[i] <= '9' {
			if start < 0 {
				start = i
			}
		} else if start >= 0 {
			n, _ := strconv.Atoi(at[start:i])
			return n
		}
	}
	if start >= 0 {
		n, _ := strconv.Atoi(at[start:])
		return n
	}
	return 0
}

// variantTokensKey renders the resolved variants as a stable secondary key.
func variantTokensKey(vs []variant) string {
	parts := make([]string, 0, len(vs))
	for _, v := range vs {
		parts = append(parts, v.kind+":"+v.sel+v.at)
	}
	return strings.Join(parts, ",")
}

// wrapAtRules wraps a selector/declaration block in the given at-rules
// (outermost first) so nesting is correct.
func wrapAtRules(sel, decls string, ats []variant) string {
	out := sel + "{" + decls + "}"
	for i := len(ats) - 1; i >= 0; i-- {
		kw := "@media"
		if ats[i].kind == "supports" {
			kw = "@supports"
		}
		if ats[i].at == "print" {
			out = "@media print{" + out + "}"
			continue
		}
		out = kw + " " + ats[i].at + "{" + out + "}"
	}
	return out
}
