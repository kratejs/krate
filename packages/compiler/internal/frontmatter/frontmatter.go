// Package frontmatter parses YAML-style frontmatter found at the top of krate
// markdown/MDX doc files. It is a deliberately small, dependency-free subset of
// YAML - a single source of truth shared by the markdown renderer and the docs
// plugin (docs package), which both previously carried separate flat parsers.
// Supported YAML subset:
//
//	---
//	title: Getting Started
//	order: 3
//	draft: false
//	keywords: [a, b, c]
//	tags:
//	  - install
//	  - cli
//	hero:
//	  title: Welcome
//	  actions:
//	    - text: Get Started
//	      link: /docs/getting-started
//	      variant: primary
//	---
//
// Supported: scalars (string / number / bool / null), double and single quoted
// strings, inline `[a, b]` arrays and `{k: v}` objects, indented object blocks,
// and `- item` list blocks (including maps of key/value pairs as list items).
// Unsupported YAML (by design): anchors (`&name`) and aliases (`*name`),
// merge keys (`<<`), block scalar folding (`|`, `>`), multi-document streams
// (`---`/`...` separators inside the block), flow-format nesting of arrays
// inside inline objects, YAML tags (`!!str`), and fancy escape sequences.
// Anything the parser cannot interpret is left as a plain string.
package frontmatter

import (
	"strconv"
	"strings"
	"unicode"
)

// Parse extracts the YAML frontmatter block from markdown-style source and
// returns the parsed map plus the body after the closing `---` delimiter.
// It returns (nil, src) when src has no frontmatter block, or an empty map
// when the block itself is empty.
func Parse(src string) (map[string]any, string) {
	src = strings.TrimPrefix(src, "\ufeff")
	lines := strings.Split(src, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return nil, src
	}
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx < 0 {
		return nil, src
	}
	block := strings.Join(lines[1:closeIdx], "\n")
	body := strings.Join(lines[closeIdx+1:], "\n")
	return ParseBlock(block), body
}

// ParseBlock parses the raw text of a frontmatter block (the lines between the
// `---` delimiters) into a nested map.
func ParseBlock(block string) map[string]any {
	lines := strings.Split(block, "\n")
	if len(lines) == 0 || strings.TrimSpace(block) == "" {
		return map[string]any{}
	}
	m, _ := parseMap(lines, 0)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

// parseMap parses consecutive `key: value` lines indented at or below `indent`.
// Deeper-indented lines are consumed recursively as the value of the
// preceding key. It returns the parsed map and the number of lines consumed.
func parseMap(lines []string, indent int) (map[string]any, int) {
	out := make(map[string]any)
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			i++
			continue
		case isListItem(trimmed):
			return out, i
		}
		curIndent := indentation(line)
		if curIndent < indent {
			return out, i
		}
		if curIndent > indent {
			// Line is deeper than the current level with no pending key:
			// not valid at this level; skip rather than bail.
			i++
			continue
		}

		key, rest, ok := splitKeyValue(trimmed)
		if !ok {
			i++
			continue
		}
		rest = strings.TrimSpace(rest)

		if rest == "" {
			if idx := nextContentIndex(lines, i+1); idx < len(lines) && indentation(lines[idx]) > indent {
				childIndent := indentation(lines[idx])
				if isListItem(strings.TrimSpace(lines[idx])) {
					val, n := parseList(lines[i+1:], childIndent)
					out[key] = val
					i += 1 + n
				} else {
					val, n := parseMap(lines[i+1:], childIndent)
					out[key] = val
					i += 1 + n
				}
				continue
			}
			out[key] = ""
		} else {
			out[key] = parseScalar(rest)
		}
		i++
	}
	return out, i
}

