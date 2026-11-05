package syntaxhighlight

import (
	"fmt"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
)

// Default theme names. A plain string theme in the config applies to both
// modes; the defaults give a light page a light block and a dark page a dark
// one.
const (
	DefaultLightTheme = "github"
	DefaultDarkTheme  = "krate-dark"
)

// krate-dark keeps the github-dark token colours on the neutral surface of the
// component tokens (--krate-card-bg), so a highlighted block matches the cards
// around it. github-dark itself stays available under its own name.
func init() {
	base := styles.Get("github-dark").Get(chroma.Background)
	surface, text := chroma.MustParseColour("#171717"), chroma.MustParseColour("#e5e5e5")
	s, err := styles.Get("github-dark").Builder().
		Transform(func(e chroma.StyleEntry) chroma.StyleEntry {
			if e.Colour == base.Colour {
				e.Colour = text
			}
			if e.Background == base.Background {
				e.Background = surface
			}
			return e
		}).
		Add(chroma.LineHighlight, "bg:#262626").
		Build()
	if err != nil {
		panic(err)
	}
	s.Name = DefaultDarkTheme
	styles.Register(s)
}

// Theme names the chroma style used in each colour mode.
type Theme struct {
	Light string
	Dark  string
}

// ResolveTheme builds the Theme for the configured names. Empty names take the
// defaults. Names chroma does not know are returned in unknown and replaced by
// the default for their side, so the caller can report them and still build.
func ResolveTheme(light, dark string) (t Theme, unknown []string) {
	t = Theme{Light: strings.TrimSpace(light), Dark: strings.TrimSpace(dark)}
	if t.Light == "" {
		t.Light = DefaultLightTheme
	}
	if t.Dark == "" {
		t.Dark = DefaultDarkTheme
	}
	note := func(name string) {
		for _, u := range unknown {
			if u == name {
				return
			}
		}
		unknown = append(unknown, name)
	}
	if _, ok := styles.Registry[t.Light]; !ok {
		note(t.Light)
		t.Light = DefaultLightTheme
	}
	if _, ok := styles.Registry[t.Dark]; !ok {
		note(t.Dark)
		t.Dark = DefaultDarkTheme
	}
	return t, unknown
}

// SimilarThemes returns up to n theme names that look like name, for hints.
func SimilarThemes(name string, n int) []string {
	name = strings.ToLower(name)
	var out []string
	for _, cand := range styles.Names() {
		if strings.Contains(cand, name) || strings.Contains(name, cand) {
			out = append(out, cand)
		}
	}
	if len(out) == 0 {
		out = []string{DefaultLightTheme, DefaultDarkTheme}
	}
	sort.Strings(out)
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Fallback text colours for themes that do not set one, chosen by whether the
// background is light or dark.
const (
	fallbackLightFG = "#262626"
	fallbackDarkFG  = "#e5e5e5"
)

// decl is one CSS declaration that may differ between the two modes. An empty
// value means the theme leaves the property unset in that mode.
type decl struct {
	prop       string
	light, drk string
}

type modeEntry struct {
	color, bg               string
	bold, italic, underline bool
}

// skipClass lists chroma classes that need no token rule: the block and line
// wrappers are styled separately, and line numbers and tables are never emitted.
var skipClass = map[string]bool{
	"bg": true, "chroma": true, "ln": true, "lnt": true, "lntd": true, "lntable": true,
	"lnlinks": true, "line": true, "cl": true,
}

func styleFor(name string) *chroma.Style {
	if s, ok := styles.Registry[name]; ok {
		return s
	}
	return styles.Registry[DefaultLightTheme]
}

// classEntries returns the effective entry of every styled token class for a
// style, using the same rules as chroma's HTML formatter (entries equal to the
// background entry are dropped, so they inherit the block's text colour).
func classEntries(s *chroma.Style) map[string]modeEntry {
	bg := s.Get(chroma.Background)
	out := map[string]modeEntry{}
	for tt, class := range chroma.StandardTypes {
		if class == "" || skipClass[class] {
			continue
		}
		e := s.Get(tt).Sub(bg)
		if e.IsZero() {
			continue
		}
		m := modeEntry{
			bold:      e.Bold == chroma.Yes,
			italic:    e.Italic == chroma.Yes,
			underline: e.Underline == chroma.Yes,
		}
		if e.Colour.IsSet() {
			m.color = e.Colour.String()
		}
		if e.Background.IsSet() {
			m.bg = e.Background.String()
		}
		if m == (modeEntry{}) {
			continue
		}
		out[class] = m
	}
	return out
}

// baseColors returns the block background and text colours of a style.
func baseColors(s *chroma.Style) (bg, fg string) {
	b := s.Get(chroma.Background)
	if b.Background.IsSet() {
		bg = b.Background.String()
	}
	if b.Colour.IsSet() {
		fg = b.Colour.String()
	} else if t := s.Get(chroma.Text); t.Colour.IsSet() {
		fg = t.Colour.String()
	}
	if bg == "" {
		bg = "#ffffff"
	}
	if fg == "" {
		if isLightColour(bg) {
			fg = fallbackLightFG
		} else {
			fg = fallbackDarkFG
		}
	}
	return bg, fg
}

func isLightColour(hex string) bool {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return true
	}
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b); err != nil {
		return true
	}
	return (299*r+587*g+114*b)/1000 > 140
}

// unsetFallback is the value a rule uses when a mode leaves a property unset:
// the property then behaves as if the theme never styled that token.
var unsetFallback = map[string]string{
	"color":            "inherit",
	"background-color": "transparent",
	"font-weight":      "inherit",
	"font-style":       "inherit",
	"text-decoration":  "none",
}

