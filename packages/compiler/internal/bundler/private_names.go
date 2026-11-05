package bundler

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Every module of a page ends up in one namespace: the component table, module
// constants and the hydration script are all keyed by name. Two modules with a
// private `function up()` would otherwise share whichever came last.
// isolatePrivateNames renames such a binding in all modules but one. A default
// export can move too, since importers ask for "the default", not for its name.
// A name only changes when it really collides, so generated code stays readable.
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
	private map[string]bool
	taken   map[string]bool // exported or imported: cannot be renamed here
}

func collectModuleNames(prog *ast.Program) moduleNames {
	n := moduleNames{private: map[string]bool{}, taken: map[string]bool{}}
	for _, stmt := range prog.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok && exp.Default {
			for _, name := range declaredNames(exp.Declaration) {
				n.private[name] = true
			}
			continue
		}
		switch s := stmt.(type) {
		case *ast.ImportStmt:
			n.taken[s.Default], n.taken[s.Namespace] = true, true
			for _, spec := range s.Named {
				if spec.Local != "" {
					n.taken[spec.Local] = true
				} else {
					n.taken[spec.Remote] = true
				}
			}
		case *ast.ExportStmt:
			n.taken[s.Local] = true
			for _, spec := range s.Named {
				n.taken[spec.Local] = true
			}
			for _, name := range declaredNames(s.Declaration) {
				n.taken[name] = true
			}
		default:
			for _, name := range declaredNames(stmt) {
				n.private[name] = true
			}
		}
	}
	delete(n.taken, "")
	for name := range n.private {
		// esbuild helpers such as __publicField are the same in every file, so
		// sharing one copy is correct and smaller.
		if name == "" || n.taken[name] || strings.HasPrefix(name, "__") {
			delete(n.private, name)
		}
	}
	return n
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
	// The entry keeps its names, then modules in bundle order. Exports and
	// imports of every module are claimed first, since they cannot move.
	sort.SliceStable(mods, func(i, j int) bool { return mods[i].IsEntry && !mods[j].IsEntry })
	names := make([]moduleNames, len(mods))
	used := map[string]bool{}
	for i, mod := range mods {
		names[i] = collectModuleNames(mod.Program)
		if ownsExports(mod) {
			// Their export lists and actions are built from source by name.
			for name := range names[i].private {
				names[i].taken[name] = true
			}
			names[i].private = nil
		}
		for name := range names[i].taken {
			used[name] = true
		}
	}
	for i, mod := range mods {
		var clash []string
		for name := range names[i].private {
			if used[name] {
				clash = append(clash, name)
			}
		}
		for name := range names[i].private {
			used[name] = true
		}
		if len(clash) > 0 {
			sort.Strings(clash)
			b.renamePrivate(mod, clash)
		}
	}
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

// renamePrivate renames names in mod. Cached programs are shared between
// pages, so the rename happens on a copy, and the copy is kept for the next page
// that needs the same names renamed.
func (b *Bundler) renamePrivate(mod *Module, names []string) {
	orig := mod.Program
	shared := !mod.IsEntry && !mod.Rewritable
	key := strings.Join(names, ",")
	if shared {
		if prog := b.cache.getRenamed(mod.Path, orig, key); prog != nil {
			mod.Program = prog
			return
		}
		mod.Program = cloneProgram(orig)
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	renameBindings(mod.Program, func(name string) bool { return set[name] }, b.privateSuffix(mod.Path))
	if shared {
		b.cache.putRenamed(mod.Path, orig, key, mod.Program)
	}
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
