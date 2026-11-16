package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
	"github.com/kratejs/krate/packages/compiler/internal/pluginapi"
)

// The Go reader of literal metadata: a page whose metadata slice (see
// sliceModule) is pure, that is its statements only declare values the
// reader can compute, and its imports reach only modules that are pure too,
// gives the same `metadata` and `viewport` the slice would evaluate to, with
// no observable effect skipped. Supported values: strings, decimal numbers,
// booleans, null, undefined, arrays, objects (with spreads of such objects),
// template literals and `+` over strings, and `const` bindings of such values,
// local or imported by name. Anything else leaves the page to the engine.

// litRuntimeSpec is the one package a pure module may import: every metadata
// entry imports it already, so importing it again runs nothing new.
const litRuntimeSpec = "@krate/runtime/server"

// litValue is a computed JavaScript value.
type litValue struct {
	kind  byte // 's' string, 'n' number, 'b' boolean, 'z' null, 'u' undefined, 'a' array, 'o' object
	s     string
	items []litValue
	keys  []string
}

// appendTagged writes v in the tagged JSON form of metadataDecoderJS.
func (v litValue) appendTagged(sb *strings.Builder) {
	switch v.kind {
	case 's':
		q, _ := json.Marshal(v.s)
		sb.Write(q)
	case 'n', 'b':
		sb.WriteString(v.s)
	case 'z':
		sb.WriteString("null")
	case 'u':
		sb.WriteString(`{"u":1}`)
	case 'a':
		sb.WriteString(`{"a":[`)
		for i, it := range v.items {
			if i > 0 {
				sb.WriteByte(',')
			}
			it.appendTagged(sb)
		}
		sb.WriteString("]}")
	case 'o':
		sb.WriteString(`{"o":[`)
		for i, it := range v.items {
			if i > 0 {
				sb.WriteByte(',')
			}
			k, _ := json.Marshal(v.keys[i])
			sb.WriteByte('[')
			sb.Write(k)
			sb.WriteByte(',')
			it.appendTagged(sb)
			sb.WriteByte(']')
		}
		sb.WriteString("]}")
	}
}

// litModule is a parsed module and what the reader knows about it.
type litModule struct {
	path    string
	stmts   []ast.Stmt
	consts  map[string]ast.Expr // single-name const bindings
	imports map[string]litImport
	exports map[string]string // exported name -> local binding
	pure    int8              // 0 unknown, 1 pure, -1 not, 2 being checked
	values  map[string]*litMemo
}

type litImport struct {
	spec   string
	remote string // "" for a default or namespace import
}

type litMemo struct {
	busy bool
	done bool
	v    litValue
	ok   bool
}

// litCache memoizes parsed modules for one build.
type litCache struct {
	mu   sync.Mutex
	mods map[string]*litModule
	page map[string]litPage
}

type litPage struct {
	enc string
	ok  bool
}

func newLitCache() *litCache {
	return &litCache{mods: map[string]*litModule{}, page: map[string]litPage{}}
}

var (
	litNumberRe = regexp.MustCompile(`^(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)
	litIndexRe  = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,14})$`)
)

// literalPageModule returns the tagged module value ({metadata, viewport})
// of a page whose metadata the reader resolves.
func (b *Builder) literalPageModule(abs string, bnd *bundler.Bundler) (string, bool) {
	c := b.metadataStatic().lits
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.page[abs]; ok {
		return p.enc, p.ok
	}
	enc, ok := b.readLiteralPage(c, abs, bnd)
	c.page[abs] = litPage{enc, ok}
	return enc, ok
}

