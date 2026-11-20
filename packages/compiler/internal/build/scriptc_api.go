package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/gohost"
	"github.com/kratejs/krate/packages/compiler/internal/native/runtimes"
)

// Runtimes recorded in the scriptc manifest for each API route.
const (
	scriptcRouteScriptc = "scriptc"
	scriptcRouteQuickJS = "quickjs"
	scriptcRouteNode    = "node"
)

// scriptcAPIMethods are the HTTP method exports a scriptc route can have, in
// the order the Node sidecar lists them in an Allow header.
var scriptcAPIMethods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}

// scriptcManifest is .krate/api-scriptc.json: where each JS/TS API route
// runs when ssr.apiRuntime is "scriptc", and the executable that serves the
// routes marked scriptc.
type scriptcManifest struct {
	Version int `json:"version"`
	// Executable is the server's path relative to .krate; empty when no
	// route is compiled.
	Executable string                `json:"executable,omitempty"`
	Routes     []scriptcManifestItem `json:"routes"`
}

type scriptcManifestItem struct {
	// File is the route's out-dir relative bundle name, as in api-routes.json.
	File string `json:"file"`
	// Source is the route module, relative to the project root.
	Source string `json:"source"`
	// Runtime is "scriptc", or "quickjs" / "node" for the runtime that
	// serves the route instead.
	Runtime string `json:"runtime"`
	// Reason says why a route is not compiled.
	Reason string `json:"reason,omitempty"`
}

func scriptcManifestPath(root string) string {
	return filepath.Join(root, ".krate", "api-scriptc.json")
}

// scriptcServerPath is the compiled API server.
func scriptcServerPath(root string) string {
	name := "api-scriptc-server"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(root, ".krate", name)
}

func scriptcEntryDir(root string) string {
	return filepath.Join(root, ".krate", "api-scriptc")
}

// loadScriptcManifest reads the manifest; ok is false when it is missing or
// unreadable.
func loadScriptcManifest(root string) (m scriptcManifest, ok bool) {
	data, err := os.ReadFile(scriptcManifestPath(root))
	if err != nil || json.Unmarshal(data, &m) != nil {
		return scriptcManifest{}, false
	}
	return m, true
}

// scriptcRoute is one route module compiled into the server.
type scriptcRoute struct {
	file    string // out-dir relative bundle name, "users/[id].js"
	source  string // absolute route module
	methods []string
}

// apiRouteExports lists what a route module exports by name. star is set for
// an `export *` the scan cannot see through.
type apiRouteExports struct {
	names      map[string]bool
	hasDefault bool
	star       bool
}

func routeExportsFromProgram(prog *ast.Program) apiRouteExports {
	ex := apiRouteExports{names: map[string]bool{}}
	for _, stmt := range prog.Body {
		e, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		if e.Default {
			ex.hasDefault = true
			continue
		}
		if e.StarReexport {
			ex.star = true
		}
		if e.Namespace != "" {
			ex.names[e.Namespace] = true
		}
		for _, n := range e.Named {
			ex.names[n.Remote] = true
		}
		for _, n := range e.ReexportNamed {
			ex.names[n.Remote] = true
		}
		switch d := e.Declaration.(type) {
		case *ast.FnDecl:
			ex.names[d.Name] = true
		case *ast.ClassDecl:
			ex.names[d.Name] = true
		case *ast.VarStmt:
			for _, v := range d.Decls {
				if v.Name != "" {
					ex.names[v.Name] = true
				}
				for _, n := range v.Names {
					ex.names[n] = true
				}
			}
		}
	}
	return ex
}

// scriptcMethods returns the HTTP methods a route exports. A route is not
// eligible (reason says why) when it has none, a default export (which scriptc
// rejects and which the other runtimes treat as a catch-all handler), or an
// `export *` the scan cannot read.
func scriptcMethods(file string) (methods []string, reason string) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, "the source cannot be read: " + err.Error()
	}
	prog, err := parseSourceFile(file, data)
	if err != nil || prog == nil {
		return nil, "the source cannot be parsed for its exports"
	}
	ex := routeExportsFromProgram(prog)
	switch {
	case ex.hasDefault:
		return nil, "it has a default export, which scriptc does not compile"
	case ex.star:
		return nil, "it re-exports with `export *`, so its handlers cannot be listed"
	}
	for _, m := range scriptcAPIMethods {
		if ex.names[m] {
			methods = append(methods, m)
		}
	}
	if len(methods) == 0 {
		return nil, "it exports no GET, POST or other HTTP method handler by name"
	}
	return methods, ""
}

