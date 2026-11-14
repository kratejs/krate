package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	irtree "github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

// Client boundaries ("use client").
//
// A "use client" module reached from a server component tree is not rendered
// as inert server markup. In the page server bundle each of its exports is
// wrapped by clientBoundary (@krate/runtime/server): the server renders the
// component's initial HTML with Preact (React semantics, like external
// islands) into <krate-island data-module data-export data-src data-props>,
// and the module's client chunk hydrates that element in the browser with the
// same serialized props. Children passed from the server are rendered on the
// server and travel as HTML. Pages without server components are untouched:
// their "use client" modules compile through the normal Krate pipeline.

// ClientBoundary is one "use client" module of a server-component page.
type ClientBoundary struct {
	ModuleID   string   // stable id derived from the root-relative path
	SourcePath string   // absolute module path
	Exports    []string // export names ("default" for the default export)
	ChunkURL   string   // hydration chunk URL
	// Spec is set for a package boundary: the bare specifier of a React-bound
	// package whose components a server module imports. Entry then defines
	// `__m` (the used exports) in place of importing SourcePath, ResolveDir is
	// where the specifier resolves from, and Hydrate is false for a package
	// whose components have no state or effects (icons), which ship no island.
	Spec       string
	Entry      string
	ResolveDir string
	Hydrate    bool
	// Real marks a boundary that runs on real react and react-dom: a React
	// reconciler library or a module that imports one. The server emits a
	// fallback placeholder instead of rendering it (see realreact.go).
	Real bool
}

// hydrates reports whether the boundary's components become islands.
func (cb ClientBoundary) hydrates() bool { return cb.Real || cb.Spec == "" || cb.Hydrate }

// anyBoundaryHydrates reports whether a page needs the island loader: some
// boundary of it becomes an island.
func anyBoundaryHydrates(boundaries []ClientBoundary) bool {
	for _, cb := range boundaries {
		if cb.hydrates() {
			return true
		}
	}
	return false
}

// entrySource is the module preamble that binds `__m`, the exports of the
// boundary, for the client chunk and the server render module.
func (cb ClientBoundary) entrySource() string {
	if cb.Entry != "" {
		return cb.Entry
	}
	return "import * as __m from " + strconv.Quote(cb.SourcePath) + ";\n"
}

func (cb ClientBoundary) resolveDir() string {
	if cb.ResolveDir != "" {
		return cb.ResolveDir
	}
	return filepath.Dir(cb.SourcePath)
}

// clientIslandLoaderJS mounts every <krate-island> that is not mounted yet
// from its chunk. It is a classic script in a chunk file (not inline) so the
// SPA router loads it when a soft navigation arrives on a page with islands,
// and it is idempotent: it only touches islands it has not mounted.
const clientIslandLoaderJS = `(function(){document.querySelectorAll("krate-island[data-src]:not([data-krate-island-mounted])").forEach(function(el){el.setAttribute("data-krate-island-mounted","");import(el.getAttribute("data-src")).then(function(m){return m.mount(el)}).catch(function(e){console.error("krate client component:",e);el.setAttribute("data-krate-island-error","")})})})();`

// islandLoaderHTML writes the island loader chunk (once per content) and
// returns the script tag that references it. The tag is deferred so a hard
// load runs it after the document, including spliced regions, is parsed.
func (b *Builder) islandLoaderHTML() (string, error) {
	name := "chunks/island-loader." + hashContent([]byte(clientIslandLoaderJS)) + ".js"
	if err := os.MkdirAll(filepath.Join(b.Cfg.OutDir, "chunks"), 0755); err != nil {
		return "", err
	}
	if err := fsutil.WriteFileIfChanged(filepath.Join(b.Cfg.OutDir, name), []byte(clientIslandLoaderJS), 0644); err != nil {
		return "", err
	}
	return `<script defer src="` + assetHref(b.Cfg.BaseURLPath(), name) + `"></script>`, nil
}

