package css

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/jseval"
)

// TailwindConfig represents the tailwind.config.ts/js parsed values.
type TailwindConfig struct {
	Theme    TailwindTheme `json:"theme"`
	DarkMode string        `json:"darkMode"`
	// Warnings lists config constructs Krate cannot honour (unsupported keys,
	// values the static parser cannot evaluate). The caller prints them.
	Warnings []string `json:"warnings"`
	// Extend holds theme.extend values from the evaluated config; they merge
	// onto the built-in scales instead of replacing them.
	Extend TailwindTheme `json:"extend"`
}

// TailwindOptions carries site config that influences generation (scan roots,
// preflight, strict diagnostics).
type TailwindOptions struct {
	// ScanDirs are directories (relative to root) to scan for classes.
	ScanDirs []string
	// ContentOverrides, when non-empty, replaces ScanDirs (Tailwind `content`).
	ContentOverrides []string
	// Preflight enables the base reset.
	Preflight bool
	// Strict collects unrecognized classes.
	Strict bool
	// DarkMode overrides the config's dark-mode strategy.
	DarkMode string
	// ExecuteConfig forces in-process config evaluation (esbuild + QuickJS)
	// instead of the static parser.
	ExecuteConfig bool
}

// configFileNames are the supported tailwind config filenames.
var configFileNames = []string{
	"tailwind.config.ts", "tailwind.config.js", "tailwind.config.mjs",
}

