package css

import (
	"regexp"
	"sort"
	"strings"
)

// builtinKeyframes are Tailwind v4's keyframes for the built-in animate-*
// utilities. Theme.Keyframes entries override or extend them.
var builtinKeyframes = map[string]string{
	"spin":   "to{transform:rotate(360deg)}",
	"ping":   "75%,100%{transform:scale(2);opacity:0}",
	"pulse":  "50%{opacity:0.5}",
	"bounce": "0%,100%{transform:translateY(-25%);animation-timing-function:cubic-bezier(0.8,0,1,1)}50%{transform:none;animation-timing-function:cubic-bezier(0,0,0.2,1)}",
}

// builtinAnimations are Tailwind v4's built-in animate-* values.
var builtinAnimations = map[string]string{
	"spin":   "spin 1s linear infinite",
	"ping":   "ping 1s cubic-bezier(0, 0, 0.2, 1) infinite",
	"pulse":  "pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite",
	"bounce": "bounce 1s infinite",
}

// animationValue resolves animate-<name> against the theme, then the built-ins.
func animationValue(name string, theme TailwindTheme) (string, bool) {
	if v, ok := theme.Animation[name]; ok && v != "" {
		return v, true
	}
	if v, ok := theme.Animation["animate-"+name]; ok && v != "" {
		return v, true
	}
	v, ok := builtinAnimations[name]
	return v, ok
}

// keyframesBody resolves a keyframes name to its body (without the wrapper).
func keyframesBody(name string, theme TailwindTheme) (string, bool) {
	if v, ok := theme.Keyframes[name]; ok && v != "" {
		return v, true
	}
	v, ok := builtinKeyframes[name]
	return v, ok
}

// twProperty is a custom property that must be registered with @property so
// the --tw-* composition variables do not inherit from ancestors.
type twProperty struct {
	name    string
	initial string
	hasInit bool
	// syntax is the registered syntax; "" means "*". Typed colors and
	// lengths interpolate, so transitions on gradient stops animate.
	syntax string
}

func tp(name string) twProperty        { return twProperty{name: name} }
func tpi(name, init string) twProperty { return twProperty{name: name, initial: init, hasInit: true} }

// tps is tpi with a typed syntax, matching Tailwind's registration.
func tps(name, syntax, init string) twProperty {
	return twProperty{name: name, initial: init, hasInit: true, syntax: syntax}
}

