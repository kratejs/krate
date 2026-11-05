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
// Named exports cannot move without rewriting every importer, so two modules
// that export one name and are both imported by it get a warning instead.

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
	b.warnExportCollisions(mods)
	// The entry keeps its names, then modules in bundle order.
	sort.SliceStable(mods, func(i, j int) bool { return mods[i].IsEntry && !mods[j].IsEntry })

	dynamic := b.dynamicImportTargets(mods)
	names := make(map[string]moduleNames, len(mods))
	used := map[string]bool{}
	for _, mod := range mods {
		n := collectModuleNames(mod.Program)
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
	for _, mod := range mods {
		if len(renames[mod.Path]) > 0 || len(imports[mod.Path]) > 0 {
			b.applyPrivateRenames(mod, renames[mod.Path], imports[mod.Path])
		}
	}
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

// warnExportCollisions reports a name that is imported from two modules which
// both export it, on the entry module so it reaches the page's warnings.
func (b *Bundler) warnExportCollisions(mods []*Module) {
	owners := map[string]map[string]bool{}
	for _, mod := range mods {
		// A package's own copies are out of the project's hands.
		if strings.Contains(filepath.ToSlash(mod.Path), "/node_modules/") {
			continue
		}
		for _, stmt := range mod.Program.Body {
			if exp, ok := stmt.(*ast.ExportStmt); ok && !exp.Default {
				for _, name := range declaredNames(exp.Declaration) {
					if owners[name] == nil {
						owners[name] = map[string]bool{}
					}
					owners[name][mod.Path] = true
				}
			}
		}
	}
	imported := map[string]map[string]bool{}
	var entry *Module
	for _, mod := range mods {
		if mod.IsEntry {
			entry = mod
		}
		for _, stmt := range mod.Program.Body {
			imp, ok := stmt.(*ast.ImportStmt)
			if !ok {
				continue
			}
			src := b.resolveImportForModule(mod.Path, strings.Trim(imp.Source, "\"'`"))
			for _, spec := range imp.Named {
				if name := spec.Remote; owners[name][src] {
					if imported[name] == nil {
						imported[name] = map[string]bool{}
					}
					imported[name][src] = true
				}
			}
		}
	}
	if entry == nil {
		return
	}
	var clashes []string
	for name, from := range imported {
		if len(from) > 1 {
			clashes = append(clashes, name)
		}
	}
	sort.Strings(clashes)
	for _, name := range clashes {
		var files []string
		for path := range imported[name] {
			files = append(files, filepath.Base(path))
		}
		sort.Strings(files)
		d := diag.Diagnostic{
			File: entry.Path, Line: 1, Col: 1,
			Message: "both " + strings.Join(files, " and ") + " export " + strconv.Quote(name) + ", and this page imports it from each",
			Hint:    "modules on a page share one namespace, so both imports resolve to the same declaration; rename one of the exports",
		}
		d.Code, d.Severity = diag.CodeExportCollision, diag.SeverityWarning
		entry.Diagnostics = append(entry.Diagnostics, d)
	}
}

// applyPrivateRenames renames bindings and default imports in mod. Cached
// programs are shared between pages, so this happens on a copy, and the copy is
// kept for the next page that needs the same renames.
func (b *Bundler) applyPrivateRenames(mod *Module, bindings, imports map[string]string) {
	orig := mod.Program
	shared := !mod.IsEntry && !mod.Rewritable
	key := renameKey(bindings) + "|" + renameKey(imports)
	if shared {
		if prog := b.cache.getRenamed(mod.Path, orig, key); prog != nil {
			mod.Program = prog
			return
		}
		mod.Program = cloneProgram(orig)
	}
	all := make(map[string]string, len(bindings)+len(imports))
	for name := range renamableNames(mod.Program, func(n string) bool { _, ok := bindings[n]; return ok }) {
		all[name] = bindings[name]
	}
	for from, to := range imports {
		all[from] = to
	}
	applyRenames(mod.Program, all)
	for _, stmt := range mod.Program.Body {
		if imp, ok := stmt.(*ast.ImportStmt); ok {
			if to, ok := imports[imp.Default]; ok {
				imp.Default = to
			}
		}
	}
	if shared {
		b.cache.putRenamed(mod.Path, orig, key, mod.Program)
	}
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
