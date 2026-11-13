package bundler

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Every module of a page ends up in one namespace: the component table, module
// constants and the hydration script are all keyed by name. Two modules with a
// private `function up()` would otherwise share whichever came last.
// isolatePrivateNames renames such a binding in all modules but the first. A
// default export can move too: importers ask for "the default", and one that
// binds it under the old name follows the rename. A name only changes when two
// modules really declare it, so generated code stays readable.
//
// Named exports move too, by rewriting the modules that import them: the
// importing specifier asks for the new name, and the local binding follows when
// it had the old one. That is only safe when every consumer is visible as an
// import of the module, so a module stays as it is, and the two modules that
// export one name keep a warning (KR7116), when it is re-exported through a
// barrel or `export *`, loaded with import(), or is a
// "use client" or "use server" module, whose consumers read export names from
// source. An ordinary import renames nothing: only a name that the bundle
// imports from two modules is touched.

// PrivateNameMark separates a renamed binding from its module suffix.
const PrivateNameMark = "$m"

// OriginalName undoes the rename, for matching a binding against names written
// in config.
func OriginalName(name string) string {
	base, _, _ := strings.Cut(name, PrivateNameMark)
	return base
}

// moduleNames holds a module's top-level names.
type moduleNames struct {
	private map[string]bool // renameable: private bindings and the default export
	fixed   map[string]bool // named exports, which cannot move
}

func collectModuleNames(prog *ast.Program) moduleNames {
	n := moduleNames{private: map[string]bool{}, fixed: map[string]bool{}}
	for _, stmt := range prog.Body {
		exp, isExport := stmt.(*ast.ExportStmt)
		switch {
		case isExport && exp.Default:
			for _, name := range declaredNames(exp.Declaration) {
				n.private[name] = true
			}
		case isExport:
			n.fixed[exp.Local] = true
			for _, spec := range exp.Named {
				n.fixed[spec.Local] = true
			}
			for _, name := range declaredNames(exp.Declaration) {
				n.fixed[name] = true
			}
		default:
			for _, name := range declaredNames(stmt) {
				n.private[name] = true
			}
		}
	}
	delete(n.fixed, "")
	for name := range n.private {
		// esbuild helpers such as __publicField are the same in every file, so
		// sharing one copy is correct and smaller.
		if name == "" || n.fixed[name] || strings.HasPrefix(name, "__") {
			delete(n.private, name)
		}
	}
	return n
}

func (n moduleNames) declares(name string) bool { return n.private[name] || n.fixed[name] }

// importLocals calls fn with each name a module binds by importing, and the
// module the binding comes from ("" when it is not part of the bundle).
func (b *Bundler) importLocals(mod *Module, fn func(local, target string, isDefault bool)) {
	for _, stmt := range mod.Program.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		target := b.resolveImportForModule(mod.Path, strings.Trim(imp.Source, "\"'`"))
		if imp.Default != "" {
			fn(imp.Default, target, true)
		}
		if imp.Namespace != "" {
			fn(imp.Namespace, "", false)
		}
		for _, spec := range imp.Named {
			local := spec.Local
			if local == "" {
				local = spec.Remote
			}
			fn(local, target, false)
		}
	}
}

func declaredNames(stmt ast.Stmt) []string {
	switch s := stmt.(type) {
	case *ast.FnDecl:
		return []string{s.Name}
	case *ast.ClassDecl:
		return []string{s.Name}
	case *ast.VarStmt:
		var names []string
		for _, d := range s.Decls {
			if d != nil {
				names = append(names, declBindingNames(d)...)
			}
		}
		return names
	}
	return nil
}

