package build

import (
	"path/filepath"
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
