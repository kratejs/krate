// Package hostts splits the TypeScript host code of a Krate Native app from
// its UI and generates the plumbing between them.
//
// Host code runs in the native process on an embedded JavaScript runtime. The
// UI imports it and calls it as async functions. Build produces:
//
//   - a host bundle, .krate/native/host/host.js (one ES2020 IIFE) with an
//     external source map and a manifest, which registers every host function
//     the UI imports with globalThis.__krateHost;
//   - source overlays for the UI bundler: a stub module for each host module
//     the UI imports, and each UI module with its inline `@host` functions
//     replaced by stubs;
//   - .d.ts files that type the stubs of whole host modules.
//
// Registry ids are "<module id>.<export>" for a host module export and
// "<module id>#<function>" for an inline function. The module id is the path
// relative to the project root without its extension, slash-separated.
//
// Only host code the UI imports is registered. A host module imported only by
// another host module is bundled into the host but not callable.
package hostts

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native"
	"github.com/kratejs/krate/packages/compiler/internal/native/bake"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// Options tune Build.
type Options struct {
	// Entries are the absolute paths of the UI entry modules (pages and
	// layouts). The UI import graph is followed from them. Default: every
	// script under the pages dir, or every non-host project script when the
	// project has no pages dir.
	Entries []string
	// Resolve resolves an import specifier of importer to an absolute file
	// path, or "" (for example Builder's path-alias aware resolver). Default:
	// bundler.ResolveImport.
	Resolve func(importer, spec string) string
	// OutDir is where the outputs go. Default: <root>/.krate/native.
	OutDir string
}

// Registration describes one registered host export.
type Registration struct {
	// ID is the registry id, "src/lib/sync.syncNow" or "src/pages/index#addTask".
	ID string `json:"id"`
	// Kind is "func", "stream", "signal" or "auto" (decided at run time).
	Kind string `json:"kind"`
	// File and Line locate the declaration, relative to the project root.
	File string `json:"file"`
	Line int    `json:"line"`
	// Module is the module id; Export the export or function name.
	Module string `json:"module"`
	Export string `json:"export"`
	// Inline is true for a function split out of a UI module.
	Inline bool `json:"inline,omitempty"`
}

// Result is what Build produced.
type Result struct {
	// HostBundle, SourceMap and Manifest are absolute paths; empty when Build
	// reported an error.
	HostBundle string
	SourceMap  string
	Manifest   string
	// Registered lists the registered ids in id order.
	Registered []Registration
	// Externals lists the imports left external, such as "node:fs".
	Externals []string
	// Overlays maps absolute module paths to the source the UI bundler must
	// use instead of the file (see bundler.Bundler.SetSourceOverlays).
	Overlays map[string]string
	// TypesDir holds the .d.ts files, one per host module the UI imports.
	TypesDir string
	Types    []string
	// Baked holds the results of the build functions (`@build`), by function
	// id and canonical argument key (see package bake). The UI stubs of those
	// functions read it through the app module.
	Baked bake.Table
	// Calls lists the UI calls of build functions with their baked keys, so
	// the caller can replace them by the values.
	Calls []BakedCall
}

// BakedCall is one literal call of a build function in a UI module.
type BakedCall struct {
	// File is the UI module relative to the project root; Offset and Local
	// locate the callee in the source the build saw (the module with its
	// inline host functions split out).
	File   string
	Offset int
	Local  string
	ID     string
	Key    string
}

// Use is a use of an imported host name in a UI module.
type Use struct {
	bake.Site
	// File is the UI module relative to the project root; Text the source
	// line.
	File string
	Text string
}

// loc is a place in a UI module, for diagnostics.
type loc struct {
	rel       string
	line, col int
	src       string
}

type usage struct {
	all    bool
	allLoc loc
	names  map[string]loc
}

type builder struct {
	root    string
	cfg     *config.Config
	p       *native.Placement
	opts    Options
	an      *analyzer
	diags   []diag.Diagnostic
	res     *Result
	hostMod map[string]*native.Module
	use     map[string]*usage
	gen     map[string]*genModule // module id -> generated inline host module
	genList []*genModule
	// srcs holds the original source of each UI module that was split.
	srcs   map[string]string
	goImps []GoImport
	// uses holds the uses of host names, by host module path or, for the
	// inline functions of a UI module, "inline:<module id>".
	uses map[string][]Use
	// builds lists the build functions the UI uses.
	builds []buildFn
}

