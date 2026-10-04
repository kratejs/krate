package css

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

// Hoisted matchers (compiled once, not per class). Kept as package vars for the
// hot generator path.
var (
	opacityRe     = regexp.MustCompile(`^opacity-(\d+)$`)
	leadingRe     = regexp.MustCompile(`^leading-(.+)$`)
	translateRe   = regexp.MustCompile(`^translate-([xy])-(.+)$`)
	rotateRe      = regexp.MustCompile(`^rotate-(.+)$`)
	skewRe        = regexp.MustCompile(`^skew-([xy])-(.+)$`)
	scaleAxisRe   = regexp.MustCompile(`^scale-([xy])-(.+)$`)
	scaleRe       = regexp.MustCompile(`^scale-(.+)$`)
	originRe      = regexp.MustCompile(`^origin-(.+)$`)
	roundedRe     = regexp.MustCompile(`^rounded-([a-zA-Z0-9]+)$`)
	roundedSideRe = regexp.MustCompile(`^rounded-(tl|tr|bl|br|ss|se|es|ee|t|r|b|l|s|e)-([a-zA-Z0-9]+)$`)
	maxWRe        = regexp.MustCompile(`^max-w-(.+)$`)
	minWRe        = regexp.MustCompile(`^min-w-(.+)$`)
	maxHRe        = regexp.MustCompile(`^max-h-(.+)$`)
	minHRe        = regexp.MustCompile(`^min-h-(.+)$`)
	spaceXRe      = regexp.MustCompile(`^space-x-(.+)$`)
	spaceYRe      = regexp.MustCompile(`^space-y-(.+)$`)
	gridColsRe    = regexp.MustCompile(`^grid-cols-(\d+)$`)
	colSpanRe     = regexp.MustCompile(`^col-span-(\d+)$`)
)

// TailwindScanner extracts Tailwind utility class names from source files.
type TailwindScanner struct {
	Root string
}

// NewTailwindScanner creates a scanner for the given project root.
func NewTailwindScanner(root string) *TailwindScanner {
	return &TailwindScanner{Root: root}
}

// ScanClasses walks the project and extracts every Tailwind class candidate
// used in the scanned files. It is a broad candidate extractor (matching real
// Tailwind's approach): every string and template literal is scanned and its
// tokens filtered by shape, so classes inside `class={cond ? "a" : "b"}`,
// `clsx()/cn()/cva()` arguments, and arrays are all found.
func (s *TailwindScanner) ScanClasses(dirs []string) map[string]bool {
	classes := make(map[string]bool)
	exts := map[string]bool{
		".tsx": true, ".jsx": true, ".ts": true, ".js": true,
		".mdx": true, ".md": true, ".html": true, ".mjs": true, ".cjs": true,
	}
	skipDirs := map[string]bool{
		"node_modules": true, ".git": true, "dist": true, "out": true,
		".krate": true, "build": true, ".next": true, "coverage": true,
	}

	for _, dir := range dirs {
		_ = fsutil.WalkExt(dir, exts, skipDirs, func(path string, _ os.FileInfo) error {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			for _, tok := range candidateTokens(string(data)) {
				classes[tok] = true
			}
			return nil
		})
	}

	return classes
}

// candidateTokens extracts Tailwind-looking tokens from source text. It scans
// every string literal ('...', "...") and template literal (`...`, with static
// segments only) and keeps whitespace-separated tokens that look like utility
// classes (optionally variant-prefixed, possibly with `/`, `[`, `]`, `.`, `-`).
func candidateTokens(src string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		for _, tok := range strings.Fields(s) {
			tok = strings.Trim(tok, "\"'`,;(){}")
			if tok == "" || seen[tok] || !looksLikeUtility(tok) {
				continue
			}
			seen[tok] = true
			out = append(out, tok)
		}
	}
	for _, lit := range stringLiterals(src) {
		add(lit)
	}
	return out
}

// looksLikeUtility is a conservative shape filter so prose and code identifiers
// are not emitted as utilities. A candidate must consist only of characters
// valid in a utility (letters, digits, `- _ / . [ ] % ( ) # ! :` plus `,` for
// arbitrary values) and must contain a `-` (all utilities are hyphenated or
// standalone keywords).
func looksLikeUtility(tok string) bool {
	if len(tok) == 0 || len(tok) > 200 {
		return false
	}
	// Reject obvious non-classes.
	if strings.ContainsAny(tok, "\n\t ") {
		return false
	}
	hasHyphen := false
	depth := 0
	for _, r := range tok {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-':
			hasHyphen = true
		case r == '_' || r == '/' || r == '.' || r == '%' || r == '#' || r == '!':
		case r == '@' || r == '*':
			// Container-query variants (@sm:) and child variants (*:, **:).
		case r == '[':
			depth++
		case r == ']':
			if depth == 0 {
				return false
			}
			depth--
		case r == '(' || r == ')':
			// allowed inside arbitrary values
		case r == ':':
			// variant separator
		case r == ',':
			// arbitrary value list
		default:
			return false
		}
	}
	if depth != 0 {
		return false
	}
	if !hasHyphen {
		// The token may be a variant-prefixed standalone keyword (`sm:flex`,
		// `hover:block`, `dark:hidden`); check the base after the last colon.
		base := tok
		if i := strings.LastIndexByte(tok, ':'); i >= 0 {
			base = tok[i+1:]
		}
		return bareUtilities[base]
	}
	return true
}

// bareUtilities is the set of valid Tailwind utilities that contain no hyphen.
// The scanner needs it because a hyphen-less token is otherwise indistinguishable
// from a prose word or identifier.
var bareUtilities = map[string]bool{
	"flex": true, "grid": true, "block": true, "inline": true, "hidden": true,
	"contents": true, "static": true, "fixed": true, "absolute": true,
	"relative": true, "sticky": true, "visible": true, "invisible": true,
	"underline": true, "overline": true, "truncate": true, "italic": true,
	"not-italic": true, "uppercase": true, "lowercase": true, "capitalize": true,
	"normal-case": true, "container": true, "sr-only": true, "not-sr-only": true,
	"ring": true, "border": true, "shadow": true, "rounded": true,
	"transform": true, "antialiased": true, "subpixel-antialiased": true,
	"inline-block": true, "inline-flex": true, "inline-grid": true,
	"inline-table": true, "table": true, "flow-root": true, "list-item": true,
	"isolate": true, "grayscale": true, "sepia": true, "invert": true,
	"grow": true, "shrink": true, "resize": true, "filter": true, "outline": true,
	"ordinal": true, "slashed-zero": true, "lining-nums": true,
	"oldstyle-nums": true, "proportional-nums": true, "tabular-nums": true,
	"diagonal-fractions": true, "stacked-fractions": true, "hyphens": true,
}

