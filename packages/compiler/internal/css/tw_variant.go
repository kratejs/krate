package css

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// variant is one resolved modifier. A variant either rewrites the selector
// (sel, a template where "&" stands for the selector so far) or wraps the rule
// in at-rules (ats), or both (hover is a selector inside a media query).
type variant struct {
	// sel is the selector template, "" meaning "&".
	sel string
	// ats are at-rule preludes such as "@media (hover: hover)", outermost first.
	ats []string
	// alts, when set, are union arms: the rule is emitted once per arm.
	alts []variant
	// rank is the v4 sort rank of this variant (registration order plus a
	// value-dependent suffix); see variantOrderKey.
	rank string
	// comp is the compounds mask: 1 = at-rule, 2 = selector, 0 = neither.
	comp int
	// hook marks ::before/::after, which need the content declaration.
	hook bool
	// rel marks a relative arbitrary selector such as [>img] (only valid in has-*).
	rel bool
}

const (
	compAt  = 1
	compSel = 2
)

// variantRegistration lists variant roots in Tailwind v4's registration order.
// The position is the primary sort rank: later variants win the cascade.
// Entries starting with "#" are groups whose members share one rank.
var variantRegistration = []string{
	"*", "**", "not", "group", "peer", "first-letter", "first-line", "marker",
	"selection", "file", "placeholder", "backdrop", "details-content", "before",
	"after", "first", "last", "only", "odd", "even", "first-of-type",
	"last-of-type", "only-of-type", "visited", "target", "open", "default",
	"checked", "indeterminate", "placeholder-shown", "autofill", "optional",
	"required", "valid", "invalid", "user-valid", "user-invalid", "in-range",
	"out-of-range", "read-only", "empty", "focus-within", "hover", "focus",
	"focus-visible", "active", "enabled", "disabled", "inert", "in", "has",
	"aria", "data", "nth", "nth-last", "nth-of-type", "nth-last-of-type",
	"supports", "motion-safe", "motion-reduce", "contrast-more", "contrast-less",
	"#max", "#min", "#cmax", "#cmin", "portrait", "landscape", "ltr", "rtl",
	"dark", "starting", "print", "forced-colors", "inverted-colors",
	"pointer-none", "pointer-coarse", "pointer-fine", "any-pointer-none",
	"any-pointer-coarse", "any-pointer-fine", "noscript",
}

// arbitraryVariantOrder sorts arbitrary variants after every named one.
const arbitraryVariantOrder = 99

var variantOrderIndex = func() map[string]int {
	m := make(map[string]int, len(variantRegistration))
	for i, n := range variantRegistration {
		m[n] = i + 1
	}
	return m
}()

func orderRank(name, sub string) string {
	return fmt.Sprintf("%02d:%s", variantOrderIndex[name], sub)
}

type staticDef struct {
	sels []string
	at   string
	comp int
	hook bool
}

func pseudoDef(sel string) staticDef {
	comp := compSel
	if strings.Contains(sel, "::") {
		comp = 0
	}
	return staticDef{sels: []string{sel}, comp: comp}
}

func mediaDef(params string) staticDef {
	return staticDef{at: "@media " + params, comp: compAt}
}