// findTailwindConfig returns the first existing config path under root.
func findTailwindConfig(root string) string {
	for _, c := range configFileNames {
		p := filepath.Join(root, c)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// LoadTailwindConfig loads a tailwind.config file. It prefers a static parse
// (no JS runtime required); when `execute` is true or the static parse yields no
// theme, it evaluates the config in-process (esbuild + QuickJS). On any failure
// it returns defaults (a diagnostic is surfaced by the caller).
func LoadTailwindConfig(root string) *TailwindConfig {
	return LoadTailwindConfigWithOptions(root, TailwindOptions{})
}

// LoadTailwindConfigWithOptions is LoadTailwindConfig with explicit options.
func LoadTailwindConfigWithOptions(root string, opts TailwindOptions) *TailwindConfig {
	cfg := &TailwindConfig{Theme: DefaultTailwindTheme()}

	configPath := findTailwindConfig(root)
	if configPath == "" {
		if opts.DarkMode != "" {
			cfg.Theme.DarkMode = opts.DarkMode
		}
		return cfg
	}

	// Prefer the static parse so builds need no Node.
	if !opts.ExecuteConfig {
		if parsed, ok := ParseTailwindConfigStatic(configPath); ok {
			mergeTailwindConfig(cfg, parsed)
			if opts.DarkMode != "" {
				cfg.Theme.DarkMode = opts.DarkMode
			}
			return cfg
		}
	}

	if parsed, ok := executeTailwindConfig(root, configPath); ok {
		mergeTailwindConfig(cfg, parsed)
	} else if parsed, ok := ParseTailwindConfigStatic(configPath); ok {
		mergeTailwindConfig(cfg, parsed)
	}
	if opts.DarkMode != "" {
		cfg.Theme.DarkMode = opts.DarkMode
	}
	return cfg
}

// executeTailwindConfig evaluates the config in-process via jseval (esbuild
// bundle + embedded QuickJS) - no Node subprocess. It returns ok=false when the
// config can't be bundled or executed, so the caller falls back to the static
// parser.
func executeTailwindConfig(root, configPath string) (*TailwindConfig, bool) {
	rel, err := filepath.Rel(root, configPath)
	if err != nil {
		return nil, false
	}
	spec := "./" + filepath.ToSlash(rel)
	bootstrap := "import cfg from " + strconv.Quote(spec) + ";\n" + execConfigJS +
		"\nglobalThis." + jseval.ResultGlobal + " = __krateNormalizeConfig(cfg);\n"

	data, err := jseval.Eval(jseval.Options{
		Source:     bootstrap,
		ResolveDir: root,
		Sourcefile: "krate-tailwind-bootstrap.ts",
	})
	if err != nil {
		return nil, false
	}

	var parsed TailwindConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, false
	}
	return &parsed, true
}

// mergeTailwindConfig folds a parsed config into cfg, applying `extend`
// semantics: top-level theme keys replace defaults, `extend` keys merge in.
// (The static parser already resolves extend/override into concrete maps, so
// here we only copy non-empty maps and the dark-mode strategy.)
func mergeTailwindConfig(cfg *TailwindConfig, parsed *TailwindConfig) {
	if parsed == nil {
		return
	}
	t := parsed.Theme
	if len(t.Spacing) > 0 {
		cfg.Theme.Spacing = t.Spacing
	}
	if len(t.Sizing) > 0 {
		cfg.Theme.Sizing = t.Sizing
	}
	if len(t.MaxWidth) > 0 {
		cfg.Theme.MaxWidth = t.MaxWidth
	}
	if len(t.MinWidth) > 0 {
		cfg.Theme.MinWidth = t.MinWidth
	}
	if len(t.LineHeight) > 0 {
		cfg.Theme.LineHeight = t.LineHeight
	}
	if len(t.Opacity) > 0 {
		cfg.Theme.Opacity = t.Opacity
	}
	if len(t.Screens) > 0 {
		cfg.Theme.Screens = t.Screens
	}
	if len(t.Colors) > 0 {
		cfg.Theme.Colors = t.Colors
	}
	if len(t.TextSizes) > 0 {
		cfg.Theme.TextSizes = t.TextSizes
	}
	if len(t.FontWeights) > 0 {
		cfg.Theme.FontWeights = t.FontWeights
	}
	if len(t.FontFamily) > 0 {
		cfg.Theme.FontFamily = t.FontFamily
	}
	if len(t.Radii) > 0 {
		cfg.Theme.Radii = t.Radii
	}
	if len(t.Shadows) > 0 {
		cfg.Theme.Shadows = t.Shadows
	}
	if len(t.BgColors) > 0 {
		cfg.Theme.BgColors = t.BgColors
	}
	if len(t.Animation) > 0 {
		cfg.Theme.Animation = t.Animation
	}
	if len(t.Keyframes) > 0 {
		cfg.Theme.Keyframes = t.Keyframes
	}
	if len(t.Tracking) > 0 {
		cfg.Theme.Tracking = t.Tracking
	}
	if len(t.Containers) > 0 {
		cfg.Theme.Containers = t.Containers
	}
	if t.SpacingUnit != "" {
		cfg.Theme.SpacingUnit = t.SpacingUnit
	}
	if t.DarkSelector != "" {
		cfg.Theme.DarkSelector = t.DarkSelector
	}
	if t.Prefix != "" {
		cfg.Theme.Prefix = t.Prefix
	}
	if t.Important != "" {
		cfg.Theme.Important = t.Important
	}
	cfg.Warnings = append(cfg.Warnings, parsed.Warnings...)
	mergeExtend(&cfg.Theme, &parsed.Extend)
	if t.DarkMode != "" {
		cfg.Theme.DarkMode = t.DarkMode
	}
	if parsed.DarkMode != "" {
		cfg.Theme.DarkMode = parsed.DarkMode
	}
}

// mergeExtend merges theme.extend maps onto dst.
func mergeExtend(dst, ext *TailwindTheme) {
	merge := func(d *map[string]string, s map[string]string) {
		if len(s) == 0 {
			return
		}
		if *d == nil {
			*d = map[string]string{}
		}
		for k, v := range s {
			(*d)[k] = v
		}
	}
	merge(&dst.Spacing, ext.Spacing)
	merge(&dst.Sizing, ext.Sizing)
	merge(&dst.MaxWidth, ext.MaxWidth)
	merge(&dst.MinWidth, ext.MinWidth)
	merge(&dst.LineHeight, ext.LineHeight)
	merge(&dst.Opacity, ext.Opacity)
	merge(&dst.Screens, ext.Screens)
	merge(&dst.TextSizes, ext.TextSizes)
	merge(&dst.FontWeights, ext.FontWeights)
	merge(&dst.FontFamily, ext.FontFamily)
	merge(&dst.Radii, ext.Radii)
	merge(&dst.Shadows, ext.Shadows)
	merge(&dst.Animation, ext.Animation)
	merge(&dst.Keyframes, ext.Keyframes)
	merge(&dst.Tracking, ext.Tracking)
	if len(ext.Colors) > 0 {
		if dst.Colors == nil {
			dst.Colors = map[string]map[string]string{}
		}
		for k, v := range ext.Colors {
			dst.Colors[k] = v
		}
	}
}

// MergeConfig merges a TailwindConfig into the generator's theme.
func (g *TailwindGenerator) MergeConfig(cfg *TailwindConfig) {
	if cfg == nil {
		return
	}
	merged := &TailwindConfig{Theme: g.Theme}
	mergeTailwindConfig(merged, cfg)
	g.Theme = merged.Theme
}

// GenerateTailwind runs the full Tailwind pipeline: scan classes -> generate CSS.
func GenerateTailwind(root string, cfg *TailwindConfig) (string, error) {
	return GenerateTailwindWithOptions(root, cfg, TailwindOptions{})
}

// GenerateTailwindWithOptions is GenerateTailwind with explicit options.
func GenerateTailwindWithOptions(root string, cfg *TailwindConfig, opts TailwindOptions) (string, error) {
	res, err := GenerateTailwindDirectives(root, cfg, opts, nil)
	return res.CSS, err
}

// TailwindResult is the output of a Tailwind generation run.
type TailwindResult struct {
	// CSS is the layered stylesheet (empty when nothing was generated).
	CSS string
	// Unknown lists scanned candidates that produced no rule (strict only).
	Unknown []string
	// Warnings lists unsupported config or directive constructs.
	Warnings []string
}

// GenerateTailwindDirectives scans the project, applies the config and the
// CSS-first directives (d may be nil), and returns the layered stylesheet:
// `@layer theme, base, components, utilities;` then theme variables, the
// preflight and the utilities each in their own layer, then @keyframes. User
// CSS that is unlayered therefore beats preflight and utilities alike.
func GenerateTailwindDirectives(root string, cfg *TailwindConfig, opts TailwindOptions, d *TailwindDirectives) (TailwindResult, error) {
	if d == nil {
		d = NewTailwindDirectives(root)
	}
	var res TailwindResult

	generator := NewTailwindGenerator()
	generator.MergeConfig(cfg)
	d.ApplyTo(&generator.Theme)
	generator.Strict = opts.Strict
	if cfg != nil {
		res.Warnings = append(res.Warnings, cfg.Warnings...)
	}

	var dirs []string
	if !d.NoAutoSource {
		dirs = resolveScanDirs(root, opts)
	}
	dirs = append(dirs, d.Sources...)
	scanner := NewTailwindScanner(root)
	scanner.Exclude = d.Excludes
	classes := scanner.ScanClasses(dirs)
	for _, c := range d.Inline {
		classes[c] = true
	}
	userVars := 0
	for _, v := range d.Theme.Vars {
		if !v.Reference {
			userVars++
		}
	}
	res.Warnings = append(res.Warnings, d.Warnings()...)
	if len(classes) == 0 && userVars == 0 {
		sort.Strings(res.Warnings)
		return res, nil
	}

	utilities := generator.Generate(classes)
	res.Unknown = append(res.Unknown, generator.Unknown...)
	sort.Strings(res.Unknown)

	refs := map[string]bool{}
	for k := range scanner.VarRefs {
		refs[k] = true
	}
	for k := range d.VarRefs {
		refs[k] = true
	}
	res.CSS = assembleTailwindSheet(&generator.Theme, &d.Theme, refs, utilities, generator.KeyframesCSS(), opts.Preflight)
	return res, nil
}

// assembleTailwindSheet lays the pieces out in cascade layers like Tailwind v4.
func assembleTailwindSheet(theme *TailwindTheme, at *AtTheme, refs map[string]bool, utilities, keyframes string, preflight bool) string {
	vars := themeLayerVars(theme, at, refs)
	if utilities == "" && len(vars) == 0 && keyframes == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("@layer theme, base, components, utilities;\n")
	if len(vars) > 0 {
		b.WriteString("@layer theme{:root,:host{")
		for _, v := range vars {
			b.WriteString("--" + v.Name + ":" + v.Value + ";")
		}
		b.WriteString("}}\n")
	}
	if preflight {
		b.WriteString("@layer base{\n")
		b.WriteString(TailwindPreflight(*theme))
		b.WriteString("}\n")
	}
	if utilities != "" {
		b.WriteString("@layer utilities{\n")
		b.WriteString(utilities)
		b.WriteString("}\n")
	}
	b.WriteString(keyframes)
	if keyframes != "" && !strings.HasSuffix(keyframes, "\n") {
		b.WriteByte('\n')
	}
	return b.String()
}

// resolveScanDirs returns absolute scan roots: `content` wins over `scanDirs`,
// both default to `<root>/src`. Globs are reduced to their base directory.
func resolveScanDirs(root string, opts TailwindOptions) []string {
	patterns := opts.ContentOverrides
	if len(patterns) == 0 {
		patterns = opts.ScanDirs
	}
	if len(patterns) == 0 {
		patterns = []string{"src"}
	}
	var dirs []string
	seen := map[string]bool{}
	for _, p := range patterns {
		// Reduce a glob (`./src/**/*.tsx`) to its static base directory.
		base := p
		if i := strings.IndexAny(base, "*?{"); i >= 0 {
			base = base[:i]
			if i := strings.LastIndexByte(base, '/'); i >= 0 {
				base = base[:i]
			}
		}
		base = strings.TrimPrefix(base, "./")
		if base == "" {
			base = "."
		}
		abs := base
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, base)
		}
		if !seen[abs] {
			seen[abs] = true
			dirs = append(dirs, abs)
		}
	}
	return dirs
}

// atTailwindRe matches a `@tailwind base|components|utilities;` directive.
var atTailwindRe = regexp.MustCompile(`(?m)^[ \t]*@tailwind[^;{}]*;[ \t]*\r?\n?`)

// StripAtTailwind removes `@tailwind base|components|utilities;` directives.
// Krate generates Tailwind output itself, so a leftover directive would ship as
// an invalid at-rule.
func StripAtTailwind(css string) string {
	return atTailwindRe.ReplaceAllString(css, "")
}
