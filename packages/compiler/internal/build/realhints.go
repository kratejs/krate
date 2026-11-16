package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
	"github.com/kratejs/krate/packages/native/jsruntime"
)

// Real-React islands and client boundaries are rendered only in the browser,
// so the resource hints they call while rendering (react-dom preload,
// preinit, preconnect, ...) would reach the document only after hydration, and
// a file the page fetches before that is requested twice. The build therefore
// renders each of them once with react-dom/server in a throwaway QuickJS
// runtime, only to record the hints: npm packages other than React are
// stubbed (see realHintsStubPlugin), the markup is discarded, a render error
// is ignored (the hints called before it are kept), and the recorded tags go
// into the head of the page (static and force-dynamic pages) or are added to
// the render scope whenever the boundary renders (async pages). In the browser
// react-dom then finds the <link> and adds nothing.
//
// The render runs with the build-time props of a static island and with no
// props for a client boundary, so a hint whose URL depends on props, state,
// data or effects is not collected; one whose URL reads a missing prop
// ("/img/undefined") is dropped.

const realHintsResultGlobal = "__krate_real_hints"

// realHintNames are the react-dom exports the hints render records.
var realHintNames = []string{"preload", "preinit", "preconnect", "prefetchDNS", "preloadModule", "preinitModule"}

// realHintsCache holds the tags of each hints bundle, by its content, so pages
// that share a real-React component render it once.
var realHintsCache sync.Map

// realIslandHintsJob returns the job rendering the resource hints of a static
// page's real-React island with its build-time props.
func (b *Builder) realIslandHintsJob(e irtree.ExternalComponent, imports []irtree.ExternalImport, resolveDir string) *roundJob {
	var src strings.Builder
	writeIslandImportLines(&src, imports)
	props := "{}"
	if e.SSRPropsKnown && e.SSRPropsJS != "" {
		props = e.SSRPropsJS
	}
	fmt.Fprintf(&src, "\nconst props = %s;\n__krate_hints_render(() => (%s));\n", props, e.Subtree)
	return b.realHintsJob(src.String(), resolveDir)
}

// realBoundaryHintsJobs returns the hints job of each export of cb.
func (b *Builder) realBoundaryHintsJobs(cb ClientBoundary) map[string]*roundJob {
	out := map[string]*roundJob{}
	for _, name := range cb.Exports {
		src := cb.entrySource() + "__krate_hints_render(() => { const C = __m[" + strconv.Quote(name) + "]; return typeof C === \"function\" || (C && typeof C === \"object\" && C.$$typeof) ? React.createElement(C, {}) : null; });\n"
		if j := b.realHintsJob(src, cb.resolveDir()); j != nil {
			out[name] = j
		}
	}
	return out
}

// realBoundaryHintsOf collects the tags of finished hints jobs by export.
func realBoundaryHintsOf(jobs map[string]*roundJob) map[string][]string {
	out := map[string][]string{}
	for name, j := range jobs {
		if tags := realHintsOutput(j); len(tags) > 0 {
			out[name] = tags
		}
	}
	return out
}

// realHintsReq is the input of a real-hints job.
type realHintsReq struct{ body, resolveDir string }

// realHintsJob returns the build's job rendering the hints of body, or nil
// when hints are off.
func (b *Builder) realHintsJob(body, resolveDir string) *roundJob {
	if os.Getenv("KRATE_NO_REAL_HINTS") != "" {
		return nil
	}
	j := b.roundJobs.job("real-hints", resolveDir+"\x00"+body, realHintsReq{body, resolveDir}, b.runRealHintsJobs)
	// A render can run long after its bundle (react-dom/server over a whole
	// three.js scene): start it as soon as a page asks for it.
	b.rounds.eager(j)
	return j
}

func realHintsOutput(j *roundJob) []string {
	if j == nil {
		return nil
	}
	tags, _ := j.output.([]string)
	return tags
}

// realHintsSetup returns the shared esbuild options of hints renders and the
// module preamble every render starts with; ok is false when the project
// lacks what a render needs.
func (b *Builder) realHintsSetup() (api.BuildOptions, string, bool) {
	hints := resolver.NodeModule(b.Root, "@krate/runtime/hints")
	dom := findNodePackage(b.Root, "react-dom")
	if hints == "" || dom == "" {
		return api.BuildOptions{}, "", false
	}
	server := ""
	for _, name := range []string{"react-dom-server-legacy.browser.production.js", "react-dom-server-legacy.browser.production.min.js"} {
		if p := filepath.Join(dom, "cjs", name); statOK(p) {
			server = p
			break
		}
	}
	if server == "" {
		return api.BuildOptions{}, "", false
	}
	sink := &islandCSSSink{root: b.Root}
	opts, err := b.realReactOptions(sink, b.newUnavailableGuard())
	if err != nil {
		return api.BuildOptions{}, "", false
	}
	prelude := "import * as React from \"react\";\nimport { renderToString as __krate_rts } from " + strconv.Quote(filepath.ToSlash(server)) + ";\n" +
		"function __krate_hints_render(el) {\n  globalThis.__krate_island_hints = new Set();\n  try { __krate_rts(el()); } catch (e) {}\n" +
		"  globalThis." + realHintsResultGlobal + " = JSON.stringify([...globalThis.__krate_island_hints]);\n}\n"
	opts.Format = api.FormatIIFE
	opts.MinifyWhitespace, opts.MinifyIdentifiers, opts.MinifySyntax = false, false, false
	opts.Define["process.env.NODE_ENV"] = `"production"`
	opts.Plugins = append([]api.Plugin{realHintsDOMPlugin(dom, hints), realHintsStubPlugin()}, opts.Plugins...)
	return opts, prelude, true
}

