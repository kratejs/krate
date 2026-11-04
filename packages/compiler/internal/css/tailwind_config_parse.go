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
// the caller may fall back to in-process evaluation.
func ParseTailwindConfigStatic(path string) (*TailwindConfig, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	src := string(data)

	cfg := &TailwindConfig{Theme: DefaultTailwindTheme()}

	applyConfigScalars(src, cfg)

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
	w := func(msg string) { cfg.Warnings = append(cfg.Warnings, msg) }
	applyThemeKey("spacing", override, extend, &cfg.Theme.Spacing, nil, w)
	applyThemeKey("screens", override, extend, &cfg.Theme.Screens, screenValue, w)
	applyThemeKey("maxWidth", override, extend, &cfg.Theme.MaxWidth, nil, w)
	applyThemeKey("minWidth", override, extend, &cfg.Theme.MinWidth, nil, w)
	applyThemeKey("lineHeight", override, extend, &cfg.Theme.LineHeight, nil, w)
	applyThemeKey("opacity", override, extend, &cfg.Theme.Opacity, nil, w)
	applyThemeKey("fontFamily", override, extend, &cfg.Theme.FontFamily, fontFamilyValue, w)
	applyThemeKey("borderRadius", override, extend, &cfg.Theme.Radii, nil, w)
	applyThemeKey("boxShadow", override, extend, &cfg.Theme.Shadows, nil, w)
	applyThemeKey("fontSize", override, extend, &cfg.Theme.TextSizes, fontSizeValue, w)
	applyThemeKey("fontWeight", override, extend, &cfg.Theme.FontWeights, nil, w)
	applyThemeKey("letterSpacing", override, extend, &cfg.Theme.Tracking, nil, w)
	applyThemeKey("animation", override, extend, &cfg.Theme.Animation, nil, w)
	applyKeyframes(override, extend, &cfg.Theme.Keyframes, w)

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

// splitThemeBlocks extracts the top-level theme source and the `extend` source.
func splitThemeBlocks(themeSrc string) (override, extend string) {
	if ext, ok := extractBlock(themeSrc, "extend"); ok {
		// Remove the extend block from the override source.
		override = removeBlock(themeSrc, "extend")
		return override, ext
	}
	return themeSrc, ""
}

// applyThemeKey parses a flat map under `key`. A top-level theme key replaces
// the target map; an `extend` key merges onto the target's existing (default)
// contents. conv converts one raw value (nil means a plain scalar); entries the
// static parser cannot evaluate are reported through warn, never dropped
// silently.
func applyThemeKey(key, override, extend string, target *map[string]string, conv func(string) (string, bool), warn func(string)) {
	if m, ok := parseFlatMap(override, key, conv, warn); ok && len(m) > 0 {
		*target = m
		return
	}
	if m, ok := parseFlatMap(extend, key, conv, warn); ok && len(m) > 0 {
		if *target == nil {
			*target = map[string]string{}
		}
		for k, v := range m {
			(*target)[k] = v
		}
	}
}

// parseFlatMap parses `key: { a: 'x', b: 4 }` into a string map.
func parseFlatMap(src, key string, conv func(string) (string, bool), warn func(string)) (map[string]string, bool) {
	block, ok := extractBlock(src, key)
	if !ok {
		return nil, false
	}
	if conv == nil {
		conv = scalarValue
	}
	out := map[string]string{}
	for _, kv := range topLevelEntriesOrdered(block) {
		if v, ok := conv(kv.val); ok {
			out[kv.key] = v
		} else if warn != nil {
			warn("tailwind.config theme." + key + "." + kv.key + ": value `" + abbreviate(kv.val) + "` cannot be evaluated statically; set tailwind.executeConfig to evaluate the config")
		}
	}
	return out, len(out) > 0
}

func abbreviate(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}

// screenValue accepts plain min-width screens; {min,max} / {raw} objects need
// the range form Krate's screens map cannot hold.
func screenValue(raw string) (string, bool) { return scalarValue(raw) }

// fontFamilyValue accepts a string or an array of family names.
func fontFamilyValue(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[") {
		inner, ok := bracketBody(raw)
		if !ok {
			return "", false
		}
		var parts []string
		for _, item := range splitTopLevelCommas(inner) {
			v, ok := scalarValue(item)
			if !ok {
				return "", false
			}
			parts = append(parts, quoteFamily(v))
		}
		return strings.Join(parts, ", "), len(parts) > 0
	}
	return scalarValue(raw)
}

func quoteFamily(v string) string {
	if strings.ContainsAny(v, " \t") && !strings.HasPrefix(v, "\"") && !strings.HasPrefix(v, "'") {
		return "\"" + v + "\""
	}
	return v
}

// bracketBody returns the inner text of a `[ ... ]` value.
func bracketBody(s string) (string, bool) {
	if len(s) < 2 || s[0] != '[' {
		return "", false
	}
	depth := 0
	for j := 0; j < len(s); j++ {
		switch s[j] {
		case '\'', '"', '`':
			j = skipString(s, j) - 1
		case '[', '{', '(':
			depth++
		case ']', '}', ')':
			depth--
			if depth == 0 {
				return s[1:j], true
			}
		}
	}
	return "", false
}

// fontSizeValue accepts '1rem' and the tuple form
// ['0.875rem', { lineHeight: '1.25rem', letterSpacing: '...' }] / ['1rem', '1.5'].
func fontSizeValue(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "[") {
		return scalarValue(raw)
	}
	inner, ok := bracketBody(raw)
	if !ok {
		return "", false
	}
	items := splitTopLevelCommas(inner)
	if len(items) == 0 {
		return "", false
	}
	size, ok := scalarValue(items[0])
	if !ok {
		return "", false
	}
	out := "font-size: " + size + ";"
	if len(items) > 1 {
		second := strings.TrimSpace(items[1])
		if strings.HasPrefix(second, "{") {
			body, _ := braceBody(second)
			for _, e := range topLevelEntriesOrdered(body) {
				if v, ok := scalarValue(e.val); ok {
					out += " " + camelToKebab(e.key) + ": " + v + ";"
				}
			}
		} else if v, ok := scalarValue(second); ok {
			out += " line-height: " + v + ";"
		}
	}
	return out, true
}

