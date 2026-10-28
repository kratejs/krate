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
}

// clientIslandLoaderJS mounts every <krate-island> that is not mounted yet
// from its chunk. It is a classic script in a chunk file (not inline) so the
// SPA router loads it when a soft navigation arrives on a page with islands,
// and it is idempotent: it only touches islands it has not mounted.
const clientIslandLoaderJS = `(function(){document.querySelectorAll("krate-island[data-src]:not([data-krate-island-mounted])").forEach(function(el){el.setAttribute("data-krate-island-mounted","");import(el.getAttribute("data-src")).then(function(m){m.mount(el)}).catch(function(e){console.error("krate client component:",e);el.setAttribute("data-krate-island-error","")})})})();`

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
function __krateRevive(v, h, call) {
  if (Array.isArray(v)) return v.map(function (x) { return __krateRevive(x, h, call); });
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
      else out[k] = __krateRevive(v[k], h, call);
    }
    return out;
  }
  return v;
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
  if (typeof payload.redirect === "string" && payload.redirect) location.assign(payload.redirect);
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
		if bundler.HasUseClientDirective(mod.SourceCode) {
			clientMods[filepath.Clean(mod.Path)] = mod
			if resolved := canonicalPath(mod.Path); resolved != filepath.Clean(mod.Path) {
				clientMods[resolved] = mod
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
		if fn == nil || isClient(name) {
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

// clientChunk builds (once per build) the hydration chunk of a "use client"
// module and returns its URL. The chunk exports mount(el), called by
// clientIslandLoaderHTML for each <krate-island> of the module.
func (b *Builder) clientChunk(path string) (string, error) {
	b.clientChunkMu.Lock()
	defer b.clientChunkMu.Unlock()
	if url, ok := b.clientChunks[path]; ok {
		return url, nil
	}
	if _, err := b.ensurePreactShared(); err != nil {
		return "", diag.Diagnostic{
			File:    path,
			Message: "client component runtime could not be built: " + err.Error(),
			Hint:    "\"use client\" components inside server components hydrate with Preact; install it with `pnpm add preact`, then rebuild",
		}.WithCode(diag.CodeIslandAsset)
	}
	src := "import { h, hydrate } from \"preact\";\nimport * as __m from " + strconv.Quote(path) + ";\n" +
		clientReviveJS + clientActionCallJS + `
export function mount(el) {
  var name = el.getAttribute("data-export");
  var C = __m[name];
  if (typeof C !== "function") throw new Error("client module " + el.getAttribute("data-module") + " has no component export " + name);
  var props = __krateRevive(JSON.parse(el.getAttribute("data-props") || "{}"), h, __krateCallAction);
  hydrate(h(C, props), el);
  el.setAttribute("data-krate-island-hydrated", "");
}
`
	result := api.Build(api.BuildOptions{
		Stdin: &api.StdinOptions{
			Contents:   src,
			ResolveDir: filepath.Dir(path),
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
		Plugins:           []api.Plugin{clientOnlyMarkersPlugin(true), actionServerBundlePlugin(b.actionModules(), b.Cfg.BaseURLPath())},
		LogLevel:          api.LogLevelSilent,
	})
	if len(result.Errors) > 0 || len(result.OutputFiles) == 0 {
		return "", clientBuildError(path, result.Errors)
	}
	out := result.OutputFiles[0].Contents
	name := "chunks/client." + hashContent(out) + ".js"
	if err := os.MkdirAll(filepath.Join(b.Cfg.OutDir, "chunks"), 0755); err != nil {
		return "", err
	}
	if err := fsutil.WriteFileIfChanged(filepath.Join(b.Cfg.OutDir, name), out, 0644); err != nil {
		return "", err
	}
	url := assetHref(b.Cfg.BaseURLPath(), name)
	if b.clientChunks == nil {
		b.clientChunks = map[string]string{}
	}
	b.clientChunks[path] = url
	return url, nil
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
func clientBuildError(path string, errs []api.Message) error {
	var msgs []string
	for _, e := range errs {
		if strings.Contains(e.Text, serverOnlyMarker) {
			importer := strings.TrimSpace(strings.TrimSuffix(strings.SplitN(e.Text, serverOnlyMarker+": ", 2)[1], "imports \"server-only\""))
			return diag.New(path, 0, 0,
				"client component module "+filepath.Base(path)+" reaches a server-only module ("+importer+" imports \"server-only\")",
				"import the server-only module from a server component and pass the data to the client component as props", "").WithCode(diag.CodeServerOnlyInClient)
		}
		if e.Location != nil {
			msgs = append(msgs, fmt.Sprintf("%s:%d:%d: %s", e.Location.File, e.Location.Line, e.Location.Column, e.Text))
		} else {
			msgs = append(msgs, e.Text)
		}
	}
	joined := strings.Join(msgs, "\n")
	hint := "check the client component and its imports, then rebuild"
	if strings.Contains(joined, "preact") {
		hint = "\"use client\" components inside server components hydrate with Preact; install it with `pnpm add preact`"
	}
	return diag.Diagnostic{File: path, Message: "client component chunk could not be built:\n" + joined, Hint: hint}.WithCode(diag.CodeIslandAsset)
}

// clientSSRImpls builds the server-side render module of each "use client"
// module (Preact + preact-render-to-string, React aliased to preact/compat)
// once per CompileServerBundles call, under server-bundles/client/.
type clientSSRImpls struct {
	root, bundleDir string
	// actions are the "use server" modules; a client module that imports one
	// gets the action proxy instead of the server source.
	actions  map[string]map[string]string
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
	src := "import { h } from \"preact\";\nimport renderToString from \"preact-render-to-string\";\nimport * as __m from " + strconv.Quote(cb.SourcePath) + ";\n" +
		clientReviveJS + `
function __krateServerAction() { throw new Error("a server action passed to a client component cannot be called while the server renders it"); }
export const __krate_module = __m;
export function __krate_render(C, props) {
  return renderToString(h(C, __krateRevive(props, h, __krateServerAction)));
}
`
	plugin := islandRenderToStringPlugin(filepath.Dir(cb.SourcePath))
	result := api.Build(api.BuildOptions{
		Stdin: &api.StdinOptions{
			Contents:   src,
			ResolveDir: filepath.Dir(cb.SourcePath),
			Sourcefile: "krate-client-ssr.js",
			Loader:     api.LoaderJS,
		},
		AbsWorkingDir:   c.root,
		Bundle:          true,
		Write:           true,
		Outfile:         c.implPath(cb),
		Format:          api.FormatESModule,
		Platform:        api.PlatformNode,
		Target:          api.ES2020,
		JSX:             api.JSXAutomatic,
		JSXImportSource: "preact",
		TsconfigRaw:     clientTsconfigRaw(c.root),
		Alias:           islandAlias(c.root, false),
		External:        []string{"preact", "preact/compat", "preact/hooks", "preact/jsx-runtime"},
		Plugins:         []api.Plugin{*plugin, clientOnlyMarkersPlugin(false), actionServerBundlePlugin(c.actions, c.basePath)},
		LogLevel:        api.LogLevelSilent,
	})
	var err error
	if len(result.Errors) > 0 {
		err = clientBuildError(cb.SourcePath, result.Errors)
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
		wrapped := fmt.Sprintf("__krateCB(%s, %s, %s, __m[%s], __r)", strconv.Quote(cb.ModuleID), strconv.Quote(name), strconv.Quote(cb.ChunkURL), strconv.Quote(name))
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
	for _, cb := range boundaries {
		if err := impls.build(cb); err != nil {
			return api.Plugin{}, err
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
			if len(byPath) == 0 {
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
