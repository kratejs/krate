package css

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// TailwindDirectives accumulates the Tailwind v4 CSS-first directives found in
// project CSS: @import "tailwindcss", @theme, @source, @utility,
// @custom-variant, plus the unsupported @config/@plugin (reported).
type TailwindDirectives struct {
	Root  string
	Theme AtTheme
	// Utilities maps a static @utility name to its declaration block.
	Utilities map[string]string
	// Functional maps the prefix of an `@utility name-*` to its declarations.
	Functional map[string]string
	// Variants maps @custom-variant names to a selector template or at-rule.
	Variants map[string]string
	// Sources are extra directories to scan (absolute); Excludes are skipped.
	Sources  []string
	Excludes []string
	// Inline are classes forced by @source inline(...).
	Inline []string
	// NoAutoSource is set by @import "tailwindcss" source(none).
	NoAutoSource bool
	Prefix       string
	Important    bool
	// Imported is true when an @import "tailwindcss" was seen.
	Imported bool
	// VarRefs are theme variables referenced via var(--name) in project CSS.
	VarRefs map[string]bool

	warnings map[string]bool
	seenSrc  map[string]bool
}

// NewTailwindDirectives creates an empty collection rooted at root.
func NewTailwindDirectives(root string) *TailwindDirectives {
	return &TailwindDirectives{Root: root, Utilities: map[string]string{}, Functional: map[string]string{}, Variants: map[string]string{}, VarRefs: map[string]bool{},
		warnings: map[string]bool{}, seenSrc: map[string]bool{}}
}

func pathDir(p string) string { return filepath.Dir(p) }

func (d *TailwindDirectives) warn(format string, args ...any) {
	d.warnings[fmt.Sprintf(format, args...)] = true
}

// Warnings returns the deduplicated warnings, sorted.
func (d *TailwindDirectives) Warnings() []string {
	for _, w := range d.Theme.Warnings {
		d.warnings[w] = true
	}
	d.Theme.Warnings = nil
	out := make([]string, 0, len(d.warnings))
	for w := range d.warnings {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// tailwindImportRe matches the target of an `@import "tailwindcss..."`.
var tailwindImportRe = regexp.MustCompile(`^(?:url\()?["'](tailwindcss(?:/[A-Za-z0-9._/-]*)?)["']\)?\s*(.*)$`)

var modifierRe = regexp.MustCompile(`([A-Za-z-]+)(?:\(([^)]*)\))?`)

// Add parses one CSS text. baseDir resolves relative @source paths.
func (d *TailwindDirectives) Add(cssText, baseDir string) {
	if strings.Contains(cssText, "var(") {
		for _, m := range varRefRe.FindAllStringSubmatch(cssText, -1) {
			d.VarRefs[m[1]] = true
		}
	}
	if !strings.Contains(cssText, "@") {
		return
	}
	for _, r := range topLevelAtRules(cssText) {
		d.handle(r, baseDir)
	}
}

func (d *TailwindDirectives) handle(r cssAtRule, baseDir string) {
	switch r.Name {
	case "import":
		m := tailwindImportRe.FindStringSubmatch(r.Prelude)
		if m == nil {
			return
		}
		d.Imported = true
		for _, mm := range modifierRe.FindAllStringSubmatch(m[2], -1) {
			arg := strings.Trim(strings.TrimSpace(mm[2]), `"'`)
			switch mm[1] {
			case "prefix":
				d.Prefix = arg
			case "important":
				d.Important = true
			case "source":
				if arg == "none" {
					d.NoAutoSource = true
				} else {
					d.addSource(arg, baseDir)
					d.NoAutoSource = true
				}
			case "theme":
				// static/inline only change var emission; all vars are emitted.
			case "layer":
			default:
				d.warn("@import \"tailwindcss\": unsupported modifier %q", mm[0])
			}
		}
	case "theme":
		if r.HasBody {
			d.Theme.addBlock(r)
		}
	case "config":
		d.warn("@config %s is not supported: Krate reads tailwind.config.{ts,js,mjs} from the project root; the directive was removed", r.Prelude)
	case "plugin":
		d.warn("@plugin %s is not supported: JS plugins are not executed; the directive was removed", r.Prelude)
	case "source":
		d.handleSource(r.Prelude, baseDir)
	case "utility":
		d.handleUtility(r)
	case "custom-variant":
		d.handleCustomVariant(r)
	}
}

func (d *TailwindDirectives) addSource(p, baseDir string) {
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(baseDir, p)
	}
	abs = filepath.Clean(abs)
	if !withinPath(d.Root, abs) {
		d.warn("@source %q is outside the project root and was ignored", p)
		return
	}
	if !d.seenSrc[abs] {
		d.seenSrc[abs] = true
		d.Sources = append(d.Sources, abs)
	}
}

func (d *TailwindDirectives) handleSource(prelude, baseDir string) {
	p := strings.TrimSpace(prelude)
	not := false
	if strings.HasPrefix(p, "not") && (len(p) == 3 || p[3] == ' ' || p[3] == '(' || p[3] == '"' || p[3] == '\'') {
		not = true
		p = strings.TrimSpace(p[3:])
	}
	if strings.HasPrefix(p, "inline(") && strings.HasSuffix(p, ")") {
		if not {
			d.warn("@source not inline(...) is not supported and was ignored")
			return
		}
		arg := strings.Trim(strings.TrimSpace(p[len("inline("):len(p)-1]), `"'`)
		d.Inline = append(d.Inline, expandBraces(arg)...)
		return
	}
	if len(p) >= 2 && (p[0] == '"' || p[0] == '\'') && p[len(p)-1] == p[0] {
		path := p[1 : len(p)-1]
		// Reduce a glob to its static base directory like content globs.
		if i := strings.IndexAny(path, "*?{["); i >= 0 {
			path = path[:i]
			if j := strings.LastIndexByte(path, '/'); j >= 0 {
				path = path[:j]
			} else {
				path = "."
			}
		}
		if path == "" {
			path = "."
		}
		if not {
			abs := path
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(baseDir, path)
			}
			d.Excludes = append(d.Excludes, filepath.Clean(abs))
			return
		}
		d.addSource(path, baseDir)
		return
	}
	d.warn("@source %s: unrecognized form; use @source \"path\", @source not \"path\" or @source inline(\"classes\")", prelude)
}

