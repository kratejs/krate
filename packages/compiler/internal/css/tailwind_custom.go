package css

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

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

// Custom utilities (`@utility`).
//
// A static `@utility name { ... }` and a functional `@utility name-* { ... }`
// both keep their body as written. A class resolves it per use: --value() and
// --modifier() pick the value and the modifier of the class (`name-VALUE/MOD`),
// a declaration whose call cannot resolve is dropped, and nested rules,
// at-rules and `@variant` blocks come out as CSS nesting inside the class rule.
// The accepted forms follow Tailwind v4 (verified against tailwindcss 4.3 in
// testdata/tailwind_utility_golden.json).

// utilArg is the value or the modifier part of a class matched against a
// functional utility.
type utilArg struct {
	// arbitrary is true for `[...]` and `(--var)` parts.
	arbitrary bool
	// value is the text; arbitrary values are already normalized.
	value string
	// hint is the explicit type of an arbitrary value, from `[length:...]`.
	hint string
	// fraction is "3/2" for a named value followed by a named modifier, which
	// --value(ratio) reads as one value.
	fraction string
}

var utilHintRe = regexp.MustCompile(`^[a-z][a-z-]*$`)

// parseUtilArg reads one `/`-separated segment of a class.
func parseUtilArg(seg string, isValue bool) (*utilArg, bool) {
	if seg == "" {
		return nil, false
	}
	var inner string
	switch {
	case seg[0] == '[' && seg[len(seg)-1] == ']':
		inner = seg[1 : len(seg)-1]
	case seg[0] == '(' && seg[len(seg)-1] == ')':
		inner = seg[1 : len(seg)-1]
		hint := ""
		if i := strings.IndexByte(inner, ':'); i > 0 && isValue {
			hint, inner = inner[:i], inner[i+1:]
		}
		if !strings.HasPrefix(inner, "--") || strings.ContainsAny(inner, "[]()") {
			return nil, false
		}
		inner = "var(" + inner + ")"
		if hint != "" && !utilHintRe.MatchString(hint) {
			return nil, false
		}
		return &utilArg{arbitrary: true, value: inner, hint: hint}, true
	default:
		if strings.ContainsAny(seg, "[]()") {
			return nil, false
		}
		return &utilArg{value: seg}, true
	}
	if strings.TrimSpace(inner) == "" {
		return nil, false
	}
	a := &utilArg{arbitrary: true}
	if i := strings.IndexByte(inner, ':'); i > 0 && isValue && utilHintRe.MatchString(inner[:i]) {
		a.hint, inner = inner[:i], inner[i+1:]
	}
	a.value = normalizeArbitrary(inner)
	return a, true
}

// parseUtilArgs splits what follows a functional utility's prefix into its
// value and modifier. Either may be absent.
func parseUtilArgs(rest string) (value, modifier *utilArg, ok bool) {
	segs := topLevelSplit(rest, '/')
	if len(segs) > 2 {
		return nil, nil, false
	}
	if segs[0] != "" {
		if value, ok = parseUtilArg(segs[0], true); !ok {
			return nil, nil, false
		}
	}
	if len(segs) == 2 {
		if modifier, ok = parseUtilArg(segs[1], false); !ok {
			return nil, nil, false
		}
		if value != nil && !value.arbitrary && !modifier.arbitrary {
			value.fraction = segs[0] + "/" + segs[1]
		}
	}
	return value, modifier, true
}

// customUtility resolves base against the project's `@utility` blocks. The
// result is a declaration block that may also hold nested rules; important
// marks every declaration in it !important.
func customUtility(base string, important bool, theme TailwindTheme) (string, bool) {
	if len(theme.utilStack) > 8 || (len(theme.CustomUtilities) == 0 && len(theme.FunctionalUtilities) == 0) {
		return "", false
	}
	for _, open := range theme.utilStack {
		if open == base {
			return "", false
		}
	}
	theme.utilStack = append(append([]string(nil), theme.utilStack...), base)
	if body, ok := theme.CustomUtilities[base]; ok {
		c := &utilityCtx{theme: theme, important: important}
		if css := c.render(body); css != "" {
			return css, true
		}
	}
	// The longest prefix wins, and a shorter one is tried when it cannot
	// resolve the class.
	var prefixes []string
	for p := range theme.FunctionalUtilities {
		if base == p || strings.HasPrefix(base, p+"-") || strings.HasPrefix(base, p+"/") {
			prefixes = append(prefixes, p)
		}
	}
	sort.Slice(prefixes, func(a, b int) bool {
		if len(prefixes[a]) != len(prefixes[b]) {
			return len(prefixes[a]) > len(prefixes[b])
		}
		return prefixes[a] < prefixes[b]
	})
	for _, p := range prefixes {
		rest := strings.TrimPrefix(strings.TrimPrefix(base, p), "-")
		value, modifier, ok := parseUtilArgs(rest)
		if !ok {
			continue
		}
		c := &utilityCtx{theme: theme, important: important, functional: true, value: value, modifier: modifier}
		if css := c.render(theme.FunctionalUtilities[p]); css != "" {
			return css, true
		}
	}
	return "", false
}