// clientReviveJS turns serialized island props back into component props:
// server-rendered children become a display:contents element holding the
// HTML, and server action references become callable functions.
const clientReviveJS = `
function __krateTree(n, h, call, comp) {
  if (n === null || n === undefined || typeof n === "boolean") return null;
  if (typeof n === "string" || typeof n === "number") return n;
  if (Array.isArray(n)) return n.map(function (x) { return __krateTree(x, h, call, comp); });
  if (n.$ === "h") return h("krate-children", { style: "display:contents", dangerouslySetInnerHTML: { __html: n.h } });
  var kids = (n.c || []).map(function (x) { return __krateTree(x, h, call, comp); });
  var props = __krateRevive(n.p || {}, h, call, comp);
  if (n.$ === "e") return h.apply(null, [n.t, props].concat(kids));
  var C = comp(n.m, n.e);
  if (typeof C !== "function") throw new Error("client component " + n.e + " of " + n.m + " is not loaded");
  return h.apply(null, [C, props].concat(kids));
}
function __krateRevive(v, h, call, comp) {
  if (Array.isArray(v)) return v.map(function (x) { return __krateRevive(x, h, call, comp); });
  if (v && typeof v === "object") {
    if (typeof v.__krateAction === "string") {
      var url = v.__krateAction;
      var fn = function () { return call(url, Array.prototype.slice.call(arguments)); };
      fn.actionUrl = url;
      return fn;
    }
    var out = {};
    for (var k in v) {
      if (k === "__krateChildren") out.children = h("krate-children", { style: "display:contents", dangerouslySetInnerHTML: { __html: v[k] } });
      else if (k === "__krateTree") out.children = __krateTree(v[k], h, call, comp);
      else out[k] = __krateRevive(v[k], h, call, comp);
    }
    return out;
  }
  return v;
}
function __krateClientRefs(n, out) {
  if (!n || typeof n !== "object") return out;
  if (Array.isArray(n)) { n.forEach(function (x) { __krateClientRefs(x, out); }); return out; }
  if (n.$ === "c") out.push({ m: n.m, e: n.e, s: n.s });
  (n.c || []).forEach(function (x) { __krateClientRefs(x, out); });
  if (n.p) for (var k in n.p) { if (k === "__krateTree") __krateClientRefs(n.p[k], out); }
  return out;
}
`

// clientActionCallJS posts a server action call from a client component,
// using the same protocol as the Krate runtime (callServerAction).
const clientActionCallJS = `
async function __krateCallAction(url, args) {
  var headers = { accept: "application/json", "x-krate-action": "fetch", "content-type": "application/json" };
  if (typeof location !== "undefined") headers["x-krate-return"] = location.pathname + location.search;
  var res = await fetch(url, { method: "POST", credentials: "same-origin", headers: headers, body: JSON.stringify({ args: args }) });
  var payload = null;
  try { payload = await res.json(); } catch (e) { payload = null; }
  if (!res.ok || !payload || payload.ok === false) throw new Error((payload && payload.error && (payload.error.message || payload.error)) || ("server action failed: " + res.status));
  if (typeof payload.redirect === "string" && payload.redirect) {
    var to = payload.redirect;
    if (to.charAt(0) === "/" && to.charAt(1) !== "/" && to.indexOf("\\\\") === -1 && !/[\\x00-\\x1f\\x7f]/.test(to)) location.assign(to);
  }
  return payload.result;
}
`

