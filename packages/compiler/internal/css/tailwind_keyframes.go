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
}

func tp(name string) twProperty        { return twProperty{name: name} }
func tpi(name, init string) twProperty { return twProperty{name: name, initial: init, hasInit: true} }

var twProperties = append([]twProperty{
	tp("--tw-backdrop-blur"), tp("--tw-backdrop-brightness"), tp("--tw-backdrop-contrast"),
	tp("--tw-backdrop-grayscale"), tp("--tw-backdrop-hue-rotate"), tp("--tw-backdrop-invert"),
	tp("--tw-backdrop-opacity"), tp("--tw-backdrop-saturate"), tp("--tw-backdrop-sepia"),
	tp("--tw-blur"), tp("--tw-brightness"), tp("--tw-contrast"), tp("--tw-grayscale"),
	tp("--tw-hue-rotate"), tp("--tw-invert"), tp("--tw-opacity"), tp("--tw-saturate"),
	tp("--tw-sepia"), tp("--tw-drop-shadow"), tp("--tw-drop-shadow-color"),
	tpi("--tw-drop-shadow-alpha", "100%"), tp("--tw-drop-shadow-size"),
	tpi("--tw-border-style", "solid"),
	tpi("--tw-border-spacing-x", "0"), tpi("--tw-border-spacing-y", "0"),
	tpi("--tw-content", `""`),
	tpi("--tw-divide-x-reverse", "0"), tpi("--tw-divide-y-reverse", "0"),
	tp("--tw-duration"), tp("--tw-ease"), tp("--tw-font-weight"), tp("--tw-leading"), tp("--tw-tracking"),
	tp("--tw-gradient-position"), tpi("--tw-gradient-from", "#0000"),
	tpi("--tw-gradient-via", "#0000"), tpi("--tw-gradient-to", "#0000"),
	tp("--tw-gradient-stops"), tp("--tw-gradient-via-stops"),
	tpi("--tw-gradient-from-position", "0%"), tpi("--tw-gradient-via-position", "50%"),
	tpi("--tw-gradient-to-position", "100%"),
	tpi("--tw-shadow", "0 0 #0000"), tp("--tw-shadow-color"), tpi("--tw-shadow-alpha", "100%"),
	tpi("--tw-inset-shadow", "0 0 #0000"), tp("--tw-inset-shadow-color"),
	tpi("--tw-inset-shadow-alpha", "100%"),
	tp("--tw-ring-color"), tpi("--tw-ring-shadow", "0 0 #0000"),
	tp("--tw-inset-ring-color"), tpi("--tw-inset-ring-shadow", "0 0 #0000"),
	tp("--tw-ring-inset"), tpi("--tw-ring-offset-width", "0px"), tpi("--tw-ring-offset-color", "#fff"),
	tpi("--tw-ring-offset-shadow", "0 0 #0000"),
	tp("--tw-text-shadow-color"), tpi("--tw-text-shadow-alpha", "100%"),
	tpi("--tw-outline-style", "solid"),
	tp("--tw-rotate-x"), tp("--tw-rotate-y"), tp("--tw-rotate-z"), tp("--tw-skew-x"), tp("--tw-skew-y"),
	tpi("--tw-scale-x", "1"), tpi("--tw-scale-y", "1"), tpi("--tw-scale-z", "1"),
	tpi("--tw-translate-x", "0"), tpi("--tw-translate-y", "0"), tpi("--tw-translate-z", "0"),
	tpi("--tw-scroll-snap-strictness", "proximity"),
	tp("--tw-contain-size"), tp("--tw-contain-layout"), tp("--tw-contain-paint"), tp("--tw-contain-style"),
	tpi("--tw-scrollbar-thumb", "#0000"), tpi("--tw-scrollbar-track", "#0000"),
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
	twVarRe        = regexp.MustCompile(`--tw-[a-z0-9-]+`)
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
	for _, m := range animationDecl.FindAllStringSubmatch(rule, -1) {
		for _, id := range animationIdent.FindAllString(m[1], -1) {
			if _, ok := keyframesBody(id, theme); ok {
				u.keyframes[id] = true
			}
		}
	}
	if strings.Contains(rule, "--tw-") {
		for _, name := range twVarRe.FindAllString(rule, -1) {
			if _, ok := twPropertyByName[name]; ok {
				u.props[name] = true
			}
		}
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
		b.WriteString("@property " + n + "{syntax:\"*\";inherits:false")
		if p.hasInit {
			b.WriteString(";initial-value:" + p.initial)
		}
		b.WriteString("}\n")
	}
	return b.String()
}
