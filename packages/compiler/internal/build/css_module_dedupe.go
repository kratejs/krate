package build

import (
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
)

// moduleCSSRegistry remembers, per layout/slot source file, the scoped CSS of
// the CSS Modules its bundle contains, so a page can drop a module that both a
// layout and the page (or two layouts) pull in.
type moduleCSSRegistry struct {
	m sync.Map // source path -> []string
}

func (r *moduleCSSRegistry) store(path string, modules map[string]*bundler.CSSModuleInfo) {
	r.m.Store(path, moduleTexts(modules))
}

func (r *moduleCSSRegistry) load(path string) []string {
	if v, ok := r.m.Load(path); ok {
		return v.([]string)
	}
	return nil
}

func (r *moduleCSSRegistry) clear() { r.m.Clear() }

func moduleTexts(modules map[string]*bundler.CSSModuleInfo) []string {
	out := make([]string, 0, len(modules))
	for _, m := range modules {
		if m != nil && strings.TrimSpace(m.ScopedCSS) != "" {
			out = append(out, m.ScopedCSS)
		}
	}
	return out
}

// dedupeModuleCSS keeps the first occurrence of every CSS Module's scoped CSS
// in a page stylesheet assembled from several bundles (layouts, slots, page).
// Module CSS is scoped with unique class names, so dropping a later identical
// copy cannot change the cascade.
func (b *Builder) dedupeModuleCSS(page, cssText string, own map[string]*bundler.CSSModuleInfo) string {
	if cssText == "" {
		return cssText
	}
	cands := moduleTexts(own)
	for _, rb := range b.routeStack(page) {
		cands = append(cands, b.modCSS.load(rb.Path)...)
	}
	for _, t := range cands {
		first := strings.Index(cssText, t)
		if first < 0 {
			continue
		}
		end := first + len(t)
		if strings.Contains(cssText[end:], t) {
			cssText = cssText[:end] + strings.ReplaceAll(cssText[end:], t, "")
		}
	}
	return cssText
}
