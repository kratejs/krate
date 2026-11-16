package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
	"github.com/kratejs/krate/packages/native/jsruntime"
)

// Real-React islands and client boundaries are rendered only in the browser,
// so the resource hints they call while rendering (react-dom preload,
// preinit, preconnect, ...) would reach the document only after hydration, and
// a file the page fetches before that is requested twice. The build therefore
// renders each of them once with react-dom/server in a throwaway JavaScript
// context (a Node or Bun worker's, else QuickJS's), only to record the hints: npm packages other than React are
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

// realHintsMergedGlobal holds the tags of each render of a merged run, by
// its index.
const realHintsMergedGlobal = "__krate_real_hints_all"

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

// realHintsPrelude is the module preamble of a hints render with
// react-dom/server at server. slot is the render's index in a merged run, or
// -1 for a render that runs alone.
func realHintsPrelude(server string, slot int) string {
	head := "import * as React from \"react\";\nimport { renderToString as __krate_rts } from " + strconv.Quote(filepath.ToSlash(server)) + ";\n" +
		"function __krate_hints_render(el) {\n  globalThis.__krate_island_hints = new Set();\n"
	if slot < 0 {
		return head + "  try { __krate_rts(el()); } catch (e) {}\n" +
			"  globalThis." + realHintsResultGlobal + " = JSON.stringify([...globalThis.__krate_island_hints]);\n}\n"
	}
	// An engine error (out of memory, stack overflow) may depend on what the
	// renders before this one left in the runtime: the render then runs alone.
	// V8 and JSC throw a RangeError for the stack. QuickJS throws InternalError
	// for both, and its host installs realHintsEngineErrorQuickJS: the bundle
	// must not read that global itself, or the worker pool, which lacks it,
	// would leave every merged run with a throwing render to QuickJS.
	return head + "  let engine = false;\n" +
		"  try { __krate_rts(el()); } catch (e) { engine = (globalThis." + realHintsEngineErrorGlobal + " || " + realHintsEngineErrorJS + ")(e); }\n" +
		"  (globalThis." + realHintsMergedGlobal + " ||= {})[" + strconv.Itoa(slot) + "] = engine ? null : JSON.stringify([...globalThis.__krate_island_hints]);\n}\n"
}

// realHintsEngineErrorGlobal names the host's test for an engine error in a
// merged hints run; realHintsEngineErrorJS is the one V8 and JSC need.
const (
	realHintsEngineErrorGlobal  = "__krate_engine_error"
	realHintsEngineErrorJS      = "function (e) { return e instanceof RangeError && /call stack/i.test(e.message); }"
	realHintsEngineErrorQuickJS = "globalThis." + realHintsEngineErrorGlobal + " = function (e) { return e instanceof InternalError || e instanceof RangeError && /call stack/i.test(e.message); };"
)

// realHintsSetup returns the shared esbuild options of hints renders and the
// module preamble every render starts with; ok is false when the project
// lacks what a render needs.
func (b *Builder) realHintsSetup() (opts api.BuildOptions, prelude, server string, ok bool) {
	hints := resolver.NodeModule(b.Root, "@krate/runtime/hints")
	dom := findNodePackage(b.Root, "react-dom")
	if hints == "" || dom == "" {
		return api.BuildOptions{}, "", "", false
	}
	for _, name := range []string{"react-dom-server-legacy.browser.production.js", "react-dom-server-legacy.browser.production.min.js"} {
		if p := filepath.Join(dom, "cjs", name); statOK(p) {
			server = p
			break
		}
	}
	if server == "" {
		return api.BuildOptions{}, "", "", false
	}
	sink := &islandCSSSink{root: b.Root}
	opts, err := b.realReactOptions(sink, b.newUnavailableGuard())
	if err != nil {
		return api.BuildOptions{}, "", "", false
	}
	prelude = realHintsPrelude(server, -1)
	opts.Format = api.FormatIIFE
	opts.MinifyWhitespace, opts.MinifyIdentifiers, opts.MinifySyntax = false, false, false
	opts.Define["process.env.NODE_ENV"] = `"production"`
	opts.Plugins = append([]api.Plugin{realHintsDOMPlugin(dom, hints), realHintsStubPlugin()}, opts.Plugins...)
	return opts, prelude, server, true
}