// staticVariants are the variants that take no value.
var staticVariants = map[string]staticDef{
	"*":                  {sels: []string{":is(& > *)"}},
	"**":                 {sels: []string{":is(& *)"}},
	"first-letter":       pseudoDef("&::first-letter"),
	"first-line":         pseudoDef("&::first-line"),
	"marker":             {sels: []string{"& *::marker", "&::marker", "& *::-webkit-details-marker", "&::-webkit-details-marker"}},
	"selection":          {sels: []string{"& *::selection", "&::selection"}},
	"file":               pseudoDef("&::file-selector-button"),
	"placeholder":        pseudoDef("&::placeholder"),
	"backdrop":           pseudoDef("&::backdrop"),
	"details-content":    pseudoDef("&::details-content"),
	"before":             {sels: []string{"&::before"}, hook: true},
	"after":              {sels: []string{"&::after"}, hook: true},
	"first":              pseudoDef("&:first-child"),
	"last":               pseudoDef("&:last-child"),
	"only":               pseudoDef("&:only-child"),
	"odd":                pseudoDef("&:nth-child(odd)"),
	"even":               pseudoDef("&:nth-child(even)"),
	"first-of-type":      pseudoDef("&:first-of-type"),
	"last-of-type":       pseudoDef("&:last-of-type"),
	"only-of-type":       pseudoDef("&:only-of-type"),
	"visited":            pseudoDef("&:visited"),
	"target":             pseudoDef("&:target"),
	"open":               pseudoDef("&:is([open], :popover-open, :open)"),
	"default":            pseudoDef("&:default"),
	"checked":            pseudoDef("&:checked"),
	"indeterminate":      pseudoDef("&:indeterminate"),
	"placeholder-shown":  pseudoDef("&:placeholder-shown"),
	"autofill":           pseudoDef("&:autofill"),
	"optional":           pseudoDef("&:optional"),
	"required":           pseudoDef("&:required"),
	"valid":              pseudoDef("&:valid"),
	"invalid":            pseudoDef("&:invalid"),
	"user-valid":         pseudoDef("&:user-valid"),
	"user-invalid":       pseudoDef("&:user-invalid"),
	"in-range":           pseudoDef("&:in-range"),
	"out-of-range":       pseudoDef("&:out-of-range"),
	"read-only":          pseudoDef("&:read-only"),
	"empty":              pseudoDef("&:empty"),
	"focus-within":       pseudoDef("&:focus-within"),
	"hover":              {sels: []string{"&:hover"}, at: "@media (hover: hover)", comp: compSel},
	"focus":              pseudoDef("&:focus"),
	"focus-visible":      pseudoDef("&:focus-visible"),
	"active":             pseudoDef("&:active"),
	"enabled":            pseudoDef("&:enabled"),
	"disabled":           pseudoDef("&:disabled"),
	"inert":              pseudoDef("&:is([inert], [inert] *)"),
	"motion-safe":        mediaDef("(prefers-reduced-motion: no-preference)"),
	"motion-reduce":      mediaDef("(prefers-reduced-motion: reduce)"),
	"contrast-more":      mediaDef("(prefers-contrast: more)"),
	"contrast-less":      mediaDef("(prefers-contrast: less)"),
	"portrait":           mediaDef("(orientation: portrait)"),
	"landscape":          mediaDef("(orientation: landscape)"),
	"ltr":                pseudoDef(`&:where(:dir(ltr), [dir="ltr"], [dir="ltr"] *)`),
	"rtl":                pseudoDef(`&:where(:dir(rtl), [dir="rtl"], [dir="rtl"] *)`),
	"starting":           {at: "@starting-style"},
	"print":              mediaDef("print"),
	"forced-colors":      mediaDef("(forced-colors: active)"),
	"inverted-colors":    mediaDef("(inverted-colors: inverted)"),
	"pointer-none":       mediaDef("(pointer: none)"),
	"pointer-coarse":     mediaDef("(pointer: coarse)"),
	"pointer-fine":       mediaDef("(pointer: fine)"),
	"any-pointer-none":   mediaDef("(any-pointer: none)"),
	"any-pointer-coarse": mediaDef("(any-pointer: coarse)"),
	"any-pointer-fine":   mediaDef("(any-pointer: fine)"),
	"noscript":           mediaDef("(scripting: none)"),
}

// extraStaticVariants are Krate additions that Tailwind v4 does not have.
// They sort next to the registered variant named in order.
var extraStaticVariants = map[string]struct {
	def   staticDef
	order string
}{
	"closed":            {pseudoDef("&:not([open])"), "open"},
	"supports-grid":     {staticDef{at: "@supports (display: grid)", comp: compAt}, "supports"},
	"supports-backdrop": {staticDef{at: "@supports (backdrop-filter: blur(0))", comp: compAt}, "supports"},
}

var functionalRoots = map[string]bool{
	"min": true, "max": true, "aria": true, "data": true, "nth": true,
	"nth-last": true, "nth-of-type": true, "nth-last-of-type": true,
	"supports": true, "@": true, "@min": true, "@max": true,
}

