package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"strconv"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// A real-React island runs on react and react-dom, not on the Preact alias.
// Libraries built on a React reconciler (react-three-fiber, react-konva) need
// React's internals, which Preact does not provide. The island is rendered in
// the browser only; the server emits the placeholder (and its fallback text).
// All real-React islands of a site are bundled together with code splitting,
// so react and react-dom are one shared chunk that only the pages with such an
// island load.

const islandRealSharedStem = "isr-"

// reconcilerMarkers are the dependencies that mark a package as built on a
// React reconciler, directly or through another reconciler library.
var reconcilerMarkers = []string{"react-reconciler", "@react-three/fiber", "react-konva"}

// reconcilerPkgs holds the names of packages found to be reconciler libraries
// (filled in by reactPackageOf).
var reconcilerPkgs sync.Map

// isRealReactPackage reports whether the package must run on real React: it is
// listed in react.islands, or it is built on a React reconciler.
func (b *Builder) isRealReactPackage(name string) bool {
	if name == "" {
		return false
	}
	for _, p := range b.Cfg.React.Islands {
		if p == name || strings.HasPrefix(name, strings.TrimSuffix(p, "/")+"/") {
			return true
		}
	}
	_, ok := reconcilerPkgs.Load(name)
	return ok
}

// realLibPackage returns the first (by name) package of pkgs that needs real
// React, or "".
func (b *Builder) realLibPackage(pkgs map[string]bool) string {
	names := make([]string, 0, len(pkgs))
	for p := range pkgs {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		if b.isRealReactPackage(p) {
			return p
		}
	}
	return ""
}

// markRealReactModules sets Real on the island modules that import a real-React
// package, and on every island module that imports a real one: a real-React
// island runs its whole module subtree on React, so it cannot contain a Preact
// island.
func (b *Builder) markRealReactModules(out map[string]*reactIslandModule, infos map[string]*reactHookInfo, resolve func(importer, spec string) string) {
	for path, im := range out {
		if in := infos[path]; in != nil && b.realLibPackage(in.libPkgs) != "" {
			im.Real = true
			im.Direct = true
		}
	}
	for changed := true; changed; {
		changed = false
		for path, im := range out {
			if im.Real {
				continue
			}
			in := infos[path]
			if in == nil {
				continue
			}
			for _, stmt := range in.m.Program.Body {
				imp, ok := stmt.(*ast.ImportStmt)
				if !ok {
					continue
				}
				if dep, ok := out[resolve(path, strings.Trim(imp.Source, "\"'"))]; ok && dep.Real {
					im.Real = true
					changed = true
					break
				}
			}
		}
	}
}