// handleUtility supports static `@utility name { ... }` and functional
// `@utility name-* { ... }` blocks. The body is kept as written; each class
// that uses the utility resolves it (see customUtility).
func (d *TailwindDirectives) handleUtility(r cssAtRule) {
	name := strings.TrimSpace(r.Prelude)
	if !r.HasBody || name == "" {
		d.warn("@utility %q: expected a name and a { } block", name)
		return
	}
	functional := strings.HasSuffix(name, "-*")
	if strings.Contains(name, "*") && !functional {
		d.warn("@utility %s: the wildcard must end the name (`name-*`); the utility was skipped", name)
		return
	}
	body := strings.TrimSpace(r.Body)
	if len(splitCSSNodes(body)) == 0 {
		return
	}
	for _, w := range utilityWarnings(name, body) {
		d.warn("%s", w)
	}
	if functional {
		d.Functional[strings.TrimSuffix(name, "-*")] = body
		return
	}
	d.Utilities[name] = body
}

// handleCustomVariant supports `@custom-variant name (selector);` and the
// single-rule block form `@custom-variant name { &:hover { @slot; } }`.
func (d *TailwindDirectives) handleCustomVariant(r cssAtRule) {
	fields := strings.SplitN(strings.TrimSpace(r.Prelude), " ", 2)
	name := fields[0]
	if name == "" {
		d.warn("@custom-variant: missing name")
		return
	}
	if !r.HasBody {
		if len(fields) < 2 {
			d.warn("@custom-variant %s: missing selector", name)
			return
		}
		sel := strings.TrimSpace(fields[1])
		if !strings.HasPrefix(sel, "(") || !strings.HasSuffix(sel, ")") {
			d.warn("@custom-variant %s: expected (selector) after the name", name)
			return
		}
		sel = strings.TrimSpace(sel[1 : len(sel)-1])
		// Comma lists in a bare selector are a selector list: keep as written.
		d.setVariant(name, sel)
		return
	}
	_, nested := splitCSSBody(r.Body)
	if len(nested) == 1 {
		if i := strings.IndexByte(nested[0], '{'); i > 0 {
			head := strings.TrimSpace(nested[0][:i])
			inner := strings.TrimSpace(nested[0][i+1 : len(nested[0])-1])
			if inner == "@slot;" || inner == "@slot" {
				if strings.HasPrefix(head, "@media") || strings.HasPrefix(head, "@supports") {
					d.setVariant(name, head)
					return
				}
				if strings.Contains(head, "&") {
					d.setVariant(name, head)
					return
				}
			}
		}
	}
	d.warn("@custom-variant %s: only `(selector)` and a single `selector { @slot; }` block are supported; skipped", name)
}