func (b *Bundler) isolatePrivateNames() {
	var mods []*Module
	for _, mod := range b.order {
		if mod.Program != nil && !mod.IsExternal {
			mods = append(mods, mod)
		}
	}
	if len(mods) < 2 {
		return
	}
	// The entry keeps its names, then modules in bundle order.
	sort.SliceStable(mods, func(i, j int) bool { return mods[i].IsEntry && !mods[j].IsEntry })

	dynamic := b.dynamicImportTargets(mods)
	plan := b.planExportRenames(mods, dynamic)
	b.warnExportCollisions(mods, plan.shared)

	names := make(map[string]moduleNames, len(mods))
	used := map[string]bool{}
	for _, mod := range mods {
		n := collectModuleNames(mod.Program)
		for old, to := range plan.renames[mod.Path] {
			delete(n.fixed, old)
			n.fixed[to] = true
		}
		if ownsExports(mod) || dynamic[mod.Path] {
			for name := range n.private {
				n.fixed[name] = true
			}
			n.private = nil
		}
		names[mod.Path] = n
		for name := range n.fixed {
			used[name] = true
		}
	}
	// An import is a reference to the module that declares the name. Only one
	// that binds some other name, or a package, occupies it.
	for _, mod := range mods {
		b.importLocals(mod, func(local, target string, _ bool) {
			if n, ok := names[target]; !ok || !n.declares(local) {
				used[local] = true
			}
		})
	}

	renames := make(map[string]map[string]string, len(mods))
	for _, mod := range mods {
		n := names[mod.Path]
		for name := range n.private {
			if !mod.IsEntry && used[name] {
				if renames[mod.Path] == nil {
					renames[mod.Path] = map[string]string{}
				}
				renames[mod.Path][name] = name + b.privateSuffix(mod.Path)
			}
		}
		for name := range n.private {
			used[name] = true
		}
	}

	// A default import under the old name would now find another module's
	// binding, so it follows the rename.
	imports := map[string]map[string]string{}
	for _, mod := range mods {
		b.importLocals(mod, func(local, target string, isDefault bool) {
			if to, ok := renames[target][local]; ok && isDefault {
				if imports[mod.Path] == nil {
					imports[mod.Path] = map[string]string{}
				}
				imports[mod.Path][local] = to
			}
		})
	}
	// Importers of a renamed export ask for its new name.
	named := plan.importerRewrites(b, mods)
	for _, mod := range mods {
		if len(renames[mod.Path]) > 0 || len(imports[mod.Path]) > 0 || len(plan.renames[mod.Path]) > 0 || len(named[mod.Path]) > 0 {
			b.applyPrivateRenames(mod, renames[mod.Path], imports[mod.Path], plan.renames[mod.Path], named[mod.Path])
		}
	}
}

// importKey names one export of one module.
type importKey struct{ target, name string }

// exportPlan is the outcome of planning renames for exported names.
type exportPlan struct {
	// renames maps a module path to its exported bindings that move, and their
	// new names.
	renames map[string]map[string]string
	// shared lists names that stay shared between modules that cannot move.
	shared []exportClash
}

// exportClash is one name that several modules export and the bundle imports
// by that name.
type exportClash struct {
	name  string
	files []string
	// pkgOnly is set when every module involved is inside node_modules: the
	// page's author cannot rename those exports, so there is nothing to warn.
	pkgOnly bool
}

// importerRewrites returns, per importing module, the named imports that must
// follow a renamed export.
func (p exportPlan) importerRewrites(b *Bundler, mods []*Module) map[string]map[importKey]string {
	out := map[string]map[importKey]string{}
	if len(p.renames) == 0 {
		return out
	}
	for _, mod := range mods {
		for _, stmt := range mod.Program.Body {
			imp, ok := stmt.(*ast.ImportStmt)
			if !ok || len(imp.Named) == 0 {
				continue
			}
			target := b.resolveImportForModule(mod.Path, strings.Trim(imp.Source, "\"'`"))
			for _, spec := range imp.Named {
				if to, ok := p.renames[target][spec.Remote]; ok {
					if out[mod.Path] == nil {
						out[mod.Path] = map[importKey]string{}
					}
					out[mod.Path][importKey{target, spec.Remote}] = to
				}
			}
		}
	}
	return out
}