// clientModuleID is the stable boundary id of a module.
func clientModuleID(root, path string) string {
	root, path = canonicalPath(root), canonicalPath(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	return "c" + hashContent([]byte(filepath.ToSlash(rel)))
}

// prepareClientBoundaries validates the server tree of a page that renders
// server components and builds the client chunk of every "use client" module
// in its module graph. It returns nil for pages without server components.
func (b *Builder) prepareClientBoundaries(bundle *bundler.Bundle, ann *irtree.Annotations, serverPage bool) ([]ClientBoundary, error) {
	roots := serverComponentNames(ann)
	if serverPage {
		roots = append([]string{ann.EntryPoint}, roots...)
	}
	if len(roots) == 0 {
		return nil, nil
	}
	clientMods := map[string]*bundler.Module{}
	for _, mod := range bundle.Modules {
		if mod == nil || mod.IsExternal || mod.IsCSS || mod.Program == nil {
			continue
		}
		// A package's own "use client" modules are never bundled into the
		// server bundle (packages stay external); the package boundaries of
		// the modules that import it stand in for them.
		if resolver.IsCompiledDependency(mod.Path) {
			continue
		}
		if bundler.HasUseClientDirective(mod.SourceCode) {
			clientMods[filepath.Clean(mod.Path)] = mod
			if resolved := canonicalPath(mod.Path); resolved != filepath.Clean(mod.Path) {
				clientMods[resolved] = mod
			}
		}
	}
	// A module that imports a real-React package is a client boundary too: it
	// runs in the browser on real React, whether or not it says "use client".
	realMods := map[string]bool{}
	if bundle.Resolver != nil {
		islands := b.detectReactIslandModules(bundle.Modules, bundle.Resolver.Resolve)
		for _, mod := range bundle.Modules {
			if mod == nil || mod.Program == nil || mod.IsEntry {
				continue
			}
			// A "use client" module that renders a real-React module runs on
			// real React as a whole, like one that imports the package.
			if im, ok := islands[mod.Path]; ok && (im.Direct || im.Real && bundler.HasUseClientDirective(mod.SourceCode)) {
				clientMods[filepath.Clean(mod.Path)] = mod
				realMods[filepath.Clean(mod.Path)] = true
				// The canonical spelling registered above must be real too, or
				// a symlinked root (macOS /var) builds it as a Preact boundary.
				if resolved := canonicalPath(mod.Path); resolved != filepath.Clean(mod.Path) {
					clientMods[resolved] = mod
					realMods[resolved] = true
				}
			}
		}
	}
	if errs := checkServerTree(ann, roots, clientMods, bundle.ActionImports); len(errs) > 0 {
		return nil, errs[0]
	}
	if len(clientMods) == 0 {
		return nil, nil
	}
	paths := make([]string, 0, len(clientMods))
	for p := range clientMods {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var out []ClientBoundary
	for _, p := range paths {
		mod := clientMods[p]
		exports, err := clientModuleExports(mod)
		if err != nil {
			return nil, err
		}
		if realMods[p] {
			cb := ClientBoundary{ModuleID: clientModuleID(b.Root, mod.Path), SourcePath: mod.Path, Exports: exports, Real: true}
			cb.ChunkURL = b.registerRealBoundary(cb)
			out = append(out, cb)
			continue
		}
		if err := asyncClientComponentError(mod); err != nil {
			return nil, err
		}
		url, err := b.clientChunk(mod.Path)
		if err != nil {
			return nil, err
		}
		out = append(out, ClientBoundary{ModuleID: clientModuleID(b.Root, mod.Path), SourcePath: mod.Path, Exports: exports, ChunkURL: url})
	}
	return out, nil
}

// clientModuleExports lists the export names of a "use client" module.
func clientModuleExports(mod *bundler.Module) ([]string, error) {
	seen := map[string]bool{}
	var names []string
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	for _, stmt := range mod.Program.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		if exp.Default {
			add("default")
			continue
		}
		if exp.StarReexport && exp.Namespace == "" {
			return nil, diag.New(mod.Path, exp.Position.Line, exp.Position.Col,
				"`export *` in a \"use client\" module cannot be wrapped as a client boundary",
				"list the re-exported names explicitly: export { A, B } from \""+exp.ReexportSource+"\"",
				lineOf(mod.SourceCode, exp.Position.Line)).WithCode(diag.CodeClientBoundaryExports)
		}
		add(exp.Namespace)
		switch d := exp.Declaration.(type) {
		case *ast.FnDecl:
			add(d.Name)
		case *ast.ClassDecl:
			add(d.Name)
		case *ast.VarStmt:
			for _, v := range d.Decls {
				add(v.Name)
			}
		}
		for _, n := range exp.Named {
			if n.Remote != "" {
				add(n.Remote)
			} else {
				add(n.Local)
			}
		}
		for _, n := range exp.ReexportNamed {
			if n.Remote != "" {
				add(n.Remote)
			} else {
				add(n.Local)
			}
		}
	}
	return names, nil
}

// asyncClientComponentError rejects an async function component declared in a
// "use client" module: client components run in the browser and cannot await.
func asyncClientComponentError(mod *bundler.Module) error {
	var bad *ast.FnDecl
	check := func(fn *ast.FnDecl) {
		if bad == nil && fn != nil && fn.Async && fn.Name != "" && fn.Name[0] >= 'A' && fn.Name[0] <= 'Z' {
			bad = fn
		}
	}
	for _, stmt := range mod.Program.Body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			check(s)
		case *ast.ExportStmt:
			if fn, ok := s.Declaration.(*ast.FnDecl); ok {
				check(fn)
			}
		}
	}
	if bad == nil {
		return nil
	}
	return diag.New(mod.Path, bad.Position.Line, bad.Position.Col,
		"client component "+bad.Name+" is async; components in a \"use client\" module cannot be async",
		"remove \"use client\" to make "+bad.Name+" a server component, or fetch the data in a server component and pass it as props",
		lineOf(mod.SourceCode, bad.Position.Line)).WithCode(diag.CodeAsyncClientComponent)
}

// clientOnlyHooks are React hooks and Krate primitives that need a client
// runtime. useMemo, useCallback, useId and use() also work in React server
// components and are allowed.
var clientOnlyHooks = map[string]bool{
	"useState": true, "useReducer": true, "useEffect": true, "useLayoutEffect": true,
	"useInsertionEffect": true, "useRef": true, "useContext": true, "useTransition": true,
	"useDeferredValue": true, "useOptimistic": true, "useActionState": true, "useFormStatus": true,
	"useSyncExternalStore": true, "useImperativeHandle": true,
	"createSignal": true, "createEffect": true, "createMemo": true, "createResource": true,
	"createStore": true, "onMount": true, "onCleanup": true,
}

var (
	eventAttrRe = regexp.MustCompile(`^on[A-Z]`)
	reactHookRe = regexp.MustCompile(`\b(use[A-Z]\w*)\s*\(`)
)