var twProperties = append([]twProperty{
	tp("--tw-backdrop-blur"), tp("--tw-backdrop-brightness"), tp("--tw-backdrop-contrast"),
	tp("--tw-backdrop-grayscale"), tp("--tw-backdrop-hue-rotate"), tp("--tw-backdrop-invert"),
	tp("--tw-backdrop-opacity"), tp("--tw-backdrop-saturate"), tp("--tw-backdrop-sepia"),
	tp("--tw-blur"), tp("--tw-brightness"), tp("--tw-contrast"), tp("--tw-grayscale"),
	tp("--tw-hue-rotate"), tp("--tw-invert"), tp("--tw-opacity"), tp("--tw-saturate"),
	tp("--tw-sepia"), tp("--tw-drop-shadow"), tp("--tw-drop-shadow-color"),
	tps("--tw-drop-shadow-alpha", "<percentage>", "100%"), tp("--tw-drop-shadow-size"),
	tpi("--tw-border-style", "solid"),
	tps("--tw-border-spacing-x", "<length>", "0"), tps("--tw-border-spacing-y", "<length>", "0"),
	tpi("--tw-content", `""`),
	tpi("--tw-divide-x-reverse", "0"), tpi("--tw-divide-y-reverse", "0"),
	tp("--tw-duration"), tp("--tw-ease"), tp("--tw-font-weight"), tp("--tw-leading"), tp("--tw-tracking"),
	tp("--tw-gradient-position"), tps("--tw-gradient-from", "<color>", "#0000"),
	tps("--tw-gradient-via", "<color>", "#0000"), tps("--tw-gradient-to", "<color>", "#0000"),
	tp("--tw-gradient-stops"), tp("--tw-gradient-via-stops"),
	tps("--tw-gradient-from-position", "<length-percentage>", "0%"), tps("--tw-gradient-via-position", "<length-percentage>", "50%"),
	tps("--tw-gradient-to-position", "<length-percentage>", "100%"),
	tpi("--tw-shadow", "0 0 #0000"), tp("--tw-shadow-color"), tps("--tw-shadow-alpha", "<percentage>", "100%"),
	tpi("--tw-inset-shadow", "0 0 #0000"), tp("--tw-inset-shadow-color"),
	tps("--tw-inset-shadow-alpha", "<percentage>", "100%"),
	tp("--tw-ring-color"), tpi("--tw-ring-shadow", "0 0 #0000"),
	tp("--tw-inset-ring-color"), tpi("--tw-inset-ring-shadow", "0 0 #0000"),
	tp("--tw-ring-inset"), tps("--tw-ring-offset-width", "<length>", "0px"), tpi("--tw-ring-offset-color", "#fff"),
	tpi("--tw-ring-offset-shadow", "0 0 #0000"),
	tp("--tw-text-shadow-color"), tps("--tw-text-shadow-alpha", "<percentage>", "100%"),
	tpi("--tw-outline-style", "solid"),
	tp("--tw-rotate-x"), tp("--tw-rotate-y"), tp("--tw-rotate-z"), tp("--tw-skew-x"), tp("--tw-skew-y"),
	tpi("--tw-scale-x", "1"), tpi("--tw-scale-y", "1"), tpi("--tw-scale-z", "1"),
	tpi("--tw-translate-x", "0"), tpi("--tw-translate-y", "0"), tpi("--tw-translate-z", "0"),
	tpi("--tw-scroll-snap-strictness", "proximity"),
	tp("--tw-contain-size"), tp("--tw-contain-layout"), tp("--tw-contain-paint"), tp("--tw-contain-style"),
	tps("--tw-scrollbar-thumb", "<color>", "#0000"), tps("--tw-scrollbar-track", "<color>", "#0000"),
	tpi("--tw-space-x-reverse", "0"), tpi("--tw-space-y-reverse", "0"),
	tp("--tw-pan-x"), tp("--tw-pan-y"), tp("--tw-pinch-zoom"),
	tp("--tw-ordinal"), tp("--tw-slashed-zero"), tp("--tw-numeric-figure"),
	tp("--tw-numeric-spacing"), tp("--tw-numeric-fraction"),
}, maskProperties()...)

var twPropertyByName = func() map[string]twProperty {
	m := make(map[string]twProperty, len(twProperties))
	for _, p := range twProperties {
		m[p.name] = p
	}
	return m
}()

var (
	animationDecl  = regexp.MustCompile(`(?:^|[;{\s])animation(?:-name)?:\s*([^;}]*)`)
	animationIdent = regexp.MustCompile(`-?[A-Za-z_][A-Za-z0-9_-]*`)
)

// twUsage records which keyframes and registered --tw-* properties the emitted
// rules refer to, so KeyframesCSS can emit exactly those.
type twUsage struct {
	keyframes map[string]bool
	props     map[string]bool
}

func (u *twUsage) reset() {
	u.keyframes = map[string]bool{}
	u.props = map[string]bool{}
}

// record scans one emitted rule.
func (u *twUsage) record(rule string, theme TailwindTheme) {
	if strings.Contains(rule, "animation") {
		for _, m := range animationDecl.FindAllStringSubmatch(rule, -1) {
			value := m[1]
			// A live theme animation names its keyframes in the variable.
			if name, ok := strings.CutPrefix(strings.TrimSpace(value), "var(--animate-"); ok {
				if v, ok := animationValue(strings.TrimSuffix(name, ")"), theme); ok {
					value = v
				}
			}
			for _, id := range animationIdent.FindAllString(value, -1) {
				if _, ok := keyframesBody(id, theme); ok {
					u.keyframes[id] = true
				}
			}
		}
	}
	for rest := rule; ; {
		i := strings.Index(rest, "--tw-")
		if i < 0 {
			break
		}
		rest = rest[i:]
		n := len("--tw-")
		for n < len(rest) && (rest[n] >= 'a' && rest[n] <= 'z' || rest[n] >= '0' && rest[n] <= '9' || rest[n] == '-') {
			n++
		}
		name := rest[:n]
		if _, ok := twPropertyByName[name]; ok {
			u.props[name] = true
		}
		rest = rest[n:]
	}
}

