package build

import (
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/css"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
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
	if plugin.HasPerPageHooks() || hasCommunityPlugins(b.Cfg) {
		return false
	}
	return true
}

// pageCSSAfterDCE prunes unused CSS-module rules from a page's own CSS. It is a
// no-op unless DCE is enabled and the page is statically rendered (SSR/ISR pages
// are left untouched).
func (b *Builder) pageCSSAfterDCE(renderMode RenderMode, cssText string, modules map[string]*bundler.CSSModuleInfo, html string) string {
	if !b.dceCSSEnabled() || renderMode != RenderSSG || cssText == "" {
		return cssText
	}
	scoped := moduleClassSet(modules)
	if len(scoped) == 0 {
		return cssText
	}
	return pruneModuleCSS(cssText, scoped, collectClassTokens(html))
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
			set[scoped] = true
		}
	}
	return set
}

// pruneModuleCSS removes CSS rules that consist solely of CSS-module class
// selectors which do not appear in the page's emitted HTML. Only classes that
// belong to the bundle's module mappings are candidates, so global, Tailwind,
// and component styles are never touched. Rules with compound selectors,
// pseudo-classes, or at-rules are always kept (conservative).
func pruneModuleCSS(cssText string, scoped, used map[string]bool) string {
	if cssText == "" || len(scoped) == 0 {
		return cssText
	}
	rules := css.SplitCSSRules(cssText)
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		if keepCSSRule(rule, scoped, used) {
			out = append(out, rule)
		}
	}
	if len(out) == len(rules) {
		return cssText
	}
	return strings.Join(out, "\n")
}

func keepCSSRule(rule string, scoped, used map[string]bool) bool {
	open := strings.IndexByte(rule, '{')
	if open < 0 {
		return true
	}
	selector := strings.TrimSpace(rule[:open])
	if selector == "" || strings.HasPrefix(selector, "@") {
		return true // at-rule or malformed: keep
	}
	for _, part := range strings.Split(selector, ",") {
		p := strings.TrimSpace(part)
		if !strings.HasPrefix(p, ".") {
			return true // element/id/attr selector: keep
		}
		cls := p[1:]
		if cls == "" || strings.ContainsAny(cls, " \t\r\n>+~:.#[*") {
			return true // compound / pseudo / nested: keep
		}
		if !scoped[cls] {
			return true // not a module class: keep
		}
		if used[cls] {
			return true // referenced by the page: keep
		}
	}
	return false // every selector is an unused module class: drop
}
