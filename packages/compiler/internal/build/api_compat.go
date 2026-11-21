package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

// embeddedRuntime reports whether a runtime option (ssr.apiRuntime or
// ssr.middlewareRuntime) selects the embedded QuickJS runtime. "scriptc"
// counts: it builds every route for the embedded runtime too, which serves
// the routes scriptc does not compile.
func embeddedRuntime(option string) bool {
	switch strings.ToLower(option) {
	case "", "quickjs", "scriptc":
		return true
	}
	return false
}

// explicitQuickJS reports whether a runtime option asks for the embedded
// runtime by name, as opposed to leaving it unset (auto).
func explicitQuickJS(option string) bool {
	return strings.EqualFold(option, "quickjs")
}

// routeDecision is what routeRuntimeDecision concludes for an API route that
// was bundled for the embedded runtime.
type routeDecision struct {
	// nodeOnly moves the route to the Node sidecar; reason names the first
	// reference that made it.
	nodeOnly bool
	reason   string
	// warnings are the KR7160 diagnostics of a route that stays embedded and
	// errs the ones that fail the build.
	warnings []diag.Diagnostic
	errs     []diag.Diagnostic
	features *[]string
}

// routeRuntimeDecision decides, from the metafile of a route's QuickJS
// bundle, where the route runs. Without a choice by the user, any unguarded
// reference to an API the embedded runtime lacks (Buffer, __dirname,
// process.cwd, ...) in the bundled graph moves the route to Node. A route that
// asked for the embedded runtime (`export const runtime = 'edge'` or
// `ssr.apiRuntime: "quickjs"`) fails the build for such a reference instead.
// Both the single-route and the batched compile paths call it.
func (b *Builder) routeRuntimeDecision(metafile string, rt apiRouteRuntime) routeDecision {
	scan := jsruntime.ScanCompatFiles(esbuildBundledInputs(metafile))
	strict := rt == apiRuntimeEdge || (rt != apiRuntimeNodeJS && explicitQuickJS(b.Cfg.SSR.APIRuntime))
	var d routeDecision
	if !strict && scan.Complete {
		if ref, rel, ok := jsruntime.FirstUnsupportedRef(scan, b.Root); ok {
			d.nodeOnly = true
			d.reason = ref.API + " at " + rel + ":" + strconv.Itoa(ref.Line)
			return d
		}
	}
	for _, x := range jsruntime.CompatDiagnosticsAt(scan, b.Root, jsruntime.CompatAPIRuntime, strict) {
		if x.Severity == diag.SeverityError {
			d.errs = append(d.errs, x)
		} else {
			d.warnings = append(d.warnings, x)
		}
	}
	if scan.Complete {
		f := scan.Features()
		d.features = &f
	}
	return d
}

// reportRouteMove prints the info line for a route routeRuntimeDecision moved
// to Node.
func reportRouteMove(file, apiSrcDir, reason string) {
	rel, err := filepath.Rel(apiSrcDir, file)
	if err != nil {
		rel = filepath.Base(file)
	}
	printf("  %s*%s api/%s -> Node (%s)\n", cCyan, cReset, filepath.ToSlash(rel), reason)
}

// esbuildBundledInputs returns the absolute paths of the source files that
// contribute code to a bundle, read from its esbuild metafile. Inputs that were
// tree-shaken away entirely are left out.
func esbuildBundledInputs(metafile string) []string {
	var meta struct {
		Inputs  map[string]json.RawMessage `json:"inputs"`
		Outputs map[string]struct {
			Inputs map[string]struct {
				BytesInOutput int `json:"bytesInOutput"`
			} `json:"inputs"`
		} `json:"outputs"`
	}
	if metafile == "" || json.Unmarshal([]byte(metafile), &meta) != nil {
		return nil
	}
	used := map[string]bool{}
	for _, out := range meta.Outputs {
		for in, info := range out.Inputs {
			if info.BytesInOutput > 0 {
				used[in] = true
			}
		}
	}
	// esbuild writes input paths relative to its working directory, which
	// defaults to the process working directory.
	cwd, _ := os.Getwd()
	var files []string
	for in := range meta.Inputs {
		if !used[in] || esbuildNamespaced(in) {
			continue
		}
		p := filepath.FromSlash(in)
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		files = append(files, filepath.Clean(p))
	}
	sort.Strings(files)
	return files
}

// esbuildNamespaced reports whether a metafile input lives in a plugin
// namespace ("ns:path") instead of on disk.
func esbuildNamespaced(in string) bool {
	i := strings.Index(in, ":")
	if i <= 0 {
		return false
	}
	// A Windows drive letter ("C:\...") is a real path.
	return !(i == 1 && len(in) > 2 && (in[2] == '\\' || in[2] == '/'))
}

