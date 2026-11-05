package build

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The metadata chain of a page: every layout above it that exports `metadata`
// or `generateMetadata`, outermost first, then the page itself. The Next.js
// Metadata API merges them from the root down (see runtime/src/metadata.ts).

var (
	metadataExportRe   = regexp.MustCompile(`(?m)^\s*export\s+(?:const|let|var)\s+(?:metadata|viewport)\b`)
	viewportExportRe   = regexp.MustCompile(`(?m)^\s*export\s+(?:const|let|var)\s+viewport\b`)
	generateMetadataRe = regexp.MustCompile(`(?m)^\s*export\s+(?:async\s+)?function\s*\*?\s*generateMetadata\b|^\s*export\s+(?:const|let|var)\s+generateMetadata\b`)
	metadataListRe     = regexp.MustCompile(`(?m)^\s*export\s*\{[^}]*\b(metadata|generateMetadata)\b[^}]*\}`)
)

// moduleMetadataExports reports whether a module exports `metadata` and
// whether it exports a `generateMetadata` function.
func moduleMetadataExports(src string) (hasMetadata, hasFunc bool) {
	hasMetadata = metadataExportRe.MatchString(src)
	hasFunc = generateMetadataRe.MatchString(src)
	for _, m := range metadataListRe.FindAllStringSubmatch(src, -1) {
		for _, name := range regexp.MustCompile(`\b(metadata|generateMetadata)\b`).FindAllString(m[0], -1) {
			if name == "metadata" {
				hasMetadata = true
			} else {
				hasFunc = true
			}
		}
	}
	return hasMetadata, hasFunc
}

// metadataLayouts returns the layouts above page that export metadata or
// generateMetadata, outermost first, as absolute paths.
func (b *Builder) metadataLayouts(page string) []string {
	if strings.HasSuffix(strings.ToLower(page), ".md") || strings.HasSuffix(strings.ToLower(page), ".mdx") {
		return nil
	}
	type layoutAt struct {
		path  string
		depth int
	}
	var found []layoutAt
	for _, rb := range b.routeStack(page) {
		if rb.Kind != "layout" {
			continue
		}
		src, err := os.ReadFile(rb.src())
		if err != nil {
			continue
		}
		if m, f := moduleMetadataExports(string(src)); m || f {
			found = append(found, layoutAt{rb.src(), routeDepth(rb.Dir, b.Cfg.PagesDir)})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].depth < found[j].depth })
	out := make([]string, len(found))
	for i, f := range found {
		out[i] = f.path
	}
	return out
}

// metadataEntrySuffix exports the layouts' modules from a page's server bundle
// as __krateMetadata, so the renderer can resolve the chain per request.
func metadataEntrySuffix(layouts []string, root string) string {
	if len(layouts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n")
	var items []string
	for i, l := range layouts {
		rel := l
		if r, err := filepath.Rel(root, l); err == nil {
			rel = filepath.ToSlash(r)
		}
		fmt.Fprintf(&b, "import * as __krateMd%d from %s;\n", i, strconv.Quote(filepath.ToSlash(l)))
		items = append(items, fmt.Sprintf("{ file: %s, mod: __krateMd%d }", strconv.Quote(rel), i))
	}
	fmt.Fprintf(&b, "export const __krateMetadata = [%s];\n", strings.Join(items, ", "))
	return b.String()
}