// checkServerTree walks the components rendered by the server roots (async
// components and an async page), stopping at "use client" modules, and
// reports client-only APIs and function props crossing into a client
// component.
func checkServerTree(ann *irtree.Annotations, roots []string, clientMods map[string]*bundler.Module, actions map[string]string) []error {
	var errs []error
	visited := map[string]bool{}
	isClient := func(name string) bool {
		_, ok := clientMods[filepath.Clean(ann.ComponentSources[name])]
		return ok
	}
	at := func(name string, pos ast.Pos, code diag.Code, msg, hint string) {
		file := ann.ComponentSources[name]
		if file == "" {
			file = ann.SourceFile
		}
		errs = append(errs, diag.New(file, pos.Line, pos.Col, msg, hint, lineOf(ann.ComponentRaw[name], pos.Line)).WithCode(code))
	}
	var visit func(name string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		fn := ann.Functions[name]
		if fn == nil || isClient(name) || resolver.IsCompiledDependency(filepath.ToSlash(ann.ComponentSources[name])) {
			return
		}
		file := filepath.Base(ann.ComponentSources[name])
		addClient := "add \"use client\" at the top of the module that defines it (" + file + ") to make it a client component, or move the interactive part into one"
		var children []string
		irtree.WalkAST(fn.Body, func(n any) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if hook := calleeName(x.Callee); clientOnlyHooks[hook] {
					// The bundler lowers React hooks to Krate primitives
					// (useState -> createSignal); name what the author wrote.
					if m := reactHookRe.FindStringSubmatch(lineOf(ann.ComponentRaw[name], x.Position.Line)); m != nil && clientOnlyHooks[m[1]] {
						hook = m[1]
					}
					at(name, x.Position, diag.CodeClientAPIInServer,
						hook+"() in "+name+", which renders as a server component; it only works in a client component", addClient)
				}
			case *ast.JSXElement:
				if x.Opening == nil {
					return true
				}
				tag := x.Opening.Name
				if fnTag := ann.Functions[tag]; fnTag != nil {
					children = append(children, tag)
					if isClient(tag) {
						checkClientCallSite(x, tag, name, at)
					}
					return true
				}
				if tag != "" && tag[0] >= 'a' && tag[0] <= 'z' {
					for _, a := range x.Opening.Attributes {
						if a == nil || a.Spread || !eventAttrRe.MatchString(a.Name) || a.Value == nil {
							continue
						}
						if id, ok := a.Value.(*ast.Identifier); ok && actions[id.Name] != "" {
							continue
						}
						at(name, a.Position, diag.CodeClientAPIInServer,
							"event handler "+a.Name+" on <"+tag+"> in "+name+", which renders as a server component; server-rendered HTML has no event handlers", addClient)
					}
				}
			}
			return true
		})
		for _, c := range children {
			visit(c)
		}
	}
	for _, r := range roots {
		visit(r)
	}
	return errs
}

// checkClientCallSite reports function-literal props passed from a server
// component to a client component. Values only known at request time are
// checked by clientBoundary when the page renders.
func checkClientCallSite(el *ast.JSXElement, tag, owner string, at func(string, ast.Pos, diag.Code, string, string)) {
	for _, a := range el.Opening.Attributes {
		if a == nil || a.Spread || a.Value == nil {
			continue
		}
		if fn, ok := a.Value.(*ast.ArrowFn); ok {
			pos := a.Position
			if pos.Line == 0 {
				pos = fn.Position
			}
			at(owner, pos, diag.CodeClientPropNotSerializable,
				"prop `"+a.Name+"` passed from server component "+owner+" to client component <"+tag+"> is a function; props crossing to the client must be JSON-serializable",
				"define the handler inside the client component, or pass a server action (a \"use server\" export)")
		}
	}
}

// calleeName returns the hook-ish name of a call: `useState` or
// `React.useState` -> "useState".
func calleeName(e ast.Expr) string {
	switch c := e.(type) {
	case *ast.Identifier:
		return c.Name
	case *ast.MemberExpr:
		if p, ok := c.Property.(*ast.Identifier); ok && !c.Computed {
			return p.Name
		}
	}
	return ""
}

// clientChunkInfo is a built hydration chunk: its URL and whether its code
// has state or effects (a stateless package component needs no island).
type clientChunkInfo struct {
	url      string
	stateful bool
}

var statefulHookRe = regexp.MustCompile(`\buse(State|Reducer|Effect|LayoutEffect|InsertionEffect|SyncExternalStore|ImperativeHandle|Transition|Optimistic|ActionState|DeferredValue)\b|\b(Pure)?Component\b`)