// genModule is a generated host module for the inline functions of a UI module.
type genModule struct {
	modID  string
	path   string
	rel    string
	src    string // original UI source
	split  *splitResult
	absDir string
}

var dynImportRe = regexp.MustCompile(`\bimport\(\s*(?:"([^"\n]+)"|'([^'\n]+)')\s*\)`)

// Build splits the host code of the project at root and bundles it. It returns
// the result and the diagnostics found (errors and warnings; an error means
// the result holds no bundle). The error is for I/O failures.
func Build(root string, cfg *config.Config, p *native.Placement, opts Options) (*Result, []diag.Diagnostic, error) {
	b := &builder{
		root: root, cfg: cfg, p: p, opts: opts,
		res:     &Result{Overlays: map[string]string{}},
		hostMod: map[string]*native.Module{},
		use:     map[string]*usage{},
		gen:     map[string]*genModule{},
		srcs:    map[string]string{},
		uses:    map[string][]Use{},
	}
	resolve := opts.Resolve
	if resolve == nil {
		resolve = bundler.ResolveImport
	}
	b.opts.Resolve = resolve
	b.an = &analyzer{resolve: resolve, mods: map[string]*modInfo{}}
	if b.opts.OutDir == "" {
		b.opts.OutDir = filepath.Join(root, ".krate", "native")
	}
	for i := range p.Modules {
		m := &p.Modules[i]
		if m.Whole && isTSPath(m.Path) {
			b.hostMod[m.Path] = m
		}
	}
	if err := b.walkUI(); err != nil {
		return nil, nil, err
	}
	regs, err := b.collect()
	if err != nil {
		return nil, nil, err
	}
	if b.hasErrors() {
		return b.res, b.diags, nil
	}
	if err := b.bakeBuilds(); err != nil {
		return nil, nil, err
	}
	if b.hasErrors() {
		return b.res, b.diags, nil
	}
	if err := b.emit(regs); err != nil {
		return nil, nil, err
	}
	return b.res, b.diags, nil
}

// GoImport is one import of a .go file found in the UI import graph.
type GoImport struct {
	// File is the absolute path of the .go file.
	File string
	// Names are the imported names (the remote names of a named import or
	// re-export). A default import adds no name.
	Names []string
	// Namespace is true for `import * as x` and `export *`.
	Namespace bool
	// From is the importing UI file relative to the project root, with the
	// 1-based position of the import.
	From      string
	Line, Col int
	// Uses are the uses of the imported names in the importing module.
	Uses []Use
}

// GoImports finds every import of a .go file in the UI import graph. It
// follows the same graph as Build and has no other effect.
func GoImports(root string, cfg *config.Config, p *native.Placement, opts Options) ([]GoImport, error) {
	b := &builder{
		root: root, cfg: cfg, p: p, opts: opts,
		res:     &Result{Overlays: map[string]string{}},
		hostMod: map[string]*native.Module{},
		use:     map[string]*usage{},
		gen:     map[string]*genModule{},
		srcs:    map[string]string{},
		uses:    map[string][]Use{},
	}
	if b.opts.Resolve == nil {
		b.opts.Resolve = bundler.ResolveImport
	}
	for i := range p.Modules {
		m := &p.Modules[i]
		if m.Whole && isTSPath(m.Path) {
			b.hostMod[m.Path] = m
		}
	}
	if err := b.walkUI(); err != nil {
		return nil, err
	}
	return b.goImps, nil
}

func (b *builder) hasErrors() bool {
	for _, d := range b.diags {
		if !d.IsWarning() {
			return true
		}
	}
	return false
}

func (b *builder) rel(path string) string {
	if r, err := filepath.Rel(b.root, path); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(path)
}

func (b *builder) isUIFile(path string) bool {
	if !isTSPath(path) || strings.HasSuffix(path, ".d.ts") {
		return false
	}
	r, err := filepath.Rel(b.root, path)
	if err != nil || strings.HasPrefix(r, "..") {
		return false
	}
	for _, seg := range strings.Split(filepath.ToSlash(r), "/") {
		if seg == "node_modules" || seg == ".krate" {
			return false
		}
	}
	return true
}