func (d *TailwindDirectives) setVariant(name, tmpl string) {
	if !strings.HasPrefix(tmpl, "@") && !strings.Contains(tmpl, "&") {
		d.warn("@custom-variant %s: selector must contain & (got %q)", name, tmpl)
		return
	}
	d.Variants[name] = tmpl
}

// StripTailwindDirectives removes the CSS-first directives (and the legacy
// `@tailwind` ones) from shipped CSS, so no at-rule reaches the browser that
// it would ignore or fetch (`@import "tailwindcss"` would 404).
func StripTailwindDirectives(cssText string) string {
	cssText = StripAtTailwind(cssText)
	if !strings.Contains(cssText, "@") {
		return cssText
	}
	var b strings.Builder
	last := 0
	for _, r := range topLevelAtRules(cssText) {
		drop := false
		switch r.Name {
		case "theme", "utility", "custom-variant", "config", "plugin", "source":
			drop = true
		case "import":
			drop = tailwindImportRe.MatchString(r.Prelude)
		}
		if !drop {
			continue
		}
		b.WriteString(cssText[last:r.Start])
		last = r.End
		if last < len(cssText) && cssText[last] == '\r' {
			last++
		}
		if last < len(cssText) && cssText[last] == '\n' {
			last++
		}
	}
	b.WriteString(cssText[last:])
	return b.String()
}

// ApplyTo folds the directive content into a generator's theme.
func (d *TailwindDirectives) ApplyTo(theme *TailwindTheme) {
	d.Theme.Apply(theme)
	if len(d.Utilities) > 0 {
		if theme.CustomUtilities == nil {
			theme.CustomUtilities = map[string]string{}
		}
		for k, v := range d.Utilities {
			theme.CustomUtilities[k] = v
		}
	}
	if len(d.Functional) > 0 {
		if theme.FunctionalUtilities == nil {
			theme.FunctionalUtilities = map[string]string{}
		}
		for k, v := range d.Functional {
			theme.FunctionalUtilities[k] = v
		}
	}
	if len(d.Variants) > 0 {
		if theme.CustomVariants == nil {
			theme.CustomVariants = map[string]string{}
		}
		for k, v := range d.Variants {
			theme.CustomVariants[k] = v
		}
	}
	if d.Prefix != "" {
		theme.Prefix = d.Prefix
	}
	if d.Important {
		theme.Important = "true"
	}
}

// expandBraces expands shell-style braces: "a-{1,2}" -> a-1 a-2, "{1..3}" and
// stepped ranges "{100..300..100}", nested groups, and whitespace separation.
func expandBraces(s string) []string {
	var out []string
	for _, w := range strings.Fields(s) {
		out = append(out, expandBraceWord(w)...)
	}
	return out
}