// boundaryLinkShim is the global <Link> of a page, for components that run on
// Preact: client modules and the server render of package boundaries have no
// Krate runtime, so a bare `Link` would be a ReferenceError there. It renders
// the same anchor as the server renderer, which the router picks up.
const boundaryLinkShim = `import { h } from "preact";
function basePath() {
  var c = globalThis.__KRATE_CFG__;
  if (!c && typeof document !== "undefined") {
    var el = document.getElementById("__krate_cfg__");
    if (el) { try { c = globalThis.__KRATE_CFG__ = JSON.parse(el.textContent || "{}"); } catch (e) {} }
  }
  var bp = (c && c.basePath) || "";
  if (!bp || bp === "/") return "";
  return "/" + bp.replace(/^\/+|\/+$/g, "");
}
export function Link(props) {
  var href = props.href || "";
  var out = {};
  for (var k in props) {
    if (k !== "href" && k !== "prefetch" && k !== "replace" && k !== "scroll" && k !== "basePath" && k !== "children" && k !== "className") out[k] = props[k];
  }
  if (href) {
    var bp = basePath();
    out.href = bp && props.basePath !== false && href.charCodeAt(0) === 47 && href.charCodeAt(1) !== 47 ? bp + href : href;
  }
  var external = props.basePath === false || /^(https?:|mailto:|tel:|#|javascript:)/.test(href) || href.indexOf("//") === 0 || props.target === "_blank" || props.download !== undefined;
  if (external) {
    out["data-krate-external"] = "";
    if (props.basePath === false) out["data-krate-no-base"] = "";
  } else {
    out["data-krate-link"] = "";
    if (props.prefetch !== false) out["data-prefetch"] = "";
    if (props.replace) out["data-krate-replace"] = "";
    if (props.scroll === false) out["data-krate-scroll"] = "false";
  }
  if (props.target === "_blank" && props.rel === undefined) out.rel = "noopener noreferrer";
  if (props.className) out.class = props.className;
  return h("a", out, props.children);
}
`

// boundaryShims writes the globals shim file (once per content) next to the
// server bundles and returns its path for esbuild's inject option.
func boundaryShims(dotKrate string) (string, error) {
	if err := os.MkdirAll(dotKrate, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dotKrate, "boundary-link."+hashContent([]byte(boundaryLinkShim))+".mjs")
	if err := fsutil.WriteFileIfChanged(path, []byte(boundaryLinkShim), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// clientChunk builds (once per build) the hydration chunk of a "use client"
// module and returns its URL. The chunk exports mount(el), called by
// clientIslandLoaderHTML for each <krate-island> of the module.
func (b *Builder) clientChunk(path string) (string, error) {
	url, _, err := b.clientChunkFor(path, filepath.Dir(path), "import * as __m from "+strconv.Quote(path)+";\n")
	return url, err
}

// clientChunkFor builds the hydration chunk of a boundary. entry is the module
// preamble that binds `__m`, the exports components are looked up in by name
// (dotted for members of a namespace export such as Radix's Dialog.Root).
func (b *Builder) clientChunkFor(key, resolveDir, entry string) (string, bool, error) {
	b.clientChunkMu.Lock()
	defer b.clientChunkMu.Unlock()
	if info, ok := b.clientChunks[key]; ok {
		return info.url, info.stateful, nil
	}
	if _, err := b.ensurePreactShared(); err != nil {
		return "", false, diag.Diagnostic{
			File:    key,
			Message: "client component runtime could not be built: " + err.Error(),
			Hint:    "\"use client\" components inside server components hydrate with Preact; install it with `pnpm add preact`, then rebuild",
		}.WithCode(diag.CodeIslandAsset)
	}
	shim, err := boundaryShims(filepath.Join(b.Cfg.OutDir, ".krate"))
	if err != nil {
		return "", false, err
	}
	src := "import { h, hydrate } from \"preact\";\n" + entry +
		clientReviveJS + clientActionCallJS + `
function __resolve(name) {
  return name.split(".").reduce(function (o, k) { return o == null ? o : o[k]; }, __m);
}
export function component(name) { return __resolve(name); }
export async function mount(el) {
  var name = el.getAttribute("data-export");
  var C = __resolve(name);
  if (typeof C !== "function") throw new Error("client module " + el.getAttribute("data-module") + " has no component export " + name);
  var raw = JSON.parse(el.getAttribute("data-props") || "{}");
  var loaded = {};
  loaded[el.getAttribute("data-module") + "#" + name] = C;
  await Promise.all(__krateClientRefs(raw.__krateTree, []).map(function (r) {
    return import(r.s).then(function (m) { loaded[r.m + "#" + r.e] = m.component(r.e); });
  }));
  var props = __krateRevive(raw, h, __krateCallAction, function (m, e) { return loaded[m + "#" + e]; });
  hydrate(h(C, props), el);
  el.setAttribute("data-krate-island-hydrated", "");
}
`
	result := esbuildBuild(api.BuildOptions{
		Stdin: &api.StdinOptions{
			Contents:   src,
			ResolveDir: resolveDir,
			Sourcefile: "krate-client-boundary.js",
			Loader:     api.LoaderJS,
		},
		AbsWorkingDir:     b.Root,
		Bundle:            true,
		Write:             false,
		MinifyWhitespace:  b.shouldMinifyJS(),
		MinifyIdentifiers: b.shouldMinifyJS(),
		MinifySyntax:      b.shouldMinifyJS(),
		Format:            api.FormatESModule,
		Platform:          api.PlatformBrowser,
		Target:            api.ES2020,
		JSX:               api.JSXAutomatic,
		JSXImportSource:   "preact",
		TsconfigRaw:       clientTsconfigRaw(b.Root),
		Alias:             islandAlias(b.Root, false),
		External:          []string{"preact", "preact/compat", "preact/hooks", "preact/jsx-runtime"},
		Inject:            []string{shim},
		Plugins:           []api.Plugin{clientOnlyMarkersPlugin(true), actionServerBundlePlugin(b.importResolver(), b.actionModules(), b.Cfg.BaseURLPath())},
		LogLevel:          api.LogLevelSilent,
	})
	if len(result.Errors) > 0 || len(result.OutputFiles) == 0 {
		return "", false, clientBuildError(b.Root, key, result.Errors)
	}
	out := result.OutputFiles[0].Contents
	// This build checks the module and tells whether it holds state. The
	// chunk the page loads is built with the island entries (writeIslandBundles),
	// so a library used by a boundary, an island and a layout is one module in
	// the browser: sonner's toast() reaches the Toaster of another chunk.
	name := b.islandEntryName(src, resolveDir)
	b.setIslandEntries("client-boundary\x00"+key, []islandEntry{{Name: name, Source: src, ResolveDir: resolveDir, Boundary: true}})
	info := clientChunkInfo{url: b.islandEntryURL(name), stateful: statefulHookRe.Match(out)}
	if b.clientChunkText == nil {
		b.clientChunkText = map[string]string{}
	}
	b.clientChunkText[info.url] = string(out)
	if b.clientChunks == nil {
		b.clientChunks = map[string]clientChunkInfo{}
	}
	b.clientChunks[key] = info
	return info.url, info.stateful, nil
}

const serverOnlyMarker = "krate: server-only module in client bundle"

// clientOnlyMarkersPlugin resolves the `server-only` and `client-only` marker
// packages. In a client bundle `server-only` is an error naming the importer;
// everything else resolves to an empty module (the packages themselves need
// bundler conditions Krate does not use).
func clientOnlyMarkersPlugin(client bool) api.Plugin {
	return api.Plugin{
		Name: "krate-server-client-only",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^(server-only|client-only)$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if client && args.Path == "server-only" {
					return api.OnResolveResult{}, fmt.Errorf("%s: %s imports \"server-only\"", serverOnlyMarker, args.Importer)
				}
				return api.OnResolveResult{Path: args.Path, Namespace: "krate-marker"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-marker"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
				empty := "export {};"
				return api.OnLoadResult{Contents: &empty, Loader: api.LoaderJS}, nil
			})
		},
	}
}

