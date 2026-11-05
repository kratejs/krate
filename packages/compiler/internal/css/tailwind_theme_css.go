package css

import (
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

// ThemeVar is one `--name: value` declaration from an @theme block.
type ThemeVar struct {
	Name  string // without the leading "--"
	Value string
	// Reference vars are visible to utilities but never emitted (@theme reference).
	Reference bool
	// Inline vars are copied into utilities rather than read through var()
	// (@theme inline).
	Inline bool
}

// AtTheme is the parsed content of all `@theme` blocks (Tailwind v4 CSS-first
// configuration), in source order.
type AtTheme struct {
	Vars []ThemeVar
	// Keyframes maps a name to its block body (without the @keyframes wrapper).
	Keyframes map[string]string
	// Warnings are unsupported constructs found while parsing.
	Warnings []string
}

// ParseAtTheme parses every top-level `@theme` block in cssText with real
// brace matching (nested @keyframes and values containing "}" are handled).
func ParseAtTheme(cssText string) *AtTheme {
	at := &AtTheme{}
	for _, r := range topLevelAtRules(cssText) {
		if r.Name == "theme" && r.HasBody {
			at.addBlock(r)
		}
	}
	return at
}

func (at *AtTheme) addBlock(r cssAtRule) {
	reference, inline := false, false
	for _, mod := range strings.Fields(r.Prelude) {
		switch mod {
		case "reference":
			reference = true
		case "inline":
			inline = true
		case "static", "default":
		default:
			at.Warnings = append(at.Warnings, "@theme: unsupported modifier \""+mod+"\" (supported: inline, static, reference, default)")
		}
	}
	decls, nested := splitCSSBody(r.Body)
	for _, d := range decls {
		if !strings.HasPrefix(d.Name, "--") {
			at.Warnings = append(at.Warnings, "@theme: ignored non-variable declaration \""+d.Name+"\"")
			continue
		}
		at.Vars = append(at.Vars, ThemeVar{Name: d.Name[2:], Value: d.Value, Reference: reference, Inline: inline})
	}
	for _, n := range nested {
		if sub, ok := parseNestedAtRule(n); ok && sub.Name == "keyframes" {
			if at.Keyframes == nil {
				at.Keyframes = map[string]string{}
			}
			at.Keyframes[sub.Prelude] = strings.Join(strings.Fields(sub.Body), " ")
			continue
		}
		first := n
		if i := strings.IndexAny(first, "{;"); i > 0 {
			first = first[:i]
		}
		at.Warnings = append(at.Warnings, "@theme: unsupported content \""+strings.TrimSpace(first)+"\" (only --variables and @keyframes are allowed)")
	}
}

func parseNestedAtRule(text string) (cssAtRule, bool) {
	if !strings.HasPrefix(text, "@") {
		return cssAtRule{}, false
	}
	return readAtRule(text, 0)
}

// Merge appends the other theme's variables and keyframes.
func (at *AtTheme) Merge(o *AtTheme) {
	if o == nil {
		return
	}
	at.Vars = append(at.Vars, o.Vars...)
	for k, v := range o.Keyframes {
		if at.Keyframes == nil {
			at.Keyframes = map[string]string{}
		}
		at.Keyframes[k] = v
	}
	at.Warnings = append(at.Warnings, o.Warnings...)
}

// ExtractAtTheme returns the custom properties declared in `@theme` blocks,
// keyed without the leading `--` (e.g. "color-brand-500" -> "#f00").
func ExtractAtTheme(cssText string) map[string]string {
	out := map[string]string{}
	for _, v := range ParseAtTheme(cssText).Vars {
		out[v.Name] = v.Value
	}
	return out
}

// MergeAtTheme applies v4 `@theme` variables to a theme (see AtTheme.Apply).
// Variables are applied in name order.
func MergeAtTheme(theme *TailwindTheme, vars map[string]string) {
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	sort.Strings(names)
	at := &AtTheme{}
	for _, k := range names {
		at.Vars = append(at.Vars, ThemeVar{Name: k, Value: vars[k]})
	}
	at.Apply(theme)
}

// themeNamespaces are the v4 namespaces Krate maps onto utilities.
var unsupportedThemeNamespaces = []string{"ease-", "blur-", "drop-shadow-", "inset-shadow-", "perspective-", "aspect-", "default-"}

// Apply merges the variables into theme, generating utilities for the
// supported namespaces: --color-*, --spacing, --spacing-*, --radius-*,
// --shadow-*, --opacity-*, --leading-*, --tracking-*, --breakpoint-*,
// --container-*, --font-*, --font-weight-*, --text-*(--line-height),
// --animate-*, plus `--ns-*: initial` and `--*: initial` resets.
func (at *AtTheme) Apply(theme *TailwindTheme) {
	theme.ensureMaps()
	textTouched := map[string]bool{}
	textExtra := map[string]map[string]string{}
	for _, v := range at.Vars {
		name, val := v.Name, v.Value
		if val == "initial" && (name == "*" || strings.HasSuffix(name, "-*")) {
			theme.resetNamespace(strings.TrimSuffix(strings.TrimSuffix(name, "*"), "-"))
			continue
		}
		if theme.ThemeVars == nil {
			theme.ThemeVars = map[string]string{}
		}
		theme.ThemeVars[name] = val
		switch {
		case strings.HasPrefix(name, "color-"):
			rest := name[len("color-"):]
			if i := strings.LastIndexByte(rest, '-'); i > 0 && isNumericShade(rest[i+1:]) {
				ensureColor(theme, rest[:i])[rest[i+1:]] = val
			} else {
				ensureColor(theme, rest)["DEFAULT"] = val
				if _, ok := theme.BgColors[rest]; ok {
					theme.BgColors[rest] = val
				}
			}
		case name == "spacing":
			theme.SpacingUnit = val
		case strings.HasPrefix(name, "spacing-"):
			theme.Spacing[name[len("spacing-"):]] = val
		case strings.HasPrefix(name, "radius-"):
			theme.Radii[name[len("radius-"):]] = val
		case strings.HasPrefix(name, "shadow-"):
			theme.Shadows[name[len("shadow-"):]] = val
		case strings.HasPrefix(name, "opacity-"):
			theme.Opacity[name[len("opacity-"):]] = val
		case strings.HasPrefix(name, "leading-"):
			theme.LineHeight[name[len("leading-"):]] = val
		case strings.HasPrefix(name, "tracking-"):
			theme.Tracking[name[len("tracking-"):]] = val
		case strings.HasPrefix(name, "breakpoint-"):
			theme.Screens[name[len("breakpoint-"):]] = val
		case strings.HasPrefix(name, "container-"):
			theme.Containers[name[len("container-"):]] = val
		case strings.HasPrefix(name, "animate-"):
			theme.Animation[name[len("animate-"):]] = val
		case strings.HasPrefix(name, "font-weight-"):
			theme.FontWeights[name[len("font-weight-"):]] = val
		case strings.HasPrefix(name, "font-"):
			theme.FontFamily[name[len("font-"):]] = val
		case strings.HasPrefix(name, "text-"):
			rest := name[len("text-"):]
			if i := strings.Index(rest, "--"); i > 0 {
				if textExtra[rest[:i]] == nil {
					textExtra[rest[:i]] = map[string]string{}
				}
				textExtra[rest[:i]][rest[i+2:]] = val
				textTouched[rest[:i]] = true
			} else {
				theme.TextSizes[rest] = "font-size: " + val + ";"
				textTouched[rest] = true
				// A redefined size drops the old paired line height.
				delete(textExtra, rest)
			}
		default:
			for _, ns := range unsupportedThemeNamespaces {
				if strings.HasPrefix(name, ns) {
					at.Warnings = append(at.Warnings, "@theme: --"+name+" is emitted as a variable but Krate has no utility for the --"+strings.TrimSuffix(ns, "-")+"-* namespace")
					break
				}
			}
		}
	}
	for name := range textTouched {
		block := theme.TextSizes[name]
		if block == "" {
			block = "font-size: inherit;"
		}
		extra := textExtra[name]
		for _, k := range []string{"line-height", "letter-spacing", "font-weight"} {
			if v, ok := extra[k]; ok {
				block = setDecl(block, k, v)
			}
		}
		theme.TextSizes[name] = block
	}
	for k, v := range at.Keyframes {
		theme.Keyframes[k] = v
	}
}

// setDecl sets (or appends) a declaration in a "a: b; c: d;" block.
func setDecl(block, prop, val string) string {
	decls, _ := splitCSSBody(block)
	var parts []string
	found := false
	for _, d := range decls {
		if d.Name == prop {
			d.Value = val
			found = true
		}
		parts = append(parts, d.Name+": "+d.Value+";")
	}
	if !found {
		parts = append(parts, prop+": "+val+";")
	}
	return strings.Join(parts, " ")
}

func (t *TailwindTheme) ensureMaps() {
	for _, m := range []*map[string]string{
		&t.Spacing, &t.Radii, &t.Shadows, &t.Opacity, &t.LineHeight, &t.Tracking,
		&t.Screens, &t.Containers, &t.Animation, &t.FontWeights, &t.FontFamily,
		&t.TextSizes, &t.Keyframes, &t.BgColors,
	} {
		if *m == nil {
			*m = map[string]string{}
		}
	}
	if t.Colors == nil {
		t.Colors = map[string]map[string]string{}
	}
}

// resetNamespace implements `--ns-*: initial` (and `--*: initial` for "").
func (t *TailwindTheme) resetNamespace(ns string) {
	for k := range t.ThemeVars {
		if ns == "" || strings.HasPrefix(k, ns+"-") {
			delete(t.ThemeVars, k)
		}
	}
	clear := func(m map[string]string) {
		for k := range m {
			delete(m, k)
		}
	}
	all := ns == ""
	if all || ns == "color" {
		t.Colors = map[string]map[string]string{}
		for _, k := range []string{"white", "black"} {
			delete(t.BgColors, k)
		}
	}
	if all || ns == "spacing" {
		clear(t.Spacing)
	}
	if all || ns == "radius" {
		clear(t.Radii)
	}
	if all || ns == "shadow" {
		clear(t.Shadows)
	}
	if all || ns == "opacity" {
		clear(t.Opacity)
	}
	if all || ns == "leading" {
		clear(t.LineHeight)
	}
	if all || ns == "tracking" {
		clear(t.Tracking)
	}
	if all || ns == "breakpoint" {
		clear(t.Screens)
	}
	if all || ns == "container" {
		clear(t.Containers)
	}
	if all || ns == "animate" {
		clear(t.Animation)
	}
	if all || ns == "font-weight" {
		clear(t.FontWeights)
	}
	if all || ns == "font" {
		clear(t.FontFamily)
	}
	if all || ns == "text" {
		clear(t.TextSizes)
	}
}

func ensureColor(theme *TailwindTheme, name string) map[string]string {
	if theme.Colors == nil {
		theme.Colors = map[string]map[string]string{}
	}
	if theme.Colors[name] == nil {
		theme.Colors[name] = map[string]string{}
	}
	return theme.Colors[name]
}

func isNumericShade(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// varRefRe matches a custom property read as var(--name) or as the v4 class
// shorthand bg-(--name).
var varRefRe = regexp.MustCompile(`(?:var\(\s*|\()--([A-Za-z0-9_-]+)`)

// varDeclRe matches a custom property being set: `--name:` in CSS or a style
// string, `[--name:value]` in a class, `"--name":` in a style object, and
// `"--name",` or `"--name")` in a setProperty call.
var varDeclRe = regexp.MustCompile("(?:^|[\\s\"'`\\[;{])--([A-Za-z0-9_-]+)(?:\\s*:|[\"'`]\\s*[:,)])")

// collectVars records the custom properties text reads and sets.
func collectVars(text string, refs, decls map[string]bool) {
	if !strings.Contains(text, "--") {
		return
	}
	for _, m := range varRefRe.FindAllStringSubmatch(text, -1) {
		refs[m[1]] = true
	}
	for _, m := range varDeclRe.FindAllStringSubmatch(text, -1) {
		decls[m[1]] = true
	}
}

// themeSkipDirs are never walked for project CSS.
var themeSkipDirs = map[string]bool{".git": true, "node_modules": true, "dist": true, "out": true, ".krate": true}

// ScanAtTheme walks the Tailwind scan directories and merges every `@theme`
// block found in `.css` files into a single variable map.
func ScanAtTheme(root string, opts TailwindOptions) map[string]string {
	out := map[string]string{}
	for _, v := range ScanTailwindDirectives(root, opts).Theme.Vars {
		out[v.Name] = v.Value
	}
	return out
}

// ScanTailwindDirectives collects the Tailwind CSS-first directives from the
// project's .css files under the scan directories.
func ScanTailwindDirectives(root string, opts TailwindOptions) *TailwindDirectives {
	d := NewTailwindDirectives(root)
	for _, dir := range resolveScanDirs(root, opts) {
		_ = fsutil.WalkExt(dir, map[string]bool{".css": true}, themeSkipDirs, func(path string, _ os.FileInfo) error {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			d.Add(string(data), pathDir(path))
			return nil
		})
	}
	return d
}

// GenerateTailwindWithTheme is GenerateTailwindWithOptions with v4 `@theme`
// variables merged into the theme before generation.
func GenerateTailwindWithTheme(root string, cfg *TailwindConfig, opts TailwindOptions, themeVars map[string]string) (string, error) {
	d := NewTailwindDirectives(root)
	names := make([]string, 0, len(themeVars))
	for k := range themeVars {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		d.Theme.Vars = append(d.Theme.Vars, ThemeVar{Name: k, Value: themeVars[k]})
	}
	res, err := GenerateTailwindDirectives(root, cfg, opts, d)
	return res.CSS, err
}

// themeLayerVars returns the variables for the `theme` layer: every
// user-defined @theme variable (except `reference` ones) plus any default
// theme variable referenced as var(--name) in the project, in a stable order.
// Tailwind v4 emits only used variables unless `static`; emitting all
// user-defined ones is a documented superset.
func themeLayerVars(theme *TailwindTheme, at *AtTheme, refs map[string]bool) []ThemeVar {
	var out []ThemeVar
	have := map[string]int{}
	for _, v := range at.Vars {
		if v.Reference || (v.Value == "initial" && strings.HasSuffix(v.Name, "*")) {
			continue
		}
		if i, ok := have[v.Name]; ok {
			out[i] = v
			continue
		}
		have[v.Name] = len(out)
		out = append(out, v)
	}
	names := make([]string, 0, len(refs))
	for n := range refs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if _, ok := have[n]; ok {
			continue
		}
		if val, ok := themeVarValue(theme, n); ok {
			have[n] = len(out)
			out = append(out, ThemeVar{Name: n, Value: val})
		}
	}
	return out
}

// LiveVars returns the theme variables utilities must read through var(): every
// @theme variable, plus any default variable the project sets outside @theme,
// since Tailwind v4 utilities react to both at runtime. Other defaults stay
// inlined, which keeps the sheet free of a :root variable block. `@theme inline`
// and `@theme reference` variables are never live.
func (d *TailwindDirectives) LiveVars(theme *TailwindTheme) map[string]bool {
	live := map[string]bool{}
	for n := range d.VarDecls {
		// v4 inlines these, and --spacing-<n> is not a default v4 variable.
		if strings.HasPrefix(n, "shadow-") || strings.HasPrefix(n, "breakpoint-") || strings.HasPrefix(n, "spacing-") {
			continue
		}
		if _, ok := themeVarValue(theme, n); ok {
			live[n] = true
		}
	}
	for _, v := range d.Theme.Vars {
		if v.Inline || v.Reference || v.Value == "initial" {
			delete(live, v.Name)
		} else {
			live[v.Name] = true
		}
	}
	return live
}

// themeRef returns var(--name) for a live theme variable, otherwise inlined.
func (t TailwindTheme) themeRef(name, inlined string) string {
	if t.LiveVars[name] {
		return "var(--" + name + ")"
	}
	return inlined
}

// themeVarValue resolves a v4 theme variable name (no leading "--") against
// the theme maps, then against the raw `@theme` variables.
func themeVarValue(theme *TailwindTheme, name string) (string, bool) {
	if v, ok := mappedThemeVar(theme, name); ok {
		return v, true
	}
	v, ok := theme.ThemeVars[name]
	return v, ok && v != ""
}

func mappedThemeVar(theme *TailwindTheme, name string) (string, bool) {
	get := func(m map[string]string, key string) (string, bool) {
		v, ok := m[key]
		return v, ok && v != ""
	}
	switch {
	case name == "spacing":
		if theme.SpacingUnit != "" {
			return theme.SpacingUnit, true
		}
		return "0.25rem", true
	case strings.HasPrefix(name, "color-"):
		rest := name[len("color-"):]
		if v, ok := theme.BgColors[rest]; ok && (rest == "white" || rest == "black") {
			return v, true
		}
		if i := strings.LastIndexByte(rest, '-'); i > 0 {
			if v, ok := get(theme.Colors[rest[:i]], rest[i+1:]); ok {
				return v, true
			}
		}
		return get(theme.Colors[rest], "DEFAULT")
	case strings.HasPrefix(name, "spacing-"):
		return get(theme.Spacing, name[len("spacing-"):])
	case strings.HasPrefix(name, "radius-"):
		return get(theme.Radii, name[len("radius-"):])
	case strings.HasPrefix(name, "shadow-"):
		return get(theme.Shadows, name[len("shadow-"):])
	case strings.HasPrefix(name, "leading-"):
		return get(theme.LineHeight, name[len("leading-"):])
	case strings.HasPrefix(name, "tracking-"):
		if v, ok := get(theme.Tracking, name[len("tracking-"):]); ok {
			return v, true
		}
		return get(defaultTracking, name[len("tracking-"):])
	case strings.HasPrefix(name, "breakpoint-"):
		return get(theme.Screens, name[len("breakpoint-"):])
	case strings.HasPrefix(name, "container-"):
		return containerSize(name[len("container-"):], *theme)
	case strings.HasPrefix(name, "animate-"):
		return animationValue(name[len("animate-"):], *theme)
	case strings.HasPrefix(name, "font-weight-"):
		return get(theme.FontWeights, name[len("font-weight-"):])
	case strings.HasPrefix(name, "font-"):
		return get(theme.FontFamily, name[len("font-"):])
	case strings.HasPrefix(name, "text-"):
		rest := name[len("text-"):]
		prop := "font-size"
		if i := strings.Index(rest, "--"); i > 0 {
			prop, rest = rest[i+2:], rest[:i]
		}
		block, ok := get(theme.TextSizes, rest)
		if !ok {
			return "", false
		}
		decls, _ := splitCSSBody(block)
		for _, d := range decls {
			if d.Name == prop {
				return d.Value, true
			}
		}
		if prop == "font-size" && len(decls) == 0 {
			return block, true
		}
	}
	return "", false
}
