package css

import (
	"os"
	"regexp"
	"strings"
)

// ParseTailwindConfigStatic parses a tailwind.config.{ts,js,mjs} file without
// executing it, extracting the common `theme` / `extend` / `darkMode` shapes.
// It deliberately handles a useful subset (object/array/string literals) so the
// Go-native build needs no Node; anything it cannot parse returns ok=false and
// the caller may fall back to `npx tsx`.
func ParseTailwindConfigStatic(path string) (*TailwindConfig, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	src := string(data)

	cfg := &TailwindConfig{Theme: DefaultTailwindTheme()}

	// darkMode: 'class' | 'media' | 'selector' | ['class', '.dark'] | ['selector', ...]
	if v, ok := parseDarkMode(src); ok {
		cfg.Theme.DarkMode = v
		cfg.DarkMode = v
	}

	// Locate `theme: { ... }`.
	themeSrc, ok := extractBlock(src, "theme")
	if !ok {
		// No theme object: the config still parsed (defaults + darkMode).
		return cfg, true
	}

	// Top-level theme keys replace defaults; `extend` keys merge.
	override, extend := splitThemeBlocks(themeSrc)
	// `extend` must merge onto the built-in scale for every key, so each call
	// passes the current (already-defaulted) theme value as the merge base -
	// this also covers borderRadius/boxShadow/fontSize/fontWeight, which
	// previously had no base and so discarded the whole default scale.
	applyThemeKey("spacing", override, extend, &cfg.Theme.Spacing)
	applyThemeKey("screens", override, extend, &cfg.Theme.Screens)
	applyThemeKey("maxWidth", override, extend, &cfg.Theme.MaxWidth)
	applyThemeKey("minWidth", override, extend, &cfg.Theme.MinWidth)
	applyThemeKey("lineHeight", override, extend, &cfg.Theme.LineHeight)
	applyThemeKey("opacity", override, extend, &cfg.Theme.Opacity)
	applyThemeKey("fontFamily", override, extend, &cfg.Theme.FontFamily)
	applyThemeKey("borderRadius", override, extend, &cfg.Theme.Radii)
	applyThemeKey("boxShadow", override, extend, &cfg.Theme.Shadows)
	applyThemeKey("fontSize", override, extend, &cfg.Theme.TextSizes)
	applyThemeKey("fontWeight", override, extend, &cfg.Theme.FontWeights)

	// Colors: nested map (name -> shade -> value). Replaced when top-level.
	if colors, ok := parseColorMap(override, "colors"); ok {
		cfg.Theme.Colors = colors
	} else if colors, ok := parseColorMap(extend, "colors"); ok {
		if cfg.Theme.Colors == nil {
			cfg.Theme.Colors = map[string]map[string]string{}
		}
		for k, v := range colors {
			cfg.Theme.Colors[k] = v
		}
	}

	return cfg, true
}

var (
	reDarkMode    = regexp.MustCompile(`darkMode\s*:\s*['"]([a-zA-Z]+)['"]`)
	reDarkModeArr = regexp.MustCompile(`darkMode\s*:\s*\[\s*['"]([a-zA-Z]+)['"]`)
)

// parseDarkMode extracts the dark-mode strategy from either the string form
// (`darkMode: 'class'`) or the array form (`darkMode: ['class', '.dark']`).
func parseDarkMode(src string) (string, bool) {
	for _, re := range []*regexp.Regexp{reDarkModeArr, reDarkMode} {
		if m := re.FindStringSubmatch(src); m != nil {
			switch m[1] {
			case "class", "selector", "media":
				return m[1], true
			}
		}
	}
	return "", false
}

// splitThemeBlocks extracts the top-level theme source and the `extend` source.
func splitThemeBlocks(themeSrc string) (override, extend string) {
	if ext, ok := extractBlock(themeSrc, "extend"); ok {
		// Remove the extend block from the override source.
		override = removeBlock(themeSrc, "extend")
		return override, ext
	}
	return themeSrc, ""
}

// applyThemeKey parses a flat string/number map under `key`. A top-level theme
// key replaces the target map; an `extend` key merges onto the target's
// existing (default) contents.
func applyThemeKey(key, override, extend string, target *map[string]string) {
	if m, ok := parseFlatMap(override, key); ok && len(m) > 0 {
		*target = m
		return
	}
	if m, ok := parseFlatMap(extend, key); ok && len(m) > 0 {
		if *target == nil {
			*target = map[string]string{}
		}
		for k, v := range m {
			(*target)[k] = v
		}
	}
}

// parseFlatMap parses `key: { a: 'x', b: 4 }` into a string map.
func parseFlatMap(src, key string) (map[string]string, bool) {
	block, ok := extractBlock(src, key)
	if !ok {
		return nil, false
	}
	out := map[string]string{}
	for _, m := range reKV.FindAllStringSubmatch(block, -1) {
		out[unquote(m[1])] = unquote(m[2])
	}
	return out, len(out) > 0
}