// clientBuildError converts esbuild errors of a client boundary build into a
// diagnostic, recognizing the server-only marker.
func clientBuildError(root, path string, errs []api.Message) error {
	var msgs []string
	file, line, col := path, 0, 0
	located := false
	for _, e := range errs {
		if strings.Contains(e.Text, serverOnlyMarker) {
			importer := strings.TrimSpace(strings.TrimSuffix(strings.SplitN(e.Text, serverOnlyMarker+": ", 2)[1], "imports \"server-only\""))
			return diag.New(path, 0, 0,
				"client component module "+filepath.Base(path)+" reaches a server-only module ("+importer+" imports \"server-only\")",
				"import the server-only module from a server component and pass the data to the client component as props", "").WithCode(diag.CodeServerOnlyInClient)
		}
		if e.Location != nil {
			rel := esbuildRelFile(root, root, e.Location.File)
			msgs = append(msgs, fmt.Sprintf("%s:%d:%d: %s", rel, e.Location.Line, e.Location.Column+1, e.Text))
			if !located {
				// Point at the module esbuild found the problem in, which can be
				// a package the client component imports.
				file, line, col, located = rel, e.Location.Line, e.Location.Column+1, true
			}
		} else {
			msgs = append(msgs, e.Text)
		}
	}
	joined := strings.Join(msgs, "\n")
	hint := "check the client component and its imports, then rebuild"
	if strings.Contains(joined, "preact") {
		hint = "\"use client\" components inside server components hydrate with Preact; install it with `pnpm add preact`"
	}
	return diag.Diagnostic{File: file, Line: line, Col: col, Message: "client component chunk could not be built:\n" + joined, Hint: hint}.WithCode(diag.CodeIslandAsset)
}