// stringLiterals returns the contents of every string and template literal in
// src, with `${...}` interpolations stripped from template bodies. Escapes are
// not fully unwound; the shape filter tolerates that.
func stringLiterals(src string) []string {
	var out []string
	i, n := 0, len(src)
	for i < n {
		c := src[i]
		// Skip line comments.
		if c == '/' && i+1 < n && src[i+1] == '/' {
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		}
		// Skip block comments.
		if c == '/' && i+1 < n && src[i+1] == '*' {
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		if c == '"' || c == '\'' || c == '`' {
			quote := c
			i++
			start := i
			var sb strings.Builder
			for i < n && src[i] != quote {
				if src[i] == '\\' && i+1 < n {
					sb.WriteByte(src[i])
					sb.WriteByte(src[i+1])
					i += 2
					continue
				}
				if quote == '`' && src[i] == '$' && i+1 < n && src[i+1] == '{' {
					// Skip the interpolation (nested braces).
					i += 2
					depth := 1
					for i < n && depth > 0 {
						switch src[i] {
						case '{':
							depth++
						case '}':
							depth--
						}
						i++
					}
					sb.WriteByte(' ')
					continue
				}
				sb.WriteByte(src[i])
				i++
			}
			_ = start
			out = append(out, sb.String())
			i++ // closing quote
			continue
		}
		i++
	}
	return out
}

// TailwindGenerator converts class names to CSS rules.
type TailwindGenerator struct {
	Theme TailwindTheme
	// Strict, when true, collects classes that produced no rule.
	Strict bool
	// Unknown collects class names that yielded no rule (populated when Strict).
	Unknown []string
}

// NewTailwindGenerator creates a generator with default theme.
func NewTailwindGenerator() *TailwindGenerator {
	return &TailwindGenerator{
		Theme: DefaultTailwindTheme(),
	}
}

// generatedRule is one emitted rule with its sort key for deterministic output.
type generatedRule struct {
	// key orders rules deterministically: variant depth, media, selector.
	key  string
	text string
}

// Generate produces CSS for the given set of class names. Output is
// deterministic: rules are sorted by a stable key so identical inputs always
// produce byte-identical stylesheets (stable content hashing).
func (g *TailwindGenerator) Generate(classes map[string]bool) string {
	if len(classes) == 0 {
		return ""
	}

	// Deduplicate base class names (the scanner may return pre-split tokens).
	seen := make(map[string]bool)
	var rules []generatedRule

	for cls := range classes {
		for _, c := range strings.Fields(cls) {
			if c == "" || seen[c] {
				continue
			}
			seen[c] = true

			text, key := classToRule(c, g.Theme)
			if text == "" {
				if g.Strict {
					g.Unknown = append(g.Unknown, c)
				}
				continue
			}
			rules = append(rules, generatedRule{key: key, text: text})
		}
	}

	if len(rules) == 0 {
		return ""
	}

	sort.Slice(rules, func(i, j int) bool {
		if rules[i].key != rules[j].key {
			return rules[i].key < rules[j].key
		}
		return rules[i].text < rules[j].text
	})

	var b strings.Builder
	for _, r := range rules {
		b.WriteString(r.text)
		b.WriteByte('\n')
	}
	return b.String()
}

// classToRule converts a single Tailwind class to its CSS rule text plus a
// deterministic sort key. An empty text means "unrecognized".
func classToRule(cls string, theme TailwindTheme) (text, key string) {
	important := strings.Contains(cls, "!")
	variantTokens, base := parseVariants(cls)
	base = strings.TrimPrefix(base, "!")
	if base == "" {
		return "", ""
	}

	css := generateCSS(base, theme)
	if css == "" && strings.HasPrefix(base, "-") {
		// Negative utility: generate the positive form, then negate numeric values.
		css = negateCSS(strings.TrimPrefix(base, "-"), theme)
	}
	if css == "" || !validDeclarationBlock(css) {
		return "", ""
	}
	if important {
		css = addImportant(css)
	}

	// Resolve every variant; any unknown variant means we cannot faithfully
	// represent the class, so skip it rather than emit it unconditionally.
	var variants []variant
	for _, vt := range variantTokens {
		v, ok := resolveVariant(vt, theme)
		if !ok {
			return "", ""
		}
		variants = append(variants, v)
	}

	// The before/after variants inject the pseudo-element content hook so that
	// `content-*` utilities (which set --tw-content) take effect.
	if usesContentHook(variants) && !strings.Contains(css, "content:") {
		css = "content: var(--tw-content); " + strings.TrimSpace(css)
	}

	sel, ats := applyVariants(EscapeClass(cls), variants)
	// Utilities that target child/sibling/pseudo elements append a suffix.
	if suffix := selectorSuffix(base); suffix != "" {
		if strings.HasPrefix(suffix, "::") {
			sel += suffix
		} else {
			sel += " " + suffix
		}
	}
	text = wrapAtRules(sel, strings.TrimSpace(css), ats)

	// Sort key reproduces Tailwind's cascade order (base before variants,
	// responsive ascending) so later, more specific utilities win.
	key = variantOrderKey(cls, variants, ats)
	return text, key
}

// borderWidthCSS resolves a border width utility (sides and shorthands) so all
// forms emit a consistent, visible border (width + solid style). It only matches
// width forms: `border`, `border-2`, `border-t`, `border-t-2`, `border-t-[3px]`,
// `border-x-2`, etc. — never colors (`border-red-500`) or styles.
func borderWidthCSS(cls string) string {
	width := func(v string) string {
		if v == "" {
			return "1px"
		}
		if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
			return v[1 : len(v)-1]
		}
		return v + "px"
	}
	isWidthValue := func(v string) bool {
		if v == "" {
			return true
		}
		if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
			return true
		}
		for _, r := range v {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	sideProps := map[string]string{
		"t": "border-top-width", "r": "border-right-width",
		"b": "border-bottom-width", "l": "border-left-width",
		"s": "border-inline-start-width", "e": "border-inline-end-width",
	}
	emit := func(prop, w string) string {
		if w == "0" || w == "0px" {
			return prop + ": 0;"
		}
		return prop + ": " + w + "; border-style: solid;"
	}

	if cls == "border" {
		return "border-width: 1px; border-style: solid;"
	}
	rest, ok := strings.CutPrefix(cls, "border-")
	if !ok {
		return ""
	}

	// Axis shorthand: x / y, optionally `-<width>`.
	for _, ax := range []string{"x", "y"} {
		if rest == ax {
			return emit2Axis(ax, "1px")
		}
		if after, ok := strings.CutPrefix(rest, ax+"-"); ok {
			if isWidthValue(after) {
				return emit2Axis(ax, width(after))
			}
			return ""
		}
	}

	// Single side: `t`, `r`, `b`, `l`, `s`, `e`, optionally `-<width>`.
	if len(rest) >= 1 {
		if prop, ok := sideProps[rest[:1]]; ok {
			tail := rest[1:]
			if tail == "" {
				return emit(prop, "1px")
			}
			if after, ok := strings.CutPrefix(tail, "-"); ok && isWidthValue(after) {
				return emit(prop, width(after))
			}
			return ""
		}
	}

	// Bare numeric / arbitrary width on all sides: `border-2`, `border-[3px]`.
	if isWidthValue(rest) {
		w := width(rest)
		if w == "0" || w == "0px" {
			return "border-width: 0;"
		}
		return "border-width: " + w + "; border-style: solid;"
	}
	return ""
}

// emit2Axis emits the left/right or top/bottom width pair.
func emit2Axis(axis, w string) string {
	if axis == "x" {
		return "border-left-width: " + w + "; border-right-width: " + w + "; border-style: solid;"
	}
	return "border-top-width: " + w + "; border-bottom-width: " + w + "; border-style: solid;"
}

// validDeclarationBlock reports whether every declaration in a `prop: value;`
// string has a plausible value. This is a safety net: a resolver that fails to
// recognize an arbitrary key returns it verbatim (e.g. `margin: project`), which
// would emit invalid CSS. Such declarations are rejected instead.
func validDeclarationBlock(css string) bool {
	for _, decl := range strings.Split(css, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		prop, val, ok := strings.Cut(decl, ":")
		if !ok || strings.TrimSpace(prop) == "" {
			return false
		}
		val = strings.TrimSpace(val)
		if val == "" {
			return false
		}
		if !validCSSValue(val) {
			return false
		}
	}
	return true
}

// validCSSValue rejects bare identifier values that are not valid keywords, and
// values containing characters that cannot appear in a CSS value.
func validCSSValue(val string) bool {
	// A value must not contain braces/angle brackets. Quotes are permitted:
	// quoted font stacks and content values are valid declaration values and
	// are written to a stylesheet (never interpolated into HTML or a selector).
	if strings.ContainsAny(val, "{}<>") {
		return false
	}
	// A bare word (no digits, units, %, functions, or punctuation) is valid
	// only if it is shaped like a CSS keyword: a lowercase identifier made of
	// letters, digits, and hyphens, optionally with a leading vendor prefix.
	// Resolvers no longer echo unknown keys, so this shape check is the safety
	// net against malformed values like `margin: project` reaching output via
	// some future path.
	if isBareKeyword(val) {
		if keywordWhitelist[val] {
			return true
		}
		// Accept any identifier-shaped keyword (e.g. `isolate`, `break-spaces`,
		// `tabular-nums`, `preserve-3d`) — CSS keywords are lowercase idents.
		return cssIdentifierRe.MatchString(val)
	}
	return true
}

// isBareKeyword reports whether val contains no digits, units, %, functions or
// other value punctuation — i.e. it can only be a keyword or identifier.
func isBareKeyword(val string) bool {
	for _, r := range val {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '-', r == '_':
			// identifier characters
		default:
			return false
		}
	}
	return val != ""
}

var cssIdentifierRe = regexp.MustCompile(`^(-{1,2})?[a-zA-Z][a-zA-Z0-9_-]*$`)

// keywordWhitelist holds the bare values that are explicitly recognized; kept
// for documentation and for rejecting mixed-case non-keywords. The identifier
// shape check above is the actual gate.
var keywordWhitelist = map[string]bool{
	"auto": true, "none": true, "inherit": true, "initial": true, "unset": true,
	"revert": true, "transparent": true, "currentColor": true, "currentcolor": true,
	"solid": true, "dashed": true, "dotted": true, "double": true, "hidden": true,
	"visible": true, "collapse": true, "separate": true, "normal": true,
}