// runRealHintsJobs bundles the hints renders of jobs in one esbuild call and
// runs each. When that build fails, each render is bundled on its own, so one
// broken component costs only its own hints.
func (b *Builder) runRealHintsJobs(jobs []*roundJob) {
	stop := b.phase("batch:real-hints")
	defer stop()
	opts, prelude, server, ok := b.realHintsSetup()
	if !ok {
		return
	}
	// Each render bundles react-dom/server and the component's graph: keep the
	// outputs one esbuild call holds small.
	runSubBatches(jobs, realHintsBatchSize, 2, func(part []*roundJob) {
		o := opts
		o.Plugins = append([]api.Plugin(nil), opts.Plugins...)
		o.Define = maps.Clone(opts.Define)
		if rest := b.runRealHintsMerged(o, server, part); len(rest) > 0 {
			b.runRealHintsBatch(o, prelude, rest)
		}
	})
}

// realHintsBatchSize is the most hints renders one esbuild call bundles.
const realHintsBatchSize = 16

func realHintsMergeOff() bool { return os.Getenv("KRATE_NO_HINTS_MERGE") != "" }

// runRealHintsMerged runs the renders of jobs in one runtime, from one
// bundle that holds react-dom/server and the modules the renders share
// once, in job order. It returns the jobs that must run alone: all of them
// when the bundle fails, and those the run did not finish or that hit an
// engine error.
//
// The renders share module instances, as the param sets of one page's
// metadata do. The tags of a render depend on its props and the modules it
// reads; a component that changes module state while it renders, in a way a
// later render reads back, is the case this does not reproduce.
func (b *Builder) runRealHintsMerged(opts api.BuildOptions, server string, jobs []*roundJob) []*roundJob {
	if realHintsMergeOff() || len(jobs) < 2 {
		return jobs
	}
	var entry strings.Builder
	bodies := make([]batchEntry, len(jobs))
	for i, j := range jobs {
		r := j.input.(realHintsReq)
		bodies[i] = batchEntry{Source: realHintsPrelude(server, i) + r.body, ResolveDir: r.resolveDir, Sourcefile: "krate-real-hints.tsx", Loader: api.LoaderTSX}
		fmt.Fprintf(&entry, "import %s;\n", strconv.Quote(realHintsBodyPrefix+strconv.Itoa(i)))
	}
	o := opts
	o.Plugins = append([]api.Plugin{realHintsBodiesPlugin(bodies)}, opts.Plugins...)
	o.Stdin = &api.StdinOptions{Contents: entry.String(), ResolveDir: b.Root, Sourcefile: "krate-real-hints-merged.js", Loader: api.LoaderJS}
	result := esbuildBuild(o)
	if len(result.Errors) > 0 || len(result.OutputFiles) == 0 {
		return jobs
	}
	tags := b.runRealHintsMergedCode(string(result.OutputFiles[0].Contents), len(jobs))
	var rest []*roundJob
	for i, j := range jobs {
		if tags[i] == nil {
			rest = append(rest, j)
			continue
		}
		j.detach()
		j.output = *tags[i]
		j.complete()
	}
	return rest
}

// realHintsBodyPrefix names the render modules of a merged hints bundle.
const realHintsBodyPrefix = "krate-real-hints-body:"

// realHintsBodiesPlugin serves the render modules of a merged hints bundle
// from memory, each in a directory of its own in its ResolveDir (see
// batchModulePath), so its imports resolve as those of its own bundle do.
func realHintsBodiesPlugin(bodies []batchEntry) api.Plugin {
	path := func(i int) string {
		return filepath.Join(bodies[i].ResolveDir, ".krate-hints-"+strconv.Itoa(i), bodies[i].Sourcefile)
	}
	return api.Plugin{
		Name: "krate-real-hints-bodies",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^` + regexp.QuoteMeta(realHintsBodyPrefix)}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				i, err := strconv.Atoi(strings.TrimPrefix(args.Path, realHintsBodyPrefix))
				if err != nil || i < 0 || i >= len(bodies) {
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: path(i), Namespace: "file"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `[/\\]\.krate-hints-[0-9]+[/\\][^/\\]+$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				for i, e := range bodies {
					if args.Path == path(i) {
						src := tsstrip.BlankTierDecorators(e.Sourcefile, e.Source)
						return api.OnLoadResult{Contents: &src, ResolveDir: e.ResolveDir, Loader: e.Loader}, nil
					}
				}
				return api.OnLoadResult{}, nil
			})
		},
	}
}