// clientSSRImpls builds the server-side render module of each "use client"
// module (Preact + preact-render-to-string, React aliased to preact/compat)
// once per CompileServerBundles call, under server-bundles/client/.
type clientSSRImpls struct {
	root, bundleDir string
	// actions are the "use server" modules; a client module that imports one
	// gets the action proxy instead of the server source.
	actions  map[string]map[string]string
	res      *bundler.ImportResolver
	basePath string
	mu       sync.Mutex
	done     map[string]error
}

func (c *clientSSRImpls) implPath(cb ClientBoundary) string {
	return filepath.Join(c.bundleDir, "client", cb.ModuleID+".ssr.mjs")
}

func (c *clientSSRImpls) build(cb ClientBoundary) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err, ok := c.done[cb.SourcePath]; ok {
		return err
	}
	shim, err := boundaryShims(filepath.Dir(c.bundleDir))
	if err != nil {
		return err
	}
	src := "import { h } from \"preact\";\nimport renderToString from \"preact-render-to-string\";\n" + cb.entrySource() +
		clientReviveJS + `
function __krateServerAction() { throw new Error("a server action passed to a client component cannot be called while the server renders it"); }
function __krateComp(m, e) {
  var reg = globalThis.__krateClientImpls;
  var hit = reg && reg.get(m + "#" + e);
  return hit && hit.impl;
}
export const __krate_module = __m;
export function __krate_render(C, props) {
  return renderToString(h(C, __krateRevive(props, h, __krateServerAction, __krateComp)));
}
`
	plugin := islandRenderToStringPlugin(cb.resolveDir())
	result := esbuildBuild(api.BuildOptions{
		Stdin: &api.StdinOptions{
			Contents:   src,
			ResolveDir: cb.resolveDir(),
			Sourcefile: "krate-client-ssr.js",
			Loader:     api.LoaderJS,
		},
		AbsWorkingDir:   c.root,
		Bundle:          true,
		Write:           true,
		Outfile:         c.implPath(cb),
		Format:          api.FormatESModule,
		Platform:        api.PlatformNode,
		MainFields:      []string{"module", "main"},
		Target:          api.ES2020,
		JSX:             api.JSXAutomatic,
		JSXImportSource: "preact",
		TsconfigRaw:     clientTsconfigRaw(c.root),
		Alias:           islandAlias(c.root, false),
		External:        []string{"preact", "preact/compat", "preact/hooks", "preact/jsx-runtime"},
		Inject:          []string{shim},
		Plugins:         []api.Plugin{serverCSSPlugin(c.root), *plugin, clientOnlyMarkersPlugin(false), actionServerBundlePlugin(c.res, c.actions, c.basePath)},
		LogLevel:        api.LogLevelSilent,
	})
	if len(result.Errors) > 0 {
		err = clientBuildError(c.root, cb.SourcePath, result.Errors)
	}
	if c.done == nil {
		c.done = map[string]error{}
	}
	c.done[cb.SourcePath] = err
	return err
}

// clientBoundaryStub is the page-bundle replacement of a "use client" module:
// every export wrapped by clientBoundary around its server render module.
func clientBoundaryStub(cb ClientBoundary, implRel string) string {
	var sb strings.Builder
	sb.WriteString("import { clientBoundary as __krateCB } from \"@krate/runtime/server\";\n")
	fmt.Fprintf(&sb, "import { __krate_module as __m, __krate_render as __r } from %s;\n", strconv.Quote(implRel))
	for _, name := range cb.Exports {
		wrapped := fmt.Sprintf("__krateCB(%s, %s, %s, __m[%s], __r, %t)", strconv.Quote(cb.ModuleID), strconv.Quote(name), strconv.Quote(cb.ChunkURL), strconv.Quote(name), cb.hydrates())
		if name == "default" {
			fmt.Fprintf(&sb, "export default %s;\n", wrapped)
		} else {
			fmt.Fprintf(&sb, "export const %s = %s;\n", name, wrapped)
		}
	}
	return sb.String()
}

