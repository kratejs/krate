package css

import (
	"fmt"
	"hash/fnv"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

var classSelector = regexp.MustCompile(`\.([a-zA-Z_][a-zA-Z0-9_-]*)`)

type Asset struct {
	Path     string
	Content  string
	IsModule bool
}

type ModuleMapping struct {
	LocalVar string
	Mappings map[string]string
}

func Collect(dir string) ([]*Asset, error) {
	var assets []*Asset

	err := fsutil.WalkExt(dir, map[string]bool{".css": true}, nil, func(path string, info os.FileInfo) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		isModule := strings.Contains(info.Name(), ".module.")

		assets = append(assets, &Asset{
			Path:     path,
			Content:  string(data),
			IsModule: isModule,
		})
		return nil
	})

	return assets, err
}

func hashPath(path string) string {
	h := fnv.New32a()
	h.Write([]byte(path))
	v := h.Sum32()
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	var buf [6]byte
	for i := 5; i >= 0; i-- {
		buf[i] = chars[v%36]
		v /= 36
	}
	return string(buf[:])
}

func Minify(cssText string) string {
	// Protect strings, url(...) and /*! license comments so the whitespace and
	// value-level transforms cannot corrupt their contents; restore at the end.
	p := &cssLiteralProtector{}
	minified := collapseCSS(p.protect(cssText))

	minified = rgbaToHex(minified)
	minified = shortenHexColors(minified)
	minified = removeZeroUnits(minified)
	minified = simplifyCalc(minified)
	minified = removeTrailingSemicolons(minified)
	minified = removeEmptyRules(minified)
	minified = removeDuplicateDeclarations(minified)

	return strings.TrimSpace(p.restore(minified))
}

// shortenHexColors shortens #rrggbb to #rgb when possible, and #rrggbbaa to #rgba.
func shortenHexColors(css string) string {
	return hexColorRe.ReplaceAllStringFunc(css, func(match string) string {
		if len(match) == 7 { // #rrggbb
			if match[1] == match[2] && match[3] == match[4] && match[5] == match[6] {
				return "#" + string(match[1]) + string(match[3]) + string(match[5])
			}
		}
		if len(match) == 9 { // #rrggbbaa
			if match[1] == match[2] && match[3] == match[4] && match[5] == match[6] && match[7] == match[8] {
				return "#" + string(match[1]) + string(match[3]) + string(match[5]) + string(match[7])
			}
		}
		return match
	})
}

// customPropRe matches a custom property declaration (--name: value). Custom
// property values are substituted verbatim into var()/calc() later, so their
// bytes must never be rewritten by value-level transforms like removeZeroUnits.
var customPropRe = regexp.MustCompile(`(?:^|[;{})\s])--[a-zA-Z0-9_-]+\s*:\s*[^;}]*`)

// removeZeroUnits removes units from 0 values (0px → 0), EXCEPT inside custom
// property values. Rewriting `--x: 0rem` to `--x: 0` changes the substituted
// value — calc(1rem + var(--x)) is only valid when --x carries a unit — so
// custom property declarations are stashed and restored verbatim.
func removeZeroUnits(css string) string {
	type placeholder struct {
		token, value string
	}
	var stash []placeholder
	protect := customPropRe.ReplaceAllStringFunc(css, func(m string) string {
		colon := strings.Index(m, ":")
		if colon < 0 {
			return m
		}
		// Keep any leading boundary character (space/;/{/}) so the declaration
		// still parses, but stash the raw value untouched.
		prefix := m[:colon+1]
		tok := fmt.Sprintf("\x00cp%d\x00", len(stash))
		stash = append(stash, placeholder{tok, m[colon+1:]})
		return prefix + tok
	})
	protect = zeroUnitRe.ReplaceAllString(protect, "0")
	for _, p := range stash {
		protect = strings.ReplaceAll(protect, p.token, p.value)
	}
	return protect
}

