package css

import "strings"

// minCtx carries the enclosing at-rule context into declaration handling.
type minCtx struct {
	keyframes bool // inside @keyframes: !important declarations are ignored
	property  bool // inside @property: values are descriptors, keep them as written
}

func childCtx(ctx minCtx, head string) minCtx {
	switch preludeAtName(head) {
	case "keyframes":
		ctx.keyframes = true
	case "property":
		ctx.property = true
	}
	return ctx
}

// preludeAtName returns the lowercase, vendor-unprefixed name of the at-rule that
// introduces head, or "" when head is not an at-rule prelude.
func preludeAtName(head string) string {
	h := strings.TrimSpace(head)
	if !strings.HasPrefix(h, "@") {
		return ""
	}
	h = h[1:]
	end := 0
	for end < len(h) && (isIdentByte(h[end]) || h[end] == '-') {
		end++
	}
	return stripVendorPrefix(strings.ToLower(h[:end]))
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= 0x80 || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func stripVendorPrefix(s string) string {
	for _, p := range []string{"-webkit-", "-moz-", "-ms-", "-o-"} {
		if strings.HasPrefix(s, p) {
			return s[len(p):]
		}
	}
	return s
}

// declInfo describes one declaration for the dedupe planner.
type declInfo struct {
	key string
	val string
}

// planDrops decides which declarations of one rule body are redundant. The
// LAST occurrence of a property stays at its own position and earlier ones are
// dropped, but only when the fold cannot change the cascade:
//   - an earlier !important beats a later normal declaration (the later one is
//     dropped instead); inside @keyframes !important is ignored by browsers;
//   - both values must be of the same kind, otherwise the earlier one may be
//     an intentional fallback (width:100%;width:-moz-fit-content);
//   - no related property (shorthand or longhand family) may sit between them.
func planDrops(decls []declInfo, ctx minCtx) []bool {
	drop := make([]bool, len(decls))
	last := make(map[string]int, len(decls))
	for j, d := range decls {
		i, seen := last[d.key]
		if !seen {
			last[d.key] = j
			continue
		}
		prevCore, prevImp := splitImportant(decls[i].val)
		core, imp := splitImportant(d.val)
		if ctx.keyframes {
			prevImp, imp = false, false
		}
		if prevImp && !imp {
			drop[j] = true
			continue
		}
		if !valuesFoldable(d.key, prevCore, core) || relatedBetween(decls, drop, i, j) {
			last[d.key] = j
			continue
		}
		drop[i] = true
		last[d.key] = j
	}
	return drop
}

func relatedBetween(decls []declInfo, drop []bool, i, j int) bool {
	for k := i + 1; k < j; k++ {
		if !drop[k] && propsRelated(decls[k].key, decls[j].key) {
			return true
		}
	}
	return false
}

// propFamily maps a property to the family of properties that can overwrite
// each other through shorthands. Custom properties return "".
func propFamily(prop string) string {
	if strings.HasPrefix(prop, "--") {
		return ""
	}
	p := stripVendorPrefix(prop)
	switch p {
	case "top", "right", "bottom", "left":
		return "inset"
	case "gap", "row-gap", "column-gap", "grid-gap", "grid-row-gap", "grid-column-gap":
		return "gap"
	case "place-content", "align-content", "justify-content":
		return "content"
	case "place-items", "align-items", "justify-items":
		return "items"
	case "place-self", "align-self", "justify-self":
		return "self"
	case "white-space", "text-wrap", "text-wrap-mode", "text-wrap-style", "white-space-collapse":
		return "wrap"
	case "word-wrap", "overflow-wrap":
		return "overflow-wrap"
	case "line-height":
		return "font"
	case "columns":
		return "column"
	case "width", "inline-size":
		return "size-inline"
	case "height", "block-size":
		return "size-block"
	}
	if i := strings.IndexByte(p, '-'); i > 0 {
		return p[:i]
	}
	return p
}

func propsRelated(a, b string) bool {
	if strings.HasPrefix(a, "--") || strings.HasPrefix(b, "--") {
		return false
	}
	if stripVendorPrefix(a) == "all" || stripVendorPrefix(b) == "all" {
		return true
	}
	return propFamily(a) == propFamily(b)
}

// valuesFoldable reports whether a later declaration may replace an earlier
// one. Identical values always fold. Otherwise both must have the same shape
// (for example two plain lengths or two hex colors) and neither may use a
// vendor prefix or a keyword whose support is uncertain.
func valuesFoldable(key, prev, next string) bool {
	if prev == next {
		return true
	}
	sp, pfxP, kwP := valueShape(prev)
	sn, pfxN, kwN := valueShape(next)
	if pfxP || pfxN || len(sp) != len(sn) {
		return false
	}
	for i := range sp {
		if !shapesCompatible(sp[i], sn[i]) {
			return false
		}
	}
	if kwP || kwN {
		return foldableKeywords(key, prev) && foldableKeywords(key, next)
	}
	return true
}

var legacyLengthUnits = map[string]bool{
	"px": true, "em": true, "rem": true, "ex": true, "ch": true, "pt": true,
	"pc": true, "in": true, "cm": true, "mm": true, "q": true, "vw": true,
	"vh": true, "vmin": true, "vmax": true, "%": true,
}

// valueShape reduces a value to a signature of its top-level tokens.
func valueShape(val string) (sig []string, prefixed, hasKeyword bool) {
	n := len(val)
	for i := 0; i < n; {
		c := val[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' || c == '/' {
			i++
			continue
		}
		start := i
		paren := 0
		for i < n {
			c = val[i]
			if c == '\\' {
				i += 2
				continue
			}
			if c == '"' || c == '\'' {
				i = cssSkipString(val, i)
				continue
			}
			if c == 0 {
				j := strings.IndexByte(val[i+1:], 0)
				if j < 0 {
					i = n
				} else {
					i += j + 2
				}
				continue
			}
			if c == '(' {
				paren++
			} else if c == ')' {
				if paren > 0 {
					paren--
				}
			} else if paren == 0 && (c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' || c == '/') {
				break
			}
			i++
		}
		if i > n {
			i = n
		}
		tok := val[start:i]
		kind, pfx := tokenShape(tok)
		if pfx {
			prefixed = true
		}
		if kind == "kw" {
			hasKeyword = true
		}
		sig = append(sig, kind)
	}
	return sig, prefixed, hasKeyword
}

// shapesCompatible compares two token shapes; a bare zero is valid wherever a
// length or a number is.
func shapesCompatible(a, b string) bool {
	if a == b {
		return true
	}
	if a == "zero" {
		return b == "len" || b == "num"
	}
	if b == "zero" {
		return a == "len" || a == "num"
	}
	return false
}

func tokenShape(tok string) (kind string, prefixed bool) {
	lower := strings.ToLower(tok)
	switch {
	case tok == "":
		return "", false
	case tok[0] == 0 || tok[0] == '"' || tok[0] == '\'':
		return "lit", false
	case tok[0] == '#':
		return "hex", false
	case strings.HasPrefix(lower, "url("):
		return "lit", false
	case len(tok) > 2 && tok[0] == '-' && tok[1] != '-' && stripVendorPrefix(lower) != lower:
		return "pfx", true
	}
	if open := strings.IndexByte(tok, '('); open > 0 {
		name := strings.ToLower(tok[:open])
		switch name {
		case "rgb", "rgba", "hsl", "hsla":
			return "color-legacy", false
		}
		return "fn:" + name, false
	}
	// Number with optional unit.
	k := 0
	if k < len(tok) && (tok[k] == '+' || tok[k] == '-') {
		k++
	}
	digits := 0
	for k < len(tok) && (tok[k] >= '0' && tok[k] <= '9' || tok[k] == '.') {
		k++
		digits++
	}
	if digits > 0 {
		unit := strings.ToLower(tok[k:])
		switch {
		case unit == "":
			if strings.Trim(lower, "+-0.") == "" {
				return "zero", false
			}
			return "num", false
		case legacyLengthUnits[unit]:
			return "len", false
		}
		return "dim:" + unit, false
	}
	return "kw", false
}

var oldKeywords = map[string]bool{
	"auto": true, "none": true, "normal": true, "inherit": true, "initial": true,
	"solid": true, "dashed": true, "dotted": true, "double": true, "hidden": true,
	"visible": true, "bold": true, "italic": true, "center": true, "left": true,
	"right": true, "top": true, "bottom": true, "block": true, "inline": true,
	"inline-block": true, "nowrap": true, "pre": true, "absolute": true,
	"relative": true, "fixed": true, "static": true, "transparent": true,
	"currentcolor": true, "uppercase": true, "lowercase": true, "capitalize": true,
	"underline": true, "pointer": true, "default": true, "text": true,
}

func isColorProp(key string) bool {
	return strings.HasSuffix(key, "color") || key == "fill" || key == "stroke"
}

// foldableKeywords reports whether every keyword of val is safe to treat as a
// plain replacement value (old, universally supported keywords, or any named
// color for color properties).
func foldableKeywords(key, val string) bool {
	if isColorProp(key) {
		return true
	}
	core, _ := splitImportant(val)
	for _, f := range strings.FieldsFunc(strings.ToLower(core), func(r rune) bool {
		return r == ' ' || r == ',' || r == '/' || r == '\t' || r == '\n'
	}) {
		if f == "" || f[0] == 0 || f[0] == '"' || f[0] == '\'' || f[0] == '#' || f[0] == '-' || f[0] == '+' || f[0] == '.' || (f[0] >= '0' && f[0] <= '9') || strings.ContainsRune(f, '(') {
			continue
		}
		if !oldKeywords[f] {
			return false
		}
	}
	return true
}

// dedupeWalk folds duplicate declarations in every leaf rule body while
// preserving all other text (whitespace, comments, formatting) exactly.
func dedupeWalk(s string, ctx minCtx, top bool) string {
	items := splitCSSItems(s)
	hasBlock := false
	for _, it := range items {
		if it.block {
			hasBlock = true
			break
		}
	}
	if !hasBlock && !top {
		return dedupeLeafPreserving(items, ctx)
	}
	var b strings.Builder
	for _, it := range items {
		b.WriteString(it.head)
		if it.block {
			b.WriteByte('{')
			b.WriteString(dedupeWalk(it.body, childCtx(ctx, it.head), false))
			if it.closed {
				b.WriteByte('}')
			}
		} else if it.closed {
			b.WriteByte(';')
		}
	}
	return b.String()
}

func dedupeLeafPreserving(items []cssItem, ctx minCtx) string {
	type seg struct {
		prefix, core string
		closed       bool
		decl         bool
	}
	segs := make([]seg, len(items))
	var decls []declInfo
	var declAt []int
	for i, it := range items {
		prefix, core := splitLeadingTrivia(it.head)
		segs[i] = seg{prefix: prefix, core: core, closed: it.closed}
		if strings.HasPrefix(core, "@") {
			continue
		}
		prop, val, ok := parseDecl(stripCSSComments(core))
		if !ok || prop == "" {
			continue
		}
		segs[i].decl = true
		decls = append(decls, declInfo{key: declKey(prop), val: val})
		declAt = append(declAt, i)
	}
	drop := planDrops(decls, ctx)
	dropped := make(map[int]bool)
	for k, d := range drop {
		if d {
			dropped[declAt[k]] = true
		}
	}

	var b strings.Builder
	carry := ""
	for i, sg := range segs {
		if dropped[i] {
			if strings.Contains(sg.prefix, "/*") {
				carry += sg.prefix
			}
			continue
		}
		prefix := sg.prefix
		if carry != "" {
			prefix = carry + strings.TrimLeft(prefix, " \t\r\n\f")
			carry = ""
		}
		b.WriteString(prefix)
		b.WriteString(sg.core)
		if sg.closed {
			b.WriteByte(';')
		}
	}
	b.WriteString(carry)
	return b.String()
}