// runRealHintsJobs bundles the hints renders of jobs in one esbuild call and
// runs each. When that build fails, each render is bundled on its own, so one
// broken component costs only its own hints.
func (b *Builder) runRealHintsJobs(jobs []*roundJob) {
	stop := b.phase("batch:real-hints")
	defer stop()
	opts, prelude, ok := b.realHintsSetup()
	if !ok {
		return
	}
	// Each render bundles react-dom/server and the component's graph: keep the
	// outputs one esbuild call holds small.
	runSubBatches(jobs, realHintsBatchSize, 2, func(part []*roundJob) {
		o := opts
		o.Plugins = append([]api.Plugin(nil), opts.Plugins...)
		o.Define = maps.Clone(opts.Define)
		b.runRealHintsBatch(o, prelude, part)
	})
}

// realHintsBatchSize is the most hints renders one esbuild call bundles.
const realHintsBatchSize = 16

func (b *Builder) runRealHintsBatch(opts api.BuildOptions, prelude string, jobs []*roundJob) {
	codes := make([]string, len(jobs))
	built := false
	if len(jobs) > 1 {
		entries := make([]batchEntry, len(jobs))
		for i, j := range jobs {
			r := j.input.(realHintsReq)
			entries[i] = batchEntry{Source: prelude + r.body, ResolveDir: r.resolveDir, Sourcefile: "krate-real-hints.tsx", Loader: api.LoaderTSX}
		}
		outs, res := esbuildBatch(opts, entries)
		if len(res.Errors) == 0 {
			built = true
			for i, j := range jobs {
				if outs[i].Unsafe {
					codes[i] = b.realHintsCode(opts, prelude, j.input.(realHintsReq))
				} else if len(outs[i].Files) > 0 {
					codes[i] = string(outs[i].Files[0].Contents)
				}
			}
		}
	}
	if !built {
		for i, j := range jobs {
			codes[i] = b.realHintsCode(opts, prelude, j.input.(realHintsReq))
		}
	}
	// Each render finishes on its own: a page waits only for its own renders,
	// not for the slowest one of the round.
	sem := make(chan struct{}, buildWorkerLimit())
	for i, j := range jobs {
		if codes[i] == "" {
			continue
		}
		j.detach()
		go func() {
			defer j.complete()
			defer func() { _ = recover() }() // a failed render has no hints
			sem <- struct{}{}
			defer func() { <-sem }()
			j.output = realHintsTags(codes[i])
		}()
	}
}

// realHintsCode bundles one hints render on its own ("" when it fails).
func (b *Builder) realHintsCode(opts api.BuildOptions, prelude string, r realHintsReq) string {
	o := opts
	o.Plugins = append([]api.Plugin(nil), opts.Plugins...)
	o.Stdin = &api.StdinOptions{Contents: prelude + r.body, ResolveDir: r.resolveDir, Sourcefile: "krate-real-hints.tsx", Loader: api.LoaderTSX}
	result := esbuildBuild(o)
	if len(result.Errors) == 0 && len(result.OutputFiles) > 0 {
		return string(result.OutputFiles[0].Contents)
	}
	return ""
}

// realHintsTags runs a bundled hints render, once per distinct bundle.
func realHintsTags(code string) []string {
	key := hashContent([]byte(code))
	if v, ok := realHintsCache.Load(key); ok {
		return v.([]string)
	}
	tags := runRealHints(code)
	realHintsCache.Store(key, tags)
	return tags
}

func statOK(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func runRealHints(code string) []string {
	rt, err := jsruntime.New()
	if err != nil {
		return nil
	}
	defer rt.Close()
	if _, err := rt.Execute(code); err != nil {
		return nil
	}
	v, err := rt.GetGlobal(realHintsResultGlobal)
	if err != nil || v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return nil
	}
	var tags []string
	if json.Unmarshal([]byte(s), &tags) != nil {
		return nil
	}
	kept := tags[:0]
	for _, t := range tags {
		if !bogusHintURL(t) {
			kept = append(kept, t)
		}
	}
	return kept
}

var hintURLRe = regexp.MustCompile(`\s(?:href|src)="([^"]*)"`)

// bogusHintURL reports a tag whose URL was built from a value the render did
// not have (a prop of a boundary rendered without props).
func bogusHintURL(tag string) bool {
	m := hintURLRe.FindStringSubmatch(tag)
	if m == nil {
		return true
	}
	u := m[1]
	return strings.Contains(u, "undefined") || strings.Contains(u, "[object ") || strings.Contains(u, "NaN")
}