// compoundWith is the compounds mask each compound root accepts.
var compoundWith = map[string]int{
	"not": compAt | compSel, "group": compSel, "peer": compSel, "in": compSel, "has": compSel,
}

// splitClass splits a class name into its variant tokens (left to right), the
// base utility, and whether the important flag (`!` prefix or suffix) is set.
// It respects `[...]` brackets (which may contain `:`) and leaves `/` opacity
// modifiers untouched, so `sm:hover:bg-red-500/50` yields
// (["sm","hover"], "bg-red-500/50", false).
func splitClass(cls string) ([]string, string, bool) {
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
	important := false
	if strings.HasPrefix(base, "!") {
		important = true
		base = base[1:]
	} else if strings.HasSuffix(base, "!") && !strings.HasSuffix(base, `\!`) {
		important = true
		base = base[:len(base)-1]
	}
	return variants, base, important
}

// parseVariants splits a full class name into its variant tokens and the base
// utility with any important flag removed.
func parseVariants(cls string) ([]string, string) {
	v, b, _ := splitClass(cls)
	return v, b
}

// topLevelSplit splits s at sep outside of (), [] and quotes.
func topLevelSplit(s string, sep byte) []string {
	var parts []string
	depth := 0
	var quote byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			if depth > 0 {
				depth--
			}
		case c == sep && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// decodeArbitrary turns the `_` of an arbitrary value into a space; `\_` is a
// literal underscore.
func decodeArbitrary(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == '_':
			b.WriteByte('_')
			i++
		case s[i] == '_':
			b.WriteByte(' ')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func validNamedValue(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !alnum && !(i > 0 && (c == '-' || c == '_')) {
			return false
		}
	}
	return !strings.HasSuffix(s, "-") && !strings.HasSuffix(s, "_")
}

// resolveVariant maps a variant token to its CSS form using the theme (for
// breakpoints, containers, dark mode and the class prefix). ok=false means
// "unknown variant" so the caller can skip the class rather than emit it
// unconditionally.
func resolveVariant(v string, theme TailwindTheme) (variant, bool) {
	if cv, ok := customVariant(v, theme); ok {
		return cv, true
	}
	r, ok := parseVariantToken(v, theme)
	if !ok || r.rel {
		return variant{}, false
	}
	return r, true
}

func parseVariantToken(v string, theme TailwindTheme) (variant, bool) {
	if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
		return arbitraryVariant(v[1 : len(v)-1])
	}
	parts := topLevelSplit(v, '/')
	if len(parts) > 2 {
		return variant{}, false
	}
	name, mod := parts[0], ""
	hasMod := len(parts) == 2
	if hasMod {
		mod = parts[1]
		if mod == "" {
			return variant{}, false
		}
	}
	if sv, ok := staticVariant(name, theme); ok {
		if hasMod {
			return variant{}, false
		}
		return sv, true
	}
	for _, c := range rootCandidates(name) {
		switch {
		case compoundWith[c.root] != 0:
			if !c.hasVal || c.val == "" {
				return variant{}, false
			}
			inner := c.val
			if hasMod && (c.root == "not" || c.root == "has" || c.root == "in") {
				inner += "/" + mod
				mod = ""
			}
			iv, ok := parseVariantToken(inner, theme)
			if !ok {
				return variant{}, false
			}
			return compoundVariant(c.root, iv, mod, theme)
		case functionalRoots[c.root]:
			return functionalVariant(c.root, c.val, c.hasVal, mod, hasMod, theme)
		}
	}
	return variant{}, false
}

type rootCand struct {
	root   string
	val    string
	hasVal bool
}