// planExportRenames decides which modules give up an exported name that the
// bundle imports from several modules. One module keeps the name (a module that
// cannot move, else the first); every other movable module gets a private one.
func (b *Bundler) planExportRenames(mods []*Module, dynamic map[string]bool) exportPlan {
	plan := exportPlan{renames: map[string]map[string]string{}}
	owners := map[string]map[string]bool{}
	aliased := map[string]map[string]bool{} // name -> modules exporting it as an alias
	for _, mod := range mods {
		for _, stmt := range mod.Program.Body {
			exp, ok := stmt.(*ast.ExportStmt)
			if !ok || exp.Default || exp.ReexportSource != "" {
				continue
			}
			add := func(name string) {
				if owners[name] == nil {
					owners[name] = map[string]bool{}
				}
				owners[name][mod.Path] = true
			}
			for _, name := range declaredNames(exp.Declaration) {
				add(name)
			}
			for _, spec := range exp.Named {
				if spec.Local == spec.Remote {
					add(spec.Local)
				} else if spec.Remote != "" && spec.Remote != "default" {
					// `export { remoteOnEvent as onEvent }` declares no onEvent,
					// but an importer's onEvent lands in the page scope, where it
					// meets another module's onEvent. The alias owner keeps the
					// name; a declaring owner moves.
					add(spec.Remote)
					if aliased[spec.Remote] == nil {
						aliased[spec.Remote] = map[string]bool{}
					}
					aliased[spec.Remote][mod.Path] = true
				}
			}
		}
	}

	imported := map[string]map[string]bool{}
	pinned := map[string]bool{} // modules whose consumers are not all imports
	for _, mod := range mods {
		locals := map[string]string{} // import local -> target module
		for _, stmt := range mod.Program.Body {
			switch st := stmt.(type) {
			case *ast.ImportStmt:
				// A namespace import does not pin its module: the page builds
				// the namespace object from the export table, keyed by the
				// original names (build.exportBindings).
				target := b.resolveImportForModule(mod.Path, strings.Trim(st.Source, "\"'`"))
				for _, spec := range st.Named {
					if owners[spec.Remote][target] {
						if imported[spec.Remote] == nil {
							imported[spec.Remote] = map[string]bool{}
						}
						imported[spec.Remote][target] = true
					}
					local := spec.Local
					if local == "" {
						local = spec.Remote
					}
					locals[local] = target
				}
				if st.Default != "" {
					locals[st.Default] = target
				}
			case *ast.ExportStmt:
				if st.ReexportSource != "" {
					pinned[b.resolveImportForModule(mod.Path, strings.Trim(st.ReexportSource, "\"'`"))] = true
				}
			}
		}
		// `import { X } from "./m"; export { X }` is a barrel as well.
		for _, stmt := range mod.Program.Body {
			if exp, ok := stmt.(*ast.ExportStmt); ok && exp.ReexportSource == "" {
				for _, spec := range exp.Named {
					if target, ok := locals[spec.Local]; ok {
						pinned[target] = true
					}
				}
			}
		}
	}

	byPath := make(map[string]*Module, len(mods))
	for _, mod := range mods {
		byPath[mod.Path] = mod
	}
	// A package module moves like a project one: its renamed copy is private to
	// this bundle, and every consumer of its exports is a visible import here.
	movable := func(mod *Module) bool {
		return !mod.IsEntry && !pinned[mod.Path] && !dynamic[mod.Path] && !ownsExports(mod)
	}

	// Two modules that both export a name collide in the page's namespace
	// whether or not the page imports it from both: a module calling its own
	// export would otherwise find the other module's declaration.
	clashNames := make([]string, 0, len(owners))
	for name, from := range owners {
		if len(from) > 1 {
			clashNames = append(clashNames, name)
		}
	}
	sort.Strings(clashNames)
	for _, name := range clashNames {
		var group []*Module
		for _, mod := range mods {
			if owners[name][mod.Path] {
				group = append(group, mod)
			}
		}
		// An alias export cannot move (its binding has another name), so it
		// stays where it is like a module that cannot move.
		canMove := func(mod *Module) bool { return movable(mod) && !aliased[name][mod.Path] }
		keeper := group[0]
		for _, mod := range group {
			if !canMove(mod) {
				keeper = mod
				break
			}
		}
		stuck := []*Module{keeper}
		for _, mod := range group {
			if mod == keeper {
				continue
			}
			if !canMove(mod) {
				stuck = append(stuck, mod)
				continue
			}
			if plan.renames[mod.Path] == nil {
				plan.renames[mod.Path] = map[string]string{}
			}
			plan.renames[mod.Path][name] = name + b.privateSuffix(mod.Path)
		}
		if len(stuck) > 1 && len(imported[name]) > 1 {
			var files []string
			for _, mod := range stuck {
				files = append(files, filepath.Base(mod.Path))
			}
			sort.Strings(files)
			pkgOnly := true
			for _, mod := range stuck {
				if !inNodeModules(mod.Path) {
					pkgOnly = false
				}
			}
			plan.shared = append(plan.shared, exportClash{name: name, files: files, pkgOnly: pkgOnly})
		}
	}
	return plan
}