type themeBuilder struct {
	vars     map[string]int // "prop|light|dark" -> index
	order    []decl
	varNames []string
}

func (tb *themeBuilder) varFor(d decl, hint string) string {
	key := d.prop + "|" + d.light + "|" + d.drk
	if i, ok := tb.vars[key]; ok {
		return tb.varNames[i]
	}
	name := "--chroma-" + hint
	for _, existing := range tb.varNames {
		if existing == name {
			name = fmt.Sprintf("%s-%d", name, len(tb.varNames))
			break
		}
	}
	tb.vars[key] = len(tb.order)
	tb.order = append(tb.order, d)
	tb.varNames = append(tb.varNames, name)
	return name
}

// value renders one declaration as a rule fragment: a literal when both modes
// agree, otherwise a custom property.
func (tb *themeBuilder) value(d decl, hint string) string {
	if d.light == d.drk {
		return d.light
	}
	name := tb.varFor(d, hint)
	if fb, ok := unsetFallback[d.prop]; ok && (d.light == "" || d.drk == "") {
		return "var(" + name + "," + fb + ")"
	}
	return "var(" + name + ")"
}

// ThemeCSS returns one stylesheet for both modes: the token rules read custom
// properties, and the light and dark blocks set them under the same selectors
// as the component tokens (system preference, then an explicit data-theme).
// The HTML a highlighter produces is the same for every theme, so one span per
// token serves both modes.
func ThemeCSS(t Theme) string {
	light, dark := styleFor(t.Light), styleFor(t.Dark)
	lEntries, dEntries := classEntries(light), classEntries(dark)
	lbg, lfg := baseColors(light)
	dbg, dfg := baseColors(dark)

	tb := &themeBuilder{vars: map[string]int{}}

	var classes []string
	seen := map[string]bool{}
	for c := range lEntries {
		classes = append(classes, c)
		seen[c] = true
	}
	for c := range dEntries {
		if !seen[c] {
			classes = append(classes, c)
		}
	}
	rank := map[string]int{}
	for tt, class := range chroma.StandardTypes {
		if r, ok := rank[class]; !ok || int(tt) < r {
			rank[class] = int(tt)
		}
	}
	sort.Slice(classes, func(i, j int) bool { return rank[classes[i]] < rank[classes[j]] })

	// The block itself. These are always custom properties, even when both
	// modes agree: code.css reads --chroma-bg / --chroma-fg for the wrapper too.
	bgv := "var(" + tb.varFor(decl{"background-color", lbg, dbg}, "bg") + ")"
	fgv := "var(" + tb.varFor(decl{"color", lfg, dfg}, "fg") + ")"

	type rule struct {
		sig     string
		classes []string
	}
	var rules []*rule
	bySig := map[string]*rule{}
	for _, c := range classes {
		l, d := lEntries[c], dEntries[c]
		var parts []string
		add := func(prop, lv, dv string) {
			if lv == "" && dv == "" {
				return
			}
			parts = append(parts, prop+":"+tb.value(decl{prop, lv, dv}, c))
		}
		add("color", l.color, d.color)
		add("background-color", l.bg, d.bg)
		add("font-weight", boolVal(l.bold, "bold"), boolVal(d.bold, "bold"))
		add("font-style", boolVal(l.italic, "italic"), boolVal(d.italic, "italic"))
		add("text-decoration", boolVal(l.underline, "underline"), boolVal(d.underline, "underline"))
		sig := strings.Join(parts, ";")
		if sig == "" {
			continue
		}
		r, ok := bySig[sig]
		if !ok {
			r = &rule{sig: sig}
			bySig[sig] = r
			rules = append(rules, r)
		}
		r.classes = append(r.classes, c)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, ".chroma{background-color:var(--krate-code-bg,%s);color:var(--krate-code-text,%s);-webkit-text-size-adjust:none}\n", bgv, fgv)
	sb.WriteString(".chroma .line{display:flex}\n")
	for _, r := range rules {
		if len(r.classes) == 1 {
			fmt.Fprintf(&sb, ".chroma .%s{%s}\n", r.classes[0], r.sig)
			continue
		}
		fmt.Fprintf(&sb, ".chroma :is(.%s){%s}\n", strings.Join(r.classes, ",."), r.sig)
	}

	if len(tb.order) > 0 {
		fmt.Fprintf(&sb, ":where(:root){%s}\n", tb.block(false))
		if tb.hasModeVars() {
			darkBlock := tb.block(true)
			fmt.Fprintf(&sb, "@media (prefers-color-scheme:dark){:where(:root){%s}}\n", darkBlock)
			fmt.Fprintf(&sb, ":where(:root[data-theme='dark']){%s}\n", darkBlock)
			fmt.Fprintf(&sb, ":where(:root[data-theme='light']){%s}\n", tb.block(false))
		}
	}
	return sb.String()
}

func boolVal(on bool, v string) string {
	if on {
		return v
	}
	return ""
}

// hasModeVars reports whether any custom property differs between the modes.
func (tb *themeBuilder) hasModeVars() bool {
	for _, d := range tb.order {
		if d.light != d.drk {
			return true
		}
	}
	return false
}

// block renders the custom property declarations for one mode. A property the
// theme leaves unset in this mode is reset with `initial` so the rule's
// fallback applies. The dark block lists only the properties that change.
func (tb *themeBuilder) block(dark bool) string {
	var parts []string
	for i, d := range tb.order {
		v := d.light
		if dark {
			if d.light == d.drk {
				continue
			}
			v = d.drk
		}
		if v == "" {
			v = "initial"
		}
		parts = append(parts, tb.varNames[i]+":"+v)
	}
	return strings.Join(parts, ";")
}