// rootCandidates yields the possible (root, value) splits of a variant name,
// longest root first, mirroring Tailwind's variant parser.
func rootCandidates(name string) []rootCand {
	var out []rootCand
	if isVariantRoot(name) {
		out = append(out, rootCand{root: name})
	}
	for i := strings.LastIndexByte(name, '-'); i > 0; i = strings.LastIndexByte(name[:i], '-') {
		root := name[:i]
		if isVariantRoot(root) {
			val := name[i+1:]
			if val == "" || (root == "@" && name[i] == '-') {
				break
			}
			out = append(out, rootCand{root: root, val: val, hasVal: true})
		}
	}
	if strings.HasPrefix(name, "@") && len(name) > 1 {
		out = append(out, rootCand{root: "@", val: name[1:], hasVal: true})
	}
	return out
}

func isVariantRoot(s string) bool {
	return functionalRoots[s] || compoundWith[s] != 0
}

func staticVariant(name string, theme TailwindTheme) (variant, bool) {
	if bp, ok := theme.Screens[name]; ok && bp != "" && !strings.Contains(bp, "var(") {
		return variant{
			ats:  []string{"@media (min-width: " + bp + ")"},
			rank: orderRank("#min", dimKey(bp, false, true)), comp: compAt,
		}, true
	}
	if name == "dark" {
		return darkVariant(theme), true
	}
	if d, ok := staticVariants[name]; ok {
		return fromStatic(name, "", d), true
	}
	if e, ok := extraStaticVariants[name]; ok {
		return fromStatic(e.order, "~", e.def), true
	}
	return variant{}, false
}

func fromStatic(orderName, sub string, d staticDef) variant {
	v := variant{rank: orderRank(orderName, sub), comp: d.comp, hook: d.hook}
	if len(d.sels) > 0 {
		v.sel = strings.Join(d.sels, ", ")
	}
	if d.at != "" {
		v.ats = []string{d.at}
	}
	return v
}

func darkVariant(theme TailwindTheme) variant {
	rank := orderRank("dark", "")
	switch theme.DarkMode {
	case "class", "selector":
		sel := theme.DarkSelector
		if sel == "" {
			sel = ".dark"
		}
		return variant{sel: "&:where(" + sel + ", " + sel + " *)", rank: rank, comp: compSel}
	}
	return variant{ats: []string{"@media (prefers-color-scheme: dark)"}, rank: rank, comp: compAt}
}

// arbitraryVariant handles [&_p], [&:nth-child(3)], [dir=rtl] and
// [@media(min-width:900px)].
func arbitraryVariant(raw string) (variant, bool) {
	inner := strings.TrimSpace(decodeArbitrary(raw))
	if inner == "" {
		return variant{}, false
	}
	rank := fmt.Sprintf("%02d:%s", arbitraryVariantOrder, inner)
	if inner[0] == '@' {
		if strings.Contains(inner, "&") {
			return variant{}, false
		}
		i := 1
		for i < len(inner) && (inner[i] >= 'a' && inner[i] <= 'z' || inner[i] == '-') {
			i++
		}
		name, params := inner[:i], strings.TrimSpace(inner[i:])
		if len(name) < 2 {
			return variant{}, false
		}
		comp := 0
		switch name {
		case "@media", "@supports", "@container":
			comp = compAt
		}
		at := name
		if params != "" {
			at += " " + params
		}
		return variant{ats: []string{at}, rank: rank, comp: comp}, true
	}
	rel := inner[0] == '>' || inner[0] == '+' || inner[0] == '~'
	if rel {
		inner = inner[:1] + " " + strings.TrimSpace(inner[1:])
	}
	sel := inner
	if !rel && !strings.Contains(inner, "&") {
		sel = "&:is(" + inner + ")"
	}
	comp := compSel
	if strings.Contains(inner, "::") {
		comp = 0
	}
	// Tailwind orders arbitrary variants by their final selector text.
	rank = fmt.Sprintf("%02d:%s", arbitraryVariantOrder, sel)
	return variant{sel: sel, rank: rank, comp: comp, rel: rel}, true
}

// attrValue quotes the value of an attribute selector the way Tailwind does:
// `state=open` -> `state="open"`, `x=y i` -> `x="y" i`.
func attrValue(e string) string {
	k, val, ok := strings.Cut(e, "=")
	if !ok {
		return e
	}
	val = strings.TrimSpace(val)
	if val == "" || val[0] == '\'' || val[0] == '"' {
		return e
	}
	if len(val) > 1 {
		last := val[len(val)-1]
		if val[len(val)-2] == ' ' && (last == 'i' || last == 'I' || last == 's' || last == 'S') {
			return k + `="` + val[:len(val)-2] + `" ` + string(last)
		}
	}
	return k + `="` + val + `"`
}