// splitTopLevelCommas splits a list body on commas outside strings/groups.
func splitTopLevelCommas(body string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '\'', '"', '`':
			i = skipString(body, i) - 1
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		case ',':
			if depth == 0 {
				if t := strings.TrimSpace(body[start:i]); t != "" {
					out = append(out, t)
				}
				start = i + 1
			}
		}
	}
	if t := strings.TrimSpace(body[start:]); t != "" {
		out = append(out, t)
	}
	return out
}

func camelToKebab(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			b.WriteByte('-')
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

// applyKeyframes parses `keyframes: { name: { '0%, 100%': { prop: 'v' } } }`
// from the override and extend sources into @keyframes bodies.
func applyKeyframes(override, extend string, target *map[string]string, warn func(string)) {
	parse := func(src string) map[string]string {
		block, ok := extractBlock(src, "keyframes")
		if !ok {
			return nil
		}
		out := map[string]string{}
		for _, kf := range topLevelEntriesOrdered(block) {
			body, ok := braceBody(kf.val)
			if !ok {
				warn("tailwind.config theme.keyframes." + kf.key + ": expected an object of frames")
				continue
			}
			var sb strings.Builder
			for _, frame := range topLevelEntriesOrdered(body) {
				props, ok := braceBody(frame.val)
				if !ok {
					warn("tailwind.config theme.keyframes." + kf.key + "." + frame.key + ": expected an object of declarations")
					continue
				}
				sb.WriteString(frame.key + "{")
				for _, p := range topLevelEntriesOrdered(props) {
					v, ok := scalarValue(p.val)
					if !ok {
						warn("tailwind.config theme.keyframes." + kf.key + "." + frame.key + "." + p.key + ": value cannot be evaluated statically")
						continue
					}
					sb.WriteString(camelToKebab(unquote(p.key)) + ":" + v + ";")
				}
				sb.WriteString("}")
			}
			out[kf.key] = sb.String()
		}
		return out
	}
	if m := parse(override); len(m) > 0 {
		*target = m
		return
	}
	if m := parse(extend); len(m) > 0 {
		if *target == nil {
			*target = map[string]string{}
		}
		for k, v := range m {
			(*target)[k] = v
		}
	}
}

// applyConfigScalars reads the top-level darkMode, prefix, important and the
// unsupported keys (plugins, presets, ...) from the config source.
func applyConfigScalars(src string, cfg *TailwindConfig) {
	if raw, ok := rawValueAfterKey(src, "darkMode"); ok {
		strategy, selector, warn := parseDarkModeValue(raw)
		if strategy != "" {
			cfg.Theme.DarkMode = strategy
			cfg.DarkMode = strategy
			cfg.Theme.DarkSelector = selector
		}
		if warn != "" {
			cfg.Warnings = append(cfg.Warnings, warn)
		}
	}
	if raw, ok := rawValueAfterKey(src, "prefix"); ok {
		if v, ok := scalarValue(raw); ok && v != "" {
			p := strings.TrimRight(v, "-:")
			cfg.Theme.Prefix = p
			cfg.Warnings = append(cfg.Warnings, "tailwind.config prefix '"+v+"': Krate uses the v4 prefix syntax ("+p+":flex)")
		}
	}
	if raw, ok := rawValueAfterKey(src, "important"); ok {
		raw = strings.TrimSpace(raw)
		if raw == "true" {
			cfg.Theme.Important = "true"
		} else if v, ok := scalarValue(raw); ok && raw != "false" && v != "" {
			cfg.Theme.Important = v
		}
	}
	if raw, ok := rawValueAfterKey(src, "plugins"); ok {
		if inner, ok := bracketBody(strings.TrimSpace(raw)); !ok || strings.TrimSpace(inner) != "" {
			cfg.Warnings = append(cfg.Warnings, "tailwind.config plugins: JS plugins are not executed by Krate; utilities and variants they add will not be generated")
		}
	}
	for _, k := range []string{"presets", "corePlugins", "safelist", "blocklist", "separator"} {
		if raw, ok := rawValueAfterKey(src, k); ok {
			if inner := strings.TrimSpace(raw); inner != "[]" && inner != "{}" && inner != "" {
				cfg.Warnings = append(cfg.Warnings, "tailwind.config "+k+" is not supported by Krate and was ignored")
			}
		}
	}
}

// parseDarkModeValue interprets 'class' | 'media' | 'selector' |
// ['class', sel] | ['selector', sel] | ['media'] | ['variant', ...].
func parseDarkModeValue(raw string) (strategy, selector, warn string) {
	raw = strings.TrimSpace(raw)
	var items []string
	if strings.HasPrefix(raw, "[") {
		inner, ok := bracketBody(raw)
		if !ok {
			return "", "", "tailwind.config darkMode: could not parse the value"
		}
		for _, item := range splitTopLevelCommas(inner) {
			v, _ := scalarValue(item)
			items = append(items, v)
		}
	} else if v, ok := scalarValue(raw); ok {
		items = []string{v}
	}
	if len(items) == 0 {
		return "", "", ""
	}
	switch items[0] {
	case "class", "selector":
		if len(items) > 1 {
			selector = items[1]
		}
		return items[0], selector, ""
	case "media":
		return "media", "", ""
	case "variant":
		return "class", "", "tailwind.config darkMode: the 'variant' form is not supported; use ['selector', '.dark'] or @custom-variant dark"
	}
	return "", "", "tailwind.config darkMode: unknown strategy '" + items[0] + "'"
}

// rawValueAfterKey returns the raw source of a top-level-looking `key: value`
// pair (first occurrence), reading up to the next top-level comma or closer.
func rawValueAfterKey(src, key string) (string, bool) {
	re := regexp.MustCompile(`(?m)(?:^|[\s{,])['"]?` + regexp.QuoteMeta(key) + `['"]?\s*:\s*`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return "", false
	}
	i := loc[1]
	start := i
	depth := 0
	for i < len(src) {
		c := src[i]
		if c == '\'' || c == '"' || c == '`' {
			i = skipString(src, i)
			continue
		}
		if c == '{' || c == '[' || c == '(' {
			depth++
		} else if c == '}' || c == ']' || c == ')' {
			if depth == 0 {
				break
			}
			depth--
		} else if c == ',' && depth == 0 {
			break
		}
		i++
	}
	return strings.TrimSpace(src[start:i]), true
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
	for _, kv := range topLevelEntriesOrdered(body) {
		out[kv.key] = kv.val
	}
	return out
}

type kvEntry struct{ key, val string }

// topLevelEntriesOrdered is topLevelEntries preserving source order.
func topLevelEntriesOrdered(body string) []kvEntry {
	var out []kvEntry
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
			if body[i] == '\'' || body[i] == '"' || body[i] == '`' {
				i = skipString(body, i)
				continue
			}
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
			out = append(out, kvEntry{key, strings.TrimSpace(body[valStart:i])})
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