// scriptcParamSpec returns the TypeScript array literal of a route's dynamic
// params: [name, segment index, kind] with kind 0 for [id], 1 for [...rest]
// and 2 for [[...rest]].
func scriptcParamSpec(file string) string {
	rel := strings.TrimSuffix(file, filepath.Ext(file))
	rel = strings.TrimSuffix(strings.Trim(rel, "/"), "index")
	segs := strings.Split(strings.Trim(rel, "/"), "/")
	var parts []string
	for i, seg := range segs {
		kind, name := -1, ""
		switch {
		case strings.HasPrefix(seg, "[[...") && strings.HasSuffix(seg, "]]"):
			kind, name = 2, seg[5:len(seg)-2]
		case strings.HasPrefix(seg, "[...") && strings.HasSuffix(seg, "]"):
			kind, name = 1, seg[4:len(seg)-1]
		case strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]"):
			kind, name = 0, seg[1:len(seg)-1]
		}
		if kind < 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("{ name: %s, index: %d, kind: %d }", tsString(name), i, kind))
		if kind > 0 {
			break
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func tsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// scriptcAllow is a route's Allow header value, as the Node sidecar lists it.
func scriptcAllow(methods []string) string {
	has := map[string]bool{}
	for _, m := range methods {
		has[m] = true
	}
	var out []string
	for _, m := range scriptcAPIMethods {
		if has[m] || (m == "HEAD" && has["GET"]) || m == "OPTIONS" {
			out = append(out, m)
		}
	}
	return strings.Join(out, ", ")
}

// scriptcImportPath is how the entry in dir imports the module file.
func scriptcImportPath(dir, file string) string {
	rel, err := filepath.Rel(dir, file)
	if err != nil {
		rel = file
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

// scriptcRouteTable generates the imports and the route table for routes. The
// imports are named only: scriptc rejects default imports and exports.
func scriptcRouteTable(dir string, routes []scriptcRoute) string {
	var imports, table strings.Builder
	for i, r := range routes {
		var names []string
		for _, m := range r.methods {
			names = append(names, m+" as r"+strconv.Itoa(i)+"_"+m)
		}
		fmt.Fprintf(&imports, "import { %s } from %s;\n", strings.Join(names, ", "), tsString(scriptcImportPath(dir, r.source)))
		fmt.Fprintf(&table, "const route%d = addRoute(%s, %s, %s);\n", i, tsString(r.file), scriptcParamSpec(r.file), tsString(scriptcAllow(r.methods)))
		for _, m := range r.methods {
			fmt.Fprintf(&table, "route%d.handlers.set(%s, r%d_%s as unknown as Handler);\n", i, tsString(m), i, m)
		}
	}
	return imports.String() + `
type Ctx = any;
type Handler = (request: Request, ctx: Ctx) => Response | Promise<Response>;
type PSpec = { name: string; index: number; kind: number };
type Route = { params: PSpec[]; handlers: Map<string, Handler>; allow: string };

const routes = new Map<string, Route>();
function addRoute(file: string, params: PSpec[], allow: string): Route {
  const route: Route = { params, handlers: new Map<string, Handler>(), allow };
  routes.set(file, route);
  return route;
}

` + table.String()
}

// scriptcEntrySource is the generated main.ts: the route modules' named
// imports and the server.
func scriptcEntrySource(dir string, routes []scriptcRoute) string {
	return "// Generated by krate build for ssr.apiRuntime \"scriptc\". Do not edit.\n" +
		"import { createServer } from 'node:http';\n" +
		"import type { IncomingMessage, ServerResponse } from 'node:http';\n" +
		scriptcRouteTable(dir, routes) + scriptcServerSource
}

// scriptcCheckSource is the entry of one route's coverage run: the route
// table and a call of each handler, so that scriptc analyses what the server
// would reach.
func scriptcCheckSource(dir string, r scriptcRoute) string {
	return "// Generated by krate build. Do not edit.\n" + scriptcRouteTable(dir, []scriptcRoute{r}) + `
const probe = new Request('http://localhost/');
for (const route of routes.values()) {
  for (const handler of route.handlers.values()) {
    handler(probe, { params: {} });
  }
}
`
}

const scriptcPackageJSON = "{\n  \"private\": true,\n  \"type\": \"module\"\n}\n"

// scriptcToolchainError words a toolchain failure for an API server build.
func scriptcToolchainError(err error) error {
	var d diag.Diagnostic
	if errors.As(err, &d) && d.Code == diag.CodeScriptcToolchain {
		d.Message = strings.Replace(d.Message, "native.host.runtime scriptc: ", "ssr.apiRuntime \"scriptc\": ", 1)
		d.File = "krate.config"
		return d
	}
	return err
}

// scriptcOriginFor maps diagnostics to the project's sources; a blocker in the
// generated entry is the generated server's, not the app's.
func (b *Builder) scriptcOrigin(dir string) runtimes.Origin {
	entry := filepath.ToSlash(filepath.Join(dir, "main.ts"))
	return runtimes.Origin{
		Root: b.Root,
		RuntimeSource: func(source string) bool {
			abs := source
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(b.Root, source)
			}
			return filepath.ToSlash(abs) == entry
		},
	}
}

// buildScriptcAPI compiles the API routes scriptc accepts into one native
// server when ssr.apiRuntime is "scriptc", and records where every route runs
// in .krate/api-scriptc.json. A route scriptc cannot compile keeps its
// embedded-runtime bundle and runs there, with a KR7238 warning (an error with
// ssr.scriptcStrict). Dev builds never compile: dev runs the embedded runtime.
func (b *Builder) buildScriptcAPI() error {
	if b.DevMode || b.Cfg == nil || !strings.EqualFold(b.Cfg.SSR.APIRuntime, "scriptc") {
		return nil
	}
	apiSrcDir := filepath.Join(b.Root, filepath.FromSlash(gohost.DefaultAPIDir))
	sources, err := apiRouteSourceFiles(apiSrcDir)
	if err != nil {
		return fmt.Errorf("scanning api routes: %w", err)
	}
	sort.Strings(sources)
	metas := map[string]apiRouteMeta{}
	for _, m := range readAPIRouteMeta(b.Root) {
		metas[m.File] = m
	}

	var candidates []scriptcRoute
	var items []scriptcManifestItem
	var fallbacks []diag.Diagnostic
	fallback := func(src, file, runtimeName, reason string, d *diag.Diagnostic) {
		rel := b.relToRoot(src)
		items = append(items, scriptcManifestItem{File: file, Source: rel, Runtime: runtimeName, Reason: reason})
		if d == nil {
			x := diag.New(rel, 1, 1, "API route "+file+" runs on "+runtimeLabel(runtimeName)+", not scriptc: "+reason,
				"see the ssr.apiRuntime \"scriptc\" limits in the API routes guide", "").WithCode(diag.CodeScriptcAPIRoute)
			d = &x
		}
		fallbacks = append(fallbacks, *d)
	}
	for _, src := range sources {
		rel, err := filepath.Rel(apiSrcDir, src)
		if err != nil {
			continue
		}
		file := strings.TrimSuffix(filepath.ToSlash(rel), filepath.Ext(rel)) + ".js"
		meta, ok := metas[file]
		if !ok {
			continue
		}
		if meta.NodeOnly {
			// The route needs Node already; that is not a scriptc matter.
			items = append(items, scriptcManifestItem{File: file, Source: b.relToRoot(src), Runtime: scriptcRouteNode, Reason: "the route needs Node"})
			continue
		}
		methods, reason := scriptcMethods(src)
		if reason != "" {
			fallback(src, file, scriptcRouteQuickJS, reason, nil)
			continue
		}
		candidates = append(candidates, scriptcRoute{file: file, source: src, methods: methods})
	}

	dir := scriptcEntryDir(b.Root)
	exe := scriptcServerPath(b.Root)
	if len(candidates) == 0 {
		return b.finishScriptc(items, fallbacks, "")
	}
	if err := runtimes.ScriptcToolchain(runtimes.HostTarget()); err != nil {
		return scriptcToolchainError(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeOutput(filepath.Join(dir, "package.json"), []byte(scriptcPackageJSON)); err != nil {
		return err
	}
	origin := b.scriptcOrigin(dir)
	main := filepath.Join(dir, "main.ts")
	check := func(routes []scriptcRoute) (*runtimes.ScriptcReport, []diag.Diagnostic, error) {
		if err := writeOutput(main, []byte(scriptcEntrySource(dir, routes))); err != nil {
			return nil, nil, err
		}
		return runtimes.ScriptcCoverageReport(dir, main, "blockers", origin)
	}

	rep, ds, err := check(candidates)
	if err != nil {
		return err
	}
	routes := candidates
	if !rep.Success {
		// Find the blocked routes one by one, then confirm the rest together.
		routes = nil
		for _, r := range candidates {
			blockers, err := b.scriptcRouteBlockers(dir, r)
			if err != nil {
				return err
			}
			if len(blockers) == 0 {
				routes = append(routes, r)
				continue
			}
			first := blockers[0]
			reason := first.Message
			first.Message = "API route " + r.file + " runs on QuickJS, not scriptc: " + first.Message
			first.Code = diag.CodeScriptcAPIRoute
			fallback(r.source, r.file, scriptcRouteQuickJS, reason, &first)
		}
		if len(routes) > 0 {
			rep, ds, err = check(routes)
			if err != nil {
				return err
			}
		}
	}
	if len(routes) > 0 && !rep.Success {
		return &runtimes.ScriptcError{Diagnostics: errorDiagnostics(ds)}
	}
	if b.Cfg.SSR.ScriptcStrict && len(fallbacks) > 0 {
		return strictScriptcError(fallbacks)
	}
	if len(routes) == 0 {
		return b.finishScriptc(items, fallbacks, "")
	}

	_ = os.Remove(exe)
	if err := runtimes.ScriptcBuild(runtimes.HostTarget(), main, exe); err != nil {
		return err
	}
	// scriptc leaves the LLVM IR of the entry next to the executable.
	_ = os.Remove(filepath.Join(filepath.Dir(exe), "main.ll"))
	for _, r := range routes {
		items = append(items, scriptcManifestItem{File: r.file, Source: b.relToRoot(r.source), Runtime: scriptcRouteScriptc})
	}
	printf("  %s*%s API: %d route(s) compiled by scriptc -> %s\n", cCyan, cReset, len(routes), filepath.ToSlash(b.relToRoot(exe)))
	return b.finishScriptc(items, fallbacks, filepath.Base(exe))
}

func runtimeLabel(rt string) string {
	if rt == scriptcRouteQuickJS {
		return "QuickJS"
	}
	return "Node"
}

// scriptcRouteBlockers runs scriptc coverage on one route alone and returns
// its blockers.
func (b *Builder) scriptcRouteBlockers(dir string, r scriptcRoute) ([]diag.Diagnostic, error) {
	checkDir := filepath.Join(dir, "check")
	if err := os.MkdirAll(checkDir, 0o755); err != nil {
		return nil, err
	}
	entry := filepath.Join(checkDir, "route.ts")
	if err := writeOutput(entry, []byte(scriptcCheckSource(checkDir, r))); err != nil {
		return nil, err
	}
	rep, ds, err := runtimes.ScriptcCoverageReport(dir, entry, "blockers", b.scriptcOrigin(dir))
	if err != nil {
		return nil, err
	}
	if rep.Success {
		return nil, nil
	}
	errs := errorDiagnostics(ds)
	if len(errs) == 0 {
		// A failure scriptc reports outside the diagnostics in scope.
		errs = []diag.Diagnostic{diag.New(b.relToRoot(r.source), 1, 1, "scriptc reported a blocker without a location",
			"run scriptc coverage on the route by hand", "")}
	}
	return errs, nil
}

func errorDiagnostics(ds []diag.Diagnostic) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, d := range ds {
		if d.Severity != diag.SeverityWarning {
			out = append(out, d)
		}
	}
	return out
}

func strictScriptcError(ds []diag.Diagnostic) error {
	errs := make([]error, len(ds))
	for i, d := range ds {
		d.Severity = diag.SeverityError
		d.Hint = strings.TrimSuffix(d.Hint, ".") + "; or set ssr.scriptcStrict to false to run such routes on QuickJS"
		errs[i] = d
	}
	return errors.Join(errs...)
}

// finishScriptc prints the fallback warnings and writes the manifest.
func (b *Builder) finishScriptc(items []scriptcManifestItem, fallbacks []diag.Diagnostic, exe string) error {
	if b.Cfg.SSR.ScriptcStrict && len(fallbacks) > 0 {
		return strictScriptcError(fallbacks)
	}
	if exe == "" {
		_ = os.Remove(scriptcServerPath(b.Root))
	}
	for i := range fallbacks {
		fallbacks[i].Severity = diag.SeverityWarning
	}
	printWarningsOnce(os.Stderr, fallbacks)
	sort.Slice(items, func(i, j int) bool { return items[i].File < items[j].File })
	data, err := json.MarshalIndent(scriptcManifest{Version: 1, Executable: exe, Routes: items}, "", "  ")
	if err != nil {
		return err
	}
	return writeOutput(scriptcManifestPath(b.Root), append(data, '\n'))
}

func (b *Builder) relToRoot(p string) string {
	if rel, err := filepath.Rel(b.Root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(p)
}