// clientBoundaryPlugin swaps "use client" modules for their boundary stubs in
// a page server bundle, keeps the server render modules external (loaded from
// server-bundles/client/ at run time), and resolves the server-only /
// client-only markers to empty modules.
func clientBoundaryPlugin(boundaries []ClientBoundary, impls *clientSSRImpls, outPath string) (api.Plugin, error) {
	byPath := map[string]ClientBoundary{}
	var pkgs []ClientBoundary
	for _, cb := range boundaries {
		if !cb.Real {
			if err := impls.build(cb); err != nil {
				return api.Plugin{}, err
			}
		}
		if cb.Spec != "" {
			pkgs = append(pkgs, cb)
			continue
		}
		byPath[filepath.Clean(cb.SourcePath)] = cb
		if resolved := canonicalPath(cb.SourcePath); resolved != filepath.Clean(cb.SourcePath) {
			byPath[resolved] = cb
		}
	}
	markers := clientOnlyMarkersPlugin(false)
	return api.Plugin{
		Name: "krate-client-boundaries",
		Setup: func(pb api.PluginBuild) {
			markers.Setup(pb)
			if len(pkgs) > 0 {
				setupPackageBoundaries(pb, pkgs, outPath, impls)
			}
			if len(byPath) == 0 && len(pkgs) == 0 {
				return
			}
			pb.OnResolve(api.OnResolveOptions{Filter: `\.ssr\.mjs$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if strings.Contains(args.Path, "/client/c") || strings.Contains(args.Path, "\\client\\c") {
					return api.OnResolveResult{Path: args.Path, External: true}, nil
				}
				return api.OnResolveResult{}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `\.[cm]?[jt]sx?$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				cb, ok := byPath[filepath.Clean(args.Path)]
				if !ok {
					for _, candidate := range boundaries {
						if sameFileIdentity(args.Path, candidate.SourcePath) {
							cb, ok = candidate, true
							break
						}
					}
				}
				if !ok {
					return api.OnLoadResult{}, nil
				}
				if cb.Real {
					stub := realBoundaryStub(cb)
					return api.OnLoadResult{Contents: &stub, Loader: api.LoaderJS, ResolveDir: filepath.Dir(args.Path)}, nil
				}
				rel, err := filepath.Rel(filepath.Dir(outPath), impls.implPath(cb))
				if err != nil {
					return api.OnLoadResult{}, err
				}
				rel = filepath.ToSlash(rel)
				if !strings.HasPrefix(rel, ".") {
					rel = "./" + rel
				}
				stub := clientBoundaryStub(cb, rel)
				return api.OnLoadResult{Contents: &stub, Loader: api.LoaderJS, ResolveDir: filepath.Dir(args.Path)}, nil
			})
		},
	}, nil
}

// clientTsconfigRaw is the project's tsconfig.json with the JSX settings
// forced to Preact's automatic runtime. A project that sets
// jsxImportSource to @krate/runtime (every migrated app does) would otherwise
// make esbuild compile client components against Krate's DOM runtime, which
// cannot render on the server and is not what the islands mount with.
func clientTsconfigRaw(root string) string {
	opts := map[string]any{}
	if data, err := os.ReadFile(filepath.Join(root, "tsconfig.json")); err == nil {
		var cfg map[string]any
		if json.Unmarshal(data, &cfg) == nil {
			if co, ok := cfg["compilerOptions"].(map[string]any); ok {
				opts = co
			}
		}
	}
	delete(opts, "jsxFactory")
	delete(opts, "jsxFragmentFactory")
	opts["jsx"] = "react-jsx"
	opts["jsxImportSource"] = "preact"
	out, err := json.Marshal(map[string]any{"compilerOptions": opts})
	if err != nil {
		return `{ "compilerOptions": { "jsx": "react-jsx", "jsxImportSource": "preact" } }`
	}
	return string(out)
}

// setupPackageBoundaries makes the page's own modules import the boundary stub
// of a React-bound package instead of the package, which the server bundle
// otherwise leaves external. Modules inside node_modules and the render
// modules under server-bundles/client/ are separate builds and never see it.
func setupPackageBoundaries(pb api.PluginBuild, pkgs []ClientBoundary, outPath string, impls *clientSSRImpls) {
	bySpec := map[string]ClientBoundary{}
	var specs []string
	for _, cb := range pkgs {
		bySpec[cb.Spec] = cb
		specs = append(specs, specFilterEscape.ReplaceAllStringFunc(cb.Spec, func(s string) string { return `\` + s }))
	}
	const ns = "krate-pkg-boundary"
	pb.OnResolve(api.OnResolveOptions{Filter: "^(" + strings.Join(specs, "|") + ")$"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
		if strings.Contains(filepath.ToSlash(args.Importer), "/node_modules/") {
			return api.OnResolveResult{}, nil
		}
		return api.OnResolveResult{Path: args.Path, Namespace: ns}, nil
	})
	pb.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: ns}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
		cb := bySpec[args.Path]
		if cb.Real {
			stub := realBoundaryStub(cb)
			return api.OnLoadResult{Contents: &stub, Loader: api.LoaderJS, ResolveDir: filepath.Dir(outPath)}, nil
		}
		rel, err := filepath.Rel(filepath.Dir(outPath), impls.implPath(cb))
		if err != nil {
			return api.OnLoadResult{}, err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, ".") {
			rel = "./" + rel
		}
		stub := clientBoundaryStub(cb, rel)
		return api.OnLoadResult{Contents: &stub, Loader: api.LoaderJS, ResolveDir: filepath.Dir(outPath)}, nil
	})
}

var specFilterEscape = regexp.MustCompile(`[.*+?^${}()|\[\]\\/-]`)
