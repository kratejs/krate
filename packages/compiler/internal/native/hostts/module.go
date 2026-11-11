package hostts

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// Kind says how a host export is called from the UI.
type Kind string

// Export kinds.
const (
	// KindFunc is a function called as an async function.
	KindFunc Kind = "func"
	// KindStream is a (async) generator function read as an async iterable.
	KindStream Kind = "stream"
	// KindSignal is a value made by hostSignal() from @krate/app/host.
	KindSignal Kind = "signal"
	// KindAuto is decided at run time by the host entry (a re-export or a
	// value of unknown origin). The UI stub treats it as a function.
	KindAuto Kind = "auto"
	// kindValue is a plain value; the UI cannot use it.
	kindValue Kind = "value"
)

const (
	hostSignalPkg  = "@krate/app/host"
	hostSignalName = "hostSignal"
)

// export is one export of a host module.
type export struct {
	Name string // exported name; "default" for the default export
	Kind Kind
	// Off is the offset of the declaration in the module source, 0 for a
	// re-export. Line is its 1-based line.
	Off, Line int
	// Fn is set for a function the parameters of which can be checked.
	Fn bool
}

// modInfo is a parsed TypeScript module.
type modInfo struct {
	Path    string
	Src     string
	Masked  string
	Prog    *ast.Program
	Exports []*export
	// direct and ns name the local bindings of hostSignal and of the
	// @krate/app/host namespace.
	direct, ns map[string]bool
}

func (m *modInfo) export(name string) *export {
	for _, e := range m.Exports {
		if e.Name == name {
			return e
		}
	}
	return nil
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.Expr
	}
}

// local is a top-level binding of a module.
type local struct {
	kind Kind
	off  int
	fn   bool
}

type analyzer struct {
	resolve func(importer, spec string) string
	mods    map[string]*modInfo
}

func unquote(s string) string { return strings.Trim(s, "\"'`") }

