package css

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// TailwindConfig represents the tailwind.config.ts/js parsed values.
type TailwindConfig struct {
	Theme    TailwindTheme `json:"theme"`
	DarkMode string        `json:"darkMode"`
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
	// ExecuteConfig forces `npx tsx` config execution instead of static parse.
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
// (no Node required); when `execute` is true or the static parse yields no
// theme, it falls back to executing the config via `npx tsx`. On any failure it
// returns defaults (a diagnostic is surfaced by the caller).
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

// executeTailwindConfig runs the config via `npx --yes tsx`.
func executeTailwindConfig(root, configPath string) (*TailwindConfig, bool) {
	bootstrap := fmt.Sprintf(`import cfg from "%s"; console.log(JSON.stringify({theme:cfg.theme||{},darkMode:cfg.darkMode||""}));`, configPath)
	tmpDir, err := os.MkdirTemp("", "krate-tailwind-*")
	if err != nil {
		return nil, false
	}
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, "bootstrap.mjs")
	if err := os.WriteFile(tmpFile, []byte(bootstrap), 0644); err != nil {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "npx", "--yes", "tsx", tmpFile)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return nil, false
	}

	var parsed TailwindConfig
	if err := json.Unmarshal(output, &parsed); err != nil {
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
	if t.DarkMode != "" {
		cfg.Theme.DarkMode = t.DarkMode
	}
	if parsed.DarkMode != "" {
		cfg.Theme.DarkMode = parsed.DarkMode
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

// GenerateTailwind runs the full Tailwind pipeline: scan classes → generate CSS.
func GenerateTailwind(root string, cfg *TailwindConfig) (string, error) {
	return GenerateTailwindWithOptions(root, cfg, TailwindOptions{})
}

// GenerateTailwindWithOptions is GenerateTailwind with explicit options.
func GenerateTailwindWithOptions(root string, cfg *TailwindConfig, opts TailwindOptions) (string, error) {
	dirs := resolveScanDirs(root, opts)

	scanner := NewTailwindScanner(root)
	classes := scanner.ScanClasses(dirs)
	if len(classes) == 0 {
		return "", nil
	}

	generator := NewTailwindGenerator()
	generator.MergeConfig(cfg)
	generator.Strict = opts.Strict

	css := generator.Generate(classes)
	if opts.Preflight {
		css = TailwindPreflight(generator.Theme) + css
	}
	return css, nil
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