func expandBraceWord(w string) []string {
	open := strings.IndexByte(w, '{')
	if open < 0 {
		return []string{w}
	}
	depth, closeIdx := 0, -1
	for i := open; i < len(w); i++ {
		if w[i] == '{' {
			depth++
		} else if w[i] == '}' {
			depth--
			if depth == 0 {
				closeIdx = i
				break
			}
		}
	}
	if closeIdx < 0 {
		return []string{w}
	}
	pre, body, post := w[:open], w[open+1:closeIdx], w[closeIdx+1:]
	var alts []string
	if r := expandRange(body); r != nil {
		alts = r
	} else {
		depth, start := 0, 0
		for i := 0; i <= len(body); i++ {
			if i == len(body) || (body[i] == ',' && depth == 0) {
				alts = append(alts, body[start:i])
				start = i + 1
			} else if body[i] == '{' {
				depth++
			} else if body[i] == '}' {
				depth--
			}
		}
	}
	var out []string
	for _, a := range alts {
		out = append(out, expandBraceWord(pre+a+post)...)
	}
	return out
}

func expandRange(body string) []string {
	parts := strings.Split(body, "..")
	if len(parts) < 2 || len(parts) > 3 {
		return nil
	}
	from, err1 := strconv.Atoi(parts[0])
	to, err2 := strconv.Atoi(parts[1])
	step := 1
	if len(parts) == 3 {
		s, err := strconv.Atoi(parts[2])
		if err != nil || s <= 0 {
			return nil
		}
		step = s
	}
	if err1 != nil || err2 != nil || from > to || (to-from)/step > 10000 {
		return nil
	}
	var out []string
	for i := from; i <= to; i += step {
		out = append(out, strconv.Itoa(i))
	}
	return out
}

// MergeFrom folds o into d; o's values win on conflicts.
func (d *TailwindDirectives) MergeFrom(o *TailwindDirectives) {
	if o == nil {
		return
	}
	have := map[ThemeVar]bool{}
	for _, v := range d.Theme.Vars {
		have[v] = true
	}
	for _, v := range o.Theme.Vars {
		if !have[v] {
			d.Theme.Vars = append(d.Theme.Vars, v)
		}
	}
	for k, v := range o.Theme.Keyframes {
		if d.Theme.Keyframes == nil {
			d.Theme.Keyframes = map[string]string{}
		}
		d.Theme.Keyframes[k] = v
	}
	d.Theme.Warnings = append(d.Theme.Warnings, o.Theme.Warnings...)
	for k, v := range o.Utilities {
		d.Utilities[k] = v
	}
	for k, v := range o.Functional {
		d.Functional[k] = v
	}
	for k, v := range o.Variants {
		d.Variants[k] = v
	}
	for _, s := range o.Sources {
		if !d.seenSrc[s] {
			d.seenSrc[s] = true
			d.Sources = append(d.Sources, s)
		}
	}
	d.Excludes = append(d.Excludes, o.Excludes...)
	d.Inline = append(d.Inline, o.Inline...)
	d.NoAutoSource = d.NoAutoSource || o.NoAutoSource
	d.Imported = d.Imported || o.Imported
	d.Important = d.Important || o.Important
	if o.Prefix != "" {
		d.Prefix = o.Prefix
	}
	for k := range o.VarRefs {
		d.VarRefs[k] = true
	}
	for w := range o.warnings {
		d.warnings[w] = true
	}
}

var (
	sourcePathRe  = regexp.MustCompile(`(@source\s+(?:not\s+)?)(["'])([^"'\n]+)(["'])`)
	importSrcRe   = regexp.MustCompile(`(@import\s+["']tailwindcss(?:/[^"']*)?["'][^;{]*?\bsource\()(["'])([^"'\n]+)(["'])`)
	absolutizeFns = []*regexp.Regexp{sourcePathRe, importSrcRe}
)

// AbsolutizeSourcePaths rewrites relative paths in `@source "..."` and
// `@import "tailwindcss" source("...")` to absolute paths under dir, so they
// still resolve once the sheet has been merged into another file.
func AbsolutizeSourcePaths(cssText, dir string) string {
	if !strings.Contains(cssText, "source") {
		return cssText
	}
	for _, re := range absolutizeFns {
		cssText = re.ReplaceAllStringFunc(cssText, func(m string) string {
			sm := re.FindStringSubmatch(m)
			p := sm[3]
			if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
				return m
			}
			return sm[1] + sm[2] + filepath.ToSlash(filepath.Join(dir, p)) + sm[4]
		})
	}
	return cssText
}
