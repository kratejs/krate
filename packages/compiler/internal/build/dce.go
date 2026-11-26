package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/css"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// aggressiveDCE reports whether JS dead-code elimination may drop local helper
// functions the reference scan cannot prove are used.
func (b *Builder) aggressiveDCE() bool {
	return b.Cfg.Dce.JSEnabled() && b.Cfg.Dce.AggressiveEnabled()
}

// dceCSSEnabled reports whether CSS dead-code elimination may run for this
// build. It is skipped when disabled, or when plugins could have injected class
// names the static scan cannot see.
func (b *Builder) dceCSSEnabled() bool {
	if !b.Cfg.Dce.CSSEnabled() {
		return false
	}
	if b.plugins.HasPerPageHooks() || hasCommunityPlugins(b.Cfg) {
		return false
	}
	return true
}

// pageCSSAfterDCE prunes unused CSS-module rules from a page's own CSS. It is a
// no-op unless DCE is enabled and the page is statically rendered (SSR/ISR pages
// are left untouched). clientClasses are the scoped classes of CSS modules
// that client island code imports: they are kept whole, since the island may
// render any of them after interaction and its chunk text is not scanned.
func (b *Builder) pageCSSAfterDCE(renderMode RenderMode, cssText string, modules map[string]*bundler.CSSModuleInfo, clientClasses map[string]bool, html string, clientJS ...string) string {
	if !b.dceCSSEnabled() || renderMode != RenderSSG || cssText == "" {
		return cssText
	}
	scoped := moduleClassSet(modules)
	if len(scoped) == 0 {
		return cssText
	}
	used := collectClassTokens(html)
	for cls := range clientClasses {
		used[cls] = true
	}
	// Classes applied only on the client (signal-driven class expressions) show
	// up in the emitted JS. Scoped names carry a unique hash suffix, so a
	// substring search over the JS is safe.
	for cls := range scoped {
		if used[cls] {
			continue
		}
		for _, js := range clientJS {
			if js != "" && strings.Contains(js, cls) {
				used[cls] = true
				break
			}
		}
	}
	// Prune each source on its own: a part marker must not hide the selector
	// of the first rule after it.
	return css.MapParts(cssText, func(text string) string { return pruneModuleCSS(text, scoped, used) })
}

// clientChunkSources returns the contents of the "use client" chunks already
// written under the output directory, for the CSS usage scan. The scan finds
// the same scoped classes of modules in them as in separate builds.
func (b *Builder) clientChunkSources(boundaries []ClientBoundary, modules map[string]*bundler.CSSModuleInfo) []string {
	var out []string
	var scoped map[string]bool
	for _, cb := range boundaries {
		b.clientChunkMu.Lock()
		text, ok := b.clientChunkText[cb.ChunkURL]
		info, measured := b.clientChunks[cb.SourcePath]
		b.clientChunkMu.Unlock()
		if ok {
			if f := info.split; measured && f != nil && info.url == cb.ChunkURL && b.dceCSSEnabled() {
				// The text of a split batch also holds code only other
				// boundaries use: a class only that code names needs the
				// chunk's own check.
				if scoped == nil {
					scoped = moduleClassSet(modules)
				}
				for cls := range scoped {
					if strings.Contains(text, cls) && !strings.Contains(f.ownText, cls) {
						if _, exact, err := b.exactChunk(info); err == nil {
							text = exact
						}
						break
					}
				}
			}
			out = append(out, text)
			continue
		}
		i := strings.LastIndex(cb.ChunkURL, "chunks/")
		if i < 0 {
			continue
		}
		if data, err := os.ReadFile(filepath.Join(b.Cfg.OutDir, filepath.FromSlash(cb.ChunkURL[i:]))); err == nil {
			out = append(out, string(data))
		}
	}
	return out
}

// lazyChunkSources returns the hydration code of lazy() chunks.
func lazyChunkSources(chunks []renderer.LazyChunk) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, c.JS)
	}
	return out
}

var classAttrRe = regexp.MustCompile(`class="([^"]*)"`)

// collectClassTokens returns the set of class tokens present in an HTML fragment.
func collectClassTokens(htmlStr string) map[string]bool {
	used := make(map[string]bool)
	for _, m := range classAttrRe.FindAllStringSubmatch(htmlStr, -1) {
		for _, tok := range strings.Fields(m[1]) {
			used[tok] = true
		}
	}
	return used
}