func inNodeModules(path string) bool {
	return strings.Contains(filepath.ToSlash(path), "/node_modules/")
}

var dynamicImportRe = regexp.MustCompile("\\bimport\\(\\s*['\"`]([^'\"`]+)['\"`]\\s*\\)")

// dynamicImportTargets returns the modules loaded with import(). lazy() finds
// its component by the target's default export name.
func (b *Bundler) dynamicImportTargets(mods []*Module) map[string]bool {
	out := map[string]bool{}
	for _, mod := range mods {
		if !strings.Contains(mod.SourceCode, "import(") {
			continue
		}
		for _, m := range dynamicImportRe.FindAllStringSubmatch(mod.SourceCode, -1) {
			out[b.resolveImportForModule(mod.Path, m[1])] = true
		}
	}
	return out
}

func ownsExports(mod *Module) bool {
	if HasUseClientDirective(mod.SourceCode) {
		return true
	}
	_, isAction := isUseServerDirective(mod.Program.Body)
	return isAction
}

// warnExportCollisions reports a name that is imported from several modules
// which all export it and could not be told apart, on the entry module so it
// reaches the page's warnings.
func (b *Bundler) warnExportCollisions(mods []*Module, clashes []exportClash) {
	var entry *Module
	for _, mod := range mods {
		if mod.IsEntry {
			entry = mod
		}
	}
	if entry == nil {
		return
	}
	for _, c := range clashes {
		if c.pkgOnly {
			continue
		}
		d := diag.Diagnostic{
			File: entry.Path, Line: 1, Col: 1,
			Message: "both " + strings.Join(c.files, " and ") + " export " + strconv.Quote(c.name) + ", and this page imports it from each",
			Hint:    "modules on a page share one namespace, so both imports resolve to the same declaration; rename one of the exports",
		}
		d.Code, d.Severity = diag.CodeExportCollision, diag.SeverityWarning
		entry.Diagnostics = append(entry.Diagnostics, d)
	}
}

// applyPrivateRenames renames bindings and default imports in mod. exports are
// the module's own exported bindings that move (the export name follows), and
// named are the named imports that ask for a renamed export. Cached programs are
// shared between pages, so this happens on a copy, and the copy is kept for the
// next page that needs the same renames.
func (b *Bundler) applyPrivateRenames(mod *Module, bindings, imports, exports map[string]string, named map[importKey]string) {
	orig := mod.Program
	shared := !mod.IsEntry && !mod.Rewritable
	key := renameKey(bindings) + "|" + renameKey(imports) + "|" + renameKey(exports) + "|" + namedKey(named)
	if shared {
		if prog := b.cache.getRenamed(mod.Path, orig, key); prog != nil {
			mod.Program = prog
			return
		}
		// Another page's build may be transforming the cached program in
		// place (icons, images) while this copy is taken.
		if b.readLock != nil {
			unlock := b.readLock(orig)
			mod.Program = cloneProgram(orig)
			unlock()
		} else {
			mod.Program = cloneProgram(orig)
		}
	}
	all := make(map[string]string, len(bindings)+len(imports)+len(exports))
	for name := range renamableNames(mod.Program, func(n string) bool { _, ok := bindings[n]; return ok }) {
		all[name] = bindings[name]
	}
	for name := range renamableNames(mod.Program, func(n string) bool { _, ok := exports[n]; return ok }) {
		all[name] = exports[name]
	}
	for from, to := range imports {
		all[from] = to
	}
	// A named import whose local binding is the old name follows the new one.
	type specRewrite struct {
		spec     *ast.NamedImport
		to       string
		rebinds  bool
		oldLocal string
	}
	var rewrites []specRewrite
	for _, stmt := range mod.Program.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok || len(imp.Named) == 0 {
			continue
		}
		target := b.resolveImportForModule(mod.Path, strings.Trim(imp.Source, "\"'`"))
		for i := range imp.Named {
			spec := &imp.Named[i]
			to, ok := named[importKey{target, spec.Remote}]
			if !ok {
				continue
			}
			local := spec.Local
			if local == "" {
				local = spec.Remote
			}
			rewrites = append(rewrites, specRewrite{spec: spec, to: to, rebinds: local == spec.Remote, oldLocal: local})
			if local == spec.Remote {
				all[local] = to
			}
		}
	}
	applyRenames(mod.Program, all)
	for _, stmt := range mod.Program.Body {
		switch st := stmt.(type) {
		case *ast.ImportStmt:
			if to, ok := imports[st.Default]; ok {
				st.Default = to
			}
		case *ast.ExportStmt:
			if st.ReexportSource != "" {
				continue
			}
			// applyRenames leaves export lists alone: `export { Card }` names
			// the new binding and, like a declaration, the new export name.
			for i, spec := range st.Named {
				if to, ok := exports[spec.Local]; ok {
					if spec.Remote == spec.Local {
						st.Named[i].Remote = to
					}
					st.Named[i].Local = to
				}
			}
		}
	}
	for _, r := range rewrites {
		r.spec.Remote = r.to
		if r.rebinds {
			r.spec.Local = r.to
		}
	}
	if shared {
		b.cache.putRenamed(mod.Path, orig, key, mod.Program)
	}
}

