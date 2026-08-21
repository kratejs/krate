package css

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TailwindConfig represents the tailwind.config.ts/js parsed values.
type TailwindConfig struct {
	Theme TailwindTheme `json:"theme"`
}

// LoadTailwindConfig attempts to load a tailwind.config file by executing it via npx tsx.
// Falls back to defaults if not found or execution fails.
func LoadTailwindConfig(root string) *TailwindConfig {
	cfg := &TailwindConfig{
		Theme: DefaultTailwindTheme(),
	}

	// Look for tailwind.config files in order of preference
	candidates := []string{
		"tailwind.config.ts",
		"tailwind.config.js",
		"tailwind.config.mjs",
	}

	var configPath string
	for _, c := range candidates {
		p := filepath.Join(root, c)
		if _, err := os.Stat(p); err == nil {
			configPath = p
			break
		}
	}
	if configPath == "" {
		return cfg
	}

	// Try to execute via npx tsx
	bootstrap := fmt.Sprintf(`import cfg from "%s"; console.log(JSON.stringify({theme:cfg.theme||{}}));`, configPath)
	tmpDir, err := os.MkdirTemp("", "krate-tailwind-*")
	if err != nil {
		return cfg
	}
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, "bootstrap.mjs")
	if err := os.WriteFile(tmpFile, []byte(bootstrap), 0644); err != nil {
		return cfg
	}

	// Bound the npx tsx execution so a hung tailwind.config (blocking import,
	// infinite loop) falls back to defaults instead of blocking the build.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "npx", "tsx", tmpFile)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return cfg
	}

	var parsed TailwindConfig
	if err := json.Unmarshal(output, &parsed); err != nil {
		return cfg
	}

	// Merge with defaults
	if parsed.Theme.Spacing != nil {
		for k, v := range cfg.Theme.Spacing {
			if _, ok := parsed.Theme.Spacing[k]; !ok {
				parsed.Theme.Spacing[k] = v
			}
		}
		cfg.Theme.Spacing = parsed.Theme.Spacing
	}
	if parsed.Theme.Colors != nil {
		for k, v := range cfg.Theme.Colors {
			if _, ok := parsed.Theme.Colors[k]; !ok {
				parsed.Theme.Colors[k] = v
			}
		}
		for k, v := range parsed.Theme.Colors {
			cfg.Theme.Colors[k] = v
		}
	}
	if parsed.Theme.TextSizes != nil {
		for k, v := range parsed.Theme.TextSizes {
			cfg.Theme.TextSizes[k] = v
		}
	}
	if parsed.Theme.FontWeights != nil {
		for k, v := range parsed.Theme.FontWeights {
			cfg.Theme.FontWeights[k] = v
		}
	}
	if parsed.Theme.Radii != nil {
		for k, v := range parsed.Theme.Radii {
			cfg.Theme.Radii[k] = v
		}
	}
	if parsed.Theme.Shadows != nil {
		for k, v := range parsed.Theme.Shadows {
			cfg.Theme.Shadows[k] = v
		}
	}
	if parsed.Theme.Sizing != nil {
		for k, v := range parsed.Theme.Sizing {
			cfg.Theme.Sizing[k] = v
		}
	}

	return cfg
}

// MergeConfig merges a TailwindConfig into the generator's theme.
func (g *TailwindGenerator) MergeConfig(cfg *TailwindConfig) {
	if cfg == nil {
		return
	}
	t := cfg.Theme
	if len(t.Spacing) > 0 {
		g.Theme.Spacing = t.Spacing
	}
	if len(t.Colors) > 0 {
		g.Theme.Colors = t.Colors
	}
	if len(t.TextSizes) > 0 {
		g.Theme.TextSizes = t.TextSizes
	}
	if len(t.FontWeights) > 0 {
		g.Theme.FontWeights = t.FontWeights
	}
	if len(t.Radii) > 0 {
		g.Theme.Radii = t.Radii
	}
	if len(t.Shadows) > 0 {
		g.Theme.Shadows = t.Shadows
	}
	if len(t.Sizing) > 0 {
		g.Theme.Sizing = t.Sizing
	}
	if len(t.BgColors) > 0 {
		g.Theme.BgColors = t.BgColors
	}
}

// GenerateTailwind runs the full Tailwind pipeline: scan classes → generate CSS → return.
func GenerateTailwind(root string, cfg *TailwindConfig) (string, error) {
	scanner := NewTailwindScanner(root)

	// Scan source directories for class names
	dirs := []string{filepath.Join(root, "src")}
	classes := scanner.ScanClasses(dirs)

	if len(classes) == 0 {
		return "", nil
	}

	generator := NewTailwindGenerator()
	generator.MergeConfig(cfg)

	return generator.Generate(classes), nil
}

// StripTailwindDirectives removes @tailwind and @apply directives from CSS.
func StripTailwindDirectives(css string) string {
	var result strings.Builder
	for _, line := range strings.Split(css, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@tailwind") || strings.HasPrefix(trimmed, "@apply") {
			continue
		}
		result.WriteString(line)
		result.WriteByte('\n')
	}
	return strings.TrimSpace(result.String())
}
