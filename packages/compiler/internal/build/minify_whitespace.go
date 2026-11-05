package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// whiteSpaceRules names the elements whose text must keep its white space
// because a stylesheet sets white-space to a value that preserves it. A rule
// matches any element a selector of the rule could select (a class, id or tag
// anywhere in it), which can only keep more white space, never less.
type whiteSpaceRules struct {
	classes map[string]bool
	ids     map[string]bool
	tags    map[string]bool
	// all is set by a universal rule (`*`, html, body).
	all bool
}

// parseWhiteSpaceRules scans CSS text for rules that set white-space to pre,
// pre-wrap, pre-line, break-spaces, and returns nil when there are none.
func parseWhiteSpaceRules(css string) *whiteSpaceRules {
	var rules *whiteSpaceRules
	i := 0
	for i < len(css) {
		open := strings.IndexByte(css[i:], '{')
		if open < 0 {
			break
		}
		open += i
		sel := css[i:open]
		// Skip to the start of this rule's selector: after the previous `}` or `;`.
		if cut := strings.LastIndexAny(sel, "};"); cut >= 0 {
			sel = sel[cut+1:]
		}
		closeAt := strings.IndexByte(css[open+1:], '}')
		nextOpen := strings.IndexByte(css[open+1:], '{')
		if closeAt < 0 {
			break
		}
		if nextOpen >= 0 && nextOpen < closeAt {
			// An at-rule or nested rule: its contents are read as rules.
			i = open + 1
			continue
		}
		body := css[open+1 : open+1+closeAt]
		i = open + 1 + closeAt + 1
		if !declaresWhiteSpace(body) {
			continue
		}
		if rules == nil {
			rules = &whiteSpaceRules{classes: map[string]bool{}, ids: map[string]bool{}, tags: map[string]bool{}}
		}
		rules.addSelector(sel)
	}
	return rules
}

// declaresWhiteSpace reports whether a declaration block sets white-space
// (not white-space-collapse etc.) to a value that keeps spaces.
func declaresWhiteSpace(body string) bool {
	rest := strings.ToLower(body)
	for {
		i := strings.Index(rest, "white-space")
		if i < 0 {
			return false
		}
		after := strings.TrimLeft(rest[i+len("white-space"):], " \t\r\n")
		rest = rest[i+len("white-space"):]
		if !strings.HasPrefix(after, ":") {
			continue
		}
		val := strings.TrimLeft(after[1:], " \t\r\n")
		if end := strings.IndexAny(val, ";}"); end >= 0 {
			val = val[:end]
		}
		if preservesWhiteSpaceValue(val) {
			return true
		}
	}
}

func preservesWhiteSpaceValue(val string) bool {
	val = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(val), "!important"))
	switch val {
	case "pre", "pre-wrap", "pre-line", "break-spaces":
		return true
	}
	// A variable or other function may resolve to one of them.
	return strings.Contains(val, "var(") || strings.Contains(val, "(")
}

func (r *whiteSpaceRules) addSelector(sel string) {
	for _, part := range strings.Split(sel, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || part == "html" || part == "body" || part == ":root" {
			r.all = true
		}
		for i := 0; i < len(part); {
			c := part[i]
			switch {
			case c == '.' || c == '#':
				j := i + 1
				for j < len(part) && isSelectorIdentChar(part[j]) {
					if part[j] == '\\' {
						j++
					}
					j++
				}
				if j > len(part) {
					j = len(part)
				}
				name := unescapeCSSIdent(part[i+1 : j])
				if c == '.' {
					r.classes[name] = true
				} else {
					r.ids[name] = true
				}
				i = j
			case isASCIIAlpha(c):
				j := i
				for j < len(part) && isSelectorIdentChar(part[j]) {
					j++
				}
				r.tags[strings.ToLower(part[i:j])] = true
				i = j
			default:
				i++
			}
		}
	}
}

func isSelectorIdentChar(c byte) bool {
	return c == '-' || c == '_' || c == '\\' || c >= 0x80 || (c >= '0' && c <= '9') || isASCIIAlpha(c)
}