// moduleClassSet collects every scoped (hashed) class name a bundle's CSS
// modules produced, so pruning can be limited to module-owned rules.
func moduleClassSet(modules map[string]*bundler.CSSModuleInfo) map[string]bool {
	set := make(map[string]bool)
	for _, m := range modules {
		for _, scoped := range m.Mappings {
			// Composed classes map to "a_h b_h".
			for _, cls := range strings.Fields(scoped) {
				set[cls] = true
			}
		}
	}
	return set
}

// pruneModuleCSS removes CSS rules that only target CSS-module classes the page
// never uses. Pruning is per class: a selector is dropped only when every
// class in it is a module class that is unused, so a class's base rule and its
// pseudo or compound rules are kept or dropped together. Only classes in the
// bundle's module mappings are candidates, so global, Tailwind, and component
// styles are never touched. Rules inside @media, @supports, @container and
// @layer blocks are pruned the same way; other at-rules are kept whole.
func pruneModuleCSS(cssText string, scoped, used map[string]bool) string {
	if cssText == "" || len(scoped) == 0 {
		return cssText
	}
	rules := css.SplitCSSRules(cssText)
	out := make([]string, 0, len(rules))
	changed := false
	for _, rule := range rules {
		kept, ok := pruneRule(rule, scoped, used)
		if !ok || kept != rule {
			changed = true
		}
		if ok {
			out = append(out, kept)
		}
	}
	if !changed {
		return cssText
	}
	return strings.Join(out, "\n")
}

// pruneRule returns the rule to emit and whether any of it survives.
func pruneRule(rule string, scoped, used map[string]bool) (string, bool) {
	open := strings.IndexByte(rule, '{')
	if open < 0 {
		return rule, true
	}
	selector := strings.TrimSpace(rule[:open])
	if selector == "" {
		return rule, true
	}
	if strings.HasPrefix(selector, "@") {
		if !isGroupingAtRule(selector) || !strings.HasSuffix(rule, "}") {
			return rule, true
		}
		body := rule[open+1 : len(rule)-1]
		pruned := pruneModuleCSS(body, scoped, used)
		if pruned == body {
			return rule, true
		}
		if strings.TrimSpace(pruned) == "" {
			return "", false
		}
		return selector + "{" + pruned + "}", true
	}
	return rule, keepCSSRule(rule, scoped, used)
}

func isGroupingAtRule(sel string) bool {
	for _, p := range []string{"@media", "@supports", "@container", "@layer"} {
		if strings.HasPrefix(sel, p) {
			return true
		}
	}
	return false
}

var selectorClassRe = regexp.MustCompile(`\.(-?[_a-zA-Z\x{80}-\x{10FFFF}][-_a-zA-Z0-9\x{80}-\x{10FFFF}]*)`)

// keepCSSRule reports whether a style rule may match the page. A comma part
// is droppable only when it names at least one module class and every class
// it names is an unused module class; any other class (global, escaped,
// unknown) keeps the part.
func keepCSSRule(rule string, scoped, used map[string]bool) bool {
	open := strings.IndexByte(rule, '{')
	if open < 0 {
		return true
	}
	selector := strings.TrimSpace(rule[:open])
	if selector == "" || strings.HasPrefix(selector, "@") {
		return true // at-rule or malformed: keep
	}
	for _, part := range splitSelectorList(selector) {
		if strings.ContainsAny(part, "\\\"'") {
			return true // escapes and attribute values: keep
		}
		classes := selectorClassRe.FindAllStringSubmatch(part, -1)
		if len(classes) == 0 {
			return true // element/id/attr selector: keep
		}
		for _, m := range classes {
			if !scoped[m[1]] || used[m[1]] {
				return true // global class or a class the page uses: keep
			}
		}
	}
	return false // every selector only needs unused module classes: drop
}

// splitSelectorList splits a selector list at top-level commas, so commas in
// :is(), :not() and attribute selectors do not split a selector.
func splitSelectorList(sel string) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(sel); i++ {
		switch sel[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(sel[start:i]))
				start = i + 1
			}
		}
	}
	return append(parts, strings.TrimSpace(sel[start:]))
}

// islandClasses returns the scoped classes of CSS modules the page's islands
// import, or nil when it has none.
func islandClasses(st *islandStyles) map[string]bool {
	if st == nil {
		return nil
	}
	return st.Classes
}