// parseList parses consecutive `- item` lines indented at `indent`. Items may
// be scalars or maps (`- key: value` along with more-indented key lines).
// It returns the parsed slice and the number of lines consumed.
func parseList(lines []string, indent int) ([]any, int) {
	var out []any
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			i++
			continue
		}
		curIndent := indentation(line)
		if curIndent < indent {
			return out, i
		}
		if curIndent > indent {
			// Deeper content was already consumed by the previous item.
			i++
			continue
		}
		if !isListItem(trimmed) {
			return out, i
		}

		itemRaw := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "-"), " "))
		if itemRaw == "" {
			// "-" followed by a deeper block: a bare nested list or map.
			if idx := nextContentIndex(lines, i+1); idx < len(lines) && indentation(lines[idx]) > indent {
				childIndent := indentation(lines[idx])
				if isListItem(strings.TrimSpace(lines[idx])) {
					val, n := parseList(lines[i+1:], childIndent)
					out = append(out, val)
					i += 1 + n
				} else {
					val, n := parseMap(lines[i+1:], childIndent)
					out = append(out, val)
					i += 1 + n
				}
				continue
			}
			out = append(out, "")
			i++
			continue
		}

		if strings.HasPrefix(itemRaw, "-") && (itemRaw == "-" || strings.HasPrefix(itemRaw, "- ")) {
			// Nested list starting on the same line as the parent dash, e.g.
			// `- - a` (the list whose first element is the scalar `a`) or
			// `- - a` followed by deeper `- b` lines. The nested dash sits at
			// column indent+2; its first item is re-dashed onto that column so
			// parseList consumes it and any deeper continuation lines.
			restInner := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(itemRaw, "-"), " "))
			childIndent := indent + 2
			var innerLines []string
			innerLines = append(innerLines, strings.Repeat(" ", childIndent)+"- "+restInner)
			j := i + 1
			for j < len(lines) && indentation(lines[j]) > indent {
				innerLines = append(innerLines, lines[j])
				j++
			}
			val, _ := parseList(innerLines, childIndent)
			out = append(out, val)
			i = j
			continue
		}

		if _, _, isKV := splitSimpleKeyValue(itemRaw); isKV {
			// Map item: re-indent the first line to one level under the dash
			// and let parseMap handle it plus any deeper continuation lines.
			j := i + 1
			var itemLines []string
			itemLines = append(itemLines, strings.Repeat(" ", indent+2)+itemRaw)
			for j < len(lines) && indentation(lines[j]) > indent {
				itemLines = append(itemLines, lines[j])
				j++
			}
			m, _ := parseMap(itemLines, indent+2)
			if m == nil {
				m = map[string]any{}
			}
			out = append(out, m)
			i = j
			continue
		}

		out = append(out, parseScalar(itemRaw))
		i++
	}
	return out, i
}

// parseScalar converts a raw YAML scalar/inline value into its typed form.
func parseScalar(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// Strip an inline comment for plain (unquoted) scalars.
	if raw[0] != '"' && raw[0] != '\'' {
		if idx := strings.Index(raw, " #"); idx >= 0 {
			raw = strings.TrimSpace(raw[:idx])
		}
		if raw == "" {
			return ""
		}
	}

	if len(raw) >= 2 {
		if raw[0] == '"' && raw[len(raw)-1] == '"' {
			return unescapeDoubleQuoted(raw[1 : len(raw)-1])
		}
		if raw[0] == '\'' && raw[len(raw)-1] == '\'' {
			return strings.ReplaceAll(raw[1:len(raw)-1], "''", "'")
		}
	}

	switch strings.ToLower(raw) {
	case "true":
		return true
	case "false":
		return false
	case "null", "~":
		return nil
	}

	if looksNumeric(raw) {
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f
		}
	}

	if strings.HasPrefix(raw, "[") {
		return parseInlineArray(raw)
	}
	if strings.HasPrefix(raw, "{") {
		return parseInlineObject(raw)
	}
	return raw
}

// unescapeDoubleQuoted decodes the double-quoted escape sequences krate
// supports: \" \\ and \n (anything else keeps its backslash removed).
func unescapeDoubleQuoted(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
			case '"', '\\':
				b.WriteByte(s[i+1])
			default:
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// parseInlineArray parses a flow `[a, b, "c, d"]` array.
func parseInlineArray(raw string) []any {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	var out []any
	for _, part := range splitOutsideQuotes(raw, ',') {
		out = append(out, parseScalar(part))
	}
	return out
}

// parseInlineObject parses a flow `{k: v, k2: v2}` object.
func parseInlineObject(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "{")
	raw = strings.TrimSuffix(raw, "}")
	out := make(map[string]any)
	for _, part := range splitOutsideQuotes(raw, ',') {
		key, value, ok := splitKeyValue(part)
		if !ok {
			continue
		}
		out[unquoteKey(key)] = parseScalar(value)
	}
	return out
}

// unquoteKey strips single or double quotes surrounding an inline object key
// (flow objects often mix JSON-style `{"k": v}` keys with bare YAML keys).
func unquoteKey(k string) string {
	k = strings.TrimSpace(k)
	if len(k) >= 2 {
		if (k[0] == '"' && k[len(k)-1] == '"') || (k[0] == '\'' && k[len(k)-1] == '\'') {
			return k[1 : len(k)-1]
		}
	}
	return k
}