func (b *builder) entries() ([]string, error) {
	if len(b.opts.Entries) > 0 {
		return b.opts.Entries, nil
	}
	files, err := native.ProjectFiles(b.root, b.cfg)
	if err != nil {
		return nil, err
	}
	pages := b.cfg.PagesDir
	if pages != "" && !filepath.IsAbs(pages) {
		pages = filepath.Join(b.root, pages)
	}
	var out, all []string
	for _, f := range files {
		if !b.isUIFile(f) || b.hostMod[f] != nil {
			continue
		}
		all = append(all, f)
		if pages != "" && strings.HasPrefix(f, pages+string(filepath.Separator)) {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return all, nil
	}
	return out, nil
}

// walkUI follows the UI import graph from the entries. It splits inline host
// functions out of the modules it visits and records which host module
// exports the UI imports.
func (b *builder) walkUI() error {
	entries, err := b.entries()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	queue := append([]string(nil), entries...)
	for len(queue) > 0 {
		path := filepath.Clean(queue[0])
		queue = queue[1:]
		if seen[path] || !b.isUIFile(path) || b.hostMod[path] != nil {
			continue
		}
		seen[path] = true
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		src := string(data)
		cur := src
		if m := b.p.Module(path); m != nil && !m.Whole {
			var inline []native.Func
			for _, f := range m.Funcs {
				if f.Inline {
					inline = append(inline, f)
				}
			}
			if len(inline) > 0 {
				sr, ds := splitInline(b.rel(path), path, src, inline)
				b.diags = append(b.diags, ds...)
				if sr != nil {
					cur = sr.UISrc
					b.res.Overlays[path] = sr.UISrc
					g := &genModule{modID: moduleID(b.rel(path)), path: path, rel: b.rel(path), src: src, split: sr, absDir: filepath.Dir(path)}
					b.gen[g.modID] = g
					b.genList = append(b.genList, g)
				}
			}
		}
		b.scanImports(path, cur, &queue)
	}
	return nil
}

// scanImports records the host imports of a UI module and queues the UI
// modules it imports. It also records how the imported host names are used,
// which build functions need.
func (b *builder) scanImports(path, src string, queue *[]string) {
	prog, errs := tsstrip.Parse(path, src, tsstrip.Options{})
	if prog == nil || len(errs) > 0 {
		return
	}
	rel := b.rel(path)
	// binding says where a local name of this module comes from.
	type binding struct {
		key    string // b.uses key, or "" for a .go import
		goImp  int    // index into b.goImps when key == ""
		remote string
	}
	bindings := map[string]binding{}
	visit := func(spec string, pos ast.Pos, all bool, names []string, locals map[string]string) {
		target := b.opts.Resolve(path, unquote(spec))
		if target == "" {
			return
		}
		target = filepath.Clean(target)
		if strings.EqualFold(filepath.Ext(target), ".go") {
			var named []string
			for _, n := range names {
				if n != "default" {
					named = append(named, n)
				}
			}
			b.goImps = append(b.goImps, GoImport{File: target, Names: named, Namespace: all, From: rel, Line: pos.Line, Col: pos.Col})
			for l, r := range locals {
				bindings[l] = binding{goImp: len(b.goImps) - 1, remote: r}
			}
			return
		}
		if b.hostMod[target] == nil {
			*queue = append(*queue, target)
			return
		}
		u := b.use[target]
		if u == nil {
			u = &usage{names: map[string]loc{}}
			b.use[target] = u
		}
		if all {
			if !u.all {
				u.allLoc = loc{rel: rel, line: pos.Line, col: pos.Col, src: lineText(src, pos.Line)}
			}
			u.all = true
		}
		for _, n := range names {
			if _, ok := u.names[n]; !ok {
				u.names[n] = loc{rel: rel, line: pos.Line, col: pos.Col, src: lineText(src, pos.Line)}
			}
		}
		for l, r := range locals {
			bindings[l] = binding{key: target, remote: r}
		}
		if len(locals) == 0 && len(names) > 0 {
			// A re-export: the name is passed on, which a build function
			// cannot be.
			for _, n := range names {
				b.uses[target] = append(b.uses[target], Use{Site: bake.Site{Name: n, Local: n, Line: pos.Line, Col: pos.Col}, File: rel, Text: lineText(src, pos.Line)})
			}
		}
	}
	for _, s := range prog.Body {
		switch d := s.(type) {
		case *ast.ImportStmt:
			var names []string
			locals := map[string]string{}
			if d.Default != "" {
				names = append(names, "default")
				locals[d.Default] = "default"
			}
			for _, n := range d.Named {
				names = append(names, n.Remote)
				locals[n.Local] = n.Remote
			}
			visit(d.Source, d.Position, d.Namespace != "", names, locals)
		case *ast.ExportStmt:
			if d.ReexportSource == "" {
				continue
			}
			var names []string
			for _, n := range d.ReexportNamed {
				names = append(names, n.Local)
			}
			visit(d.ReexportSource, d.Position, d.StarReexport || d.Namespace != "", names, nil)
		}
	}
	for _, m := range dynImportRe.FindAllStringSubmatch(src, -1) {
		spec := m[1] + m[2]
		visit(spec, ast.Pos{Line: 1, Col: 1}, true, nil, nil)
	}
	locals := map[string]string{}
	for l, bd := range bindings {
		locals[l] = bd.remote
	}
	for _, site := range bake.FindUses(prog, locals) {
		bd := bindings[site.Local]
		u := Use{Site: site, File: rel, Text: lineText(src, site.Line)}
		if bd.key == "" {
			b.goImps[bd.goImp].Uses = append(b.goImps[bd.goImp].Uses, u)
		} else {
			b.uses[bd.key] = append(b.uses[bd.key], u)
		}
	}
	if g := b.gen[moduleID(rel)]; g != nil {
		own := map[string]string{}
		for _, f := range g.split.Fns {
			if f.Build {
				own[f.Name] = f.Name
			}
		}
		for _, site := range bake.FindUses(prog, own) {
			k := "inline:" + g.modID
			b.uses[k] = append(b.uses[k], Use{Site: site, File: rel, Text: lineText(src, site.Line)})
		}
	}
}

// regEntry is a registered export plus how the entry reaches it.
type regEntry struct {
	Registration
	// importFrom is the module the entry imports it from; local is the
	// binding name in that module ("default" for the default export).
	importFrom string
	local      string
}

// collect decides what is registered, checks it, and generates the UI stubs.
func (b *builder) collect() ([]regEntry, error) {
	var regs []regEntry

	paths := make([]string, 0, len(b.use))
	for p := range b.use {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, path := range paths {
		u := b.use[path]
		mod, err := b.an.load(path, nil)
		if err != nil {
			rel := b.rel(path)
			b.diags = append(b.diags, diag.New(rel, 1, 1, "cannot read host module "+rel+": "+err.Error(),
				"fix the syntax error in the host module", "").WithCode(diag.CodeHostBundle))
			continue
		}
		rel := b.rel(path)
		modID := moduleID(rel)
		want := map[string]bool{}
		if u.all {
			for _, e := range mod.Exports {
				if e.Kind != kindValue {
					want[e.Name] = true
				}
			}
		}
		var named []string
		for n := range u.names {
			named = append(named, n)
		}
		sort.Strings(named)
		for _, n := range named {
			e := mod.export(n)
			l := u.names[n]
			switch {
			case e == nil:
				b.diags = append(b.diags, diag.New(l.rel, l.line, l.col,
					rel+" has no export named "+n+" (host module "+modID+")",
					"export "+n+" from the host module, or import a name it exports: "+exportNames(mod), l.src).WithCode(diag.CodeHostImportMissing))
			case e.Kind == kindValue:
				b.diags = append(b.diags, diag.New(l.rel, l.line, l.col,
					n+" from host module "+modID+" is a plain value, which the UI cannot read across the host boundary",
					"export a function that returns the value, or create it with hostSignal() from @krate/app/host if it changes", l.src).WithCode(diag.CodeHostValueExport))
			default:
				want[n] = true
			}
		}
		tc := b.typeChecker(mod)
		var ordered []string
		for n := range want {
			ordered = append(ordered, n)
		}
		sort.Strings(ordered)
		bn := b.buildNames(path)
		if u.all && len(bn) > 0 {
			b.diags = append(b.diags, diag.New(u.allLoc.rel, u.allLoc.line, u.allLoc.col,
				modID+" has build functions, which a namespace import or `export *` hides from the compiler",
				"import the build functions by name and call them directly", u.allLoc.src).WithCode(diag.CodeBakeRef))
		}
		for _, n := range ordered {
			e := mod.export(n)
			if e == nil {
				continue
			}
			if bn[n] {
				if e.Kind != KindFunc {
					b.diags = append(b.diags, diag.New(rel, max(e.Line, 1), 1, "build function "+n+" is a "+string(e.Kind)+", which cannot be baked",
						"a build function is a plain function that returns one value", "").WithCode(diag.CodeBakePlacement))
					continue
				}
				b.addBuild(&buildFn{ID: modID + "." + n, Name: n, Rel: rel, Line: e.Line, path: path, useKey: path})
				continue
			}
			regs = append(regs, regEntry{
				Registration: Registration{ID: modID + "." + n, Kind: string(e.Kind), File: rel, Line: max(e.Line, 1), Module: modID, Export: n},
				importFrom:   "./" + rel,
				local:        n,
			})
			if e.Fn && (e.Kind == KindFunc || e.Kind == KindStream) {
				b.checkParams(rel, mod.Src, mod.Masked, n, e.Off, tc)
			}
		}
		b.res.Overlays[path] = stubSource(modID, mod.Exports, bn)
	}

	for _, g := range b.genList {
		tc := b.typeChecker(&modInfo{Path: g.path, Src: g.src, Masked: mask(g.src), Prog: nil})
		ast0, _ := tsstrip.Parse(g.path, g.src, tsstrip.Options{})
		tc.imported = importedFrom(ast0)
		for _, f := range g.split.Fns {
			if f.Build {
				b.addBuild(&buildFn{ID: f.ID, Name: f.Name, Rel: g.rel, Line: f.Line, gen: g, useKey: "inline:" + g.modID})
				continue
			}
			regs = append(regs, regEntry{
				Registration: Registration{ID: f.ID, Kind: string(f.Kind), File: g.rel, Line: max(f.Line, 1), Module: g.modID, Export: f.Name, Inline: true},
				importFrom:   "krate-host:inline/" + g.modID,
				local:        f.Name,
			})
			b.checkParams(g.rel, g.src, mask(g.src), f.Name, f.Off, tc)
		}
	}
	sort.Slice(regs, func(i, j int) bool { return regs[i].ID < regs[j].ID })
	return regs, nil
}

func exportNames(m *modInfo) string {
	var n []string
	for _, e := range m.Exports {
		if e.Kind != kindValue {
			n = append(n, e.Name)
		}
	}
	if len(n) == 0 {
		return "(none)"
	}
	return strings.Join(n, ", ")
}

func importedFrom(prog *ast.Program) func(string) (string, bool) {
	m := map[string]string{}
	if prog != nil {
		for _, s := range prog.Body {
			if imp, ok := s.(*ast.ImportStmt); ok {
				for _, n := range imp.Named {
					m[n.Local] = unquote(imp.Source)
				}
				if imp.Default != "" {
					m[imp.Default] = unquote(imp.Source)
				}
			}
		}
	}
	return func(name string) (string, bool) {
		s, ok := m[name]
		return s, ok
	}
}

func (b *builder) typeChecker(mod *modInfo) *typeChecker {
	tc := &typeChecker{masked: mod.Masked}
	tc.imported = importedFrom(mod.Prog)
	// Type-only imports are stripped from the AST, so also treat any name the
	// source imports as imported when the text says so.
	text := mod.Masked
	prev := tc.imported
	tc.imported = func(name string) (string, bool) {
		if s, ok := prev(name); ok {
			return s, true
		}
		if regexp.MustCompile(`\bimport\b[^;]*\b` + regexp.QuoteMeta(name) + `\b[^;]*\bfrom\b`).MatchString(text) {
			return "", true
		}
		return "", false
	}
	tc.isClass = func(spec, name string) bool {
		if spec == "" {
			return false
		}
		target := b.opts.Resolve(mod.Path, spec)
		return target != "" && exportsClass(target, name)
	}
	return tc
}

// checkParams reports parameters of the function declared at off that cannot
// cross the boundary.
func (b *builder) checkParams(rel, src, masked, fn string, off int, tc *typeChecker) {
	for _, p := range paramList(masked, off) {
		if p.typ == "" {
			continue
		}
		if why := tc.problem(p.typ); why != "" {
			b.diags = append(b.diags, boundaryDiag(rel, src, fn, p, why))
		}
	}
}
