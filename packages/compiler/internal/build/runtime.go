package build

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	irtree "github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// runtimeBundleMemo caches loadNamedBundleFromDisk results (hits and misses)
// within a build. The runtime chunk is written on multiple build paths and each
// call otherwise probes several candidate directories with os.ReadFile. Cleared
// per build via resetRuntimeBundleMemo so a rebuilt runtime or a newly added dev
// bundle is picked up on the next build.
var runtimeBundleMemo = struct {
	mu sync.Mutex
	m  map[string]string
}{m: make(map[string]string)}

func resetRuntimeBundleMemo() {
	runtimeBundleMemo.mu.Lock()
	defer runtimeBundleMemo.mu.Unlock()
	runtimeBundleMemo.m = make(map[string]string)
}

func loadRuntimeFromDisk(projectRoot string) string {
	return loadNamedBundleFromDisk(projectRoot, "krate-hydrate.js", "krate-runtime.js")
}

// loadRuntimeFromDiskFor selects the full hydration runtime or the lite build
// without the SPA router. Older runtime packages may not ship the lite bundle,
// so fall back to the full one rather than failing the build.
func loadRuntimeFromDiskFor(projectRoot string, spa bool) string {
	if spa {
		return loadRuntimeFromDisk(projectRoot)
	}
	return loadNamedBundleFromDisk(projectRoot, "krate-hydrate-lite.js", "krate-hydrate.js", "krate-runtime.js")
}

// runtimeChunkOptions selects which optional pieces are appended to the shared
// runtime chunk.
type runtimeChunkOptions struct {
	SPA    bool
	Compat bool
	// Suspense appends the Suspense/use()/lazy/transition runtime.
	Suspense bool
	// Trim assembles only the runtime features Scan references (production
	// builds); otherwise the full hydration bundle is written.
	Trim bool
	// Scan is the client code checked for runtime feature references.
	Scan string
}

// runtimeNeedsCompat reports whether any page's hydration script references a
// React compatibility API that lives in CompatBootstrapJS.
func runtimeNeedsCompat(results []*PageResult) bool {
	markers := []string{
		"useImperativeHandle", "useDebugValue",
		"isValidElement", "Children", "cloneElement", "createRef",
		// Lazy chunks are separate files; their code may use compat APIs.
		"__krateLazy(",
	}
	for _, r := range results {
		if r == nil || r.HydrationJS == "" {
			continue
		}
		for _, marker := range markers {
			if strings.Contains(r.HydrationJS, marker) {
				return true
			}
		}
	}
	return false
}

// pageNeedsRuntime reports whether a page's document must load the shared
// runtime chunk. A page that has no hydration script, no client islands and no
// server action forms never calls into the runtime, so it ships none even when
// other pages of the site do. That holds for pages rendered per request (SSR,
// ISR) too: what they render is known from their module graph, so a page whose
// graph has no client code stays script-free. Streaming pages and pages with
// runtime-component regions always get it. The SPA router is started by a
// page's hydration script, so such a page has no router to lose; one already
// loaded by an earlier page stays in memory across client-side navigations.
func pageNeedsRuntime(r *PageResult) bool {
	if r == nil {
		return false
	}
	if r.HasJS || r.JSFile != "" || r.HydrationJS != "" {
		return true
	}
	if perRequestClientCode(r) {
		return true
	}
	return strings.Contains(r.HTML, "<"+islandMarkup) ||
		strings.Contains(r.HTML, actionsMarkup) ||
		strings.Contains(r.HTML, "/__krate/action/")
}

// perRequestClientCode reports whether the HTML a page produces per request
// can carry client code the build never saw: streamed or runtime regions, an
// island that hydrates, or a server action form.
func perRequestClientCode(r *PageResult) bool {
	return r.Mode == RenderStreaming || len(r.Regions) > 0 ||
		anyBoundaryHydrates(r.ClientBoundaries) || r.ServerActions
}

// hookCallRe matches a call to a hook: client code that may have effects.
var hookCallRe = regexp.MustCompile(`\buse[A-Z]\w*\(`)