// removeEmptyRules removes empty rulesets (selectors with no declarations).
func removeEmptyRules(css string) string {
	return emptyRuleRe.ReplaceAllString(css, "")
}

// rgbaToHex converts opaque color functions to #hex. Both the legacy
// comma-separated syntax (rgba(r,g,b,1), rgb(r,g,b)) and the modern
// space-separated syntax (rgb(r g b), rgb(r g b / 1)) are handled.
func rgbaToHex(css string) string {
	css = rgbaRe.ReplaceAllStringFunc(css, func(match string) string {
		parts := rgbaRe.FindStringSubmatch(match)
		if len(parts) < 5 {
			return match
		}
		a := parts[4]
		if a != "1" && a != "1.0" && a != "1.00" {
			return match
		}
		return fmt.Sprintf("#%02x%02x%02x", parseByte(parts[1]), parseByte(parts[2]), parseByte(parts[3]))
	})
	css = rgbRe.ReplaceAllStringFunc(css, func(match string) string {
		parts := rgbRe.FindStringSubmatch(match)
		if len(parts) < 4 {
			return match
		}
		return fmt.Sprintf("#%02x%02x%02x", parseByte(parts[1]), parseByte(parts[2]), parseByte(parts[3]))
	})
	css = rgbSpaceRe.ReplaceAllStringFunc(css, func(match string) string {
		parts := rgbSpaceRe.FindStringSubmatch(match)
		if len(parts) < 5 {
			return match
		}
		if a := strings.TrimSpace(parts[4]); a != "" && a != "1" && a != "1.0" {
			return match
		}
		return fmt.Sprintf("#%02x%02x%02x", parseByte(parts[1]), parseByte(parts[2]), parseByte(parts[3]))
	})
	return css
}

func parseByte(s string) uint8 {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	if n > 255 {
		n = 255
	}
	return uint8(n)
}

// simplifyCalc simplifies calc() expressions: calc(0 + X) → X, calc(X + 0) → X,
// calc(X * 1) → X, calc(X * 0) → 0, etc. Whitespace around operators is
// optional for `*` and tolerated around `+`/`-`, since minified input may be
// space-free. Substitutions are re-applied until a fixed point so nested
// simplifications fully collapse.
func simplifyCalc(css string) string {
	for {
		next := calcRe.ReplaceAllStringFunc(css, simplifyOneCalc)
		if next == css {
			return css
		}
		css = next
	}
}

func simplifyOneCalc(match string) string {
	parts := calcRe.FindStringSubmatch(match)
	if len(parts) < 2 {
		return match
	}
	expr := strings.TrimSpace(parts[1])
	if simplified, ok := simplifyCalcExpr(expr); ok {
		return simplified
	}
	return match
}

// simplifyCalcExpr applies the identity rules to a single calc() body.
func simplifyCalcExpr(expr string) (string, bool) {
	type rule struct {
		re *regexp.Regexp
		fn func(m []string) string
	}
	rules := []rule{
		// Multiplicative identities (whitespace optional).
		{calcMulOneLeftRe, func(m []string) string { return m[1] }},
		{calcMulOneRightRe, func(m []string) string { return m[1] }},
		{calcMulZeroRe, func([]string) string { return "0" }},
		// Additive identities (Tailwind emits spaces; tolerate their absence).
		{calcAddZeroLeftRe, func(m []string) string { return m[1] }},
		{calcAddZeroRightRe, func(m []string) string { return m[1] }},
		{calcSubZeroLeftRe, func(m []string) string { return "-" + m[1] }},
	}
	for _, r := range rules {
		if m := r.re.FindStringSubmatch(expr); m != nil {
			return strings.TrimSpace(r.fn(m)), true
		}
	}
	return "", false
}