func valueSub(arbitrary bool, val string) string {
	if arbitrary {
		return "1" + val
	}
	return "0" + val
}

// functionalVariant builds the variants that take a value: min-*, max-*,
// aria-*, data-*, nth-*, supports-* and the container families.
func functionalVariant(root, val string, hasVal bool, mod string, hasMod bool, theme TailwindTheme) (variant, bool) {
	arbitrary := false
	if hasVal {
		if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
			arbitrary = true
			val = strings.TrimSpace(decodeArbitrary(val[1 : len(val)-1]))
			if val == "" {
				return variant{}, false
			}
		} else if !validNamedValue(val) {
			return variant{}, false
		}
	}
	switch root {
	case "min", "max":
		if !hasVal || hasMod {
			return variant{}, false
		}
		w, ok := breakpointValue(val, arbitrary, theme.Screens)
		if !ok {
			return variant{}, false
		}
		if root == "max" {
			return variant{ats: []string{"@media (width < " + w + ")"}, rank: orderRank("#max", dimKey(w, true, !arbitrary)), comp: compAt}, true
		}
		return variant{ats: []string{"@media (min-width: " + w + ")"}, rank: orderRank("#min", dimKey(w, false, !arbitrary)), comp: compAt}, true
	case "@", "@min", "@max":
		if !hasVal {
			return variant{}, false
		}
		w, ok := breakpointValue(val, arbitrary, containerSizes(theme))
		if !ok {
			return variant{}, false
		}
		name := ""
		if hasMod {
			name = mod + " "
		}
		if root == "@max" {
			return variant{ats: []string{"@container " + name + "(width < " + w + ")"}, rank: orderRank("#cmax", dimKey(w, true, false)), comp: compAt}, true
		}
		return variant{ats: []string{"@container " + name + "(width >= " + w + ")"}, rank: orderRank("#cmin", dimKey(w, false, false)), comp: compAt}, true
	}
	if !hasVal || hasMod {
		return variant{}, false
	}
	rank := orderRank(root, valueSub(arbitrary, val))
	switch root {
	case "aria":
		if arbitrary {
			return variant{sel: "&[aria-" + attrValue(val) + "]", rank: rank, comp: compSel}, true
		}
		return variant{sel: `&[aria-` + val + `="true"]`, rank: rank, comp: compSel}, true
	case "data":
		if arbitrary {
			return variant{sel: "&[data-" + attrValue(val) + "]", rank: rank, comp: compSel}, true
		}
		return variant{sel: "&[data-" + val + "]", rank: rank, comp: compSel}, true
	case "nth", "nth-last", "nth-of-type", "nth-last-of-type":
		if !arbitrary && !validNth(val) {
			return variant{}, false
		}
		fn := map[string]string{
			"nth": ":nth-child", "nth-last": ":nth-last-child",
			"nth-of-type": ":nth-of-type", "nth-last-of-type": ":nth-last-of-type",
		}[root]
		return variant{sel: "&" + fn + "(" + val + ")", rank: rank, comp: compSel}, true
	case "supports":
		return supportsVariant(val, rank), true
	}
	return variant{}, false
}

func validNth(s string) bool {
	if s == "odd" || s == "even" {
		return true
	}
	n, err := strconv.Atoi(s)
	return err == nil && n > 0
}

func supportsVariant(val, rank string) variant {
	m := val
	if isFunctionLike(m) {
		for _, kw := range []string{"and", "or", "not"} {
			m = replaceWord(m, kw, " "+kw+" ")
		}
		return variant{ats: []string{"@supports " + m}, rank: rank, comp: compAt}
	}
	if !strings.Contains(m, ":") {
		m += ": var(--tw)"
	}
	if m[0] != '(' || m[len(m)-1] != ')' {
		m = "(" + m + ")"
	}
	return variant{ats: []string{"@supports " + m}, rank: rank, comp: compAt}
}