// unescapeCSSIdent undoes the backslash escapes of a class name in a selector
// (`.md\:whitespace-pre`).
func unescapeCSSIdent(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// merged returns the union of r and o; either may be nil.
func (r *whiteSpaceRules) merged(o *whiteSpaceRules) *whiteSpaceRules {
	if r == nil {
		return o
	}
	if o == nil {
		return r
	}
	out := &whiteSpaceRules{classes: map[string]bool{}, ids: map[string]bool{}, tags: map[string]bool{}, all: r.all || o.all}
	for _, src := range []*whiteSpaceRules{r, o} {
		for k := range src.classes {
			out.classes[k] = true
		}
		for k := range src.ids {
			out.ids[k] = true
		}
		for k := range src.tags {
			out.tags[k] = true
		}
	}
	return out
}

// preserves reports whether an element keeps the white space of its text: a
// stylesheet rule selects it, a utility class (Tailwind's whitespace-pre-wrap)
// names it, or its style attribute sets it.
func (r *whiteSpaceRules) preserves(tag, class, id, style string) bool {
	if style != "" && declaresWhiteSpace(style) {
		return true
	}
	for _, token := range strings.Fields(class) {
		if strings.Contains(token, "whitespace-pre") || strings.Contains(token, "whitespace-break-spaces") ||
			strings.Contains(token, "white-space:pre") || strings.Contains(token, "white-space:break") {
			return true
		}
		if r != nil && r.classes[token] {
			return true
		}
	}
	if r == nil {
		return false
	}
	return r.all || r.tags[tag] || (id != "" && r.ids[id])
}

// inlineStyleRules reads the rules of every <style> element in a document.
func inlineStyleRules(doc string) *whiteSpaceRules {
	var rules *whiteSpaceRules
	lower := strings.ToLower(doc)
	for from := 0; ; {
		open := strings.Index(lower[from:], "<style")
		if open < 0 {
			return rules
		}
		open += from
		gt := strings.IndexByte(lower[open:], '>')
		if gt < 0 {
			return rules
		}
		start := open + gt + 1
		end := strings.Index(lower[start:], "</style")
		if end < 0 {
			end = len(lower) - start
		}
		rules = rules.merged(parseWhiteSpaceRules(doc[start : start+end]))
		from = start + end
	}
}

var (
	linkTagRe  = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	linkAttrRe = regexp.MustCompile(`(?is)\b(rel|href)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
)

type cachedWhiteSpaceRules struct {
	size  int64
	mtime int64
	rules *whiteSpaceRules
}

var whiteSpaceRuleCache sync.Map // path -> cachedWhiteSpaceRules

// linkedWhiteSpaceRules returns the white-space rules of the local stylesheets
// a document links: files in the output directory (page and Tailwind CSS) or
// in the project's public directory.
func (b *Builder) linkedWhiteSpaceRules(doc string) *whiteSpaceRules {
	var rules *whiteSpaceRules
	base := strings.TrimSuffix(b.Cfg.BaseURLPath(), "/")
	for _, tag := range linkTagRe.FindAllString(doc, -1) {
		var rel, href string
		for _, m := range linkAttrRe.FindAllStringSubmatch(tag, -1) {
			val := m[2] + m[3] + m[4]
			if strings.EqualFold(m[1], "rel") {
				rel = strings.ToLower(val)
			} else {
				href = val
			}
		}
		if !strings.Contains(rel, "stylesheet") || href == "" || strings.Contains(href, "://") || strings.HasPrefix(href, "//") {
			continue
		}
		if i := strings.IndexAny(href, "?#"); i >= 0 {
			href = href[:i]
		}
		href = strings.TrimPrefix(strings.TrimPrefix(href, base), "/")
		if href == "" || strings.Contains(href, "..") {
			continue
		}
		for _, dir := range []string{b.Cfg.OutDir, filepath.Join(b.Root, "public")} {
			if r, ok := fileWhiteSpaceRules(filepath.Join(dir, filepath.FromSlash(href))); ok {
				rules = rules.merged(r)
				break
			}
		}
	}
	return rules
}

func fileWhiteSpaceRules(path string) (*whiteSpaceRules, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, false
	}
	if v, ok := whiteSpaceRuleCache.Load(path); ok {
		c := v.(cachedWhiteSpaceRules)
		if c.size == info.Size() && c.mtime == info.ModTime().UnixNano() {
			return c.rules, true
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	rules := parseWhiteSpaceRules(string(data))
	whiteSpaceRuleCache.Store(path, cachedWhiteSpaceRules{size: info.Size(), mtime: info.ModTime().UnixNano(), rules: rules})
	return rules, true
}
