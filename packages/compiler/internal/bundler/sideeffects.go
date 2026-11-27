package bundler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

// A package that declares `"sideEffects": false` (or a list of the files that
// do have side effects) in its package.json promises that its other modules
// do nothing when imported except define their exports. A stylesheet such a
// module imports is a side effect of that module only: it ships when the
// module is used, not whenever the package is. So importing one component from
// a barrel (`export * from './card/card'` for every component) keeps only that
// component's stylesheet.

// pkgSideEffects is the sideEffects field of one package.json.
type pkgSideEffects struct {
	dir   string // the package directory
	mtime time.Time
	size  int64
	// all is true when every module may have side effects (the field is
	// absent, true, or not understood).
	all      bool
	patterns []*regexp.Regexp // modules matching one have side effects
}

var sideEffectsCache sync.Map // package.json path -> *pkgSideEffects

// readSideEffects reads the sideEffects field of the package.json at p.
func readSideEffects(p string, st resolver.Stat) *pkgSideEffects {
	if v, ok := sideEffectsCache.Load(p); ok {
		e := v.(*pkgSideEffects)
		if e.mtime.Equal(st.Mtime) && e.size == st.Size {
			return e
		}
	}
	e := &pkgSideEffects{dir: filepath.Dir(p), mtime: st.Mtime, size: st.Size, all: true}
	var pkg struct {
		SideEffects json.RawMessage `json:"sideEffects"`
	}
	if data, err := os.ReadFile(p); err == nil && json.Unmarshal(data, &pkg) == nil && len(pkg.SideEffects) > 0 {
		var flag bool
		var globs []string
		switch {
		case json.Unmarshal(pkg.SideEffects, &flag) == nil:
			e.all = flag
		case json.Unmarshal(pkg.SideEffects, &globs) == nil:
			e.all = false
			for _, g := range globs {
				if re := sideEffectsGlob(g); re != nil {
					e.patterns = append(e.patterns, re)
				} else {
					e.all = true
				}
			}
		}
	}
	sideEffectsCache.Store(p, e)
	return e
}

// sideEffectsGlob compiles one sideEffects pattern the way bundlers read it: a
// pattern without a slash matches the file name anywhere in the package, any
// other is relative to the package directory; `*` stays within a path segment
// and `**` crosses segments.
func sideEffectsGlob(g string) *regexp.Regexp {
	g = strings.TrimPrefix(strings.TrimSpace(g), "./")
	if g == "" {
		return nil
	}
	if !strings.Contains(g, "/") {
		g = "**/" + g
	}
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(g); i++ {
		switch c := g[i]; c {
		case '*':
			if i+1 < len(g) && g[i+1] == '*' {
				i++
				if i+1 < len(g) && g[i+1] == '/' {
					i++
					sb.WriteString("(?:.*/)?")
				} else {
					sb.WriteString(".*")
				}
			} else {
				sb.WriteString("[^/]*")
			}
		case '?':
			sb.WriteString("[^/]")
		default:
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	sb.WriteString("$")
	re, err := regexp.Compile(sb.String())
	if err != nil {
		return nil
	}
	return re
}

// sideEffectFree reports whether the module at abs is declared free of side
// effects by its package, and the package.json that says so ("" when none).
func sideEffectFree(abs string) (bool, string) {
	for dir := filepath.Dir(abs); ; {
		p := filepath.Join(dir, "package.json")
		if st := resolver.StatPath(p); st.Exists && !st.IsDir {
			e := readSideEffects(p, st)
			if e.all {
				return false, p
			}
			rel, err := filepath.Rel(e.dir, abs)
			if err != nil {
				return false, p
			}
			rel = filepath.ToSlash(rel)
			for _, re := range e.patterns {
				if re.MatchString(rel) {
					return false, p
				}
			}
			return true, p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false, ""
		}
		dir = parent
	}
}

// moduleExports is what one module exports and re-exports.
type moduleExports struct {
	local map[string]bool // names the module defines itself
	// unknown is set when a local export's name could not be read: the
	// module is then taken to define every name.
	unknown bool
	// reexports are `export ... from` statements in source order.
	reexports []*ast.ExportStmt
}

func readModuleExports(prog *ast.Program) *moduleExports {
	me := &moduleExports{local: map[string]bool{}}
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.ExportStmt:
			if s.ReexportSource != "" {
				me.reexports = append(me.reexports, s)
				continue
			}
			if s.Default {
				me.local["default"] = true
			}
			for _, n := range s.Named {
				if n.Remote != "" {
					me.local[n.Remote] = true
				} else {
					me.local[n.Local] = true
				}
			}
			switch d := s.Declaration.(type) {
			case nil:
			case *ast.FnDecl:
				me.local[d.Name] = true
			case *ast.ClassDecl:
				me.local[d.Name] = true
			case *ast.VarStmt:
				for _, v := range d.Decls {
					if v.Name != "" {
						me.local[v.Name] = true
					}
					for _, n := range v.Names {
						me.local[n] = true
					}
					if v.RestName != "" {
						me.local[v.RestName] = true
					}
					if v.Name == "" && len(v.Names) == 0 {
						me.unknown = true
					}
				}
			default:
				if !s.Default {
					me.unknown = true
				}
			}
		case *ast.FnDecl:
			if s.Export {
				me.local[s.Name] = true
				if s.Default {
					me.local["default"] = true
				}
			}
		case *ast.ClassDecl:
			// A class is exported through an ExportStmt; nothing to record.
		}
	}
	return me
}