// isFunctionLike reports whether s starts like `name(` or `(`.
func isFunctionLike(s string) bool {
	i := 0
	for i < len(s) && (s[i] == '-' || s[i] == '_' || s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z' || s[i] >= '0' && s[i] <= '9') {
		i++
	}
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i < len(s) && s[i] == '('
}

func replaceWord(s, word, repl string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], word) && wordBoundary(s, i-1) && wordBoundary(s, i+len(word)) {
			b.WriteString(repl)
			i += len(word)
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func wordBoundary(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	c := s[i]
	return !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9')
}

// containerSizes returns the container-query size scale: the theme's
// containers over the v4 defaults.
func containerSizes(theme TailwindTheme) map[string]string {
	if len(theme.Containers) == 0 {
		return containerScreens
	}
	m := make(map[string]string, len(containerScreens)+len(theme.Containers))
	for k, v := range containerScreens {
		m[k] = v
	}
	for k, v := range theme.Containers {
		m[k] = v
	}
	return m
}

func breakpointValue(val string, arbitrary bool, scale map[string]string) (string, bool) {
	if arbitrary {
		return val, !strings.Contains(val, "var(")
	}
	w, ok := scale[val]
	if !ok || w == "" || strings.Contains(w, "var(") {
		return "", false
	}
	return w, true
}

// containerScreens are the default container-query widths keyed by the
// `@<name>` suffix.
var containerScreens = map[string]string{
	"3xs": "16rem", "2xs": "18rem", "xs": "20rem", "sm": "24rem", "md": "28rem",
	"lg": "32rem", "xl": "36rem", "2xl": "42rem", "3xl": "48rem", "4xl": "56rem",
	"5xl": "64rem", "6xl": "72rem", "7xl": "80rem",
}

// dimKey is the sort sub-key of a breakpoint value. Like Tailwind it groups by
// unit (px before rem) and orders numerically within a unit, ascending or
// descending, so an arbitrary `max-[600px]` lands before the named `max-sm`.
// named marks theme breakpoints: the built-in px defaults stand for Tailwind's
// rem defaults. Values that are not plain lengths sort after the rest.
func dimKey(val string, desc, named bool) string {
	n, unit, ok := splitDim(val)
	if !ok {
		return "1" + val
	}
	if named && unit == "px" && defaultScreenPx[n] {
		n, unit = n/16, "rem"
	}
	if desc {
		n = 1e7 - n
	}
	return "0" + unit + "|" + fmt.Sprintf("%016.4f", math.Max(n, 0))
}

// defaultScreenPx are the px widths of the built-in breakpoints.
var defaultScreenPx = map[float64]bool{640: true, 768: true, 1024: true, 1280: true, 1536: true}

// splitDim splits a plain CSS length into its number and lowercase unit.
func splitDim(s string) (float64, string, bool) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.' || (i == 0 && (s[i] == '-' || s[i] == '+'))) {
		i++
	}
	n, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, "", false
	}
	switch u := strings.ToLower(s[i:]); u {
	case "":
		return n, "px", true
	case "px", "rem", "em":
		return n, u, true
	}
	return 0, "", false
}

// repAmp replaces every "&" in sel with repl. With star set the replacement is
// "*", dropped when it would only prefix a compound selector (`*:hover`).
func repAmp(sel, repl string, star bool) string {
	var b strings.Builder
	for i := 0; i < len(sel); i++ {
		if sel[i] != '&' {
			b.WriteByte(sel[i])
			continue
		}
		if star {
			if i+1 < len(sel) && strings.IndexByte(":[.#", sel[i+1]) >= 0 {
				continue
			}
			b.WriteByte('*')
			continue
		}
		b.WriteString(repl)
	}
	return b.String()
}

