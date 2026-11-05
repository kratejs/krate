package css

import (
	"fmt"
	"strings"
)

// customUtilityCSS returns the declarations of a static `@utility` named base.
func customUtilityCSS(base string, theme TailwindTheme) string {
	if len(theme.CustomUtilities) == 0 {
		return ""
	}
	return theme.CustomUtilities[base]
}

// customVariant resolves a `@custom-variant` name. A selector template with
// exactly one "&" becomes the variant's selector template; "@media",
// "@supports" and "@container" templates become at-rule variants. Custom
// variants sort after the built-in ones, like Tailwind v4 registers them.
func customVariant(name string, theme TailwindTheme) (variant, bool) {
	tmpl, ok := theme.CustomVariants[name]
	if !ok {
		return variant{}, false
	}
	tmpl = strings.TrimSpace(tmpl)
	rank := fmt.Sprintf("%02d:custom:%s", arbitraryVariantOrder-1, name)
	switch {
	case strings.HasPrefix(tmpl, "@media"), strings.HasPrefix(tmpl, "@supports"), strings.HasPrefix(tmpl, "@container"):
		return variant{ats: []string{tmpl}, rank: rank, comp: compAt}, true
	case strings.HasPrefix(tmpl, "@"):
		return variant{ats: []string{tmpl}, rank: rank}, true
	case strings.Count(tmpl, "&") == 1:
		return variant{sel: tmpl, rank: rank, comp: compSel}, true
	}
	return variant{}, false
}

// functionalUtilityCSS resolves a class against the functional `@utility
// name-*` blocks: the longest prefix that matches base supplies the
// declarations, with --value() and --spacing() calls replaced by the part of
// the class after the prefix. It returns "" when no block matches or a call
// cannot be resolved for this class.
func functionalUtilityCSS(base string, theme TailwindTheme) string {
	if len(theme.FunctionalUtilities) == 0 {
		return ""
	}
	best := ""
	for prefix := range theme.FunctionalUtilities {
		if strings.HasPrefix(base, prefix+"-") && len(prefix) > len(best) {
			best = prefix
		}
	}
	if best == "" {
		return ""
	}
	value := base[len(best)+1:]
	out, ok := resolveUtilityCalls(theme.FunctionalUtilities[best], value, theme)
	if !ok {
		return ""
	}
	return out
}

// resolveUtilityCalls replaces the --value(...) and --spacing(...) calls in
// text. A --value() lists the forms it accepts, tried in order: `integer`,
// `number`, `percentage`, `ratio`, `[type]` or `[*]` (an arbitrary `[...]`
// value), a "literal", or a theme namespace such as `--color-*`.
func resolveUtilityCalls(text, value string, theme TailwindTheme) (string, bool) {
	for {
		start, name := nextUtilityCall(text)
		if start < 0 {
			return text, true
		}
		open := start + len(name)
		end := matchParen(text, open)
		if end < 0 {
			return "", false
		}
		inner := text[open+1 : end]
		var repl string
		switch name {
		case "--spacing":
			arg, ok := resolveUtilityCalls(inner, value, theme)
			if !ok {
				return "", false
			}
			arg = strings.TrimSpace(arg)
			if v, isNum := spacingMultiple(arg, theme); isNum {
				repl = v
			} else {
				repl = "calc(var(--spacing, 0.25rem) * " + arg + ")"
			}
		case "--value":
			v, ok := resolveUtilityValue(inner, value, theme)
			if !ok {
				return "", false
			}
			repl = v
		default:
			return "", false
		}
		text = text[:start] + repl + text[end+1:]
	}
}

// nextUtilityCall finds the outermost --value( / --spacing( / --modifier( call.
func nextUtilityCall(text string) (int, string) {
	best, bestName := -1, ""
	for _, name := range []string{"--value", "--spacing", "--modifier"} {
		for from := 0; ; {
			i := strings.Index(text[from:], name+"(")
			if i < 0 {
				break
			}
			i += from
			// `--spacing-4` or `var(--value-x)` are not calls: require "(".
			if best < 0 || i < best {
				best, bestName = i, name
			}
			break
		}
	}
	return best, bestName
}

func resolveUtilityValue(args, value string, theme TailwindTheme) (string, bool) {
	for _, arg := range splitTopLevelCommas(args) {
		arg = strings.TrimSpace(arg)
		switch {
		case arg == "integer":
			if plainNumber(value) && !strings.Contains(value, ".") {
				return value, true
			}
		case arg == "number":
			if plainNumber(value) {
				return value, true
			}
		case arg == "percentage":
			if strings.HasSuffix(value, "%") && plainNumber(strings.TrimSuffix(value, "%")) {
				return value, true
			}
		case arg == "ratio":
			if a, b, ok := strings.Cut(value, "/"); ok && plainNumber(a) && plainNumber(b) {
				return a + " / " + b, true
			}
		case strings.HasPrefix(arg, "["):
			if v, ok := unwrapArbitrary(value); ok {
				return v, true
			}
		case len(arg) >= 2 && (arg[0] == '"' || arg[0] == '\'') && arg[len(arg)-1] == arg[0]:
			if value == arg[1:len(arg)-1] {
				return value, true
			}
		case strings.HasPrefix(arg, "--") && strings.HasSuffix(arg, "-*"):
			name := strings.TrimPrefix(strings.TrimSuffix(arg, "*"), "--") + value
			if v, ok := themeVarValue(&theme, name); ok {
				return v, true
			}
		}
	}
	return "", false
}