var (
	calcMulOneLeftRe   = regexp.MustCompile(`^1\s*\*\s*(.+)$`)
	calcMulOneRightRe  = regexp.MustCompile(`^(.+?)\s*\*\s*1$`)
	calcMulZeroRe      = regexp.MustCompile(`^(?:0\s*\*\s*.+|.+?\s*\*\s*0)$`)
	calcAddZeroLeftRe  = regexp.MustCompile(`^0\s*\+\s*(.+)$`)
	calcAddZeroRightRe = regexp.MustCompile(`^(.+?)\s*\+\s*0$`)
	calcSubZeroLeftRe  = regexp.MustCompile(`^0\s*-\s*(.+)$`)
)

// removeTrailingSemicolons removes semicolons right before closing braces.
func removeTrailingSemicolons(css string) string {
	return trailingSemiRe.ReplaceAllString(css, "}")
}

// removeDuplicateDeclarations removes duplicate property declarations within a
// rule, keeping only the last declaration for each property. At-rules such as
// @media/@keyframes/@supports are only recurded: their leaf rules are deduped
// independently so nested rules never bleed declarations into one another.
func removeDuplicateDeclarations(css string) string {
	var out strings.Builder
	scanRules(&out, css)
	return out.String()
}

// scanRules walks a CSS chunk, locating rules and their bodies. When a body
// contains nested rules (an at-rule container) it recurses so only LEAF rules
// are deduped; rule ordering, whitespace and at-rule structure are preserved.
func scanRules(out *strings.Builder, css string) {
	for i := 0; i < len(css); {
		open := strings.IndexByte(css[i:], '{')
		if open < 0 {
			out.WriteString(css[i:])
			return
		}
		open += i
		out.WriteString(css[i:open])
		closeBrace, deep := matchBrace(css, open)
		if closeBrace < 0 {
			out.WriteString(css[open:])
			return
		}
		out.WriteByte('{')
		body := css[open+1 : closeBrace]
		if deep {
			scanRules(out, body)
		} else {
			out.WriteString(deduplicateBody(body))
		}
		out.WriteByte('}')
		i = closeBrace + 1
	}
}

// matchBrace finds the brace that closes css[open]. The second return reports
// whether the body contains a nested rule (i.e. at least one deeper '{').
func matchBrace(css string, open int) (closeBrace int, deep bool) {
	depth := 1
	for j := open + 1; j < len(css); j++ {
		switch css[j] {
		case '{':
			depth++
			if depth == 2 {
				deep = true
			}
		case '}':
			depth--
			if depth == 0 {
				return j, deep
			}
		}
	}
	return -1, deep
}

// deduplicateBody removes duplicate declarations within a rule, keeping the
// last value for each property. Two cases must be preserved verbatim:
//
//   - Vendor-prefixed fallbacks: `display:-webkit-box;display:flex` keeps both,
//     because dropping the prefixed value breaks older browsers.
//   - Custom-property names: `--Foo` and `--foo` are distinct (custom
//     properties are case-sensitive), so only standard properties are folded.
func deduplicateBody(body string) string {
	type entry struct{ prop, val string }
	var entries []entry
	index := make(map[string]int) // key → position in entries

	for _, raw := range splitDecls(body) {
		prop, val, hasColon := parseDecl(raw)
		if !hasColon {
			continue
		}
		key := prop
		if !strings.HasPrefix(prop, "--") {
			key = strings.ToLower(prop)
		}
		pos, exists := index[key]
		if !exists {
			index[key] = len(entries)
			entries = append(entries, entry{key, val})
			continue
		}
		// Keep a vendor-prefixed earlier value when the new value differs, so
		// the prefixed fallback survives.
		if isVendorPrefixedValue(entries[pos].val) && entries[pos].val != val {
			index[key] = len(entries)
			entries = append(entries, entry{key, val})
			continue
		}
		// An earlier `!important` declaration wins over a later non-important
		// one; otherwise the cascade would change.
		if hasImportant(entries[pos].val) && !hasImportant(val) {
			continue
		}
		entries[pos].val = val
	}

	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(e.prop)
		b.WriteByte(':')
		b.WriteString(e.val)
	}
	return b.String()
}