// KeyframesCSS returns the at-rules that must accompany the last Generate
// output but live outside the utilities layer: an @keyframes block for every
// keyframes name referenced by an emitted animation, then an @property
// registration for every --tw-* composition variable the utilities use.
// Order is deterministic and each at-rule appears once. It is empty when
// nothing needs them.
func (g *TailwindGenerator) KeyframesCSS() string {
	var b strings.Builder
	names := make([]string, 0, len(g.usage.keyframes))
	for n := range g.usage.keyframes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		body, _ := keyframesBody(n, g.Theme)
		b.WriteString("@keyframes " + n + "{" + body + "}\n")
	}
	props := make([]string, 0, len(g.usage.props))
	for n := range g.usage.props {
		props = append(props, n)
	}
	sort.Strings(props)
	for _, n := range props {
		p := twPropertyByName[n]
		syntax := p.syntax
		if syntax == "" {
			syntax = "*"
		}
		b.WriteString("@property " + n + "{syntax:\"" + syntax + "\";inherits:false")
		if p.hasInit {
			b.WriteString(";initial-value:" + p.initial)
		}
		b.WriteString("}\n")
	}
	return b.String()
}

// propertiesFallbackSupports selects the browsers where @property is missing
// or unreliable (older Safari and Firefox); the condition is Tailwind v4's.
const propertiesFallbackSupports = "@supports ((-webkit-hyphens: none) and (not (margin-trim: block))) or ((-moz-orient: inline) and (not (color:rgb(from red r g b))))"

const propertiesLayerHead = "@layer properties{" + propertiesFallbackSupports + "{"

var registeredTwProperty = regexp.MustCompile(`@property\s+(--tw-[a-z0-9-]+)\s*\{([^}]*)\}`)
var registeredInitial = regexp.MustCompile(`initial-value\s*:\s*([^;]*)`)

// WithPropertiesFallback adds Tailwind v4's `properties` layer to a generated
// sheet: in browsers without working @property, every registered --tw-*
// variable is reset on each element so it does not inherit, the way
// `inherits: false` behaves elsewhere. The layer is declared first, so it is
// the weakest and never overrides a utility. Calling it again on its own
// output rebuilds the block, so the caller can run it after appending blocks.
func WithPropertiesFallback(sheet string) string {
	if i := strings.Index(sheet, propertiesLayerHead); i >= 0 {
		if end := matchCSSBlock(sheet, i+len("@layer properties")); end > 0 {
			rest := sheet[end+1:]
			sheet = sheet[:i] + strings.TrimPrefix(rest, "\n")
		}
	}
	const order = "@layer theme, base, components, utilities;"
	const withProps = "@layer properties, theme, base, components, utilities;"
	var decls strings.Builder
	seen := map[string]bool{}
	for _, m := range registeredTwProperty.FindAllStringSubmatch(sheet, -1) {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		val := "initial"
		if iv := registeredInitial.FindStringSubmatch(m[2]); iv != nil && strings.TrimSpace(iv[1]) != "" {
			val = strings.TrimSpace(iv[1])
		}
		decls.WriteString(m[1] + ":" + val + ";")
	}
	if decls.Len() == 0 {
		return strings.Replace(sheet, withProps, order, 1)
	}
	switch {
	case strings.Contains(sheet, withProps):
	case strings.Contains(sheet, order):
		sheet = strings.Replace(sheet, order, withProps, 1)
	default:
		sheet = "@layer properties;\n" + sheet
	}
	if !strings.HasSuffix(sheet, "\n") {
		sheet += "\n"
	}
	return sheet + propertiesLayerHead + "*,::before,::after,::backdrop{" + decls.String() + "}}}\n"
}