// scanRuntimeCompat scans the sources of a bundle built for the embedded
// runtime, reports KR7160 for the unsupported APIs they use, and returns the
// ledger features they reference (nil when the scan could not see everything).
// option is jsruntime.CompatAPIRuntime or jsruntime.CompatMiddlewareRuntime.
// unit names the route or middleware bundled; its warnings replace the ones
// its last build reported, so dev rebuilds do not accumulate them.
func (b *Builder) scanRuntimeCompat(unit, metafile, option string) *[]string {
	scan := jsruntime.ScanCompatFiles(esbuildBundledInputs(metafile))
	ds := jsruntime.CompatDiagnostics(scan, b.Root, option)
	b.setRuntimeCompatWarnings(unit, ds)
	if len(ds) > 0 {
		b.printWarningsOnce(ds)
	}
	if !scan.Complete {
		return nil
	}
	features := scan.Features()
	return &features
}

func (b *Builder) setRuntimeCompatWarnings(unit string, ds []diag.Diagnostic) {
	b.runtimeCompatMu.Lock()
	defer b.runtimeCompatMu.Unlock()
	if len(ds) == 0 {
		delete(b.runtimeCompatWarnings, unit)
		return
	}
	if b.runtimeCompatWarnings == nil {
		b.runtimeCompatWarnings = map[string][]diag.Diagnostic{}
	}
	b.runtimeCompatWarnings[unit] = ds
}

// pruneRuntimeCompatWarnings drops the warnings of API routes that no longer
// exist; routes lists the current ones.
func (b *Builder) pruneRuntimeCompatWarnings(routes []string) {
	keep := map[string]bool{"middleware": true}
	for _, r := range routes {
		keep[r] = true
	}
	b.runtimeCompatMu.Lock()
	defer b.runtimeCompatMu.Unlock()
	for u := range b.runtimeCompatWarnings {
		if !keep[u] {
			delete(b.runtimeCompatWarnings, u)
		}
	}
}

// RuntimeCompatWarnings returns the KR7160 warnings of the latest build of
// each API route and the middleware.
func (b *Builder) RuntimeCompatWarnings() []diag.Diagnostic {
	b.runtimeCompatMu.Lock()
	defer b.runtimeCompatMu.Unlock()
	units := make([]string, 0, len(b.runtimeCompatWarnings))
	for u := range b.runtimeCompatWarnings {
		units = append(units, u)
	}
	sort.Strings(units)
	var out []diag.Diagnostic
	for _, u := range units {
		out = append(out, b.runtimeCompatWarnings[u]...)
	}
	return out
}

// middlewareMeta is .krate/middleware.json, written next to middleware.js.
type middlewareMeta struct {
	// Features lists the compat ledger features the middleware sources
	// reference; absent means unknown (load everything).
	Features *[]string `json:"features,omitempty"`
	// NodeOnly is set when the bundle imports Node built-ins or packages and
	// so only the Node sidecar can run it.
	NodeOnly bool `json:"nodeOnly,omitempty"`
}

// readMiddlewareNodeOnly reports whether the build recorded that the
// middleware must run in the Node sidecar.
func readMiddlewareNodeOnly(root string) bool {
	data, err := os.ReadFile(middlewareMetaPath(root))
	if err != nil {
		return false
	}
	var m middlewareMeta
	return json.Unmarshal(data, &m) == nil && m.NodeOnly
}

// externalImports lists the distinct specifiers a bundle still imports at
// load time, from its esbuild metafile.
func externalImports(metafile string) []string {
	var meta struct {
		Outputs map[string]struct {
			Imports []struct {
				Path     string `json:"path"`
				External bool   `json:"external"`
				Kind     string `json:"kind"`
			} `json:"imports"`
		} `json:"outputs"`
	}
	if json.Unmarshal([]byte(metafile), &meta) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, o := range meta.Outputs {
		for _, imp := range o.Imports {
			if imp.External && imp.Kind == "import-statement" && !seen[imp.Path] {
				seen[imp.Path] = true
				out = append(out, imp.Path)
			}
		}
	}
	sort.Strings(out)
	return out
}

func middlewareMetaPath(root string) string {
	return filepath.Join(root, ".krate", "middleware.json")
}

func writeMiddlewareMeta(root string, features *[]string, nodeOnly bool) error {
	data, err := json.MarshalIndent(middlewareMeta{Features: features, NodeOnly: nodeOnly}, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(middlewareMetaPath(root), data, 0o644)
}