// packageNameOf returns the npm package name of a bare specifier.
func packageNameOf(spec string) string {
	parts := strings.Split(spec, "/")
	if strings.HasPrefix(spec, "@") && len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

// markRealReactImports flags the page's own bare-package imports that need
// real React, so tags of them render as real-React islands.
func (b *Builder) markRealReactImports(byLocal map[string]irtree.ExternalImport, list []irtree.ExternalImport, resolveDir string) {
	cache := map[string]bool{}
	real := func(src string) bool {
		name := packageNameOf(src)
		if v, ok := cache[name]; ok {
			return v
		}
		if dir := findNodePackage(resolveDir, name); dir != "" {
			reactPackageOf(filepath.Join(dir, "package.json"))
		}
		cache[name] = b.isRealReactPackage(name)
		return cache[name]
	}
	for i := range list {
		if real(list[i].Source) {
			list[i].RealReact = true
		}
	}
	for local, imp := range byLocal {
		if real(imp.Source) {
			imp.RealReact = true
			byLocal[local] = imp
		}
	}
}

// findNodePackage returns the installed directory of an npm package, looking
// in node_modules of dir and its ancestors.
func findNodePackage(dir, name string) string {
	for {
		cand := filepath.Join(dir, "node_modules", filepath.FromSlash(name))
		if _, err := os.Stat(filepath.Join(cand, "package.json")); err == nil {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// realFallbackHTML is the markup a real-React island shows until React mounts:
// the text of its `client:fallback` attribute, escaped.
func realFallbackHTML(e irtree.ExternalComponent) string {
	if e.Fallback == "" {
		return ""
	}
	return escape.Text(e.Fallback)
}

const realIslandBoundaryJS = `
class KrateIslandBoundary extends React.Component {
  constructor(p) { super(p); this.state = { failed: false }; }
  static getDerivedStateFromError() { return { failed: true }; }
  componentDidCatch(e) { if (this.props && typeof this.props.onError === "function") this.props.onError(e); }
  render() {
    if (this.state.failed) return React.createElement("span", { "data-krate-island-error": "", role: "alert" }, "This component could not load.");
    return this.props.children;
  }
}
`

// realIslandGroupSource builds the entry module of the real-React islands of
// one page. It is the React counterpart of islandGroupClientSource.
func realIslandGroupSource(group []irtree.ExternalComponent, all []irtree.ExternalImport) string {
	var src strings.Builder
	src.WriteString("import * as React from \"react\";\nimport { createRoot } from \"react-dom/client\";\n")
	seen := map[string]bool{}
	for _, e := range group {
		for _, imp := range islandImportsFor(e.Subtree, all) {
			key := imp.Source + "\x00" + imp.Local
			if seen[key] {
				continue
			}
			seen[key] = true
			writeIslandImportLines(&src, []irtree.ExternalImport{imp})
		}
	}
	src.WriteString(realIslandBoundaryJS)
	src.WriteString("export function mount(only, elArg) {\n")
	for _, e := range group {
		src.WriteString(realIslandMountBlock(e))
	}
	src.WriteString("}\n")
	return src.String()
}

func realIslandMountBlock(e irtree.ExternalComponent) string {
	var src strings.Builder
	fmt.Fprintf(&src, "\nif (only === void 0 || only === %q) {\n  const el = elArg || document.querySelector('[data-krate-island=%q]');\n", e.ID, e.ID)
	src.WriteString("  if (el && el.isConnected !== false && !el.hasAttribute(\"data-krate-island-hydrated\")) {\n    try {\n")
	if e.PropsJS != "" {
		fmt.Fprintf(&src, "      const getProps=(globalThis.__krate_island_values||{})[%q];\n", e.ID)
		fmt.Fprintf(&src, "      const subscribe=(globalThis.__krate_island_subscribe||{})[%q];\n", e.ID)
		src.WriteString("      function KrateIslandData() {\n")
		src.WriteString("        const [, bump] = React.useReducer(function (n) { return n + 1; }, 0);\n")
		src.WriteString("        React.useEffect(function () { return subscribe ? subscribe(bump) : void 0; }, []);\n")
		fmt.Fprintf(&src, "        var props = typeof getProps === 'function' ? getProps() : {};\n        return (%s);\n      }\n", e.Subtree)
		src.WriteString("      var __node = React.createElement(KrateIslandData, null);\n")
	} else {
		fmt.Fprintf(&src, "      var __node = (%s);\n", e.Subtree)
	}
	src.WriteString("      var __boundary = React.createElement(KrateIslandBoundary, { onError: function (err) { console.error(\"krate real React island:\", String((err && (err.name ? err.name + \": \" : \"\")) + (err && err.message || err) || err)); el.setAttribute(\"data-krate-island-error\", \"\"); } }, __node);\n")
	src.WriteString("      var __start = typeof performance !== \"undefined\" ? performance.now() : 0;\n")
	src.WriteString("      var __root = createRoot(el);\n      __root.render(__boundary);\n")
	src.WriteString("      var __end = typeof performance !== \"undefined\" ? performance.now() : 0;\n")
	fmt.Fprintf(&src, "      (globalThis.__krate_island_hydration ||= {})[%q] = { start: __start, end: __end, ms: __end - __start };\n", e.ID)
	src.WriteString("      el.setAttribute(\"data-krate-island-hydrated\", \"\");\n")
	src.WriteString("      (globalThis.__krate_island_cleanups ||= []).push(function () { __root.unmount(); });\n")
	src.WriteString("    } catch (e) { console.error(\"krate real React island:\", String((e && (e.name ? e.name + \": \" : \"\")) + (e && e.message || e) || e)); el.setAttribute(\"data-krate-island-error\", \"\"); el.textContent = \"This component could not load.\"; }\n  }\n}\n")
	return src.String()
}

// realReactOptions returns the esbuild options shared by the validation build
// and the site-wide build of real-React islands: react and react-dom resolve to
// the project's copy (never the Preact alias), and only one copy is bundled.
func (b *Builder) realReactOptions(sink *islandCSSSink) api.BuildOptions {
	minify := b.shouldMinifyJS()
	env := "\"production\""
	if b.DevMode {
		env = "\"development\""
	}
	opts := api.BuildOptions{
		Bundle:            true,
		Write:             false,
		MinifyWhitespace:  minify,
		MinifyIdentifiers: minify,
		MinifySyntax:      minify,
		Format:            api.FormatESModule,
		Platform:          api.PlatformBrowser,
		Target:            api.ES2020,
		JSX:               api.JSXAutomatic,
		JSXImportSource:   "react",
		LogLevel:          api.LogLevelSilent,
		Define:            map[string]string{"process.env.NODE_ENV": env},
		Plugins:           []api.Plugin{islandCSSPlugin(sink), islandJSXPluginFor("react")},
	}
	alias := map[string]string{}
	for _, name := range []string{"react", "react-dom"} {
		if dir := findNodePackage(b.Root, name); dir != "" {
			alias[name] = filepath.ToSlash(dir)
		}
	}
	if len(alias) > 0 {
		opts.Alias = alias
	}
	return opts
}

// realReactMissing is the message for a project without react or react-dom.
func (b *Builder) realReactMissing(joined string) *bundleError {
	if findNodePackage(b.Root, "react") == "" || findNodePackage(b.Root, "react-dom") == "" {
		return &bundleError{Msg: "a real-React island needs react and react-dom, which are not installed in this project\n  hint: install them with `pnpm add react react-dom` (React 18.3 or newer)\n  detail: " + joined}
	}
	return nil
}

// buildRealIslandBundle bundles one page's real-React islands once to surface
// errors with the page's source location and to collect imported stylesheets.
// The output is discarded: the shipped chunks come from writeRealIslandBundles.
func (b *Builder) buildRealIslandBundle(src, resolveDir string, sink *islandCSSSink) error {
	opts := b.realReactOptions(sink)
	opts.Stdin = &api.StdinOptions{Contents: src, ResolveDir: resolveDir, Sourcefile: "krate-island.tsx", Loader: api.LoaderTSX}
	result := esbuildBuild(opts)
	if len(result.Errors) == 0 {
		return nil
	}
	var msgs []string
	var loc *bundleLocation
	for _, e := range result.Errors {
		if e.Location != nil {
			file := b.esbuildFile(e.Location.File)
			msgs = append(msgs, fmt.Sprintf("%s:%d:%d: %s", file, e.Location.Line, e.Location.Column+1, e.Text))
			if loc == nil && !strings.HasPrefix(e.Location.File, "krate-island") {
				loc = &bundleLocation{File: file, Line: e.Location.Line, Col: e.Location.Column + 1}
			}
		} else {
			msgs = append(msgs, e.Text)
		}
	}
	joined := strings.Join(msgs, "\n")
	if be := b.realReactMissing(joined); be != nil {
		return be
	}
	return &bundleError{Loc: loc, Msg: "bundling real-React island:\n" + joined}
}

// writeRealIslandBundles bundles every registered real-React island entry into
// /chunks with code splitting and records the written names in keep.
func (b *Builder) writeRealIslandBundles(entries []islandEntry, chunksDir string, keep map[string]bool) error {
	if len(entries) == 0 {
		return nil
	}
	virtual := map[string]islandVirtualModule{}
	points := make([]api.EntryPoint, 0, len(entries))
	for _, e := range entries {
		for k, v := range e.Virtual {
			virtual[k] = v
		}
		points = append(points, api.EntryPoint{InputPath: "krate-island-entry:" + e.Name, OutputPath: e.Name})
	}
	sink := &islandCSSSink{root: b.Root}
	opts := b.realReactOptions(sink)
	opts.EntryPointsAdvanced = points
	opts.Outdir = chunksDir
	opts.EntryNames = "[name]"
	opts.ChunkNames = islandRealSharedStem + "[hash]"
	opts.Splitting = true
	opts.Plugins = append([]api.Plugin{islandEntryPlugin(entries, virtual)}, opts.Plugins...)
	result := esbuildBuild(opts)
	if len(result.Errors) > 0 {
		var msgs []string
		for _, e := range result.Errors {
			if e.Location != nil {
				msgs = append(msgs, fmt.Sprintf("%s:%d:%d: %s", b.esbuildFile(e.Location.File), e.Location.Line, e.Location.Column+1, e.Text))
			} else {
				msgs = append(msgs, e.Text)
			}
		}
		joined := strings.Join(msgs, "\n")
		if be := b.realReactMissing(joined); be != nil {
			return be
		}
		return fmt.Errorf("bundling real-React island chunks:\n%s", joined)
	}
	rename := map[string]string{}
	for _, f := range result.OutputFiles {
		base := filepath.Base(f.Path)
		if strings.HasPrefix(base, islandRealSharedStem) {
			rename[base] = strings.ToLower(base)
		}
	}
	if err := os.MkdirAll(chunksDir, 0755); err != nil {
		return fmt.Errorf("creating chunks dir: %w", err)
	}
	for _, f := range result.OutputFiles {
		base := filepath.Base(f.Path)
		if strings.HasSuffix(base, ".map") {
			continue
		}
		data := string(f.Contents)
		for from, to := range rename {
			if from != to {
				data = strings.ReplaceAll(data, from, to)
			}
		}
		if to, ok := rename[base]; ok {
			base = to
		}
		keep[base] = true
		if err := fsutil.WriteFileIfChanged(filepath.Join(chunksDir, base), []byte(data), 0644); err != nil {
			return fmt.Errorf("writing real-React island chunk: %w", err)
		}
	}
	return nil
}

// reconcilerGuardPlugin fails a Preact island bundle that pulls in
// react-reconciler, which cannot run on Preact, and names the fix.
func reconcilerGuardPlugin() api.Plugin {
	return api.Plugin{
		Name: "krate-reconciler-guard",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^react-reconciler(/|$)`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{}, diag.Diagnostic{
					Message: "a library built on react-reconciler cannot run on Preact; list the package that imports it in `react: { islands: [...] }` in krate.config.ts so it runs as a real-React island",
				}.WithCode(diag.CodeIslandAsset)
			})
		},
	}
}

// Real-React boundaries: real-React components inside the server tree of an
// async page or layout. The server module is a stub that emits a
// <krate-island> carrying the serialized props (and children as a tree) with
// the fallback text inside; the shared island loader imports the boundary's
// chunk, which mounts it with react-dom. The component itself never runs on
// the server, so the package is never imported there.

// realBoundaryChunkJS is the part of a real-React boundary chunk that follows
// its entry preamble (which binds `__m`).
const realBoundaryChunkJS = `
function __resolve(name) {
  return name.split(".").reduce(function (o, k) { return o == null ? o : o[k]; }, __m);
}
export function component(name) { return __resolve(name); }
export async function mount(el) {
  var name = el.getAttribute("data-export");
  var C = __resolve(name);
  if (C == null) throw new Error("real-React module " + el.getAttribute("data-module") + " has no component export " + name);
  var raw = JSON.parse(el.getAttribute("data-props") || "{}");
  delete raw["client:fallback"];
  var loaded = {};
  loaded[el.getAttribute("data-module") + "#" + name] = C;
  await Promise.all(__krateClientRefs(raw.__krateTree, []).map(function (r) {
    return import(r.s).then(function (m) { loaded[r.m + "#" + r.e] = m.component(r.e); });
  }));
  var props = __krateRevive(raw, React.createElement, __krateCallAction, function (m, e) { return loaded[m + "#" + e]; });
  var root = createRoot(el);
  root.render(React.createElement(C, props));
  (globalThis.__krate_island_cleanups ||= []).push(function () { root.unmount(); });
  el.setAttribute("data-krate-island-hydrated", "");
}
`

// registerRealBoundary adds the chunk of a real-React boundary to the
// site-wide real-React bundle and returns its URL.
func (b *Builder) registerRealBoundary(cb ClientBoundary) string {
	src := "import * as React from \"react\";\nimport { createRoot } from \"react-dom/client\";\n" +
		cb.entrySource() + clientReviveJS + clientActionCallJS + realBoundaryChunkJS
	dir := cb.resolveDir()
	name := b.islandEntryName(src, dir)
	b.setIslandEntries("realboundary:"+cb.ModuleID, []islandEntry{{Name: name, Source: src, ResolveDir: dir, Real: true}})
	return b.islandEntryURL(name)
}

// realBoundaryStub is the server-bundle replacement of a real-React boundary.
// Each export is a placeholder component wrapped by clientBoundary, with
// children always sent as a tree and the `client:fallback` prop as the markup.
func realBoundaryStub(cb ClientBoundary) string {
	var sb strings.Builder
	sb.WriteString(boundaryStubHeader)
	realBoundaryExports(&sb, cb, nil)
	return sb.String()
}

// realBoundaryExports writes the exports of a real-React boundary, skipping
// the names already in seen.
func realBoundaryExports(sb *strings.Builder, cb ClientBoundary, seen map[string]bool) {
	if !strings.Contains(sb.String(), "__fallback =") {
		sb.WriteString("const __fallback = (C, p) => { const f = p && p[\"client:fallback\"]; return typeof f === \"string\" ? f.replace(/&/g, \"&amp;\").replace(/</g, \"&lt;\").replace(/>/g, \"&gt;\") : \"\"; };\n")
	}
	for _, name := range cb.Exports {
		if seen[name] {
			continue
		}
		if seen != nil {
			seen[name] = true
		}
		wrapped := fmt.Sprintf("__krateCB(%s, %s, %s, function %s() { return null; }, __fallback, true, true)",
			strconv.Quote(cb.ModuleID), strconv.Quote(name), strconv.Quote(cb.ChunkURL), stubFuncName(name))
		if name == "default" {
			fmt.Fprintf(sb, "export default %s;\n", wrapped)
		} else {
			fmt.Fprintf(sb, "export const %s = %s;\n", name, wrapped)
		}
	}
}

// stubFuncName is a valid function name for an export (the name shows up in
// error messages).
func stubFuncName(export string) string {
	if export == "default" {
		return "RealReactComponent"
	}
	return export
}

// realModulePaths is the set of user modules that import a real-React package;
// the package boundaries skip them (the package is bundled in their chunk).
func realModulePaths(mods []*bundler.Module, islands map[string]*reactIslandModule) map[string]bool {
	skip := map[string]bool{}
	for _, m := range mods {
		if m == nil || m.Program == nil || m.IsEntry || m.IsExternal {
			continue
		}
		if im, ok := islands[m.Path]; ok && (im.Direct || im.Real && bundler.HasUseClientDirective(m.SourceCode)) {
			skip[m.Path] = true
		}
	}
	return skip
}