// compoundVariant builds not-*, group-*, peer-*, in-* and has-*.
func compoundVariant(root string, inner variant, mod string, theme TailwindTheme) (variant, bool) {
	if len(inner.alts) > 0 || inner.comp&compoundWith[root] == 0 {
		return variant{}, false
	}
	rank := orderRank(root, inner.rank+"|")
	if mod != "" {
		rank += "1" + mod
	} else {
		rank += "0"
	}
	switch root {
	case "not":
		return notVariant(inner, rank)
	case "group", "peer":
		if inner.rel || inner.sel == "" {
			return variant{}, false
		}
		cls := root
		if theme.Prefix != "" {
			cls = theme.Prefix + ":" + root
		}
		cls = "." + EscapeClass(cls)
		if mod != "" {
			cls += `\/` + EscapeClass(mod)
		}
		m := ":where(" + cls + ")"
		k := repAmp(inner.sel, m, false)
		if len(topLevelSplit(k, ',')) > 1 {
			k = ":is(" + k + ")"
		}
		comb := " *"
		if root == "peer" {
			comb = " ~ *"
		}
		return variant{sel: "&:is(" + k + comb + ")", ats: inner.ats, rank: rank, comp: compSel}, true
	case "in":
		if mod != "" || inner.rel || inner.sel == "" {
			return variant{}, false
		}
		return variant{sel: ":where(" + repAmp(inner.sel, "*", true) + ") &", ats: inner.ats, rank: rank, comp: compSel}, true
	case "has":
		if mod != "" || inner.sel == "" {
			return variant{}, false
		}
		return variant{sel: "&:has(" + repAmp(inner.sel, "*", true) + ")", ats: inner.ats, rank: rank, comp: compSel}, true
	}
	return variant{}, false
}

func negateAt(at string) string {
	name, params, _ := strings.Cut(at, " ")
	if rest, ok := strings.CutPrefix(params, "not "); ok {
		return name + " " + rest
	}
	return name + " not " + params
}

func notVariant(inner variant, rank string) (variant, bool) {
	if inner.rel || len(inner.ats) > 1 {
		return variant{}, false
	}
	var selArm, atArm *variant
	if inner.sel != "" {
		if strings.Contains(inner.sel, "::") {
			return variant{}, false
		}
		parts := topLevelSplit(inner.sel, ',')
		for i, p := range parts {
			parts[i] = repAmp(strings.TrimSpace(p), "*", true)
		}
		selArm = &variant{sel: "&:not(" + strings.Join(parts, ", ") + ")", rank: rank, comp: compSel}
	}
	if len(inner.ats) == 1 {
		at := inner.ats[0]
		if at == "@starting-style" || !(strings.HasPrefix(at, "@media") || strings.HasPrefix(at, "@supports") || strings.HasPrefix(at, "@container")) {
			return variant{}, false
		}
		atArm = &variant{ats: []string{negateAt(at)}, rank: rank, comp: compAt}
	}
	switch {
	case selArm != nil && atArm != nil:
		return variant{alts: []variant{*selArm, *atArm}, rank: rank, comp: compSel}, true
	case selArm != nil:
		return *selArm, true
	case atArm != nil:
		return *atArm, true
	}
	return variant{}, false
}

// substSel substitutes the selector built so far into a variant template,
// distributing over a comma-separated list.
func substSel(tpl, cur string) string {
	parts := topLevelSplit(cur, ',')
	if len(parts) == 1 {
		return strings.ReplaceAll(tpl, "&", cur)
	}
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.ReplaceAll(tpl, "&", strings.TrimSpace(p))
	}
	return strings.Join(out, ", ")
}

// applyVariants builds the final selector and the wrapping at-rules for a base
// utility. Variants nest left to right: the leftmost is outermost, as in
// Tailwind v4, so `hover:before:` is `.x:hover::before` and `*:hover:` is
// `:is(.x > *):hover`. baseSel is the escaped class name without the dot.
func applyVariants(baseSel string, vs []variant) (string, []string) {
	cur := "&"
	var ats []string
	for _, v := range vs {
		if v.sel != "" {
			cur = substSel(v.sel, cur)
		}
		ats = append(ats, v.ats...)
	}
	return strings.ReplaceAll(cur, "&", "."+baseSel), ats
}