// customUtilityRoots lists the hyphen-free names of the project's own
// utilities, which the class scanner would otherwise take for prose.
func customUtilityRoots(theme *TailwindTheme) map[string]bool {
	var out map[string]bool
	for _, m := range []map[string]string{theme.CustomUtilities, theme.FunctionalUtilities} {
		for name := range m {
			if !strings.Contains(name, "-") {
				if out == nil {
					out = map[string]bool{}
				}
				out[name] = true
			}
		}
	}
	return out
}

// utilityCtx is the state of resolving one class against one utility body.
type utilityCtx struct {
	theme      TailwindTheme
	important  bool
	functional bool
	value      *utilArg
	modifier   *utilArg

	usedValue, gotValue, gotModifier, ratio bool
}

// outNode is a resolved declaration or rule.
type outNode struct {
	decl     string // "name: value" for a declaration
	prelude  string // selector or at-rule prelude for a rule
	kids     []*outNode
	nonRatio bool // a declaration whose --value() was not a ratio
}

func (c *utilityCtx) render(body string) string {
	nodes := c.build(splitCSSNodes(body))
	if c.functional {
		hasMod := c.modifier != nil
		switch {
		case !c.usedValue || !c.gotValue:
			return ""
		case c.ratio && c.gotModifier:
			return ""
		case hasMod && !c.ratio && !c.gotModifier:
			return ""
		}
	}
	if c.ratio {
		nodes = pruneNonRatio(nodes)
	}
	return serializeOut(nodes)
}

func pruneNonRatio(nodes []*outNode) []*outNode {
	var out []*outNode
	for _, n := range nodes {
		if n.decl != "" {
			if !n.nonRatio {
				out = append(out, n)
			}
			continue
		}
		if n.kids = pruneNonRatio(n.kids); len(n.kids) > 0 {
			out = append(out, n)
		}
	}
	return out
}

func serializeOut(nodes []*outNode) string {
	var b strings.Builder
	for i, n := range nodes {
		if i > 0 {
			b.WriteByte(' ')
		}
		if n.decl != "" {
			b.WriteString(n.decl)
			b.WriteByte(';')
			continue
		}
		b.WriteString(n.prelude)
		b.WriteByte('{')
		b.WriteString(serializeOut(n.kids))
		b.WriteByte('}')
	}
	return b.String()
}

func (c *utilityCtx) build(nodes []cssNode) []*outNode {
	var out []*outNode
	for _, n := range nodes {
		switch {
		case !n.Block && strings.HasPrefix(n.Text, "@"):
			out = append(out, c.statement(n.Text)...)
		case !n.Block:
			name, val, ok := strings.Cut(n.Text, ":")
			name, val = strings.TrimSpace(name), strings.TrimSpace(val)
			if !ok || name == "" || val == "" {
				continue
			}
			if d := c.declaration(name, val); d != nil {
				out = append(out, d)
			}
		case strings.HasPrefix(n.Prelude, "@variant") && (len(n.Prelude) == len("@variant") || n.Prelude[len("@variant")] == ' '):
			out = append(out, c.variantBlock(strings.TrimSpace(n.Prelude[len("@variant"):]), n.Body)...)
		case n.Prelude == "&":
			// A bare `&` block only repeats its parent selector.
			out = append(out, c.build(splitCSSNodes(n.Body))...)
		default:
			kids := c.build(splitCSSNodes(n.Body))
			if len(kids) > 0 {
				out = append(out, &outNode{prelude: strings.Join(strings.Fields(n.Prelude), " "), kids: kids})
			}
		}
	}
	return out
}

func (c *utilityCtx) declaration(name, val string) *outNode {
	nonRatio := false
	if strings.Contains(val, "--value(") || strings.Contains(val, "--modifier(") || strings.Contains(val, "--spacing(") {
		var ok bool
		if val, nonRatio, ok = c.resolveCalls(val); !ok {
			return nil
		}
	}
	if !validUtilityDecl(name, val) {
		return nil
	}
	decl := name + ": " + val
	if c.important && !strings.HasSuffix(val, "!important") {
		decl += " !important"
	}
	return &outNode{decl: decl, nonRatio: nonRatio}
}

// statement handles a body entry that ends in `;` and starts with `@`.
func (c *utilityCtx) statement(text string) []*outNode {
	rest, ok := strings.CutPrefix(text, "@apply")
	if !ok || (rest != "" && !isCSSSpace(rest[0])) {
		return nil
	}
	g := NewTailwindGenerator()
	g.Theme = c.theme
	decls, nested, _ := g.applyRules(strings.Fields(rest))
	return c.build(splitCSSNodes(decls + " " + nested))
}