func namedKey(named map[importKey]string) string {
	keys := make([]string, 0, len(named))
	for k := range named {
		keys = append(keys, k.target+"#"+k.name)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func renameKey(names map[string]string) string {
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// privateSuffix depends on the path within the project, so builds are
// reproducible across machines.
func (b *Bundler) privateSuffix(path string) string {
	rel := path
	if b.root != "" {
		if r, err := filepath.Rel(b.root, path); err == nil {
			rel = r
		}
	}
	sum := sha256.Sum256([]byte(filepath.ToSlash(rel)))
	return PrivateNameMark + hex.EncodeToString(sum[:3])
}

// cloneProgram deep-copies an AST.
func cloneProgram(prog *ast.Program) *ast.Program {
	out := reflect.New(reflect.TypeOf(prog).Elem())
	cloneValue(out.Elem(), reflect.ValueOf(prog).Elem(), map[uintptr]reflect.Value{})
	return out.Interface().(*ast.Program)
}

func cloneValue(dst, src reflect.Value, seen map[uintptr]reflect.Value) {
	switch src.Kind() {
	case reflect.Ptr:
		if src.IsNil() {
			return
		}
		if prev, ok := seen[src.Pointer()]; ok {
			dst.Set(prev)
			return
		}
		next := reflect.New(src.Type().Elem())
		seen[src.Pointer()] = next
		cloneValue(next.Elem(), src.Elem(), seen)
		dst.Set(next)
	case reflect.Interface:
		if src.IsNil() {
			return
		}
		next := reflect.New(src.Elem().Type()).Elem()
		cloneValue(next, src.Elem(), seen)
		dst.Set(next)
	case reflect.Struct:
		dst.Set(src)
		for i := 0; i < src.NumField(); i++ {
			if dst.Field(i).CanSet() {
				cloneValue(dst.Field(i), src.Field(i), seen)
			}
		}
	case reflect.Slice:
		if src.IsNil() {
			return
		}
		dst.Set(reflect.MakeSlice(src.Type(), src.Len(), src.Len()))
		for i := 0; i < src.Len(); i++ {
			cloneValue(dst.Index(i), src.Index(i), seen)
		}
	case reflect.Map:
		if src.IsNil() {
			return
		}
		dst.Set(reflect.MakeMapWithSize(src.Type(), src.Len()))
		for _, k := range src.MapKeys() {
			v := reflect.New(src.Type().Elem()).Elem()
			cloneValue(v, src.MapIndex(k), seen)
			dst.SetMapIndex(k, v)
		}
	default:
		dst.Set(src)
	}
}
