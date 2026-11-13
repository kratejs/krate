package gobind

import (
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

type itemKind int

const (
	itemFunc itemKind = iota
	itemStream
	itemSignal
)

type param struct {
	name string
	w    *wtype
}

// hostItem is one callable the UI imports.
type hostItem struct {
	name   string
	id     string
	kind   itemKind
	ctx    bool
	params []param
	// result is the value type; nil for void. For streams it is the element.
	result *wtype
	hasErr bool
	// export, when set, is the name the UI and the registry id use instead of
	// name (plugin ops are lowerCamel).
	export string
	// build marks a //krate:build function: the UI gets its baked values.
	build bool
}

// module is one imported .go file (or package directory).
type module struct {
	id         string
	src        string // absolute path of the file or directory
	isDir      bool
	pkg        *userPkg
	names      []string
	items      []*hostItem
	components []string
	imp        Import
	// rename maps a Go name to the op name (Import.Rename).
	rename map[string]string
	// build holds the names marked //krate:build in the module's source.
	build map[string]bool
}

// analyzer collects diagnostics and maps positions.
type analyzer struct {
	root  string
	fset  *token.FileSet
	diags []diag.Diagnostic
	seen  map[string]bool
	lines map[string][]string
}

func newAnalyzer(root string, fset *token.FileSet) *analyzer {
	return &analyzer{root: root, fset: fset, seen: map[string]bool{}, lines: map[string][]string{}}
}

// display returns a path relative to the root, slash-separated.
func (a *analyzer) display(path string) string {
	if rel, err := filepath.Rel(a.root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

func (a *analyzer) inRoot(path string) bool {
	rel, err := filepath.Rel(a.root, path)
	return err == nil && !strings.HasPrefix(rel, "..")
}

func (a *analyzer) sourceLine(path string, line int) string {
	ls, ok := a.lines[path]
	if !ok {
		if b, err := os.ReadFile(path); err == nil {
			ls = strings.Split(string(b), "\n")
		}
		a.lines[path] = ls
	}
	if line >= 1 && line <= len(ls) {
		return strings.TrimRight(ls[line-1], "\r")
	}
	return ""
}

// at builds a diagnostic at an explicit file position.
func (a *analyzer) at(p token.Position, code diag.Code, msg, hint string) diag.Diagnostic {
	if p.Filename == "" {
		return diag.New("", 0, 0, msg, hint, "").WithCode(code)
	}
	return diag.New(a.display(p.Filename), p.Line, p.Column, msg, hint, a.sourceLine(p.Filename, p.Line)).WithCode(code)
}

func (a *analyzer) add(d diag.Diagnostic) {
	key := d.File + ":" + itoa(d.Line) + ":" + itoa(d.Col) + ":" + string(d.Code) + ":" + d.Message
	if a.seen[key] {
		return
	}
	a.seen[key] = true
	a.diags = append(a.diags, d)
}

// report records a diagnostic at pos, or at fallback when pos is not in a
// file of the project. Field positions dedupe by location and code, so a
// type used in several places is reported once.
func (a *analyzer) report(pos, fallback token.Pos, code diag.Code, msg, hint string) {
	p := a.fset.Position(pos)
	if !pos.IsValid() || p.Filename == "" || !a.inRoot(p.Filename) {
		p = a.fset.Position(fallback)
	}
	d := a.at(p, code, msg, hint)
	if pos.IsValid() && pos != fallback {
		key := d.File + ":" + itoa(d.Line) + ":" + itoa(d.Col) + ":" + string(code)
		if a.seen[key] {
			return
		}
		a.seen[key] = true
	}
	a.add(d)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// classify resolves one imported name of m.
func (a *analyzer) classify(m *module, mp *mapper, name string, comps map[string]bool, uiPos func() token.Position) {
	if comps[name] {
		m.components = append(m.components, name)
		return
	}
	importDiag := func(msg, hint string) {
		p := uiPos()
		if p.Filename == "" {
			p = token.Position{Filename: m.src}
		}
		code := diag.CodeGoBindImport
		if strings.Contains(msg, "//krate:build") {
			code = diag.CodeBakePlacement
		}
		a.add(a.at(p, code, msg, hint))
	}
	scope := m.pkg.pkg.Scope()
	obj := scope.Lookup(name)
	if obj == nil || !token.IsExported(name) {
		hint := "export a function or a krate.NewSignal variable with this name from " + a.display(m.src)
		if obj != nil {
			hint = "host imports must be exported: rename " + name + " to start with an upper-case letter"
		}
		if ex := exportedNames(scope); len(ex) > 0 {
			hint += " (exported names: " + strings.Join(ex, ", ") + ")"
		}
		importDiag(name+" is not an exported name of "+a.display(m.src), hint)
		return
	}
	if !m.isDir {
		if dp := a.fset.Position(obj.Pos()); filepath.Clean(dp.Filename) != filepath.Clean(m.src) {
			importDiag(name+" is declared in "+a.display(dp.Filename)+", not in "+a.display(m.src),
				"import it from "+strings.TrimSuffix(filepath.Base(dp.Filename), ".go")+".go instead")
			return
		}
	}
	switch o := obj.(type) {
	case *types.Func:
		it := a.analyzeFunc(m, mp, o)
		if it != nil && m.build[name] {
			switch {
			case it.kind != itemFunc:
				importDiag(name+" is marked //krate:build but returns a channel, which cannot be baked",
					"a build function returns one value (and optionally an error)")
				return
			case it.result == nil:
				importDiag(name+" is marked //krate:build but returns no value",
					"a build function has to return the value to bake")
				return
			}
			it.build = true
		}
		if it != nil {
			m.applyRename(it)
			m.items = append(m.items, it)
		}
	case *types.Var:
		it := a.analyzeSignal(m, mp, o)
		if it != nil {
			m.applyRename(it)
			m.items = append(m.items, it)
		} else if !a.signalType(o.Type()) {
			importDiag(name+" is a variable of type "+typeName(o.Type())+", which cannot be imported",
				"only package-level signals created with krate.NewSignal can be imported; export a function that returns the value instead")
		}
	case *types.TypeName:
		importDiag(name+" is a type, and types cannot be imported at runtime",
			"import only functions and signals; the TypeScript type for "+name+" is generated next to the functions that use it")
	case *types.Const:
		importDiag(name+" is a constant, which cannot be imported",
			"export a function that returns the value, or repeat the constant in TypeScript")
	default:
		importDiag(name+" cannot be imported from a .go file", "export a function or a krate.NewSignal variable")
	}
}

func exportedNames(s *types.Scope) []string {
	var out []string
	for _, n := range s.Names() {
		if token.IsExported(n) {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	if len(out) > 8 {
		out = append(out[:8], "...")
	}
	return out
}

// signalType reports whether t is *krate.Signal[T].
func (a *analyzer) signalType(t types.Type) bool {
	p, ok := types.Unalias(t).(*types.Pointer)
	if !ok {
		return false
	}
	n, ok := types.Unalias(p.Elem()).(*types.Named)
	return ok && isNamedFrom(n, krateImportPath, "Signal") && n.TypeArgs().Len() == 1
}

func (a *analyzer) analyzeSignal(m *module, mp *mapper, v *types.Var) *hostItem {
	if !a.signalType(v.Type()) {
		return nil
	}
	p := types.Unalias(v.Type()).(*types.Pointer)
	n := types.Unalias(p.Elem()).(*types.Named)
	w := mp.Map(n.TypeArgs().At(0), &site{label: v.Name() + " signal value", pos: v.Pos()})
	if w == nil {
		return nil
	}
	return &hostItem{name: v.Name(), id: m.id + "." + v.Name(), kind: itemSignal, result: w}
}

func (it *hostItem) exportName() string {
	if it.export != "" {
		return it.export
	}
	return it.name
}

// applyRename gives the item the op name an Import renamed it to.
func (m *module) applyRename(it *hostItem) {
	if to, ok := m.rename[it.name]; ok && it != nil {
		it.export = to
		it.id = m.id + "." + to
	}
}

func isErrorType(t types.Type) bool {
	return types.Identical(t, types.Universe.Lookup("error").Type())
}

func (a *analyzer) analyzeFunc(m *module, mp *mapper, fn *types.Func) *hostItem {
	sig := fn.Type().(*types.Signature)
	name := fn.Name()
	bad := func(pos token.Pos, msg, hint string) *hostItem {
		a.report(pos, fn.Pos(), diag.CodeGoBindSignature, name+": "+msg, hint)
		return nil
	}
	if sig.TypeParams().Len() > 0 {
		return bad(fn.Pos(), "generic functions cannot be host calls", "make a concrete wrapper function and import that")
	}
	if sig.Variadic() {
		last := sig.Params().At(sig.Params().Len() - 1)
		return bad(last.Pos(), "variadic parameters are not supported", "take a slice parameter instead")
	}
	it := &hostItem{name: name, id: m.id + "." + name, kind: itemFunc}
	ok := true
	ps := sig.Params()
	for i := 0; i < ps.Len(); i++ {
		p := ps.At(i)
		if isNamedFrom(p.Type(), krateImportPath, "Ctx") {
			if i != 0 {
				bad(p.Pos(), "krate.Ctx must be the first parameter", "move the ctx parameter to the front")
				ok = false
			} else {
				it.ctx = true
			}
			continue
		}
		if i == 0 && isNamedFrom(p.Type(), "context", "Context") {
			bad(p.Pos(), "the first parameter is context.Context", "use krate.Ctx, which embeds context.Context and also reports the calling window")
			ok = false
			continue
		}
		pname := p.Name()
		label := name + " parameter " + quoteOr(pname, itoa(i))
		w := mp.Map(p.Type(), &site{label: label, pos: p.Pos()})
		if w == nil {
			ok = false
			continue
		}
		it.params = append(it.params, param{name: pname, w: w})
	}

	rs := sig.Results()
	elemOf := func(t types.Type, pos token.Pos) bool {
		ch := types.Unalias(t).(*types.Chan)
		if ch.Dir() == types.SendOnly {
			bad(pos, "a send-only channel cannot be read", "return <-chan T")
			return false
		}
		w := mp.Map(ch.Elem(), &site{label: name + " stream element", pos: pos})
		if w == nil {
			return false
		}
		it.kind, it.result = itemStream, w
		return true
	}
	resPos := func(i int) token.Pos {
		if p := rs.At(i).Pos(); p.IsValid() {
			return p
		}
		return fn.Pos()
	}
	isChan := func(i int) bool { _, c := types.Unalias(rs.At(i).Type()).(*types.Chan); return c }
	switch rs.Len() {
	case 0:
	case 1:
		switch {
		case isErrorType(rs.At(0).Type()):
			it.hasErr = true
		case isChan(0):
			ok = elemOf(rs.At(0).Type(), resPos(0)) && ok
		default:
			it.result = mp.Map(rs.At(0).Type(), &site{label: name + " result", pos: resPos(0)})
			ok = ok && it.result != nil
		}
	case 2:
		if !isErrorType(rs.At(1).Type()) {
			bad(resPos(1), "the second result must be error", "return (T, error), error, T or (<-chan T, error)")
			return nil
		}
		it.hasErr = true
		if isChan(0) {
			ok = elemOf(rs.At(0).Type(), resPos(0)) && ok
		} else {
			it.result = mp.Map(rs.At(0).Type(), &site{label: name + " result", pos: resPos(0)})
			ok = ok && it.result != nil
		}
	default:
		return bad(fn.Pos(), "returns "+itoa(rs.Len())+" values; at most a value and an error are supported",
			"return one struct that holds the values, plus an error")
	}
	if !ok {
		return nil
	}
	return it
}

func quoteOr(name, fallback string) string {
	if name == "" || name == "_" {
		return fallback
	}
	return `"` + name + `"`
}
