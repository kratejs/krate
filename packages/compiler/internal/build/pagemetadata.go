package build

import (
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/migrate"
)

// pageMetadataLiteral returns the head tags for a page's `export const
// metadata` when every value is a literal the build can read without running
// code. declared reports that the page exports metadata at all; tags is empty
// when it does but some value needs evaluation (a module constant, a call),
// which pageMetadataHeadFor then runs (see metadataeval.go). The tags follow
// the page's own <Head> and precede the layout's head, and the first tag per
// key wins (dedupeHead), so an explicit <Head> overrides metadata and metadata
// overrides a layout default.
func pageMetadataLiteral(page, source string) (tags string, declared bool) {
	switch strings.ToLower(filepath.Ext(page)) {
	case ".md", ".mdx":
		return "", false
	}
	tags, declared = migrate.PageMetadataTags([]byte(source))
	if tags != "" && !strings.HasSuffix(tags, "\n") {
		tags += "\n"
	}
	return tags, declared
}