func (b *Builder) readLiteralPage(c *litCache, abs string, bnd *bundler.Bundler) (string, bool) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", false
	}
	if hasMeta, fn := moduleMetadataExports(string(data)); !hasMeta || fn {
		return "", false
	}
	sl, ok := sliceModule(abs, metadataSliceExports)
	if !ok {
		return "", false
	}
	m := parseLitModule(sl.Path, sl.Source)
	if m == nil {
		return "", false
	}
	if _, ok := m.exports["generateMetadata"]; ok {
		return "", false
	}
	if !b.litPure(c, m, bnd) {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString(`{"o":[`)
	n := 0
	for _, name := range []string{"metadata", "viewport"} {
		local, ok := m.exports[name]
		if !ok {
			continue
		}
		v, ok := b.litBinding(c, m, local, bnd)
		if !ok {
			return "", false
		}
		if n > 0 {
			sb.WriteByte(',')
		}
		n++
		sb.WriteString(`["` + name + `",`)
		v.appendTagged(&sb)
		sb.WriteByte(']')
	}
	if n == 0 {
		return "", false
	}
	sb.WriteString("]}")
	return sb.String(), true
}

// parseLitModule parses src and indexes its bindings; nil when it does not
// parse.
func parseLitModule(path, src string) *litModule {
	p := parser.New(lexer.ForFile(path, src).Tokenize())
	p.Filename = path
	p.SetSource(src)
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		return nil
	}
	m := &litModule{
		path:    path,
		stmts:   prog.Body,
		consts:  map[string]ast.Expr{},
		imports: map[string]litImport{},
		exports: map[string]string{},
		values:  map[string]*litMemo{},
	}
	declare := func(st ast.Stmt, exported bool) {
		vs, ok := st.(*ast.VarStmt)
		if !ok {
			return
		}
		for _, d := range vs.Decls {
			if d == nil || d.IsDestructuring || d.Name == "" {
				continue
			}
			if vs.Kind == ast.VarConst && d.Init != nil {
				m.consts[d.Name] = d.Init
			}
			if exported {
				m.exports[d.Name] = d.Name
			}
		}
	}
	for _, st := range prog.Body {
		switch s := st.(type) {
		case *ast.ImportStmt:
			src := strings.Trim(s.Source, `'"`)
			if s.Default != "" {
				m.imports[s.Default] = litImport{spec: src}
			}
			if s.Namespace != "" {
				m.imports[s.Namespace] = litImport{spec: src}
			}
			for _, nm := range s.Named {
				remote := nm.Remote
				if remote == "" {
					remote = nm.Local
				}
				m.imports[nm.Local] = litImport{spec: src, remote: remote}
			}
		case *ast.ExportStmt:
			if s.Declaration != nil && !s.Default {
				declare(s.Declaration, true)
				if fn, ok := s.Declaration.(*ast.FnDecl); ok {
					m.exports[fn.Name] = ""
				}
			}
			if s.ReexportSource == "" {
				for _, nm := range s.Named {
					remote := nm.Remote
					if remote == "" {
						remote = nm.Local
					}
					m.exports[remote] = nm.Local
				}
			}
		default:
			declare(st, false)
		}
	}
	return m
}

// litModuleAt parses the project module at path once per build.
func (c *litCache) litModuleAt(path string) *litModule {
	if m, ok := c.mods[path]; ok {
		return m
	}
	var m *litModule
	if data, err := os.ReadFile(path); err == nil {
		m = parseLitModule(path, string(data))
	}
	c.mods[path] = m
	return m
}

// litResolve resolves an import of m to a project module the reader may
// open, or "" when it cannot.
func (b *Builder) litResolve(m *litModule, spec string, bnd *bundler.Bundler) string {
	if bnd == nil {
		return ""
	}
	p := bnd.ResolveForNode(m.path, spec)
	if p == "" || !filepath.IsAbs(p) || !sliceExt[strings.ToLower(filepath.Ext(p))] {
		return ""
	}
	if strings.Contains(filepath.ToSlash(p), "/node_modules/") || !pluginapi.WithinRoot(b.Root, p) {
		return ""
	}
	return p
}