// splitOutsideQuotes splits s on sep, ignoring separators inside single or
// double quoted substrings and inside flow containers ([ ] { } ( )).
func splitOutsideQuotes(s string, sep byte) []string {
	var parts []string
	var b strings.Builder
	inSingle, inDouble := false, false
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			b.WriteByte(c)
		case c == '"' && !inSingle:
			inDouble = !inDouble
			b.WriteByte(c)
		case c == '\\' && inDouble && i+1 < len(s):
			b.WriteByte(c)
			i++
			b.WriteByte(s[i])
		case !inSingle && !inDouble && (c == '[' || c == '{' || c == '('):
			depth++
			b.WriteByte(c)
		case !inSingle && !inDouble && (c == ']' || c == '}' || c == ')'):
			if depth > 0 {
				depth--
			}
			b.WriteByte(c)
		case c == sep && !inSingle && !inDouble && depth == 0:
			if v := strings.TrimSpace(b.String()); v != "" {
				parts = append(parts, v)
			}
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	if v := strings.TrimSpace(b.String()); v != "" {
		parts = append(parts, v)
	}
	return parts
}

// splitSimpleKeyValue is splitKeyValue restricted to "simple" keys (used for
// `- key: value` list items): the key must not start with a flow marker or
// quote and must contain no spaces - so `- [meta, {...}]` is a scalar element,
// not a malformed map entry.
func splitSimpleKeyValue(s string) (key, value string, ok bool) {
	key, value, ok = splitKeyValue(s)
	if !ok {
		return key, value, ok
	}
	for _, r := range key {
		if strings.ContainsRune("[]{}()\"'", r) || r == ' ' {
			return "", "", false
		}
	}
	return key, value, true
}

// splitKeyValue splits a `key: value` line. The first colon wins so URLs and
// colons inside the value are preserved.
func splitKeyValue(s string) (key, value string, ok bool) {
	idx := strings.IndexByte(s, ':')
	if idx <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(s[:idx])
	if key == "" {
		return "", "", false
	}
	return key, strings.TrimSpace(s[idx+1:]), true
}

func isListItem(s string) bool {
	return s == "-" || strings.HasPrefix(s, "- ")
}

func indentation(line string) int {
	n := 0
	for _, r := range line {
		if r == ' ' {
			n++
			continue
		}
		if r == '\t' {
			n++
			continue
		}
		break
	}
	return n
}

// nextContentIndex returns the index of the first non-blank, non-comment line
// at or after start, or len(lines) when none exists.
func nextContentIndex(lines []string, start int) int {
	for i := start; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t != "" && !strings.HasPrefix(t, "#") {
			return i
		}
	}
	return len(lines)
}

// looksNumeric reports whether raw looks like a plain decimal integer or float.
func looksNumeric(raw string) bool {
	if raw == "" {
		return false
	}
	for i, r := range raw {
		if r == '-' || r == '+' {
			if i != 0 {
				return false
			}
			continue
		}
		if r == '.' || r == 'e' || r == 'E' {
			continue
		}
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// String coerces a parsed frontmatter value to a string. Maps and lists coerce
// to "".
func String(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	default:
		return ""
	}
}

// StringSlice coerces a parsed frontmatter value to a slice of non-empty
// strings. Lists map element-for-element; strings are split on commas and
// whitespace (matching the legacy `keywords: a,b` flat syntax).
func StringSlice(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		var out []string
		for _, item := range t {
			if s := String(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	case string:
		return splitFlatList(t)
	default:
		return nil
	}
}

// Int coerces a parsed frontmatter value to an int, falling back to def when
// the value is absent or not numeric.
func Int(v any, def int) int {
	switch t := v.(type) {
	case int64:
		return int(t)
	case float64:
		return int(t)
	case int:
		return t
	case bool:
		if t {
			return 1
		}
		return 0
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
			return n
		}
	}
	return def
}

// Bool coerces a parsed frontmatter value to a bool.
func Bool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(t))
		if err != nil {
			return false
		}
		return b
	case int64:
		return t != 0
	}
	return false
}

// Map type-asserts a parsed frontmatter value to a nested object.
func Map(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

// List type-asserts a parsed frontmatter value to a list.
func List(v any) ([]any, bool) {
	l, ok := v.([]any)
	return l, ok
}

// splitFlatList splits a flat `a,b c` frontmatter string into trimmed tokens,
// honoring [] , , and " quoting like the legacy keyword parser.
func splitFlatList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	raw = strings.Trim(raw, "[]\"'")
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	var out []string
	for _, p := range parts {
		p = strings.Trim(p, ",\"'")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