// variantBlock expands `@variant hover { ... }` (a comma list is a union) into
// the variant's selector and at-rules wrapped around the resolved body.
func (c *utilityCtx) variantBlock(names, body string) []*outNode {
	inner := c.build(splitCSSNodes(body))
	if len(inner) == 0 {
		return nil
	}
	var out []*outNode
	for _, name := range topLevelSplit(names, ',') {
		v, ok := resolveVariant(strings.TrimSpace(name), c.theme)
		if !ok {
			continue
		}
		arms := []variant{v}
		if len(v.alts) > 0 {
			arms = v.alts
		}
		for _, a := range arms {
			kids := inner
			for i := len(a.ats) - 1; i >= 0; i-- {
				kids = []*outNode{{prelude: a.ats[i], kids: kids}}
			}
			if a.sel != "" && a.sel != "&" {
				kids = []*outNode{{prelude: a.sel, kids: kids}}
			}
			out = append(out, kids...)
		}
	}
	return out
}

// validUtilityDecl rejects a declaration whose resolved value could end the
// declaration or the block early.
func validUtilityDecl(name, val string) bool {
	if name == "" || val == "" || strings.ContainsAny(name, "{};:()[]") {
		return false
	}
	depth := 0
	var quote byte
	for i := 0; i < len(val); i++ {
		ch := val[i]
		switch {
		case quote != 0:
			if ch == '\\' {
				i++
			} else if ch == quote {
				quote = 0
			}
		case ch == '"' || ch == '\'':
			quote = ch
		case ch == '(' || ch == '[':
			depth++
		case ch == ')' || ch == ']':
			depth--
		case ch == ';' && depth <= 0:
			return false
		case ch == '{' || ch == '}':
			return false
		}
	}
	return quote == 0 && depth == 0 && validCSSValue(val)
}

// resolveCalls replaces the --value(), --modifier() and --spacing() calls in a
// declaration value. nonRatio reports that a --value() resolved to something
// other than a ratio. ok is false when a call cannot resolve, which drops the
// declaration.
func (c *utilityCtx) resolveCalls(text string) (out string, nonRatio, ok bool) {
	for {
		start, name := nextUtilityCall(text)
		if start < 0 {
			return text, nonRatio, true
		}
		open := start + len(name)
		end := matchParen(text, open)
		if end < 0 {
			return "", false, false
		}
		inner := text[open+1 : end]
		var repl string
		switch name {
		case "--spacing":
			arg, nr, good := c.resolveCalls(inner)
			if !good {
				return "", false, false
			}
			nonRatio = nonRatio || nr
			arg = strings.TrimSpace(arg)
			if v, isNum := spacingMultiple(arg, c.theme); isNum {
				repl = v
			} else {
				repl = "calc(var(--spacing, 0.25rem) * " + arg + ")"
			}
		case "--value":
			c.usedValue = true
			v, ratio, good := c.pick(inner, c.value, true)
			if !good {
				return "", false, false
			}
			c.gotValue = true
			if ratio {
				c.ratio = true
			} else {
				nonRatio = true
			}
			repl = v
		default:
			v, _, good := c.pick(inner, c.modifier, false)
			if !good {
				return "", false, false
			}
			c.gotModifier = true
			repl = v
		}
		text = text[:start] + repl + text[end+1:]
	}
}

// nextUtilityCall finds the leftmost --value( / --spacing( / --modifier( call.
func nextUtilityCall(text string) (int, string) {
	best, bestName := -1, ""
	for _, name := range []string{"--value", "--spacing", "--modifier"} {
		// `--spacing-4` or `var(--value-x)` are not calls: require "(".
		if i := strings.Index(text, name+"("); i >= 0 && (best < 0 || i < best) {
			best, bestName = i, name
		}
	}
	return best, bestName
}

var (
	utilStarRunRe = regexp.MustCompile(`(?:-\*){2,}`)
	utilRatioRe   = regexp.MustCompile(`^(\d+)\s*/\s*(\d+)$`)
)

