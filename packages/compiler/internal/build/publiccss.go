package build

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

var (
	anyLinkTagRe  = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	relStyleRe    = regexp.MustCompile(`(?i)\brel\s*=\s*["']?\s*stylesheet\b`)
	linkHrefRe    = regexp.MustCompile(`(?i)\bhref\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	publicCSSExts = map[string]bool{".css": true}
)

// warnPublicStylesheets warns, once per file, about stylesheets in a page's
// <head> that are served straight from public/. Such files bypass the CSS
// pipeline: they are neither minified nor content-hashed.
func (b *Builder) warnPublicStylesheets(doc string) {
	if ds := b.publicStylesheetWarnings(doc); len(ds) > 0 {
		printTreeWarnings(b.stderr(), ds)
	}
}

// publicStylesheetWarnings returns the warnings for the public/ stylesheets
// the document links that have not been reported yet by this builder.
func (b *Builder) publicStylesheetWarnings(doc string) []diag.Diagnostic {
	var out []diag.Diagnostic
	if b.Cfg.PublicDir == "" {
		return nil
	}
	end := strings.Index(doc, "</head>")
	if end < 0 {
		return nil
	}
	for _, tag := range anyLinkTagRe.FindAllString(doc[:end], -1) {
		if !relStyleRe.MatchString(tag) {
			continue
		}
		m := linkHrefRe.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		href := m[1] + m[2] + m[3]
		if i := strings.IndexAny(href, "?#"); i >= 0 {
			href = href[:i]
		}
		if !strings.HasPrefix(href, "/") || strings.HasPrefix(href, "//") || !publicCSSExts[strings.ToLower(path.Ext(href))] {
			continue
		}
		if base := b.Cfg.BaseURLPath(); base != "" && strings.HasPrefix(href, base+"/") {
			href = strings.TrimPrefix(href, base)
		}
		clean := path.Clean(href)
		file := filepath.Join(b.Cfg.PublicDir, filepath.FromSlash(clean))
		if fi, err := os.Stat(file); err != nil || fi.IsDir() {
			continue
		}
		key := "public-css:" + clean
		b.twMu.Lock()
		if b.twWarned == nil {
			b.twWarned = map[string]bool{}
		}
		seen := b.twWarned[key]
		b.twWarned[key] = true
		b.twMu.Unlock()
		if seen {
			continue
		}
		rel, err := filepath.Rel(b.Root, file)
		if err != nil {
			rel = file
		}
		out = append(out, diag.Diagnostic{
			File:     filepath.ToSlash(rel),
			Line:     1,
			Col:      1,
			Severity: diag.SeverityWarning,
			Code:     diag.CodePublicStylesheet,
			Message:  fmt.Sprintf("%s is linked with <link rel=\"stylesheet\"> straight from public/, so it is not minified or content-hashed", clean),
			Hint:     "move it under src/ and import it from the layout or page (import \"../styles/site.css\") so the CSS pipeline handles it",
		})
	}
	return out
}