// pruneSideEffectFreeCSS drops the stylesheets that only modules of
// side-effect-free packages import when none of those modules is reachable
// from the names the page actually imports. The stylesheets that stay keep
// their place in import order.
func (b *Bundler) pruneSideEffectFreeCSS() {
	if len(b.css) == 0 {
		return
	}
	byPath := make(map[string]*Module, len(b.order))
	free := make(map[*Module]bool)
	anyFree := false
	for _, m := range b.order {
		if m.IsExternal {
			continue
		}
		abs := m.Path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(b.root, abs)
		}
		abs = filepath.Clean(abs)
		byPath[abs] = m
		byPath[m.Path] = m
		if m.IsCSS || m.IsExternal || m.Program == nil {
			continue
		}
		ok, pkgJSON := sideEffectFree(abs)
		if pkgJSON != "" && b.cssDeps != nil {
			// Editing the sideEffects field changes which sheets ship.
			b.cssDeps[pkgJSON] = true
		}
		if ok {
			free[m] = true
			anyFree = true
		}
	}
	if !anyFree {
		return
	}
	unlock := b.readLockAll(b.order)
	defer unlock()

	target := func(from *Module, spec string) *Module {
		spec = strings.Trim(spec, "\"'")
		if _, _, ok := splitImportQuery(spec); ok {
			return nil
		}
		r := b.resolveImportForModule(from.Path, spec)
		if r == "" {
			return nil
		}
		return byPath[filepath.Clean(r)]
	}
	exports := make(map[*Module]*moduleExports)
	exportsOf := func(m *Module) *moduleExports {
		if e, ok := exports[m]; ok {
			return e
		}
		e := readModuleExports(m.Program)
		exports[m] = e
		return e
	}

	evaluated := make(map[*Module]bool)
	provides := make(map[*Module]map[string]bool) // result per demanded name
	var evaluate func(m *Module)
	var demand func(m *Module, name string) bool
	// evaluate marks a module as run, which runs what it imports: a module
	// that may have side effects runs whole, a side-effect-free one only
	// when a name imported from it is defined in it or reached through it.
	evaluate = func(m *Module) {
		if evaluated[m] {
			return
		}
		evaluated[m] = true
		if m.Program == nil {
			return
		}
		for _, stmt := range m.Program.Body {
			switch s := stmt.(type) {
			case *ast.ImportStmt:
				t := target(m, s.Source)
				if t == nil {
					continue
				}
				if !free[t] {
					evaluate(t)
				}
				if s.Default != "" {
					demand(t, "default")
				}
				if s.Namespace != "" {
					demand(t, "*")
				}
				for _, n := range s.Named {
					demand(t, n.Remote)
				}
			case *ast.ExportStmt:
				if s.ReexportSource == "" {
					continue
				}
				if t := target(m, s.ReexportSource); t != nil && !free[t] {
					evaluate(t)
				}
			}
		}
	}
	// demand records that name (or every name, "*") is imported from m and
	// reports whether m provides it; a module that provides it runs.
	demand = func(m *Module, name string) bool {
		if name == "" {
			name = "default"
		}
		got := provides[m]
		if got == nil {
			got = map[string]bool{}
			provides[m] = got
		}
		if v, ok := got[name]; ok {
			return v
		}
		got[name] = false // a cycle back here provides nothing new
		found := !free[m] || m.Program == nil
		if m.Program != nil {
			e := exportsOf(m)
			if name == "*" || e.unknown || e.local[name] {
				found = true
			}
			for _, r := range e.reexports {
				t := target(m, r.ReexportSource)
				if t == nil {
					continue
				}
				switch {
				case r.Namespace != "":
					if name == "*" || name == r.Namespace {
						found = demand(t, "*") || found
					}
				case len(r.ReexportNamed) > 0:
					for _, n := range r.ReexportNamed {
						remote := n.Remote
						if remote == "" {
							remote = n.Local
						}
						if name == "*" || name == remote {
							found = demand(t, n.Local) || found
						}
					}
				case name == "*" || name != "default":
					found = demand(t, name) || found
				}
			}
		}
		got[name] = found
		if found {
			evaluate(m)
		}
		return found
	}
	for _, m := range b.order {
		if m.IsEntry {
			evaluate(m)
			demand(m, "*")
		}
	}
	// The modules of a page share one scope once bundled, so code can name
	// an export of a module in the graph without importing it. A module whose
	// export is named by code that runs runs too.
	refs := make(map[*Module]map[string]bool)
	for changed := true; changed; {
		changed = false
		named := make(map[string]bool)
		for _, m := range b.order {
			if !evaluated[m] || m.Program == nil {
				continue
			}
			r, ok := refs[m]
			if !ok {
				r = referencedNames(m.Program)
				refs[m] = r
			}
			for n := range r {
				named[n] = true
			}
		}
		for _, m := range b.order {
			if !free[m] || evaluated[m] {
				continue
			}
			for n := range exportsOf(m).local {
				if named[n] && demand(m, n) {
					changed = true
					break
				}
			}
		}
	}

	// A stylesheet stays when a module that ran imports it, or when no module
	// of the graph imports it (a stylesheet the page reached another way).
	importedBy := make(map[string]bool)
	kept := make(map[string]bool)
	for _, m := range b.order {
		if m.Program == nil {
			continue
		}
		for _, stmt := range m.Program.Body {
			s, ok := stmt.(*ast.ImportStmt)
			if !ok {
				continue
			}
			t := target(m, s.Source)
			if t == nil || !t.IsCSS {
				continue
			}
			abs := filepath.Clean(t.Path)
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(b.root, abs)
			}
			importedBy[abs] = true
			if evaluated[m] {
				kept[abs] = true
			}
		}
	}
	css, src := b.css[:0], b.cssSrc[:0]
	for i, text := range b.css {
		if s := b.cssSrc[i]; !importedBy[s] || kept[s] {
			css = append(css, text)
			src = append(src, s)
		}
	}
	b.css, b.cssSrc = css, src
}

// referencedNames returns the identifiers and JSX tag names a program uses
// (for `<Tabs.List>`, Tabs).
func referencedNames(prog *ast.Program) map[string]bool {
	out := make(map[string]bool)
	visitStmts(prog.Body, false, func(n any, _ bool) bool {
		switch x := n.(type) {
		case *ast.Identifier:
			out[x.Name] = true
		case *ast.JSXElement:
			if x.Opening != nil {
				name, _, _ := strings.Cut(x.Opening.Name, ".")
				out[name] = true
			}
		}
		return true
	})
	return out
}