// negateCSS generates the positive form of a utility and negates the numeric
// values in its declarations (for `-mt-4`, `-translate-x-1/2`, `-top-2`, …).
func negateCSS(positive string, theme TailwindTheme) string {
	css := generateCSS(positive, theme)
	if css == "" {
		return ""
	}
	// Negate the first length/number token after each `:`.
	var parts []string
	for _, decl := range strings.Split(css, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		prop, val, ok := strings.Cut(decl, ":")
		if !ok {
			parts = append(parts, decl)
			continue
		}
		val = strings.TrimSpace(val)
		if isLengthLike(val) {
			val = negateValue(val)
		}
		parts = append(parts, strings.TrimSpace(prop)+": "+val)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ") + ";"
}

// isLengthLike reports whether a value is a signed+units length or number that
// can be negated (not a color, keyword, or function like calc()).
func isLengthLike(v string) bool {
	if v == "" {
		return false
	}
	if strings.ContainsAny(v, "()#,\"' ") {
		return false
	}
	for _, bad := range []string{"auto", "none", "inherit", "currentcolor", "transparent", "normal"} {
		if strings.EqualFold(v, bad) {
			return false
		}
	}
	// Must start with a digit, dot, or minus and contain at least one digit.
	hasDigit := false
	for _, r := range v {
		if r >= '0' && r <= '9' {
			hasDigit = true
			break
		}
	}
	return hasDigit && (v[0] == '-' || v[0] == '.' || (v[0] >= '0' && v[0] <= '9'))
}

// translateValue resolves a translate-* value: fractions map to percentages and
// everything else through the spacing scale.
func translateValue(key string, theme TailwindTheme) string {
	if pct, ok := fractionPercent(key); ok {
		return pct
	}
	if strings.HasPrefix(key, "[") && strings.HasSuffix(key, "]") {
		return key[1 : len(key)-1]
	}
	return spacingValue(key, theme)
}

// fractionPercent converts an "a/b" fraction key to a percentage string.
func fractionPercent(key string) (string, bool) {
	i := strings.IndexByte(key, '/')
	if i <= 0 {
		return "", false
	}
	num, err1 := strconv.ParseFloat(key[:i], 64)
	den, err2 := strconv.ParseFloat(key[i+1:], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return "", false
	}
	pct := num / den * 100
	// Tailwind rounds repeating fractions to 6 decimal places (1/3 → 33.333333%).
	s := strconv.FormatFloat(pct, 'f', 6, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s + "%", true
}

// transformCompose is the shared transform value built from the --tw-* variables
// so multiple transform utilities compose instead of clobbering each other.
const transformCompose = "translate(var(--tw-translate-x,0),var(--tw-translate-y,0)) rotate(var(--tw-rotate,0)) skewX(var(--tw-skew-x,0)) skewY(var(--tw-skew-y,0)) scaleX(var(--tw-scale-x,1)) scaleY(var(--tw-scale-y,1))"

// shadowCompose emits the box-shadow from the composed shadow/ring variables so
// shadows and rings stack and a `shadow-<color>` utility can override the color.
const shadowCompose = "box-shadow: var(--tw-ring-offset-shadow, 0 0 #0000), var(--tw-ring-shadow, 0 0 #0000), var(--tw-shadow);"

// shadowColorRe matches the color components of a shadow value so they can be
// swapped for `var(--tw-shadow-color)`.
var shadowColorRe = regexp.MustCompile(`rgba?\([^)]*\)|hsla?\([^)]*\)|#[0-9a-fA-F]{3,8}`)

// shadowRule emits an (uncolored) shadow, plus its colorable variant.
func shadowRule(val string) string {
	colored := shadowColorRe.ReplaceAllString(val, "var(--tw-shadow-color)")
	return "--tw-shadow: " + val + "; --tw-shadow-colored: " + colored + "; " + shadowCompose
}

// shadowColorRule applies a shadow color and switches to the colorable shadow.
func shadowColorRule(color string) string {
	return "--tw-shadow-color: " + color + "; --tw-shadow: var(--tw-shadow-colored); " + shadowCompose
}

// usesContentHook reports whether any variant is the ::before/::after
// pseudo-element, which requires the `content` property to render.
func usesContentHook(variants []variant) bool {
	for _, v := range variants {
		if v.kind == "pseudo" && (v.sel == "::before" || v.sel == "::after") {
			return true
		}
	}
	return false
}

// selectorSuffix returns the descendant/sibling selector a utility applies to,
// or "" when the utility styles the element itself. Used for space-*, divide-*.
func selectorSuffix(base string) string {
	// Reverse flags set a custom property on the container itself; the sibling
	// suffix must not apply to them.
	if base == "space-x-reverse" || base == "space-y-reverse" ||
		base == "divide-x-reverse" || base == "divide-y-reverse" {
		return ""
	}
	switch {
	case strings.HasPrefix(base, "space-x-"), strings.HasPrefix(base, "space-y-"),
		base == "divide-x", base == "divide-y",
		strings.HasPrefix(base, "divide-x-"), strings.HasPrefix(base, "divide-y-"):
		return "> :not([hidden]) ~ :not([hidden])"
	case strings.HasPrefix(base, "placeholder-"):
		return "::placeholder"
	}
	return ""
}

// radiusValue resolves a border-radius scale key. "" is the `rounded` default.
func radiusValue(key string, theme TailwindTheme) string {
	if v, ok := theme.Radii[key]; ok {
		return v
	}
	if strings.HasPrefix(key, "[") && strings.HasSuffix(key, "]") {
		return key[1 : len(key)-1]
	}
	return ""
}

// maxWidthValue resolves a max-w-* key against the max-width scale, then the
// sizing scale, then arbitrary values.
func maxWidthValue(key string, theme TailwindTheme) string {
	if v, ok := theme.MaxWidth[key]; ok {
		return v
	}
	return sizingValue(key, theme)
}

// minWidthValue resolves a min-w-* key against the min-width scale.
func minWidthValue(key string, theme TailwindTheme) string {
	if v, ok := theme.MinWidth[key]; ok {
		return v
	}
	return sizingValue(key, theme)
}

// addImportant appends !important to every declaration in a CSS declaration
// block (the `!` prefix form).
func addImportant(css string) string {
	parts := strings.Split(css, ";")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p+" !important")
	}
	return strings.Join(out, "; ") + ";"
}