// pick resolves the argument list of one --value() or --modifier() call
// against the class's value or modifier e. The forms are tried in order:
// "literal", --namespace-*, number / integer / ratio / percentage, [type] and
// [*]; --default(x) supplies the result when the class has no such part.
func (c *utilityCtx) pick(args string, e *utilArg, isValue bool) (res string, ratio, ok bool) {
	list := splitTopLevelCommas(args)
	if e == nil {
		for _, a := range list {
			if def, found := strings.CutPrefix(a, "--default("); found && strings.HasSuffix(def, ")") {
				return strings.TrimSpace(def[:len(def)-1]), false, true
			}
		}
		return "", false, false
	}
	for _, a := range list {
		if strings.HasPrefix(a, "--default(") {
			continue
		}
		quoted := len(a) >= 2 && (a[0] == '"' || a[0] == '\'') && a[len(a)-1] == a[0]
		if !quoted {
			a = strings.Join(strings.Fields(strings.ReplaceAll(a, `\*`, "*")), "")
			a = utilStarRunRe.ReplaceAllString(a, "-*")
			if strings.HasPrefix(a, "--") && !strings.Contains(a, "(") && !strings.Contains(a, "-*") {
				a += "-*"
			}
		}
		switch {
		case quoted:
			if !e.arbitrary && e.value == a[1:len(a)-1] {
				return e.value, false, true
			}
		case strings.HasPrefix(a, "--"):
			if ns, found := strings.CutSuffix(a, "*"); found && !e.arbitrary {
				if v, found := utilityThemeVar(&c.theme, strings.TrimPrefix(ns, "--")+e.value); found {
					return v, false, true
				}
			}
		case a == "[*]" && e.arbitrary:
			return e.value, false, true
		case len(a) > 2 && a[0] == '[' && a[len(a)-1] == ']' && e.arbitrary:
			typ := a[1 : len(a)-1]
			if e.hint != "" {
				if e.hint == typ {
					return e.value, false, true
				}
				continue
			}
			if inferDataType(e.value, typ) {
				return e.value, false, true
			}
		case !e.arbitrary:
			if v, isRatio, good := bareUtilityValue(a, e); good {
				return v, isRatio, true
			}
		}
	}
	return "", false, false
}

// utilityThemeVar looks a theme variable up for --value(--namespace-*). Krate
// keeps Tailwind v3's numeric spacing scale and a few radius keys in its theme
// maps; those are not v4 theme variables, so only an `@theme` entry counts.
func utilityThemeVar(theme *TailwindTheme, name string) (string, bool) {
	if theme.LiveVars[name] {
		return theme.themeRef(name, ""), true
	}
	switch name {
	case "radius-full", "radius-none", "radius-DEFAULT":
		v, ok := theme.ThemeVars[name]
		return v, ok
	}
	if strings.HasPrefix(name, "spacing-") {
		v, ok := theme.ThemeVars[name]
		return v, ok
	}
	if v, ok := themeVarValue(theme, name); ok {
		return v, true
	}
	if key, ok := strings.CutPrefix(name, "tracking-"); ok {
		v, found := defaultTracking[key]
		return v, found
	}
	return "", false
}

// bareUtilityValue matches a named value against number, integer, ratio and
// percentage.
func bareUtilityValue(typ string, e *utilArg) (string, bool, bool) {
	n := e.value
	if typ == "ratio" && e.fraction != "" {
		n = e.fraction
	}
	if n == "" {
		return "", false, false
	}
	switch typ {
	case "integer":
		return n, false, twCanonInt.MatchString(n)
	case "number":
		return n, false, twCanonNumber.MatchString(n)
	case "percentage":
		return n, false, strings.HasSuffix(n, "%") && twCanonInt.MatchString(strings.TrimSuffix(n, "%"))
	case "ratio":
		if m := utilRatioRe.FindStringSubmatch(n); m != nil {
			return m[1] + " / " + m[2], true, true
		}
	}
	return "", false, false
}

// utilityWarnings reports the parts of an `@utility` body that cannot work, so
// the author hears about them at build time.
func utilityWarnings(name, body string) []string {
	var out []string
	rest := body
	for {
		start, call := nextUtilityCall(rest)
		if start < 0 {
			return out
		}
		open := start + len(call)
		end := matchParen(rest, open)
		if end < 0 {
			return out
		}
		inner := rest[open+1 : end]
		rest = rest[end+1:]
		if call == "--spacing" {
			rest = inner + " " + rest
			continue
		}
		for _, a := range splitTopLevelCommas(inner) {
			switch {
			case a == "number" || a == "integer" || a == "ratio" || a == "percentage",
				strings.HasPrefix(a, "--"), strings.HasPrefix(a, "\""), strings.HasPrefix(a, "'"):
			case strings.HasPrefix(a, "[") && strings.HasSuffix(a, "]"):
				if t := a[1 : len(a)-1]; t != "*" && !twDataTypes[t] {
					out = append(out, fmt.Sprintf("@utility %s: %s(%s) names an unknown type; use one of %s", name, call, a, "color, length, percentage, ratio, number, integer, url, position, bg-size, line-width, image, family-name, generic-name, absolute-size, relative-size, angle, vector"))
				}
			default:
				out = append(out, fmt.Sprintf("@utility %s: %s(%s) is not a supported argument; use number, integer, ratio, percentage, [type], \"literal\" or --theme-namespace-*", name, call, a))
			}
		}
	}
}
