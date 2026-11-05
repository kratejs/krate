package build

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// metaSegment is one level of a page's metadata chain: a layout that exports
// metadata (Path), and/or the images the convention files of that URL directory
// produced (Files, the JSON of a Metadata contribution).
type metaSegment struct {
	Path  string
	Files string
}

// metadataSegments returns the chain above page, outermost first, and the
// convention-file metadata of the page's own route.
func (b *Builder) metadataSegments(page string) (segs []metaSegment, pageFiles string) {
	if strings.HasSuffix(strings.ToLower(page), ".md") || strings.HasSuffix(strings.ToLower(page), ".mdx") {
		return nil, ""
	}
	pattern := ""
	if _, isSeg := segmentKindOfFile(page); !isSeg {
		pattern = strings.Trim(routeFromOutName(pageToOutput(page, b.Cfg.PagesDir)), "/")
	}
	layouts := map[string][]string{} // URL directory -> metadata layouts
	for _, rb := range b.routeStack(page) {
		if rb.Kind != "layout" {
			continue
		}
		src, err := os.ReadFile(rb.src())
		if err != nil {
			continue
		}
		if m, f := moduleMetadataExports(string(src)); m || f {
			dir := segmentURLDir(segmentDirKey(rb.src(), b.Cfg.PagesDir))
			layouts[dir] = append(layouts[dir], rb.src())
		}
	}
	var levels []string
	levels = append(levels, "")
	if pattern != "" {
		acc := ""
		for _, part := range strings.Split(pattern, "/") {
			acc = joinURLDir(acc, part)
			levels = append(levels, acc)
		}
	}
	for i, level := range levels {
		last := i == len(levels)-1
		files := b.metaImages.filesJSON(level)
		if last {
			// The page's own level: its files travel with the page module.
			pageFiles = files
			files = ""
		}
		for j, path := range layouts[level] {
			seg := metaSegment{Path: path}
			if j == 0 {
				seg.Files = files
				files = ""
			}
			segs = append(segs, seg)
		}
		if files != "" {
			segs = append(segs, metaSegment{Files: files})
		}
	}
	return segs, pageFiles
}

// metadataEntrySuffix exports the chain from a page's server bundle as
// __krateMetadata (and the page's own convention files as
// __krateMetadataFiles), so the renderer can resolve it per request.
func metadataEntrySuffix(segs []metaSegment, pageFiles, root string) string {
	if len(segs) == 0 && pageFiles == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n")
	var items []string
	for i, sg := range segs {
		var fields []string
		if sg.Path != "" {
			rel := sg.Path
			if r, err := filepath.Rel(root, sg.Path); err == nil {
				rel = filepath.ToSlash(r)
			}
			fmt.Fprintf(&b, "import * as __krateMd%d from %s;\n", i, strconv.Quote(filepath.ToSlash(sg.Path)))
			fields = append(fields, fmt.Sprintf("file: %s", strconv.Quote(rel)), fmt.Sprintf("mod: __krateMd%d", i))
		}
		if sg.Files != "" {
			fields = append(fields, "files: "+sg.Files)
		}
		items = append(items, "{ "+strings.Join(fields, ", ")+" }")
	}
	fmt.Fprintf(&b, "export const __krateMetadata = [%s];\n", strings.Join(items, ", "))
	if pageFiles != "" {
		fmt.Fprintf(&b, "export const __krateMetadataFiles = %s;\n", pageFiles)
	}
	return b.String()
}
