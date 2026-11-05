package build

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/migrate"
)

// pageMetadataHead returns the head tags for a page's `export const metadata`.
// They follow the page's own <Head> and precede the layout's head, and the
// first tag per key wins (dedupeHead), so an explicit <Head> overrides metadata
// and metadata overrides a layout default. A declaration with nothing the build
// can resolve warns instead of vanishing.
func (b *Builder) pageMetadataHead(page, source string) string {
	switch strings.ToLower(filepath.Ext(page)) {
	case ".md", ".mdx":
		return ""
	}
	tags, declared := migrate.PageMetadataTags([]byte(source))
	if !declared {
		return ""
	}
	if tags == "" {
		rel := page
		if r, err := filepath.Rel(b.Root, page); err == nil {
			rel = filepath.ToSlash(r)
		}
		printWarningsOnce(os.Stderr, []diag.Diagnostic{{
			File:     rel,
			Severity: diag.SeverityWarning,
			Code:     diag.CodeMetadataNotStatic,
			Message:  "metadata has no value that can be read at build time, so no tags were emitted",
			Hint:     "use literal values in `export const metadata`, or render <Head> from the page for values that depend on props",
		}})
		return ""
	}
	if tags != "" && !strings.HasSuffix(tags, "\n") {
		tags += "\n"
	}
	return tags
}