// realHintsStubJS is the module of every stubbed package: it exports a proxy
// that can be called, constructed, extended and read from at any depth, and
// that renders as nothing.
const realHintsStubJS = `module.exports = globalThis.__krate_hint_stub ||= (() => {
  let p;
  const f = function () { return p; };
  const get = (t, k) => {
    if (k === Symbol.toPrimitive) return () => "";
    if (k === Symbol.iterator) return function* () {};
    if (k === "then" || k === "$$typeof" || k === "__esModule") return undefined;
    if (k === "prototype") return f.prototype;
    return p;
  };
  // Named imports of a CommonJS module read through a copy whose prototype
  // is the module's: that prototype answers every name too.
  const proto = new Proxy(Object.create(null), { get });
  p = new Proxy(f, { get, apply() { return p; }, construct() { return p; }, getPrototypeOf() { return proto; } });
  return p;
})();
`

// realHintsStubPlugin replaces the npm packages of the hints render (three.js,
// a canvas library, anything but React) with the stub: they are not needed to
// reach the hint calls of the project's components, they often touch browser
// globals at import time, and their size would exhaust the runtime. Workspace
// packages and tsconfig paths resolve outside node_modules and stay real.
func realHintsStubPlugin() api.Plugin {
	// Other plugins resolve again with their own plugin data, which reaches
	// this callback once more: the inner call of the same import passes.
	var inFlight sync.Map
	return api.Plugin{
		Name: "krate-real-hints-stub",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^[^./]`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if filepath.IsAbs(args.Path) || realHintsKeep[packageName(args.Path)] {
					return api.OnResolveResult{}, nil
				}
				key := args.Importer + "\x00" + args.ResolveDir + "\x00" + args.Path
				if _, busy := inFlight.LoadOrStore(key, true); busy {
					return api.OnResolveResult{}, nil
				}
				defer inFlight.Delete(key)
				r := pb.Resolve(args.Path, api.ResolveOptions{Importer: args.Importer, Namespace: args.Namespace, ResolveDir: args.ResolveDir, Kind: args.Kind, PluginData: args.PluginData})
				if len(r.Errors) > 0 || r.Namespace == "krate-unavailable" || r.Namespace == "file" && nodeModulesPackageRe.MatchString(filepath.ToSlash(r.Path)) && !realHintsKeep[nodeModulesPackageRe.FindStringSubmatch(filepath.ToSlash(r.Path))[1]] {
					return api.OnResolveResult{Path: args.Path, Namespace: "krate-real-hints-stub"}, nil
				}
				return api.OnResolveResult{Path: r.Path, Namespace: r.Namespace, External: r.External, PluginData: r.PluginData}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-real-hints-stub"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
				contents := realHintsStubJS
				return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS}, nil
			})
		},
	}
}

// nodeModulesPackageRe finds the package of a file under node_modules (the
// last node_modules segment, as in a pnpm store path).
var nodeModulesPackageRe = regexp.MustCompile(`.*/node_modules/((?:@[^/]+/)?[^/]+)/`)

// realHintsKeep are the packages the hints render needs for real.
var realHintsKeep = map[string]bool{"react": true, "react-dom": true, "scheduler": true, "@krate/runtime": true}

// realHintsDOMPlugin makes the ESM `react-dom` imports of the hints render
// the project's react-dom with the resource hints replaced by Krate's, which
// record into globalThis.__krate_island_hints and keep recording after a
// render error. require() calls (react-dom/server's own) keep react-dom.
func realHintsDOMPlugin(dom, hints string) api.Plugin {
	return api.Plugin{
		Name: "krate-real-hints-dom",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^react-dom$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.Kind == api.ResolveJSRequireCall {
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: "react-dom", Namespace: "krate-real-hints-dom"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-real-hints-dom"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
				real := strconv.Quote(filepath.ToSlash(dom))
				list := strings.Join(realHintNames, ", ")
				contents := "export * from " + real + ";\nimport * as __rd from " + real + ";\n" +
					"import { " + list + " } from " + strconv.Quote(filepath.ToSlash(hints)) + ";\nexport { " + list + " };\n" +
					"export default { ...(__rd.default || __rd), " + list + " };\n"
				return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS, ResolveDir: dom}, nil
			})
		},
	}
}

// realHintsHead wraps each tag for liftIslandHead.
func realHintsHead(tags []string) string {
	var sb strings.Builder
	for _, t := range tags {
		sb.WriteString(islandHeadOpen + t + islandHeadClose)
	}
	return sb.String()
}

// realBoundaryHintsJS is the render function of a real-React boundary export
// that records its build-time hints on the request before it emits the
// fallback.
func realBoundaryHintsJS(tags []string) string {
	data, _ := json.Marshal(tags)
	return "((C, p) => { __krateHintTags(" + string(data) + "); return __fallback(C, p); })"
}