// realHintsMergedRead reads the tags of a merged run by render index.
const realHintsMergedRead = `JSON.stringify(globalThis.` + realHintsMergedGlobal + ` || {})`

// runRealHintsMergedCode runs a merged hints bundle of n renders and returns
// the tags of each render that finished (nil for the others).
func (b *Builder) runRealHintsMergedCode(code string, n int) (out []*[]string) {
	out = make([]*[]string, n)
	defer timeRealHints(time.Now())
	// A render that throws past its own catch stops the run: the renders
	// after it have no result and run alone.
	if res, ok := b.workerScript("real-hints", code, []string{realHintsMergedRead}); ok {
		if v := res.Reads[0]; v.IsString {
			return realHintsMergedTags(v.S, n)
		}
		return out
	}
	defer b.timeQuickJS("real-hints")()
	defer func() {
		// A crash of the engine (see jseval's guardFaults) loses the run.
		if r := recover(); r != nil {
			out = make([]*[]string, n)
		}
	}()
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	rt, err := jsruntime.New()
	if err != nil {
		return out
	}
	defer rt.Close()
	if _, err := rt.Execute(realHintsEngineErrorQuickJS); err != nil {
		return out
	}
	_, _ = rt.Execute(code)
	v, err := rt.Execute(realHintsMergedRead)
	s, ok := v.(string)
	if err != nil || !ok {
		return out
	}
	return realHintsMergedTags(s, n)
}

// realHintsMergedTags decodes the tags of a merged run's renders.
func realHintsMergedTags(s string, n int) []*[]string {
	out := make([]*[]string, n)
	var byIndex map[string]*string
	if json.Unmarshal([]byte(s), &byIndex) != nil {
		return out
	}
	for i := range out {
		raw := byIndex[strconv.Itoa(i)]
		if raw == nil {
			continue
		}
		var tags []string
		if json.Unmarshal([]byte(*raw), &tags) != nil {
			continue
		}
		kept := tags[:0]
		for _, t := range tags {
			if !bogusHintURL(t) {
				kept = append(kept, t)
			}
		}
		out[i] = &kept
	}
	return out
}

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
			j.output = b.realHintsTags(codes[i])
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
func (b *Builder) realHintsTags(code string) []string {
	key := hashContent([]byte(code))
	if v, ok := realHintsCache.Load(key); ok {
		return v.([]string)
	}
	tags := b.runRealHints(code)
	realHintsCache.Store(key, tags)
	return tags
}

func statOK(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// realHintsRuns counts the runtimes hints renders ran in and
// realHintsNanos sums their time.
var realHintsRuns, realHintsNanos atomic.Int64

func timeRealHints(start time.Time) {
	realHintsRuns.Add(1)
	realHintsNanos.Add(int64(time.Since(start)))
}

// realHintsRead reads the tags of a render that ran alone.
const realHintsRead = `typeof globalThis.` + realHintsResultGlobal + ` === "string" ? globalThis.` + realHintsResultGlobal + ` : null`

func (b *Builder) runRealHints(code string) []string {
	defer timeRealHints(time.Now())
	if res, ok := b.workerScript("real-hints", code, []string{realHintsRead}); ok {
		if res.Threw || !res.Reads[0].IsString {
			return nil
		}
		return decodeRealHints(res.Reads[0].S)
	}
	defer b.timeQuickJS("real-hints")()
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
	return decodeRealHints(s)
}

// decodeRealHints decodes the tags a render recorded, without those whose URL
// the render did not have.
func decodeRealHints(s string) []string {
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
