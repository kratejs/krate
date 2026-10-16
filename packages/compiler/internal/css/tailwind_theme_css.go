package css

import (
	"os"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

// atThemeRe matches an `@theme { ... }` block (Tailwind v4 CSS-first config).
var atThemeRe = regexp.MustCompile(`(?s)@theme\s*(?:[^{]*)\{([^}]*)\}`)

// themeVarRe matches a `--name: value;` declaration inside an @theme block.
var themeVarRe = regexp.MustCompile(`--([a-zA-Z0-9-]+)\s*:\s*([^;{}]+);?`)

// ExtractAtTheme returns the custom properties declared in `@theme` blocks,
// keyed without the leading `--` (e.g. "color-brand-500" -> "#f00").
func ExtractAtTheme(cssText string) map[string]string {
	out := map[string]string{}
	for _, m := range atThemeRe.FindAllStringSubmatch(cssText, -1) {
		for _, v := range themeVarRe.FindAllStringSubmatch(m[1], -1) {
			out[v[1]] = strings.TrimSpace(v[2])
		}
	}
	return out
}

// MergeAtTheme applies v4 `@theme` variables to a theme, generating the
// corresponding utilities: --color-*, --spacing-*, --radius-*, --shadow-*,
// --opacity-*, --leading-*, --breakpoint-*, --font-*.
func MergeAtTheme(theme *TailwindTheme, vars map[string]string) {
	for name, val := range vars {
		switch {
		case strings.HasPrefix(name, "color-"):
			rest := name[len("color-"):]
			if i := strings.LastIndexByte(rest, '-'); i > 0 && isNumericShade(rest[i+1:]) {
				ensureColor(theme, rest[:i])[rest[i+1:]] = val
			} else {
				ensureColor(theme, rest)["DEFAULT"] = val
			}
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
		case strings.HasPrefix(name, "breakpoint-"):
			theme.Screens[name[len("breakpoint-"):]] = val
		case strings.HasPrefix(name, "font-"):
			theme.FontFamily[name[len("font-"):]] = val
		}
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

// ScanAtTheme walks the Tailwind scan directories and merges every `@theme`
// block found in `.css` files into a single variable map.
func ScanAtTheme(root string, opts TailwindOptions) map[string]string {
	out := map[string]string{}
	skip := map[string]bool{".git": true, "node_modules": true, "dist": true, "out": true, ".krate": true}
	for _, dir := range resolveScanDirs(root, opts) {
		_ = fsutil.WalkExt(dir, map[string]bool{".css": true}, skip, func(path string, _ os.FileInfo) error {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			for k, v := range ExtractAtTheme(string(data)) {
				out[k] = v
			}
			return nil
		})
	}
	return out
}

// GenerateTailwindWithTheme is GenerateTailwindWithOptions with v4 `@theme`
// variables merged into the theme before generation.
func GenerateTailwindWithTheme(root string, cfg *TailwindConfig, opts TailwindOptions, themeVars map[string]string) (string, error) {
	dirs := resolveScanDirs(root, opts)
	scanner := NewTailwindScanner(root)
	classes := scanner.ScanClasses(dirs)
	if len(classes) == 0 && len(themeVars) == 0 {
		return "", nil
	}

	generator := NewTailwindGenerator()
	generator.MergeConfig(cfg)
	MergeAtTheme(&generator.Theme, themeVars)
	generator.Strict = opts.Strict

	cssText := generator.Generate(classes)
	if opts.Preflight {
		cssText = TailwindPreflight(generator.Theme) + cssText
	}
	return cssText, nil
}