// hasImportant reports whether a declaration value carries `!important`,
// tolerating surrounding whitespace (`! important`).
func hasImportant(val string) bool {
	v := strings.ReplaceAll(strings.ToLower(val), " ", "")
	return strings.Contains(v, "!important")
}

// isVendorPrefixedValue reports whether a declaration value begins with a
// vendor prefix (e.g. `-webkit-box`, `-moz-fit-content`).
func isVendorPrefixedValue(val string) bool {
	for _, p := range []string{"-webkit-", "-moz-", "-ms-", "-o-"} {
		if strings.HasPrefix(val, p) {
			return true
		}
	}
	return false
}

// splitDecls splits a rule body into declaration strings at top-level ';'
// boundaries, ignoring ';' inside quoted strings and inside balanced parens
// (e.g. url("data:image/svg+xml;utf8,...") or var(--x, ...)).
func splitDecls(body string) []string {
	var decls []string
	start := 0
	paren := 0
	var quote byte // 0 = none, '\'' or '"'
	for i := 0; i < len(body); i++ {
		c := body[i]
		if quote != 0 {
			if c == quote && (i == 0 || body[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case ';':
			if paren == 0 {
				decls = append(decls, body[start:i])
				start = i + 1
			}
		}
	}
	if start < len(body) {
		decls = append(decls, body[start:])
	}
	return decls
}

// parseDecl splits one declaration at its first top-level ':' (outside quotes
// and parens), returning the property name, value, and whether a colon existed.
func parseDecl(s string) (prop, val string, hasColon bool) {
	paren := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote && (i == 0 || s[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case ':':
			if paren == 0 {
				return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
			}
		}
	}
	return strings.TrimSpace(s), "", false
}

var hexColorRe = regexp.MustCompile(`#[0-9a-fA-F]{6,8}\b`)
var zeroUnitRe = regexp.MustCompile(`\b0(px|em|rem|vh|vw|vmin|vmax|%|pt|pc|in|cm|mm|ex|ch|fr)\b`)
var emptyRuleRe = regexp.MustCompile(`[^}]*\{\s*\}`)
var rgbaRe = regexp.MustCompile(`rgba\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(0?\.?\d+|1\.0*)\s*\)`)
var rgbRe = regexp.MustCompile(`rgb\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*\)`)
var rgbSpaceRe = regexp.MustCompile(`rgb\(\s*(\d{1,3})\s+(\d{1,3})\s+(\d{1,3})\s*(?:/\s*([0-9.]+)\s*)?\)`)
var calcRe = regexp.MustCompile(`calc\(([^()]*(?:\([^()]*\)[^()]*)*)\)`)
var trailingSemiRe = regexp.MustCompile(`;\s*\}`)

func Bundle(assets []*Asset) string {
	var b strings.Builder
	for _, a := range assets {
		if !a.IsModule {
			b.WriteString(a.Content)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func ExtractClassNames(css string) []string {
	seen := make(map[string]bool)
	matches := classSelector.FindAllStringSubmatch(css, -1)
	var result []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			result = append(result, m[1])
		}
	}
	sort.Strings(result)
	return result
}

func GenerateMapping(path string, classNames []string) map[string]string {
	hash := hashPath(path)
	mapping := make(map[string]string, len(classNames))
	for _, name := range classNames {
		mapping[name] = name + "_" + hash
	}
	return mapping
}

func ScopeCSS(content string, mapping map[string]string) string {
	return classSelector.ReplaceAllStringFunc(content, func(match string) string {
		name := match[1:]
		if scoped, ok := mapping[name]; ok {
			return "." + scoped
		}
		return match
	})
}

func VerifyMapping(mapping map[string]string) error {
	for orig, scoped := range mapping {
		if !strings.HasPrefix(scoped, orig+"_") {
			return fmt.Errorf("invalid scoped name %q for class %q", scoped, orig)
		}
	}
	return nil
}
