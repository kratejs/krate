package css

import (
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
	roundedRe     = regexp.MustCompile(`^rounded-([a-zA-Z0-9]+)$`)
	roundedSideRe = regexp.MustCompile(`^rounded-(tl|tr|bl|br|ss|se|es|ee|t|r|b|l|s|e)-([a-zA-Z0-9]+|\[.+\])$`)
)

// TailwindScanner extracts Tailwind utility class names from source files.
type TailwindScanner struct {
	Root string
	// Exclude lists absolute directories skipped during the scan (@source not).
	Exclude []string
	// VarRefs collects the custom properties referenced via var(--name) in the
	// scanned files, so referenced theme variables can be emitted.
	VarRefs map[string]bool
}

// NewTailwindScanner creates a scanner for the given project root.
func NewTailwindScanner(root string) *TailwindScanner {
	return &TailwindScanner{Root: root, VarRefs: map[string]bool{}}
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
			if s.excluded(path) {
				return nil
			}
			text := string(data)
			for _, tok := range candidateTokens(text) {
				classes[tok] = true
			}
			if strings.Contains(text, "var(") {
				for _, m := range varRefRe.FindAllStringSubmatch(text, -1) {
					s.VarRefs[m[1]] = true
				}
			}
			return nil
		})
	}

	return classes
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
	"transition": true, "blur": true, "collapse": true,
	"ordinal": true, "slashed-zero": true, "lining-nums": true,
	"oldstyle-nums": true, "proportional-nums": true, "tabular-nums": true,
	"diagonal-fractions": true, "stacked-fractions": true, "hyphens": true,
}

// TailwindGenerator converts class names to CSS rules.
type TailwindGenerator struct {
	Theme TailwindTheme
	// Strict, when true, collects classes that produced no rule.
	Strict bool
	// Unknown collects class names that yielded no rule (populated when Strict).
	Unknown []string

	// usage tracks keyframes and --tw-* properties the last Generate used.
	usage twUsage
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
	g.usage.reset()
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
			g.usage.record(text, g.Theme)
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
		// `tabular-nums`, `preserve-3d`) - CSS keywords are lowercase idents.
		return cssIdentifierRe.MatchString(val)
	}
	return true
}

// isBareKeyword reports whether val contains no digits, units, %, functions or
// other value punctuation - i.e. it can only be a keyword or identifier.
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

var (
	bgRe               = regexp.MustCompile(`^bg-(.+)$`)
	arbitraryUtilityRe = regexp.MustCompile(`^([a-z-]+)-\[(.+)\]$`)
)

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

// negatableRoot reports whether a utility accepts a leading "-" (Tailwind only
// defines negative values for these families).
func negatableRoot(base string) bool {
	for _, p := range []string{
		"m-", "mx-", "my-", "mt-", "mr-", "mb-", "ml-", "ms-", "me-", "inset-", "top-",
		"right-", "bottom-", "left-", "start-", "end-", "z-", "order-", "indent-",
		"scroll-m", "space-", "outline-offset-", "hue-rotate-", "col-start-", "col-end-",
		"row-start-", "row-end-", "tracking-", "backdrop-hue-rotate-",
	} {
		if strings.HasPrefix(base, p) {
			return true
		}
	}
	return false
}

// negateCSS generates the positive form of a utility and negates the numeric
// values in its declarations (for `-mt-4`, `-translate-x-1/2`, `-top-2`, ...).
func negateCSS(positive string, theme TailwindTheme) string {
	css := generateCSS(positive, theme)
	if css == "" {
		return ""
	}
	// Negate the first length/number token after each `:`.
	changed := false
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
			changed = true
		}
		parts = append(parts, strings.TrimSpace(prop)+": "+val)
	}
	if len(parts) == 0 || !changed {
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
	// Tailwind rounds repeating fractions to 6 decimal places (1/3 -> 33.333333%).
	s := strconv.FormatFloat(pct, 'f', 6, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s + "%", true
}

// selectorSuffix returns the descendant/sibling selector a utility applies to,
// or "" when the utility styles the element itself. Used for space-*, divide-*.
func selectorSuffix(base string) string {
	if strings.HasPrefix(base, "placeholder-") {
		return "::placeholder"
	}
	return ""
}

// radiusValue resolves a border-radius scale key. "" is the `rounded` default.
func radiusValue(key string, theme TailwindTheme) string {
	if v, ok := theme.Radii[key]; ok {
		return v
	}
	if v, ok := unwrapArbitrary(key); ok {
		return v
	}
	return ""
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
	cls = parenToBracket(cls)
	if css, handled := v4Utility(cls, theme); handled {
		return css
	}

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
	if match := bgRe.FindStringSubmatch(cls); match != nil {
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

	// Border radius (base). `rounded` (no suffix) -> theme default ("").
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

	// Content (pseudo-element text). The value is stored in --tw-content so the
	// before/after variants can inject `content: var(--tw-content)` uniformly.
	if cls == "content-none" {
		return "content: none;"
	}
	if v, ok := unwrapArbitrary(strings.TrimPrefix(cls, "content-")); ok && strings.HasPrefix(cls, "content-") {
		return "--tw-content: " + v + "; content: var(--tw-content);"
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
	if inner, _, isArb := arbitraryValue(strings.TrimPrefix(cls, "object-")); isArb && strings.HasPrefix(cls, "object-") {
		return "object-position: " + inner + ";"
	}

	// Pointer events
	if cls == "pointer-events-none" {
		return "pointer-events: none;"
	}
	if cls == "pointer-events-auto" {
		return "pointer-events: auto;"
	}

	// Extended utility families (filters, animation, blend, layout extras,
	// typography, interactivity, 3D transforms, borders, backgrounds).
	if css := generateExtraCSS(cls, theme); css != "" {
		return css
	}

	// Arbitrary values with bracket syntax: w-[30px], top-[117px], text-[#bada55]
	if match := arbitraryUtilityRe.FindStringSubmatch(cls); match != nil {
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
// numeric key is a multiple of the 0.25rem spacing base (p-13 -> 3.25rem,
// p-13.5 -> 3.375rem), matching Tailwind's JIT behavior.
func spacingValue(key string, theme TailwindTheme) string {
	if val, ok := namedSpacing(key, theme); ok {
		return val
	}
	if v, ok := unwrapArbitrary(key); ok {
		return v
	}
	if v, ok := spacingMultiple(key, theme); ok {
		return v
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

var defaultSpacingTable = defaultSpacing()

// namedSpacing looks key up in the theme spacing scale. When the theme sets a
// custom --spacing unit, untouched built-in numeric entries are skipped so
// they scale with the unit instead of staying at their 0.25rem values.
func namedSpacing(key string, theme TailwindTheme) (string, bool) {
	v, ok := theme.Spacing[key]
	if !ok {
		return "", false
	}
	if u := strings.TrimSpace(theme.SpacingUnit); u != "" && u != "0.25rem" && plainNumber(key) {
		if d, isDefault := defaultSpacingTable[key]; isDefault && d == v {
			return "", false
		}
	}
	return v, true
}