// usesContentHook reports whether any variant is the ::before/::after
// pseudo-element, which requires the `content` property to render.
func usesContentHook(variants []variant) bool {
	for _, v := range variants {
		if v.hook {
			return true
		}
	}
	return false
}

// wrapAtRules wraps a selector/declaration block in the given at-rules
// (outermost first) so nesting is correct.
func wrapAtRules(sel, decls string, ats []string) string {
	out := sel + "{" + decls + "}"
	for i := len(ats) - 1; i >= 0; i-- {
		out = ats[i] + "{" + out + "}"
	}
	return out
}

// classToRule converts a single Tailwind class to its CSS rule text plus a
// deterministic sort key. An empty text means "unrecognized". A class that
// expands to a union (not-hover) yields several rules in one text.
func classToRule(cls string, theme TailwindTheme) (text, key string) {
	// text-(length:--s) has a ':' inside parentheses; normalize the v4
	// (--var) shorthand to brackets before splitting variants.
	tokens, base, important := splitClass(parenToBracket(cls))
	if theme.Prefix != "" {
		if len(tokens) == 0 || tokens[0] != theme.Prefix {
			return "", ""
		}
		tokens = tokens[1:]
	}
	if base == "" {
		return "", ""
	}

	css := customUtilityCSS(base, theme)
	if css == "" {
		css = generateCSS(base, theme)
	}
	if css == "" && strings.HasPrefix(base, "-") && negatableRoot(base[1:]) {
		css = negateCSS(strings.TrimPrefix(base, "-"), theme)
	}
	if css == "" {
		css, _ = containerUtilityCSS(base)
	}
	if css == "" || (css != containerMarker && !validDeclarationBlock(css)) {
		return "", ""
	}
	if (important || theme.Important == "true") && css != containerMarker {
		css = addImportant(css)
	}

	// Resolve every variant; an unknown one means the class cannot be
	// represented faithfully, so skip it rather than emit it unconditionally.
	combos := [][]variant{nil}
	ranks := make([]string, 0, len(tokens))
	for _, vt := range tokens {
		v, ok := resolveVariant(vt, theme)
		if !ok {
			return "", ""
		}
		ranks = append(ranks, v.rank)
		arms := []variant{v}
		if len(v.alts) > 0 {
			arms = v.alts
		}
		var next [][]variant
		for _, c := range combos {
			for _, a := range arms {
				next = append(next, append(append([]variant(nil), c...), a))
			}
		}
		combos = next
	}

	// The before/after variants inject the content hook so that `content-*`
	// utilities (which set --tw-content) take effect.
	hook := false
	for _, c := range combos {
		hook = hook || usesContentHook(c)
	}
	if hook && !strings.Contains(css, "content:") {
		css = "content: var(--tw-content); " + strings.TrimSpace(css)
	}

	suffix := selectorSuffix(base)
	var rules []string
	for _, c := range combos {
		sel, ats := applyVariants(EscapeClass(cls), c)
		if isSiblingUtility(base) {
			// space-* and divide-* style the children, with zero specificity.
			sel = ":where(" + sel + " > :not(:last-child))"
		} else if suffix != "" {
			sel = appendToParts(sel, suffix)
		}
		if scope := theme.Important; scope != "" && scope != "true" {
			sel = scopeParts(sel, scope)
		}
		if css == containerMarker {
			rules = append(rules, containerRules(sel, ats, theme))
			continue
		}
		rules = append(rules, wrapAtRules(sel, strings.TrimSpace(css), ats))
	}
	return strings.Join(rules, "\n"), ruleSortKey(cls, ranks, css)
}

// appendToParts appends a descendant/sibling suffix to every selector of a list.
func appendToParts(sel, suffix string) string {
	parts := topLevelSplit(sel, ',')
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(suffix, "::") {
			parts[i] = p + suffix
		} else {
			parts[i] = p + " " + suffix
		}
	}
	return strings.Join(parts, ", ")
}

// scopeParts scopes every selector of a list under the configured important
// selector (the v3 `important: '#app'` form).
func scopeParts(sel, scope string) string {
	parts := topLevelSplit(sel, ',')
	for i, p := range parts {
		parts[i] = scope + " " + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}