// requestTimeInert reports whether the hydration of a page rendered per
// request would only re-evaluate expressions that read no signal. The server
// renders those with the real request, server-only values included, so
// running them again in the browser would show nothing or something else, and
// would ship the server code that computes them. Such a page needs no
// hydration.
func requestTimeInert(mode RenderMode, sigs []irtree.ComponentSignature) bool {
	if mode != RenderSSR && mode != RenderISR {
		return false
	}
	for _, sig := range sigs {
		if len(sig.Signals) > 0 || len(sig.Handlers) > 0 || len(sig.RefBindings) > 0 ||
			len(sig.Effects) > 0 || len(sig.Memos) > 0 || len(sig.FuncPropAliases) > 0 ||
			len(sig.IslandBoot) > 0 || len(sig.BodyUses) > 0 || len(sig.AttrBindings) > 0 ||
			sig.Lazy || sig.Class {
			return false
		}
		for _, list := range [][]string{sig.ExtraVars, sig.PreSignalVars} {
			for _, v := range list {
				if hookCallRe.MatchString(v) {
					return false
				}
			}
		}
	}
	return true
}

// loadDevBundleFromDisk loads the dev-only overlay/toolbar bundle
// (krate-dev.js) from the @krate/runtime dist directory, if present.
func loadDevBundleFromDisk(projectRoot string) string {
	return loadNamedBundleFromDisk(projectRoot, "krate-dev.js")
}

func loadNamedBundleFromDisk(projectRoot string, names ...string) string {
	memoKey := projectRoot + "\x00" + strings.Join(names, "\x00")
	runtimeBundleMemo.mu.Lock()
	if v, ok := runtimeBundleMemo.m[memoKey]; ok {
		runtimeBundleMemo.mu.Unlock()
		return v
	}
	runtimeBundleMemo.mu.Unlock()

	body := probeNamedBundleFromDisk(projectRoot, names)
	runtimeBundleMemo.mu.Lock()
	runtimeBundleMemo.m[memoKey] = body
	runtimeBundleMemo.mu.Unlock()
	return body
}