// litPure reports whether evaluating m has no effect beyond the values the
// reader computes: every statement declares a computable value or a function,
// and every import is the runtime or a pure project module.
func (b *Builder) litPure(c *litCache, m *litModule, bnd *bundler.Bundler) bool {
	switch m.pure {
	case 1:
		return true
	case -1, 2:
		return false
	}
	m.pure = 2
	ok := b.litPureStmts(c, m, bnd)
	if ok {
		m.pure = 1
	} else {
		m.pure = -1
	}
	return ok
}

func (b *Builder) litPureStmts(c *litCache, m *litModule, bnd *bundler.Bundler) bool {
	varOK := func(vs *ast.VarStmt) bool {
		for _, d := range vs.Decls {
			if d == nil || d.IsDestructuring || d.Name == "" {
				return false
			}
			if d.Init == nil {
				continue
			}
			if _, ok := b.litEval(c, m, d.Init, bnd); !ok {
				return false
			}
		}
		return true
	}
	for _, st := range m.stmts {
		switch s := st.(type) {
		case *ast.ImportStmt:
			if len(s.Attributes) > 0 {
				return false
			}
			spec := strings.Trim(s.Source, `'"`)
			if spec == litRuntimeSpec {
				continue
			}
			p := b.litResolve(m, spec, bnd)
			if p == "" {
				return false
			}
			dep := c.litModuleAt(p)
			if dep == nil || !b.litPure(c, dep, bnd) {
				return false
			}
		case *ast.ExportStmt:
			if s.ReexportSource != "" || s.StarReexport || s.Namespace != "" {
				return false
			}
			switch d := s.Declaration.(type) {
			case nil:
				if s.Default {
					return false
				}
			case *ast.VarStmt:
				if s.Default || !varOK(d) {
					return false
				}
			case *ast.FnDecl:
			default:
				return false
			}
		case *ast.VarStmt:
			if !varOK(s) {
				return false
			}
		case *ast.FnDecl, *ast.EmptyStmt:
		case *ast.ExprStmt:
			if l, ok := s.Expression.(*ast.Literal); !ok || l.Kind != ast.StringLit {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// litBinding computes the value of a const binding of m.
func (b *Builder) litBinding(c *litCache, m *litModule, name string, bnd *bundler.Bundler) (litValue, bool) {
	memo := m.values[name]
	if memo == nil {
		memo = &litMemo{}
		m.values[name] = memo
	}
	if memo.done {
		return memo.v, memo.ok
	}
	if memo.busy {
		return litValue{}, false
	}
	memo.busy = true
	v, ok := b.litBindingUncached(c, m, name, bnd)
	memo.busy, memo.done, memo.v, memo.ok = false, true, v, ok
	return v, ok
}

func (b *Builder) litBindingUncached(c *litCache, m *litModule, name string, bnd *bundler.Bundler) (litValue, bool) {
	if init, ok := m.consts[name]; ok {
		return b.litEval(c, m, init, bnd)
	}
	im, ok := m.imports[name]
	if !ok || im.remote == "" || im.spec == litRuntimeSpec {
		return litValue{}, false
	}
	p := b.litResolve(m, im.spec, bnd)
	if p == "" {
		return litValue{}, false
	}
	dep := c.litModuleAt(p)
	if dep == nil || !b.litPure(c, dep, bnd) {
		return litValue{}, false
	}
	local, ok := dep.exports[im.remote]
	if !ok || local == "" {
		return litValue{}, false
	}
	return b.litBinding(c, dep, local, bnd)
}

// litEval computes an expression of m.
func (b *Builder) litEval(c *litCache, m *litModule, e ast.Expr, bnd *bundler.Bundler) (litValue, bool) {
	switch x := e.(type) {
	case *ast.TypeAssertion:
		return b.litEval(c, m, x.Expr, bnd)
	case *ast.ParenExpr:
		return b.litEval(c, m, x.Expr, bnd)
	case *ast.Literal:
		switch x.Kind {
		case ast.StringLit:
			if !utf8.ValidString(x.Value) || strings.ContainsRune(x.Value, utf8.RuneError) {
				return litValue{}, false
			}
			return litValue{kind: 's', s: x.Value}, true
		case ast.NumberLit:
			if !litNumberRe.MatchString(x.Value) {
				return litValue{}, false
			}
			return litValue{kind: 'n', s: x.Value}, true
		case ast.BoolLit:
			if x.Value != "true" && x.Value != "false" {
				return litValue{}, false
			}
			return litValue{kind: 'b', s: x.Value}, true
		case ast.NullLit:
			switch x.Value {
			case "null":
				return litValue{kind: 'z'}, true
			case "undefined":
				return litValue{kind: 'u'}, true
			}
		}
		return litValue{}, false
	case *ast.UnaryExpr:
		if x.Op != "-" || x.Postfix {
			return litValue{}, false
		}
		if l, ok := x.Arg.(*ast.Literal); ok && l.Kind == ast.NumberLit && litNumberRe.MatchString(l.Value) {
			return litValue{kind: 'n', s: "-" + l.Value}, true
		}
		return litValue{}, false
	case *ast.Identifier:
		return b.litBinding(c, m, x.Name, bnd)
	case *ast.TemplateExpr:
		if len(x.Raw) != len(x.Parts)+1 {
			return litValue{}, false
		}
		var sb strings.Builder
		for i, raw := range x.Raw {
			if strings.ContainsAny(raw, "\\\r") {
				return litValue{}, false
			}
			sb.WriteString(raw)
			if i < len(x.Parts) {
				v, ok := b.litEval(c, m, x.Parts[i], bnd)
				if !ok || v.kind != 's' {
					return litValue{}, false
				}
				sb.WriteString(v.s)
			}
		}
		return litValue{kind: 's', s: sb.String()}, true
	case *ast.BinaryExpr:
		if x.Op != "+" {
			return litValue{}, false
		}
		l, ok := b.litEval(c, m, x.Left, bnd)
		if !ok || l.kind != 's' {
			return litValue{}, false
		}
		r, ok := b.litEval(c, m, x.Right, bnd)
		if !ok || r.kind != 's' {
			return litValue{}, false
		}
		return litValue{kind: 's', s: l.s + r.s}, true
	case *ast.ArrayExpr:
		out := litValue{kind: 'a'}
		for _, el := range x.Elements {
			if el == nil {
				return litValue{}, false
			}
			v, ok := b.litEval(c, m, el, bnd)
			if !ok {
				return litValue{}, false
			}
			out.items = append(out.items, v)
		}
		return out, true
	case *ast.ObjectExpr:
		out := litValue{kind: 'o'}
		set := func(k string, v litValue) {
			for i, have := range out.keys {
				if have == k {
					out.items[i] = v
					return
				}
			}
			out.keys = append(out.keys, k)
			out.items = append(out.items, v)
		}
		for _, p := range x.Properties {
			if p == nil || p.Computed || p.Method || p.Getter || p.Setter {
				return litValue{}, false
			}
			if p.Spread {
				v, ok := b.litEval(c, m, p.Value, bnd)
				if !ok {
					return litValue{}, false
				}
				switch v.kind {
				case 'o':
					for i, k := range v.keys {
						set(k, v.items[i])
					}
				case 'z', 'u':
				default:
					return litValue{}, false
				}
				continue
			}
			if p.Key == "__proto__" || p.Key == "" {
				return litValue{}, false
			}
			if p.KeyNumeric && !litIndexRe.MatchString(p.Key) {
				return litValue{}, false
			}
			val := p.Value
			if p.Shorthand && val == nil {
				val = &ast.Identifier{Name: p.Key}
			}
			if val == nil {
				return litValue{}, false
			}
			v, ok := b.litEval(c, m, val, bnd)
			if !ok {
				return litValue{}, false
			}
			set(p.Key, v)
		}
		return out, true
	}
	return litValue{}, false
}