func generateCSS(cls string, theme TailwindTheme) string {
	// Display
	if cls == "block" {
		return "display: block;"
	}
	if cls == "inline-block" {
		return "display: inline-block;"
	}
	if cls == "inline" {
		return "display: inline;"
	}
	if cls == "flex" {
		return "display: flex;"
	}
	if cls == "inline-flex" {
		return "display: inline-flex;"
	}
	if cls == "grid" {
		return "display: grid;"
	}
	if cls == "inline-grid" {
		return "display: inline-grid;"
	}
	if cls == "hidden" {
		return "display: none;"
	}
	if cls == "contents" {
		return "display: contents;"
	}

	// Flex direction
	if cls == "flex-row" {
		return "flex-direction: row;"
	}
	if cls == "flex-col" {
		return "flex-direction: column;"
	}
	if cls == "flex-row-reverse" {
		return "flex-direction: row-reverse;"
	}
	if cls == "flex-col-reverse" {
		return "flex-direction: column-reverse;"
	}
	if cls == "flex-wrap" {
		return "flex-wrap: wrap;"
	}
	if cls == "flex-wrap-reverse" {
		return "flex-wrap: wrap-reverse;"
	}
	if cls == "flex-nowrap" {
		return "flex-wrap: nowrap;"
	}
	if cls == "flex-1" {
		return "flex: 1 1 0%;"
	}
	if cls == "flex-auto" {
		return "flex: 1 1 auto;"
	}
	if cls == "flex-initial" {
		return "flex: 0 1 auto;"
	}
	if cls == "flex-none" {
		return "flex: none;"
	}

	// Flex order
	if match := regexp.MustCompile(`^order-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("order: %s;", match[1])
	}
	if cls == "order-first" {
		return "order: -9999;"
	}
	if cls == "order-last" {
		return "order: 9999;"
	}
	if cls == "order-none" {
		return "order: 0;"
	}

	// Justify content
	if cls == "justify-start" {
		return "justify-content: flex-start;"
	}
	if cls == "justify-end" {
		return "justify-content: flex-end;"
	}
	if cls == "justify-center" {
		return "justify-content: center;"
	}
	if cls == "justify-between" {
		return "justify-content: space-between;"
	}
	if cls == "justify-around" {
		return "justify-content: space-around;"
	}
	if cls == "justify-evenly" {
		return "justify-content: space-evenly;"
	}

	// Align items
	if cls == "items-start" {
		return "align-items: flex-start;"
	}
	if cls == "items-end" {
		return "align-items: flex-end;"
	}
	if cls == "items-center" {
		return "align-items: center;"
	}
	if cls == "items-baseline" {
		return "align-items: baseline;"
	}
	if cls == "items-stretch" {
		return "align-items: stretch;"
	}

	// Align self
	if cls == "self-auto" {
		return "align-self: auto;"
	}
	if cls == "self-start" {
		return "align-self: flex-start;"
	}
	if cls == "self-end" {
		return "align-self: flex-end;"
	}
	if cls == "self-center" {
		return "align-self: center;"
	}
	if cls == "self-baseline" {
		return "align-self: baseline;"
	}
	if cls == "self-stretch" {
		return "align-self: stretch;"
	}

	// Align content
	if cls == "content-center" {
		return "align-content: center;"
	}
	if cls == "content-start" {
		return "align-content: flex-start;"
	}
	if cls == "content-end" {
		return "align-content: flex-end;"
	}
	if cls == "content-between" {
		return "align-content: space-between;"
	}
	if cls == "content-around" {
		return "align-content: space-around;"
	}
	if cls == "content-evenly" {
		return "align-content: space-evenly;"
	}
	if cls == "content-baseline" {
		return "align-content: baseline;"
	}
	if cls == "content-stretch" {
		return "align-content: stretch;"
	}

	// Justify items
	if cls == "justify-items-start" {
		return "justify-items: start;"
	}
	if cls == "justify-items-end" {
		return "justify-items: end;"
	}
	if cls == "justify-items-center" {
		return "justify-items: center;"
	}
	if cls == "justify-items-stretch" {
		return "justify-items: stretch;"
	}

	// Justify self
	if cls == "justify-self-auto" {
		return "justify-self: auto;"
	}
	if cls == "justify-self-start" {
		return "justify-self: start;"
	}
	if cls == "justify-self-end" {
		return "justify-self: end;"
	}
	if cls == "justify-self-center" {
		return "justify-self: center;"
	}
	if cls == "justify-self-stretch" {
		return "justify-self: stretch;"
	}

	// Place items
	if cls == "place-items-center" {
		return "place-items: center;"
	}
	if cls == "place-items-start" {
		return "place-items: start;"
	}
	if cls == "place-items-end" {
		return "place-items: end;"
	}
	if cls == "place-items-stretch" {
		return "place-items: stretch;"
	}

	// Place content
	if cls == "place-content-center" {
		return "place-content: center;"
	}
	if cls == "place-content-start" {
		return "place-content: start;"
	}
	if cls == "place-content-end" {
		return "place-content: end;"
	}
	if cls == "place-content-between" {
		return "place-content: space-between;"
	}
	if cls == "place-content-around" {
		return "place-content: space-around;"
	}
	if cls == "place-content-evenly" {
		return "place-content: space-evenly;"
	}
	if cls == "place-content-stretch" {
		return "place-content: stretch;"
	}

	// Place self
	if cls == "place-self-auto" {
		return "place-self: auto;"
	}
	if cls == "place-self-start" {
		return "place-self: start;"
	}
	if cls == "place-self-end" {
		return "place-self: end;"
	}
	if cls == "place-self-center" {
		return "place-self: center;"
	}
	if cls == "place-self-stretch" {
		return "place-self: stretch;"
	}

	// Flex grow/shrink
	if cls == "flex-grow" || cls == "grow" {
		return "flex-grow: 1;"
	}
	if cls == "flex-grow-0" || cls == "grow-0" {
		return "flex-grow: 0;"
	}
	if cls == "flex-shrink" || cls == "shrink" {
		return "flex-shrink: 1;"
	}
	if cls == "flex-shrink-0" || cls == "shrink-0" {
		return "flex-shrink: 0;"
	}

	// Space between: sibling combinator, not the container itself.
	if cls == "space-x-reverse" {
		return "--tw-space-x-reverse: 1;"
	}
	if cls == "space-y-reverse" {
		return "--tw-space-y-reverse: 1;"
	}
	if match := spaceXRe.FindStringSubmatch(cls); match != nil {
		v := spacingValue(match[1], theme)
		return "margin-right: calc(" + v + " * var(--tw-space-x-reverse,0)); margin-left: calc(" + v + " * calc(1 - var(--tw-space-x-reverse,0)));"
	}
	if match := spaceYRe.FindStringSubmatch(cls); match != nil {
		v := spacingValue(match[1], theme)
		return "margin-top: calc(" + v + " * calc(1 - var(--tw-space-y-reverse,0))); margin-bottom: calc(" + v + " * var(--tw-space-y-reverse,0));"
	}

	// Gap
	if match := regexp.MustCompile(`^gap-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("gap: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^gap-x-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("column-gap: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^gap-y-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("row-gap: %s;", spacingValue(match[1], theme))
	}

	// Grid columns/rows and spans (1..12 plus full and arbitrary templates).
	if match := gridColsRe.FindStringSubmatch(cls); match != nil {
		return "grid-template-columns: repeat(" + match[1] + ", minmax(0, 1fr));"
	}
	if v, ok := unwrapArbitrary(strings.TrimPrefix(cls, "grid-cols-")); ok && strings.HasPrefix(cls, "grid-cols-") {
		return "grid-template-columns: " + v + ";"
	}
	if match := regexp.MustCompile(`^grid-rows-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return "grid-template-rows: repeat(" + match[1] + ", minmax(0, 1fr));"
	}
	if v, ok := unwrapArbitrary(strings.TrimPrefix(cls, "grid-rows-")); ok && strings.HasPrefix(cls, "grid-rows-") {
		return "grid-template-rows: " + v + ";"
	}
	if cls == "col-auto" {
		return "grid-column: auto;"
	}
	if cls == "row-auto" {
		return "grid-row: auto;"
	}
	if match := colSpanRe.FindStringSubmatch(cls); match != nil {
		return "grid-column: span " + match[1] + " / span " + match[1] + ";"
	}
	if cls == "col-span-full" {
		return "grid-column: 1 / -1;"
	}
	if match := regexp.MustCompile(`^row-span-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return "grid-row: span " + match[1] + " / span " + match[1] + ";"
	}
	if cls == "row-span-full" {
		return "grid-row: 1 / -1;"
	}
	if match := regexp.MustCompile(`^col-start-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return "grid-column-start: " + match[1] + ";"
	}
	if match := regexp.MustCompile(`^col-end-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return "grid-column-end: " + match[1] + ";"
	}
	if match := regexp.MustCompile(`^row-start-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return "grid-row-start: " + match[1] + ";"
	}
	if match := regexp.MustCompile(`^row-end-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return "grid-row-end: " + match[1] + ";"
	}
	if cls == "grid-flow-row" {
		return "grid-auto-flow: row;"
	}
	if cls == "grid-flow-col" {
		return "grid-auto-flow: column;"
	}
	if cls == "grid-flow-dense" {
		return "grid-auto-flow: dense;"
	}
	if cls == "grid-flow-row-dense" {
		return "grid-auto-flow: row dense;"
	}
	if cls == "grid-flow-col-dense" {
		return "grid-auto-flow: column dense;"
	}
	if match := regexp.MustCompile(`^auto-cols-(auto|min|max|fr)$`).FindStringSubmatch(cls); match != nil {
		return "grid-auto-columns: " + match[1] + ";"
	}
	if match := regexp.MustCompile(`^auto-rows-(auto|min|max|fr)$`).FindStringSubmatch(cls); match != nil {
		return "grid-auto-rows: " + match[1] + ";"
	}
	if match := regexp.MustCompile(`^basis-(.+)$`).FindStringSubmatch(cls); match != nil {
		return "flex-basis: " + sizingValue(match[1], theme) + ";"
	}

	// Padding
	if match := regexp.MustCompile(`^p-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("padding: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^px-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("padding-left: %s; padding-right: %s;", spacingValue(match[1], theme), spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^py-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("padding-top: %s; padding-bottom: %s;", spacingValue(match[1], theme), spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^pt-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("padding-top: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^pr-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("padding-right: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^pb-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("padding-bottom: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^pl-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("padding-left: %s;", spacingValue(match[1], theme))
	}

	// Margin
	if match := regexp.MustCompile(`^m-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^mx-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin-left: %s; margin-right: %s;", spacingValue(match[1], theme), spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^my-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin-top: %s; margin-bottom: %s;", spacingValue(match[1], theme), spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^mt-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin-top: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^mr-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin-right: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^mb-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin-bottom: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^ml-([a-zA-Z0-9.]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("margin-left: %s;", spacingValue(match[1], theme))
	}

	// Width
	if match := regexp.MustCompile(`^w-(.+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("width: %s;", widthValue(match[1], theme))
	}
	if match := minWRe.FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("min-width: %s;", minWidthValue(match[1], theme))
	}
	if match := maxWRe.FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("max-width: %s;", maxWidthValue(match[1], theme))
	}

	// Height (`screen` is 100vh here, not the 100vw used by width).
	if match := regexp.MustCompile(`^h-(.+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("height: %s;", heightValue(match[1], theme))
	}
	if match := minHRe.FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("min-height: %s;", heightValue(match[1], theme))
	}
	if match := maxHRe.FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("max-height: %s;", heightValue(match[1], theme))
	}

	// Size shorthand: size-4 → width + height.
	if match := regexp.MustCompile(`^size-(.+)$`).FindStringSubmatch(cls); match != nil {
		v := sizingValue(match[1], theme)
		return fmt.Sprintf("width: %s; height: %s;", v, v)
	}

	// Text: color (text-gray-600, text-white/70) or arbitrary font-size /
	// font-family, disambiguated by the value's shape (text-[14px] is a size).
	if match := regexp.MustCompile(`^text-(.+)$`).FindStringSubmatch(cls); match != nil {
		key := match[1]
		if inner, hint, isArb := arbitraryValue(key); isArb {
			switch hint {
			case "color":
				return "color: " + inner + ";"
			case "length", "size", "percentage":
				return "font-size: " + inner + ";"
			case "family":
				return "font-family: " + inner + ";"
			}
			if looksLikeColor(inner) {
				return "color: " + inner + ";"
			}
			if looksLikeLength(inner) {
				return "font-size: " + inner + ";"
			}
			return "font-size: " + inner + ";"
		}
		colorKey, alpha := splitColorModifier(key)
		if v, ok := colorValue(colorKey, alpha, theme); ok {
			return "color: " + v + ";"
		}
	}

	// Text alignment
	if cls == "text-left" {
		return "text-align: left;"
	}
	if cls == "text-center" {
		return "text-align: center;"
	}
	if cls == "text-right" {
		return "text-align: right;"
	}
	if cls == "text-justify" {
		return "text-align: justify;"
	}

	// Text decoration
	if cls == "underline" {
		return "text-decoration: underline;"
	}
	if cls == "line-through" {
		return "text-decoration: line-through;"
	}
	if cls == "no-underline" {
		return "text-decoration: none;"
	}

	// Text transform
	if cls == "uppercase" {
		return "text-transform: uppercase;"
	}
	if cls == "lowercase" {
		return "text-transform: lowercase;"
	}
	if cls == "capitalize" {
		return "text-transform: capitalize;"
	}
	if cls == "normal-case" {
		return "text-transform: none;"
	}

	// Vertical align
	if cls == "align-baseline" {
		return "vertical-align: baseline;"
	}
	if cls == "align-top" {
		return "vertical-align: top;"
	}
	if cls == "align-middle" {
		return "vertical-align: middle;"
	}
	if cls == "align-bottom" {
		return "vertical-align: bottom;"
	}
	if cls == "align-text-top" {
		return "vertical-align: text-top;"
	}
	if cls == "align-text-bottom" {
		return "vertical-align: text-bottom;"
	}

	// Font weight
	if match := regexp.MustCompile(`^font-([a-z]+)$`).FindStringSubmatch(cls); match != nil {
		if val, ok := theme.FontWeights[match[1]]; ok {
			return fmt.Sprintf("font-weight: %s;", val)
		}
	}

	// Font family
	if cls == "font-sans" {
		return "font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, \"Segoe UI\", Roboto, \"Helvetica Neue\", Arial, \"Noto Sans\", sans-serif, \"Apple Color Emoji\", \"Segoe UI Emoji\", \"Segoe UI Symbol\", \"Noto Color Emoji\";"
	}
	if cls == "font-serif" {
		return "font-family: ui-serif, Georgia, Cambria, \"Times New Roman\", Times, serif;"
	}
	if cls == "font-mono" {
		return "font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, \"Liberation Mono\", \"Courier New\", monospace;"
	}

	// Line height: numeric maps through the spacing scale (leading-6 → 1.5rem).
	if match := leadingRe.FindStringSubmatch(cls); match != nil {
		key := match[1]
		if v, ok := theme.LineHeight[key]; ok {
			return "line-height: " + v + ";"
		}
		if v, ok := theme.Spacing[key]; ok {
			return "line-height: " + v + ";"
		}
		if v, ok := unwrapArbitrary(key); ok {
			return "line-height: " + v + ";"
		}
	}

	// Letter spacing (named scale plus arbitrary values).
	if match := regexp.MustCompile(`^tracking-([a-z]+)$`).FindStringSubmatch(cls); match != nil {
		switch match[1] {
		case "tighter":
			return "letter-spacing: -0.05em;"
		case "tight":
			return "letter-spacing: -0.025em;"
		case "normal":
			return "letter-spacing: 0em;"
		case "wide":
			return "letter-spacing: 0.025em;"
		case "wider":
			return "letter-spacing: 0.05em;"
		case "widest":
			return "letter-spacing: 0.1em;"
		}
	}
	if v, ok := unwrapArbitrary(strings.TrimPrefix(cls, "tracking-")); ok && strings.HasPrefix(cls, "tracking-") {
		return "letter-spacing: " + v + ";"
	}

	// List style
	if cls == "list-none" {
		return "list-style: none;"
	}
	if cls == "list-disc" {
		return "list-style: disc;"
	}
	if cls == "list-decimal" {
		return "list-style: decimal;"
	}
	if cls == "list-inside" {
		return "list-style-position: inside;"
	}
	if cls == "list-outside" {
		return "list-style-position: outside;"
	}

	// Background attachment
	if cls == "bg-fixed" {
		return "background-attachment: fixed;"
	}
	if cls == "bg-local" {
		return "background-attachment: local;"
	}
	if cls == "bg-scroll" {
		return "background-attachment: scroll;"
	}

	// Background position
	if cls == "bg-bottom" {
		return "background-position: bottom;"
	}
	if cls == "bg-center" {
		return "background-position: center;"
	}
	if cls == "bg-left" {
		return "background-position: left;"
	}
	if cls == "bg-left-bottom" {
		return "background-position: left bottom;"
	}
	if cls == "bg-left-top" {
		return "background-position: left top;"
	}
	if cls == "bg-right" {
		return "background-position: right;"
	}
	if cls == "bg-right-bottom" {
		return "background-position: right bottom;"
	}
	if cls == "bg-right-top" {
		return "background-position: right top;"
	}
	if cls == "bg-top" {
		return "background-position: top;"
	}

	// Background size
	if cls == "bg-cover" {
		return "background-size: cover;"
	}
	if cls == "bg-contain" {
		return "background-size: contain;"
	}
	if cls == "bg-auto" {
		return "background-size: auto;"
	}

	// Background repeat
	if cls == "bg-repeat" {
		return "background-repeat: repeat;"
	}
	if cls == "bg-no-repeat" {
		return "background-repeat: no-repeat;"
	}
	if cls == "bg-repeat-x" {
		return "background-repeat: repeat-x;"
	}
	if cls == "bg-repeat-y" {
		return "background-repeat: repeat-y;"
	}
	if cls == "bg-repeat-round" {
		return "background-repeat: round;"
	}
	if cls == "bg-repeat-space" {
		return "background-repeat: space;"
	}

	// Background: color (bg-blue-500, bg-blue-500/50, bg-white) or arbitrary
	// image/size/position, selected by the arbitrary value's shape/hint.
	if match := regexp.MustCompile(`^bg-(.+)$`).FindStringSubmatch(cls); match != nil {
		key := match[1]
		if inner, hint, isArb := arbitraryValue(key); isArb {
			switch hint {
			case "image", "url":
				return "background-image: " + inner + ";"
			case "length", "size":
				return "background-size: " + inner + ";"
			case "position":
				return "background-position: " + inner + ";"
			case "color":
				return "background-color: " + inner + ";"
			}
			if strings.HasPrefix(strings.ToLower(inner), "url(") ||
				strings.HasPrefix(strings.ToLower(inner), "linear-gradient(") ||
				strings.HasPrefix(strings.ToLower(inner), "radial-gradient(") ||
				strings.HasPrefix(strings.ToLower(inner), "conic-gradient(") {
				return "background-image: " + inner + ";"
			}
			if looksLikeColor(inner) {
				return "background-color: " + inner + ";"
			}
			if looksLikeLength(inner) {
				return "background-size: " + inner + ";"
			}
			return "background-color: " + inner + ";"
		}
		colorKey, alpha := splitColorModifier(key)
		if v, ok := colorValue(colorKey, alpha, theme); ok {
			return "background-color: " + v + ";"
		}
	}

	// Border family, resolved with explicit precedence: width (sides/shorthand)
	// then color then style. Every width emits border-style so a visible border
	// results even without Preflight (matches Tailwind's `border` default).
	if css := borderWidthCSS(cls); css != "" {
		return css
	}
	if match := regexp.MustCompile(`^border-(.+)$`).FindStringSubmatch(cls); match != nil {
		colorKey, alpha := splitColorModifier(match[1])
		if v, ok := colorValue(colorKey, alpha, theme); ok {
			return "border-color: " + v + ";"
		}
	}
	// Border style
	if cls == "border-solid" {
		return "border-style: solid;"
	}
	if cls == "border-dashed" {
		return "border-style: dashed;"
	}
	if cls == "border-dotted" {
		return "border-style: dotted;"
	}
	if cls == "border-double" {
		return "border-style: double;"
	}
	if cls == "border-none" {
		return "border-style: none;"
	}

	// Outline
	if cls == "outline-none" {
		return "outline: 2px solid transparent; outline-offset: 2px;"
	}
	if cls == "outline" {
		return "outline-style: solid;"
	}
	if match := regexp.MustCompile(`^outline-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("outline-width: %spx;", match[1])
	}

	// Border radius (base). `rounded` (no suffix) → theme default ("").
	if cls == "rounded" {
		return "border-radius: " + radiusValue("", theme) + ";"
	}
	if match := roundedRe.FindStringSubmatch(cls); match != nil {
		v := radiusValue(match[1], theme)
		if v != "" {
			return "border-radius: " + v + ";"
		}
	}

	// Rounded sides/corners: rounded-t-*, rounded-tl-*, and logical s/e.
	if match := roundedSideRe.FindStringSubmatch(cls); match != nil {
		corner, val := match[1], match[2]
		v := radiusValue(val, theme)
		if v == "" {
			return ""
		}
		props := map[string]string{
			"t":  "border-top-left-radius: " + v + "; border-top-right-radius: " + v + ";",
			"r":  "border-top-right-radius: " + v + "; border-bottom-right-radius: " + v + ";",
			"b":  "border-bottom-right-radius: " + v + "; border-bottom-left-radius: " + v + ";",
			"l":  "border-top-left-radius: " + v + "; border-bottom-left-radius: " + v + ";",
			"tl": "border-top-left-radius: " + v + ";",
			"tr": "border-top-right-radius: " + v + ";",
			"bl": "border-bottom-left-radius: " + v + ";",
			"br": "border-bottom-right-radius: " + v + ";",
			"s":  "border-start-start-radius: " + v + "; border-end-start-radius: " + v + ";",
			"e":  "border-start-end-radius: " + v + "; border-end-end-radius: " + v + ";",
			"ss": "border-start-start-radius: " + v + ";",
			"se": "border-start-end-radius: " + v + ";",
			"es": "border-end-start-radius: " + v + ";",
			"ee": "border-end-end-radius: " + v + ";",
		}
		return props[corner]
	}

	// Opacity
	if match := opacityRe.FindStringSubmatch(cls); match != nil {
		if v, ok := theme.Opacity[match[1]]; ok {
			return "opacity: " + v + ";"
		}
	}

	// Shadow (named scale incl. `2xl`, `none`, arbitrary color/shadow, the
	// default, and colored shadows). Shadows compose through `--tw-shadow` so a
	// `shadow-<color>` overrides the color while keeping the shadow geometry.
	if cls == "shadow-none" {
		return "box-shadow: none;"
	}
	if v, hint, isArb := arbitraryValue(strings.TrimPrefix(cls, "shadow-")); isArb &&
		strings.HasPrefix(cls, "shadow-") {
		switch hint {
		case "color":
			return shadowColorRule(v)
		case "shadow":
			return shadowRule(v)
		}
		if looksLikeColor(v) {
			return shadowColorRule(v)
		}
		return shadowRule(v)
	}
	if rest, ok := strings.CutPrefix(cls, "shadow-"); ok {
		if val, ok := theme.Shadows[rest]; ok {
			if val == "none" {
				return "box-shadow: none;"
			}
			return shadowRule(val)
		}
		if cv, ok := colorValue(rest, "", theme); ok {
			return shadowColorRule(cv)
		}
	}
	if cls == "shadow" {
		if val, ok := theme.Shadows[""]; ok {
			return shadowRule(val)
		}
		return shadowRule("0 1px 3px 0 rgb(0 0 0 / 0.1), 0 1px 2px -1px rgb(0 0 0 / 0.1)")
	}

	// Text size (Tailwind numeric)
	if match := regexp.MustCompile(`^text-(\w+)$`).FindStringSubmatch(cls); match != nil {
		if val, ok := theme.TextSizes[match[1]]; ok {
			// A configured size may be a bare value ("0.875rem") or a full
			// declaration block (the built-in defaults).
			if strings.Contains(val, ":") {
				return val
			}
			return "font-size: " + val + ";"
		}
	}

	// Position
	if cls == "static" {
		return "position: static;"
	}
	if cls == "fixed" {
		return "position: fixed;"
	}
	if cls == "absolute" {
		return "position: absolute;"
	}
	if cls == "relative" {
		return "position: relative;"
	}
	if cls == "sticky" {
		return "position: sticky;"
	}

	// Inset (top/right/bottom/left)
	if match := regexp.MustCompile(`^inset-([a-zA-Z0-9./]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("inset: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^inset-x-([a-zA-Z0-9./]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("left: %s; right: %s;", spacingValue(match[1], theme), spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^inset-y-([a-zA-Z0-9./]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("top: %s; bottom: %s;", spacingValue(match[1], theme), spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^top-([a-zA-Z0-9./]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("top: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^right-([a-zA-Z0-9./]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("right: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^bottom-([a-zA-Z0-9./]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("bottom: %s;", spacingValue(match[1], theme))
	}
	if match := regexp.MustCompile(`^left-([a-zA-Z0-9./]+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("left: %s;", spacingValue(match[1], theme))
	}

	// Overflow
	if cls == "overflow-hidden" {
		return "overflow: hidden;"
	}
	if cls == "overflow-auto" {
		return "overflow: auto;"
	}
	if cls == "overflow-scroll" {
		return "overflow: scroll;"
	}
	if cls == "overflow-visible" {
		return "overflow: visible;"
	}
	if cls == "overflow-x-auto" {
		return "overflow-x: auto;"
	}
	if cls == "overflow-y-auto" {
		return "overflow-y: auto;"
	}
	if cls == "overflow-x-hidden" {
		return "overflow-x: hidden;"
	}
	if cls == "overflow-y-hidden" {
		return "overflow-y: hidden;"
	}
	if cls == "overflow-x-scroll" {
		return "overflow-x: scroll;"
	}
	if cls == "overflow-y-scroll" {
		return "overflow-y: scroll;"
	}

	// Z-index
	if match := regexp.MustCompile(`^z-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("z-index: %s;", match[1])
	}
	if cls == "z-auto" {
		return "z-index: auto;"
	}

	// Cursor
	if cls == "cursor-pointer" {
		return "cursor: pointer;"
	}
	if cls == "cursor-default" {
		return "cursor: default;"
	}
	if cls == "cursor-not-allowed" {
		return "cursor: not-allowed;"
	}
	if cls == "cursor-grab" {
		return "cursor: grab;"
	}
	if cls == "cursor-grabbing" {
		return "cursor: grabbing;"
	}
	if cls == "cursor-wait" {
		return "cursor: wait;"
	}
	if cls == "cursor-text" {
		return "cursor: text;"
	}
	if cls == "cursor-move" {
		return "cursor: move;"
	}
	if cls == "cursor-help" {
		return "cursor: help;"
	}

	// User select
	if cls == "select-none" {
		return "user-select: none;"
	}
	if cls == "select-text" {
		return "user-select: text;"
	}
	if cls == "select-all" {
		return "user-select: all;"
	}
	if cls == "select-auto" {
		return "user-select: auto;"
	}

	// Whitespace
	if cls == "whitespace-normal" {
		return "white-space: normal;"
	}
	if cls == "whitespace-nowrap" {
		return "white-space: nowrap;"
	}
	if cls == "whitespace-pre" {
		return "white-space: pre;"
	}
	if cls == "whitespace-pre-line" {
		return "white-space: pre-line;"
	}
	if cls == "whitespace-pre-wrap" {
		return "white-space: pre-wrap;"
	}

	// Word break
	if cls == "break-normal" {
		return "overflow-wrap: normal; word-break: normal;"
	}
	if cls == "break-words" {
		return "overflow-wrap: break-word;"
	}
	if cls == "break-all" {
		return "word-break: break-all;"
	}
	if cls == "truncate" {
		return "overflow: hidden; text-overflow: ellipsis; white-space: nowrap;"
	}

	// Box sizing
	if cls == "box-border" {
		return "box-sizing: border-box;"
	}
	if cls == "box-content" {
		return "box-sizing: content-box;"
	}

	// Appearance
	if cls == "appearance-none" {
		return "appearance: none;"
	}

	// Transition property
	if cls == "transition" {
		return "transition-property: all; transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1); transition-duration: 150ms;"
	}
	if cls == "transition-all" {
		return "transition-property: all; transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1); transition-duration: 150ms;"
	}
	if cls == "transition-colors" {
		return "transition-property: color, background-color, border-color, text-decoration-color, fill, stroke; transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1); transition-duration: 150ms;"
	}
	if cls == "transition-opacity" {
		return "transition-property: opacity; transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1); transition-duration: 150ms;"
	}
	if cls == "transition-shadow" {
		return "transition-property: box-shadow; transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1); transition-duration: 150ms;"
	}
	if cls == "transition-transform" {
		return "transition-property: transform; transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1); transition-duration: 150ms;"
	}
	if cls == "transition-none" {
		return "transition-property: none;"
	}

	if match := regexp.MustCompile(`^duration-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("transition-duration: %sms;", match[1])
	}
	if v, ok := unwrapArbitrary(strings.TrimPrefix(cls, "duration-")); ok && strings.HasPrefix(cls, "duration-") {
		return "transition-duration: " + v + ";"
	}
	if match := regexp.MustCompile(`^delay-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return fmt.Sprintf("transition-delay: %sms;", match[1])
	}
	if v, ok := unwrapArbitrary(strings.TrimPrefix(cls, "delay-")); ok && strings.HasPrefix(cls, "delay-") {
		return "transition-delay: " + v + ";"
	}
	if cls == "ease-linear" {
		return "transition-timing-function: linear;"
	}
	if cls == "ease-in" {
		return "transition-timing-function: cubic-bezier(0.4, 0, 1, 1);"
	}
	if cls == "ease-out" {
		return "transition-timing-function: cubic-bezier(0, 0, 0.2, 1);"
	}
	if cls == "ease-in-out" {
		return "transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1);"
	}
	if v, ok := unwrapArbitrary(strings.TrimPrefix(cls, "ease-")); ok && strings.HasPrefix(cls, "ease-") {
		return "transition-timing-function: " + v + ";"
	}
	if v, ok := unwrapArbitrary(strings.TrimPrefix(cls, "transition-")); ok && strings.HasPrefix(cls, "transition-") {
		return "transition-property: " + v + "; transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1); transition-duration: 150ms;"
	}

	// Transform: composed via CSS variables so utilities stack instead of
	// overwriting each other (translate/rotate/skew/scale).
	if cls == "transform" {
		return "transform: " + transformCompose + ";"
	}
	if cls == "transform-gpu" {
		// Force a compositing layer via translate3d, matching Tailwind.
		return "transform: " + transform3DCompose + ";"
	}
	if cls == "transform-none" {
		return "transform: none;"
	}
	if match := translateRe.FindStringSubmatch(cls); match != nil {
		axis, val := match[1], match[2]
		return "--tw-translate-" + axis + ": " + translateValue(val, theme) + "; transform: " + transformCompose + ";"
	}
	if match := rotateRe.FindStringSubmatch(cls); match != nil {
		if v, ok := angleValue(match[1]); ok {
			return "--tw-rotate: " + v + "; transform: " + transformCompose + ";"
		}
	}
	if match := skewRe.FindStringSubmatch(cls); match != nil {
		axis, val := match[1], match[2]
		if v, ok := angleValue(val); ok {
			return "--tw-skew-" + axis + ": " + v + "; transform: " + transformCompose + ";"
		}
	}
	// Axis-specific scale: scale-x-50 / scale-y-110.
	if match := scaleAxisRe.FindStringSubmatch(cls); match != nil {
		axis, val := match[1], match[2]
		return "--tw-scale-" + axis + ": " + scaleFactor(val) + "; transform: " + transformCompose + ";"
	}
	if match := scaleRe.FindStringSubmatch(cls); match != nil {
		v := scaleFactor(match[1])
		return "--tw-scale-x: " + v + "; --tw-scale-y: " + v + "; transform: " + transformCompose + ";"
	}
	if match := originRe.FindStringSubmatch(cls); match != nil {
		if v, ok := unwrapArbitrary(match[1]); ok {
			return "transform-origin: " + v + ";"
		}
		return "transform-origin: " + match[1] + ";"
	}

	// Ring (box-shadow ring) — width, color, offset, inset.
	if cls == "ring" {
		return "--tw-ring-offset-shadow: var(--tw-ring-inset, ) 0 0 0 var(--tw-ring-offset-width,0px) var(--tw-ring-offset-color,#fff); --tw-ring-shadow: var(--tw-ring-inset, ) 0 0 0 calc(3px + var(--tw-ring-offset-width,0px)) var(--tw-ring-color,rgb(59 130 246 / 0.5)); box-shadow: var(--tw-ring-offset-shadow), var(--tw-ring-shadow), var(--tw-shadow, 0 0 #0000);"
	}
	if match := regexp.MustCompile(`^ring-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return "--tw-ring-shadow: var(--tw-ring-inset, ) 0 0 0 calc(" + match[1] + "px + var(--tw-ring-offset-width,0px)) var(--tw-ring-color,rgb(59 130 246 / 0.5)); box-shadow: var(--tw-ring-offset-shadow, 0 0 #0000), var(--tw-ring-shadow), var(--tw-shadow, 0 0 #0000);"
	}
	if cls == "ring-inset" {
		return "--tw-ring-inset: inset;"
	}
	// ring-offset: numeric width, arbitrary width, or color.
	if match := regexp.MustCompile(`^ring-offset-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return "--tw-ring-offset-width: " + match[1] + "px;"
	}
	if rest, ok := strings.CutPrefix(cls, "ring-offset-"); ok && rest != "" && !isDigits(rest) {
		if inner, hint, isArb := arbitraryValue(rest); isArb {
			if hint == "color" || looksLikeColor(inner) {
				return "--tw-ring-offset-color: " + inner + ";"
			}
			return "--tw-ring-offset-width: " + inner + ";"
		}
		colorKey, alpha := splitColorModifier(rest)
		if v, ok := colorValue(colorKey, alpha, theme); ok {
			return "--tw-ring-offset-color: " + v + ";"
		}
	}
	// ring-* color, arbitrary color, or arbitrary width.
	if match := regexp.MustCompile(`^ring-(.+)$`).FindStringSubmatch(cls); match != nil {
		key := match[1]
		if inner, hint, isArb := arbitraryValue(key); isArb {
			if hint == "length" || (hint == "" && looksLikeLength(inner)) {
				return "--tw-ring-shadow: var(--tw-ring-inset, ) 0 0 0 calc(" + inner + " + var(--tw-ring-offset-width,0px)) var(--tw-ring-color,rgb(59 130 246 / 0.5)); box-shadow: var(--tw-ring-offset-shadow, 0 0 #0000), var(--tw-ring-shadow), var(--tw-shadow, 0 0 #0000);"
			}
			return "--tw-ring-color: " + inner + ";"
		}
		colorKey, alpha := splitColorModifier(key)
		if v, ok := colorValue(colorKey, alpha, theme); ok {
			return "--tw-ring-color: " + v + ";"
		}
	}

	// Divide (border between children) via the sibling selector.
	if cls == "divide-x-reverse" {
		return "--tw-divide-x-reverse: 1;"
	}
	if cls == "divide-y-reverse" {
		return "--tw-divide-y-reverse: 1;"
	}
	if cls == "divide-x" {
		return "border-right-width: calc(1px * var(--tw-divide-x-reverse,0)); border-left-width: calc(1px * calc(1 - var(--tw-divide-x-reverse,0))); border-style: solid;"
	}
	if cls == "divide-y" {
		return "border-bottom-width: calc(1px * var(--tw-divide-y-reverse,0)); border-top-width: calc(1px * calc(1 - var(--tw-divide-y-reverse,0))); border-style: solid;"
	}
	if match := regexp.MustCompile(`^divide-x-(\d+)$`).FindStringSubmatch(cls); match != nil {
		w := match[1] + "px"
		return "border-right-width: calc(" + w + " * var(--tw-divide-x-reverse,0)); border-left-width: calc(" + w + " * calc(1 - var(--tw-divide-x-reverse,0))); border-style: solid;"
	}
	if match := regexp.MustCompile(`^divide-y-(\d+)$`).FindStringSubmatch(cls); match != nil {
		w := match[1] + "px"
		return "border-bottom-width: calc(" + w + " * var(--tw-divide-y-reverse,0)); border-top-width: calc(" + w + " * calc(1 - var(--tw-divide-y-reverse,0))); border-style: solid;"
	}
	if v, ok := unwrapArbitrary(strings.TrimPrefix(cls, "divide-x-")); ok && strings.HasPrefix(cls, "divide-x-") {
		return "border-right-width: calc(" + v + " * var(--tw-divide-x-reverse,0)); border-left-width: calc(" + v + " * calc(1 - var(--tw-divide-x-reverse,0))); border-style: solid;"
	}
	if v, ok := unwrapArbitrary(strings.TrimPrefix(cls, "divide-y-")); ok && strings.HasPrefix(cls, "divide-y-") {
		return "border-bottom-width: calc(" + v + " * var(--tw-divide-y-reverse,0)); border-top-width: calc(" + v + " * calc(1 - var(--tw-divide-y-reverse,0))); border-style: solid;"
	}
	if match := regexp.MustCompile(`^divide-(.+)$`).FindStringSubmatch(cls); match != nil {
		colorKey, alpha := splitColorModifier(match[1])
		if v, ok := colorValue(colorKey, alpha, theme); ok {
			return "border-color: " + v + ";"
		}
	}

	// Content (pseudo-element text). The value is stored in --tw-content so the
	// before/after variants can inject `content: var(--tw-content)` uniformly.
	if cls == "content-none" {
		return "content: none;"
	}
	if v, ok := unwrapArbitrary(strings.TrimPrefix(cls, "content-")); ok && strings.HasPrefix(cls, "content-") {
		return "--tw-content: " + v + "; content: var(--tw-content);"
	}

	// Line clamp.
	if match := regexp.MustCompile(`^line-clamp-(\d+)$`).FindStringSubmatch(cls); match != nil {
		return "overflow: hidden; display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: " + match[1] + ";"
	}
	if cls == "line-clamp-none" {
		return "-webkit-line-clamp: unset;"
	}

	// Aspect ratio.
	if cls == "aspect-auto" {
		return "aspect-ratio: auto;"
	}
	if cls == "aspect-square" {
		return "aspect-ratio: 1 / 1;"
	}
	if cls == "aspect-video" {
		return "aspect-ratio: 16 / 9;"
	}
	if match := regexp.MustCompile(`^aspect-\[(.+)\]$`).FindStringSubmatch(cls); match != nil {
		return "aspect-ratio: " + strings.ReplaceAll(match[1], "_", " ") + ";"
	}

	// Visibility
	if cls == "visible" {
		return "visibility: visible;"
	}
	if cls == "invisible" {
		return "visibility: hidden;"
	}

	// Object fit
	if cls == "object-contain" {
		return "object-fit: contain;"
	}
	if cls == "object-cover" {
		return "object-fit: cover;"
	}
	if cls == "object-fill" {
		return "object-fit: fill;"
	}
	if cls == "object-none" {
		return "object-fit: none;"
	}
	if cls == "object-scale-down" {
		return "object-fit: scale-down;"
	}

	// Object position
	if cls == "object-bottom" {
		return "object-position: bottom;"
	}
	if cls == "object-center" {
		return "object-position: center;"
	}
	if cls == "object-left" {
		return "object-position: left;"
	}
	if cls == "object-left-bottom" {
		return "object-position: left bottom;"
	}
	if cls == "object-left-top" {
		return "object-position: left top;"
	}
	if cls == "object-right" {
		return "object-position: right;"
	}
	if cls == "object-right-bottom" {
		return "object-position: right bottom;"
	}
	if cls == "object-right-top" {
		return "object-position: right top;"
	}
	if cls == "object-top" {
		return "object-position: top;"
	}
	if inner, hint, isArb := arbitraryValue(strings.TrimPrefix(cls, "object-")); isArb && strings.HasPrefix(cls, "object-") {
		if hint == "position" || (!looksLikeLength(inner) && strings.ContainsAny(inner, " %")) {
			return "object-position: " + inner + ";"
		}
		return "object-fit: " + inner + ";"
	}

	// Pointer events
	if cls == "pointer-events-none" {
		return "pointer-events: none;"
	}
	if cls == "pointer-events-auto" {
		return "pointer-events: auto;"
	}

	// Screen reader only
	if cls == "sr-only" {
		return "position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border-width: 0;"
	}
	if cls == "not-sr-only" {
		return "position: static; width: auto; height: auto; padding: 0; margin: 0; overflow: visible; clip: auto; white-space: normal;"
	}

	// Background gradient direction
	if cls == "bg-gradient-to-t" {
		return "background-image: linear-gradient(to top, var(--tw-gradient-stops));"
	}
	if cls == "bg-gradient-to-tr" {
		return "background-image: linear-gradient(to top right, var(--tw-gradient-stops));"
	}
	if cls == "bg-gradient-to-r" {
		return "background-image: linear-gradient(to right, var(--tw-gradient-stops));"
	}
	if cls == "bg-gradient-to-br" {
		return "background-image: linear-gradient(to bottom right, var(--tw-gradient-stops));"
	}
	if cls == "bg-gradient-to-b" {
		return "background-image: linear-gradient(to bottom, var(--tw-gradient-stops));"
	}
	if cls == "bg-gradient-to-bl" {
		return "background-image: linear-gradient(to bottom left, var(--tw-gradient-stops));"
	}
	if cls == "bg-gradient-to-l" {
		return "background-image: linear-gradient(to left, var(--tw-gradient-stops));"
	}
	if cls == "bg-gradient-to-tl" {
		return "background-image: linear-gradient(to top left, var(--tw-gradient-stops));"
	}

	// Gradient color stops: from-*, via-*, to-*. Accept named colors, shade
	// colors, alpha modifiers (from-indigo-400/50), and arbitrary values.
	if stop, ok := gradientStop(cls, "from", theme); ok {
		// Provide the automatic `--tw-gradient-to` fallback (same hue at 0
		// alpha) so `from-*` works without an explicit `to-*`.
		return "--tw-gradient-from: " + stop + "; --tw-gradient-to: " + transparentVersion(stop) + "; --tw-gradient-stops: var(--tw-gradient-from), var(--tw-gradient-to);"
	}
	if stop, ok := gradientStop(cls, "via", theme); ok {
		return "--tw-gradient-to: " + transparentVersion(stop) + "; --tw-gradient-stops: var(--tw-gradient-from), " + stop + ", var(--tw-gradient-to);"
	}
	if stop, ok := gradientStop(cls, "to", theme); ok {
		return "--tw-gradient-to: " + stop + ";"
	}

	// Extended utility families (filters, animation, blend, layout extras,
	// typography, interactivity, 3D transforms, borders, backgrounds).
	if css := generateExtraCSS(cls, theme); css != "" {
		return css
	}

	// Arbitrary values with bracket syntax: w-[30px], top-[117px], text-[#bada55]
	if match := regexp.MustCompile(`^([a-z-]+)-\[(.+)\]$`).FindStringSubmatch(cls); match != nil {
		prop := match[1]
		val := match[2]
		cssProp := arbitraryPropToCSS(prop, val)
		if cssProp != "" {
			return cssProp
		}
	}

	return ""
}

// spacingValue resolves a spacing key. The named scale wins; otherwise any
// numeric key is a multiple of the 0.25rem spacing base (p-13 → 3.25rem,
// p-13.5 → 3.375rem), matching Tailwind's JIT behavior.
func spacingValue(key string, theme TailwindTheme) string {
	if val, ok := theme.Spacing[key]; ok {
		return val
	}
	if v, ok := unwrapArbitrary(key); ok {
		return v
	}
	if pct, ok := fractionPercent(key); ok {
		return pct
	}
	if f, err := strconv.ParseFloat(key, 64); err == nil {
		if f == 0 {
			return "0"
		}
		return formatRem(f)
	}
	return ""
}

func sizingValue(key string, theme TailwindTheme) string {
	if val, ok := theme.Sizing[key]; ok {
		return val
	}
	switch key {
	case "1/2":
		return "50%"
	case "1/3":
		return "33.333333%"
	case "2/3":
		return "66.666667%"
	case "1/4":
		return "25%"
	case "3/4":
		return "75%"
	case "1/5":
		return "20%"
	case "2/5":
		return "40%"
	case "3/5":
		return "60%"
	case "4/5":
		return "80%"
	case "1/6":
		return "16.666667%"
	case "5/6":
		return "83.333333%"
	case "full":
		return "100%"
	case "screen":
		return "100vw"
	case "auto":
		return "auto"
	case "min":
		return "min-content"
	case "max":
		return "max-content"
	case "fit":
		return "fit-content"
	}
	if _, err := strconv.ParseFloat(key, 64); err == nil {
		return spacingValue(key, theme)
	}
	if v, ok := unwrapArbitrary(key); ok {
		return v
	}
	return ""
}

func arbitraryPropToCSS(prop, val string) string {
	switch prop {
	case "w":
		return "width: " + val + ";"
	case "h":
		return "height: " + val + ";"
	case "min-w":
		return "min-width: " + val + ";"
	case "min-h":
		return "min-height: " + val + ";"
	case "max-w":
		return "max-width: " + val + ";"
	case "max-h":
		return "max-height: " + val + ";"
	case "p":
		return "padding: " + val + ";"
	case "px":
		return "padding-left: " + val + "; padding-right: " + val + ";"
	case "py":
		return "padding-top: " + val + "; padding-bottom: " + val + ";"
	case "pt":
		return "padding-top: " + val + ";"
	case "pr":
		return "padding-right: " + val + ";"
	case "pb":
		return "padding-bottom: " + val + ";"
	case "pl":
		return "padding-left: " + val + ";"
	case "m":
		return "margin: " + val + ";"
	case "mx":
		return "margin-left: " + val + "; margin-right: " + val + ";"
	case "my":
		return "margin-top: " + val + "; margin-bottom: " + val + ";"
	case "mt":
		return "margin-top: " + val + ";"
	case "mr":
		return "margin-right: " + val + ";"
	case "mb":
		return "margin-bottom: " + val + ";"
	case "ml":
		return "margin-left: " + val + ";"
	case "gap":
		return "gap: " + val + ";"
	case "gap-x":
		return "column-gap: " + val + ";"
	case "gap-y":
		return "row-gap: " + val + ";"
	case "top":
		return "top: " + val + ";"
	case "right":
		return "right: " + val + ";"
	case "bottom":
		return "bottom: " + val + ";"
	case "left":
		return "left: " + val + ";"
	case "inset":
		return "inset: " + val + ";"
	case "text":
		return "color: " + val + ";"
	case "bg":
		return "background-color: " + val + ";"
	case "border":
		return "border-color: " + val + ";"
	case "rounded":
		return "border-radius: " + val + ";"
	case "opacity":
		return "opacity: " + val + ";"
	case "z":
		return "z-index: " + val + ";"
	case "shadow":
		return "box-shadow: " + val + ";"
	case "translate-x":
		return "transform: translateX(" + val + ");"
	case "translate-y":
		return "transform: translateY(" + val + ");"
	case "rotate":
		return "transform: rotate(" + val + ");"
	case "scale":
		return "transform: scale(" + val + ");"
	case "blur":
		return "filter: blur(" + val + ");"
	case "brightness":
		return "filter: brightness(" + val + ");"
	case "contrast":
		return "filter: contrast(" + val + ");"
	case "from":
		return "--tw-gradient-from: " + val + "; --tw-gradient-stops: var(--tw-gradient-from), var(--tw-gradient-to);"
	case "via":
		return "--tw-gradient-stops: var(--tw-gradient-from), " + val + ", var(--tw-gradient-to);"
	case "to":
		return "--tw-gradient-to: " + val + ";"
	}
	return ""
}