// load parses and analyzes the module at path.
func (a *analyzer) load(path string, stack map[string]bool) (*modInfo, error) {
	if m, ok := a.mods[path]; ok {
		return m, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	src := string(data)
	prog, errs := tsstrip.Parse(path, src, tsstrip.Options{})
	if prog == nil || len(errs) > 0 {
		msg := "syntax error"
		if len(errs) > 0 {
			msg = errs[0].Error()
		}
		return nil, fmt.Errorf("%s: %s", path, msg)
	}
	m := &modInfo{Path: path, Src: src, Masked: mask(src), Prog: prog}
	if stack == nil {
		stack = map[string]bool{}
	}
	stack[path] = true
	a.collectExports(m, stack)
	delete(stack, path)
	a.mods[path] = m
	return m, nil
}

// hostSignalCall reports whether e is a call of hostSignal imported from
// @krate/app/host.
func hostSignalCall(e ast.Expr, direct, ns map[string]bool) bool {
	call, ok := unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	switch c := call.Callee.(type) {
	case *ast.Identifier:
		return direct[c.Name]
	case *ast.MemberExpr:
		if id, ok := c.Object.(*ast.Identifier); ok && ns[id.Name] {
			if p, ok := c.Property.(*ast.Identifier); ok {
				return p.Name == hostSignalName
			}
		}
	}
	return false
}

func exprKind(e ast.Expr, direct, ns map[string]bool) (Kind, bool) {
	e = unparen(e)
	switch x := e.(type) {
	case *ast.ArrowFn:
		if x.Generator {
			return KindStream, true
		}
		return KindFunc, true
	case *ast.Literal, *ast.ObjectExpr, *ast.ArrayExpr, *ast.TemplateExpr, *ast.ClassExpr:
		return kindValue, false
	case *ast.CallExpr:
		if hostSignalCall(x, direct, ns) {
			return KindSignal, false
		}
	}
	return KindAuto, false
}

func fnKind(f *ast.FnDecl) Kind {
	if f.Generator {
		return KindStream
	}
	return KindFunc
}

func (a *analyzer) collectExports(m *modInfo, stack map[string]bool) {
	direct, ns := map[string]bool{}, map[string]bool{}
	m.direct, m.ns = direct, ns
	for _, s := range m.Prog.Body {
		imp, ok := s.(*ast.ImportStmt)
		if !ok || unquote(imp.Source) != hostSignalPkg {
			continue
		}
		for _, n := range imp.Named {
			if n.Remote == hostSignalName {
				direct[n.Local] = true
			}
		}
		if imp.Namespace != "" {
			ns[imp.Namespace] = true
		}
	}

	locals := map[string]local{}
	for _, s := range m.Prog.Body {
		if exp, ok := s.(*ast.ExportStmt); ok && exp.Declaration != nil {
			s = exp.Declaration
		}
		switch d := s.(type) {
		case *ast.FnDecl:
			locals[d.Name] = local{fnKind(d), d.Position.Offset, true}
		case *ast.VarStmt:
			for _, v := range d.Decls {
				if v == nil || v.Name == "" || v.IsDestructuring {
					continue
				}
				k, fn := exprKind(v.Init, direct, ns)
				off := findWord(m.Masked, v.Name, d.Position.Offset)
				if off < 0 {
					off = d.Position.Offset
				}
				locals[v.Name] = local{k, off, fn}
			}
		case *ast.ClassDecl:
			locals[d.Name] = local{kindValue, d.Position.Offset, false}
		}
	}

	seen := map[string]bool{}
	add := func(name string, k Kind, off int, fn bool) {
		if seen[name] {
			return
		}
		seen[name] = true
		line := 0
		if off > 0 {
			line, _ = lineCol(m.Src, off)
		}
		m.Exports = append(m.Exports, &export{Name: name, Kind: k, Off: off, Line: line, Fn: fn})
	}

	for _, s := range m.Prog.Body {
		switch d := s.(type) {
		case *ast.FnDecl:
			if !d.Export {
				continue
			}
			if d.Tier != "" && d.Tier != "host" {
				continue
			}
			name := d.Name
			if d.Default {
				name = "default"
			}
			add(name, fnKind(d), d.Position.Offset, true)
		case *ast.ExportStmt:
			a.exportStmt(m, d, locals, add, stack)
		}
	}
	sort.Slice(m.Exports, func(i, j int) bool { return m.Exports[i].Name < m.Exports[j].Name })
}

func (a *analyzer) exportStmt(m *modInfo, exp *ast.ExportStmt, locals map[string]local, add func(string, Kind, int, bool), stack map[string]bool) {
	direct, ns := m.direct, m.ns
	switch {
	case exp.ReexportSource != "":
		src := unquote(exp.ReexportSource)
		if exp.StarReexport && exp.Namespace == "" {
			if p := a.resolve(m.Path, src); p != "" && !stack[p] && isTSPath(p) {
				if sub, err := a.load(p, stack); err == nil {
					for _, e := range sub.Exports {
						if e.Name != "default" {
							add(e.Name, e.Kind, 0, false)
						}
					}
				}
			}
			return
		}
		if exp.Namespace != "" {
			add(exp.Namespace, kindValue, 0, false)
			return
		}
		for _, n := range exp.ReexportNamed {
			add(n.Remote, KindAuto, 0, false)
		}
	case exp.Declaration != nil:
		switch d := exp.Declaration.(type) {
		case *ast.FnDecl:
			if d.Tier != "" && d.Tier != "host" {
				return
			}
			name := d.Name
			if exp.Default || d.Default {
				name = "default"
			}
			add(name, fnKind(d), d.Position.Offset, true)
		case *ast.VarStmt:
			for _, v := range d.Decls {
				if v == nil || v.Name == "" || v.IsDestructuring {
					continue
				}
				if v.Tier != "" && v.Tier != "host" {
					continue
				}
				l := locals[v.Name]
				add(v.Name, l.kind, l.off, l.fn)
			}
		case *ast.ClassDecl:
			name := d.Name
			if exp.Default {
				name = "default"
			}
			add(name, kindValue, d.Position.Offset, false)
		case *ast.ExprStmt:
			if exp.Default {
				k, fn := exprKind(d.Expression, direct, ns)
				if id, ok := unparen(d.Expression).(*ast.Identifier); ok {
					if l, ok := locals[id.Name]; ok {
						k, fn = l.kind, l.fn
						add("default", k, l.off, fn)
						return
					}
				}
				add("default", k, d.Position.Offset, fn)
			}
		}
	default:
		for _, n := range exp.Named {
			l, ok := locals[n.Local]
			if !ok {
				add(n.Remote, KindAuto, 0, false)
				continue
			}
			add(n.Remote, l.kind, l.off, l.fn)
		}
		if exp.Default && exp.Local != "" {
			l, ok := locals[exp.Local]
			if !ok {
				add("default", KindAuto, 0, false)
			} else {
				add("default", l.kind, l.off, l.fn)
			}
		}
	}
}

func isTSPath(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".mts":
		return true
	}
	return false
}