func probeNamedBundleFromDisk(projectRoot string, names []string) string {

	// Build candidate directories relative to the project root.
	// findKrateRoot walks up from projectRoot to locate the compiler package.
	var candidateDirs []string

	// 1. From project root, walk up to find the krate compiler directory
	krateRoot := findKrateRoot(projectRoot)
	if krateRoot != "" {
		// krateRoot = packages/compiler - runtime is at packages/runtime/dist/
		monorepoRoot := filepath.Dir(krateRoot)
		candidateDirs = append(candidateDirs, filepath.Join(monorepoRoot, "packages", "runtime", "dist"))
	}

	// 2. CWD-relative paths (works when running CLI from packages/compiler/)
	candidateDirs = append(candidateDirs,
		"packages/runtime/dist",
		"../runtime/dist",
	)

	// 3. node_modules fallback (for published packages)
	candidateDirs = append(candidateDirs, "node_modules/@krate/runtime/dist")

	// 4. Binary-relative path (works when installed as a Go binary)
	candidateDirs = append(candidateDirs, filepath.Join(filepath.Dir(os.Args[0]), "..", "runtime", "dist"))

	// 5. Walk up from project root checking each parent for packages/runtime/dist/
	dir := projectRoot
	for {
		candidate := filepath.Join(dir, "packages", "runtime", "dist")
		candidateDirs = append(candidateDirs, candidate)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	seen := make(map[string]bool)
	var uniqueDirs []string
	for _, d := range candidateDirs {
		if !seen[d] {
			seen[d] = true
			uniqueDirs = append(uniqueDirs, d)
		}
	}

	for _, name := range names {
		for _, dir := range uniqueDirs {
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err == nil {
				return string(data)
			}
		}
	}

	return ""
}

// writeRuntimeChunk writes the krate runtime to a shared chunk file in dist/chunks/.
// Returns the relative path (e.g. "chunks/runtime.abc123.js") for use in <script> tags.
// Returns empty string if runtime is unavailable.
func writeRuntimeChunk(outDir string, shouldMinify bool, projectRoot string, opts runtimeChunkOptions) (string, error) {
	runtime := ""
	if opts.Trim {
		runtime, _ = loadRuntimeFeatures(projectRoot, opts.SPA, opts.Scan)
	}
	if runtime == "" {
		runtime = loadRuntimeFromDiskFor(projectRoot, opts.SPA)
	}
	if runtime == "" {
		return "", nil
	}

	// Append the shared hydration infrastructure (findSlot, __safe, $esc,
	// kbind* helpers) so per-page hydration scripts only contain their own
	// signals and binding calls instead of duplicating this boilerplate.
	runtime += renderer.HydrationBootstrapJS
	if opts.Compat {
		runtime += renderer.CompatBootstrapJS
	}
	if opts.Suspense {
		suspense := loadNamedBundleFromDisk(projectRoot, "krate-suspense.js")
		if suspense == "" {
			fmt.Fprintf(os.Stderr, "  %sWarning:%s the Suspense runtime (krate-suspense.js) was not found next to the hydration runtime; Suspense, use(), lazy() and transitions will not work (rebuild @krate/runtime)\n", cYellow, cReset)
		}
		runtime += "\n" + suspense
	}

	runtimeToWrite := runtime
	if shouldMinify {
		runtimeToWrite = minifyJSBase(runtime)
	}

	hash := hashContent([]byte(runtimeToWrite))
	filename := "chunks/runtime." + hash + ".js"

	if err := writeOutput(filepath.Join(outDir, filename), []byte(runtimeToWrite)); err != nil {
		return "", err
	}

	return filename, nil
}

// devFallbackJS is written as chunks/krate-dev.js when the @krate/runtime dev
// bundle is unavailable (e.g. a stripped install), so dev mode still gets
// reload + error logging.
const devFallbackJS = `(function(){var s=new EventSource('/__krate/hotreload');` +
	`s.addEventListener('reload',function(){location.reload()});` +
	`s.addEventListener('build-error',function(e){try{console.error('Krate build failed',JSON.parse(e.data).diagnostics)}catch(_){}});` +
	`s.addEventListener('client-error',function(e){try{console.error('Krate client error',JSON.parse(e.data).clientError)}catch(_){}});})();`

// writeDevChunk writes the dev-only overlay/toolbar script to a fixed path
// (chunks/krate-dev.js) so dev HTML can reference it without threading a hashed
// name through the page pipeline. It is intentionally unminified and only
// written in dev mode.
func writeDevChunk(outDir, projectRoot string) (string, error) {
	content := loadDevBundleFromDisk(projectRoot)
	if content == "" {
		content = devFallbackJS
	}
	relPath := "chunks/krate-dev.js"
	if err := writeOutput(filepath.Join(outDir, relPath), []byte(content)); err != nil {
		return "", err
	}
	return relPath, nil
}

// warnMissingRuntime prints a diagnostic when pages ship hydration JS but the
// runtime chunk could not be written, which would otherwise produce pages that
// fail in the browser (e.g. "refreshSlots is not defined"). It is a warning,
// not an error, because the runtime may be supplied by a later deploy step.
func warnMissingRuntime(runtimeJS string, results []*PageResult) {
	if runtimeJS != "" {
		return
	}
	for _, r := range results {
		if r != nil && r.HasJS {
			d := diag.Diagnostic{
				File:     r.SourcePath,
				Message:  "the @krate/runtime hydration bundle (krate-hydrate.js) was not found; interactive pages will fail to hydrate in the browser",
				Hint:     "build the runtime (`pnpm --filter @krate/runtime build`) or install @krate/runtime so packages/runtime/dist or node_modules/@krate/runtime/dist exists",
				Severity: diag.SeverityWarning,
			}
			fmt.Fprintf(os.Stderr, "  %sWarning:%s %s\n      hint: %s\n", cYellow, cReset, d.Error(), d.Hint)
			return
		}
	}
}

// runtimeChunkOptions derives the shared runtime chunk's contents from the
// built pages. Dev builds always write the full runtime: a rebuild of a few
// pages must not change what the others were built against.
func (b *Builder) runtimeChunkOptions(results []*PageResult) runtimeChunkOptions {
	return runtimeChunkOptions{
		SPA:      b.Cfg.SPAEnabled(),
		Compat:   runtimeNeedsCompat(results),
		Suspense: runtimeNeedsSuspense(results),
		Trim:     !b.DevMode,
		Scan:     runtimeScanText(results),
	}
}
