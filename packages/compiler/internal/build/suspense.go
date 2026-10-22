package build

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// writeLazyChunks writes the hydration code of each lazy() component next to
// the page script (lazy-<Name>.<hash>.js) and replaces its placeholder in the
// page script with the chunk's URL. The page script loads the chunk with
// __krateLazy when it hydrates, so the code is fetched on demand rather than
// shipped in the page bundle.
func (b *Builder) writeLazyChunks(pageJS string, chunks []renderer.LazyChunk, pageDir, route string) string {
	for _, c := range chunks {
		code := strings.TrimSpace(c.JS)
		if b.shouldMinifyJS() {
			code = strings.TrimSpace(minifyJS(code))
		}
		name := "lazy-" + c.Name + "." + hashContent([]byte(code)) + ".js"
		_ = fsutil.WriteFileIfChanged(filepath.Join(pageDir, name), []byte(code), 0644)
		pageJS = strings.ReplaceAll(pageJS, c.Token, pageScriptSrc(route, name, b.Cfg.BaseURLPath()))
	}
	return pageJS
}

// suspenseMarkers are hydration identifiers provided by the Suspense runtime
// bundle (krate-suspense.js).
var suspenseMarkers = []string{
	"__krateBoundary", "__krateUse", "__krateLazy", "__krateTransitionStarter",
	"startTransition", "useTransition", "useDeferredValue", "createDeferredValue",
	"useSyncExternalStore", "createExternalStore", "runAsyncTransition",
}

// runtimeNeedsSuspense reports whether any page's hydration script uses the
// Suspense/transition runtime.
func runtimeNeedsSuspense(results []*PageResult) bool {
	for _, r := range results {
		if r == nil || r.HydrationJS == "" {
			continue
		}
		for _, marker := range suspenseMarkers {
			if strings.Contains(r.HydrationJS, marker) {
				return true
			}
		}
	}
	return false
}

var lazyChunkURLRe = regexp.MustCompile(`__krateLazy\("([^"]+\.js)"`)

// lazyChunkPreloads returns the lazy chunk URLs a page script loads, relative
// to the base path, for <link rel="modulepreload">. The fetch then overlaps
// the page load instead of starting at hydration (the module still only runs
// when the component first renders), so a lazy boundary reveals on the same
// schedule as React, whose lazy code is usually already loaded.
func lazyChunkPreloads(pageJS, basePath string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range lazyChunkURLRe.FindAllStringSubmatch(pageJS, -1) {
		url := strings.TrimPrefix(m[1], basePath)
		if !seen[url] {
			seen[url] = true
			out = append(out, url)
		}
	}
	return out
}