// parseColorMap parses `colors: { name: { shade: 'value' } }`, the flatter
// `colors: { name: { DEFAULT: '...', light: '...' } }`, and the flat
// `colors: { name: 'value' }` shape. Objects are walked with brace matching so
// arbitrary nesting depth and multi-line values are handled (a regex could not
// match nested braces).
func parseColorMap(src, key string) (map[string]map[string]string, bool) {
	block, ok := extractBlock(src, key)
	if !ok {
		return nil, false
	}
	out := map[string]map[string]string{}
	for name, rest := range topLevelEntries(block) {
		rest = strings.TrimSpace(rest)
		if strings.HasPrefix(rest, "{") {
			inner, ok := braceBody(rest)
			if !ok {
				continue
			}
			shades := map[string]string{}
			for sk, sv := range topLevelEntries(inner) {
				if val, ok := scalarValue(sv); ok {
					shades[sk] = val
				}
			}
			if len(shades) > 0 {
				out[name] = shades
			}
			continue
		}
		if val, ok := scalarValue(rest); ok {
			out[name] = map[string]string{"DEFAULT": val}
		}
	}
	return out, len(out) > 0
}

// topLevelEntries splits an object body (`a: 1, b: { ... }`) into its key ->
// raw-value pairs, honouring nested braces, brackets, parens, and strings.
func topLevelEntries(body string) map[string]string {
	out := map[string]string{}
	i, n := 0, len(body)
	for i < n {
		// Read a key up to the top-level ':'.
		for i < n && (body[i] == ',' || body[i] == ' ' || body[i] == '\n' || body[i] == '\t' || body[i] == '\r') {
			i++
		}
		if i >= n {
			break
		}
		keyStart := i
		for i < n && body[i] != ':' && body[i] != ',' {
			i++
		}
		if i >= n || body[i] == ',' {
			continue
		}
		key := unquote(strings.TrimSpace(body[keyStart:i]))
		i++ // skip ':'
		valStart := i
		depth := 0
		for i < n {
			c := body[i]
			if c == '\'' || c == '"' || c == '`' {
				i = skipString(body, i)
				continue
			}
			switch c {
			case '{', '[', '(':
				depth++
			case '}', ']', ')':
				depth--
			case ',':
				if depth == 0 {
					goto done
				}
			}
			i++
		}
	done:
		if key != "" {
			out[key] = strings.TrimSpace(body[valStart:i])
		}
		if i < n && body[i] == ',' {
			i++
		}
	}
	return out
}

// skipString returns the index just past the string literal starting at i.
func skipString(s string, i int) int {
	q := s[i]
	i++
	for i < len(s) && s[i] != q {
		if s[i] == '\\' {
			i++
		}
		i++
	}
	if i < len(s) {
		i++
	}
	return i
}

// braceBody returns the inner text of a `{ ... }` value.
func braceBody(s string) (string, bool) {
	i := strings.IndexByte(s, '{')
	if i < 0 {
		return "", false
	}
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[i+1 : j], true
			}
		}
	}
	return "", false
}

// scalarValue returns the unquoted scalar value of a config entry, accepting
// strings, numbers, and bare identifier references (e.g. `colors.blue`). It
// returns ok=false for objects, arrays, and function calls (which the static
// parser cannot resolve).
func scalarValue(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	if s[0] == '{' || s[0] == '[' {
		return "", false
	}
	if s[0] == '\'' || s[0] == '"' || s[0] == '`' {
		return unquote(s), true
	}
	if strings.ContainsAny(s, "()") {
		return "", false
	}
	// A bare identifier (possibly dotted) or number.
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '_' || c == '.' || c == '-' || c == '+' || c == '%' || c == ' ') {
			return "", false
		}
	}
	return s, true
}

var reKV = regexp.MustCompile(`['"]?([A-Za-z0-9_.-]+)['"]?\s*:\s*(['"][^'"]*['"]|[0-9.]+|[A-Za-z_][A-Za-z0-9_.]*)`)

// extractBlock finds `key: { ... }` and returns the inner source, honouring
// nested braces and string literals.
func extractBlock(src, key string) (string, bool) {
	re := regexp.MustCompile(`(['"]?` + regexp.QuoteMeta(key) + `['"]?)\s*:\s*\{`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return "", false
	}
	start := loc[1] - 1 // index of '{'
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start+1 : i], true
			}
		case '\'', '"', '`':
			// Skip the string literal.
			q := src[i]
			i++
			for i < len(src) && src[i] != q {
				if src[i] == '\\' {
					i++
				}
				i++
			}
		}
	}
	return "", false
}

// removeBlock removes `key: { ... }` from src.
func removeBlock(src, key string) string {
	re := regexp.MustCompile(`(['"]?` + regexp.QuoteMeta(key) + `['"]?)\s*:\s*\{`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return src
	}
	start := loc[1] - 1
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[:loc[0]] + src[i+1:]
			}
		case '\'', '"', '`':
			q := src[i]
			i++
			for i < len(src) && src[i] != q {
				if src[i] == '\\' {
					i++
				}
				i++
			}
		}
	}
	return src
}

// unquote strips surrounding quotes from a config value token.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
