package jsruntime

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// CompatRef is one reference from source code to a ledger API.
type CompatRef struct {
	API     string
	Status  CompatStatus
	Feature string
	File    string
	Line    int
	Col     int
	// Guarded is true for a `typeof X` operand and for references inside a
	// branch guarded by a feature check (`typeof X`, `"X" in globalThis`,
	// `globalThis.X`). Guarded references still count toward Features.
	Guarded bool
}

// CompatScan is the result of scanning sources against the ledger. Refs holds
// the first reference per (file, api, guarded), in source order per file.
type CompatScan struct {
	Refs []CompatRef
	// Complete is false when a file could not be parsed or reaches globals
	// dynamically (globalThis[name], with, eval, new Function), so Features
	// may miss something.
	Complete bool
	// Skipped lists files that could not be read or parsed.
	Skipped []string
}

// compatScanMaxBytes bounds the size of one scanned file; bigger files are
// skipped and make the scan incomplete.
const compatScanMaxBytes = 4 << 20

// compatScannable reports whether path is a JS or TS module the scanner reads.
func compatScannable(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".mjs", ".cjs", ".jsx", ".ts", ".mts", ".cts", ".tsx":
		return true
	}
	return false
}

// ScanCompatFiles reads and scans every JS/TS file in paths. Other files (JSON,
// CSS) are ignored.
func ScanCompatFiles(paths []string) CompatScan {
	out := CompatScan{Complete: true}
	for _, p := range paths {
		if !compatScannable(p) {
			continue
		}
		info, err := os.Stat(p)
		if err != nil || info.Size() > compatScanMaxBytes {
			out.Complete = false
			out.Skipped = append(out.Skipped, p)
			continue
		}
		key := compatCacheKey{path: p, size: info.Size(), mod: info.ModTime().UnixNano()}
		compatCache.Lock()
		cached, ok := compatCache.m[key]
		compatCache.Unlock()
		if !ok {
			src, err := os.ReadFile(p)
			if err != nil {
				out.Complete = false
				out.Skipped = append(out.Skipped, p)
				continue
			}
			cached = ScanCompatSource(p, string(src))
			compatCache.Lock()
			if len(compatCache.m) >= compatCacheMax {
				compatCache.m = map[compatCacheKey]CompatScan{}
			}
			compatCache.m[key] = cached
			compatCache.Unlock()
		}
		out.Merge(cached)
	}
	return out
}

// compatCache keeps per-file scans across routes and dev rebuilds; a file is
// rescanned when its size or modification time changes.
var compatCache = struct {
	sync.Mutex
	m map[compatCacheKey]CompatScan
}{m: map[compatCacheKey]CompatScan{}}

const compatCacheMax = 20000

type compatCacheKey struct {
	path string
	size int64
	mod  int64
}

// ScanCompatSource parses one JS/TS source (TypeScript is stripped with
// positions mapped back to src) and scans it. A file with syntax errors
// reports nothing and makes the scan incomplete.
func ScanCompatSource(path, src string) CompatScan {
	prog, errs := tsstrip.Parse(path, src, tsstrip.Options{})
	if prog == nil || len(errs) > 0 {
		return CompatScan{Complete: false, Skipped: []string{path}}
	}
	return ScanCompatProgram(path, prog)
}

// ScanCompatProgram scans a parsed module for references to ledger APIs. Only
// free (global) identifiers count: imports, locals and parameters shadow.
func ScanCompatProgram(path string, prog *ast.Program) CompatScan {
	w := &compatWalker{
		ledger: Compat(),
		file:   path,
		guards: map[string]int{},
		seen:   map[compatRefKey]bool{},
		out:    CompatScan{Complete: true},
	}
	if prog != nil {
		w.push(hoistedNames(prog.Body))
		w.stmts(prog.Body)
		w.pop()
	}
	return w.out
}

// Merge appends the results of o.
func (s *CompatScan) Merge(o CompatScan) {
	s.Refs = append(s.Refs, o.Refs...)
	s.Skipped = append(s.Skipped, o.Skipped...)
	if !o.Complete {
		s.Complete = false
	}
}

// Features returns the sorted set of ledger features the scanned sources
// reference, whatever the status.
func (s CompatScan) Features() []string {
	set := map[string]bool{}
	for _, r := range s.Refs {
		set[r.Feature] = true
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Unsupported returns the unguarded references to not-supported APIs.
func (s CompatScan) Unsupported() []CompatRef {
	var out []CompatRef
	for _, r := range s.Refs {
		if r.Status == CompatNotSupported && !r.Guarded {
			out = append(out, r)
		}
	}
	return out
}

type compatRefKey struct {
	api     string
	guarded bool
}

type compatScope struct {
	parent *compatScope
	names  map[string]bool
}

type compatWalker struct {
	ledger *CompatLedger
	file   string
	scope  *compatScope
	// guards counts enclosing feature checks per global name.
	guards map[string]int
	seen   map[compatRefKey]bool
	out    CompatScan
}

func (w *compatWalker) push(names []string) {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		if n != "" {
			m[n] = true
		}
	}
	w.scope = &compatScope{parent: w.scope, names: m}
}

func (w *compatWalker) pop() { w.scope = w.scope.parent }

func (w *compatWalker) bound(name string) bool {
	for s := w.scope; s != nil; s = s.parent {
		if s.names[name] {
			return true
		}
	}
	return false
}

// globalAlias reports whether name is a free alias of the global object.
func (w *compatWalker) globalAlias(name string) bool {
	return (name == "globalThis" || name == "self") && !w.bound(name)
}

// ref records a reference to the global member chain segs (segs[0] is the
// identifier the chain starts at) when it resolves to a ledger row.
func (w *compatWalker) ref(segs []string, pos ast.Pos, typeofOperand bool) {
	if len(segs) == 0 || w.bound(segs[0]) {
		return
	}
	if (segs[0] == "globalThis" || segs[0] == "self") && len(segs) > 1 {
		if segs[0] == "self" {
			// self is itself missing, so self.X fails before X is read.
			w.match([]string{"self"}, pos, typeofOperand)
		}
		segs = segs[1:]
	}
	w.match(segs, pos, typeofOperand)
}

func (w *compatWalker) match(segs []string, pos ast.Pos, typeofOperand bool) {
	for k := len(segs); k >= 1; k-- {
		row, ok := w.ledger.Lookup(strings.Join(segs[:k], "."))
		if !ok {
			continue
		}
		guarded := typeofOperand || w.guards[segs[0]] > 0
		key := compatRefKey{api: row.API, guarded: guarded}
		if w.seen[key] {
			return
		}
		w.seen[key] = true
		w.out.Refs = append(w.out.Refs, CompatRef{
			API: row.API, Status: row.Status, Feature: row.Feature,
			File: w.file, Line: pos.Line, Col: pos.Col, Guarded: guarded,
		})
		return
	}
}

// memberChain flattens `a.b["c"].d` into its segments when the chain is rooted
// at an identifier and every key is static.
func memberChain(e ast.Expr) (segs []string, root *ast.Identifier, ok bool) {
	var rev []string
	cur := e
	for {
		switch v := cur.(type) {
		case *ast.MemberExpr:
			switch {
			case !v.Computed:
				id, isID := v.Property.(*ast.Identifier)
				if !isID {
					return nil, nil, false
				}
				rev = append(rev, id.Name)
			default:
				lit, isLit := v.Property.(*ast.Literal)
				if !isLit || lit.Kind != ast.StringLit {
					return nil, nil, false
				}
				rev = append(rev, unquoteLit(lit.Value))
			}
			cur = v.Object
		case *ast.Identifier:
			segs = make([]string, 0, len(rev)+1)
			segs = append(segs, v.Name)
			for i := len(rev) - 1; i >= 0; i-- {
				segs = append(segs, rev[i])
			}
			return segs, v, true
		default:
			return nil, nil, false
		}
	}
}

// unquoteLit strips the quotes a string literal may keep in Value.
func unquoteLit(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// guardNames returns the global names a condition feature-checks.
func (w *compatWalker) guardNames(test ast.Expr) []string {
	var names []string
	var visit func(e ast.Expr)
	visit = func(e ast.Expr) {
		switch v := e.(type) {
		case *ast.UnaryExpr:
			if v.Op == "typeof" {
				if segs, _, ok := memberChain(v.Arg); ok {
					names = append(names, w.guardRoot(segs))
				}
				return
			}
			visit(v.Arg)
		case *ast.BinaryExpr:
			if v.Op == "in" {
				if lit, ok := v.Left.(*ast.Literal); ok && lit.Kind == ast.StringLit {
					if id, ok := v.Right.(*ast.Identifier); ok && w.globalAlias(id.Name) {
						names = append(names, unquoteLit(lit.Value))
					}
				}
				return
			}
			visit(v.Left)
			visit(v.Right)
		case *ast.MemberExpr:
			if segs, _, ok := memberChain(v); ok && w.globalAlias(segs[0]) {
				names = append(names, segs[1])
			}
		case *ast.ParenExpr:
			visit(v.Expr)
		case *ast.ConditionalExpr:
			visit(v.Test)
			visit(v.Consequent)
			visit(v.Alternate)
		case *ast.CallExpr:
			// Boolean(globalThis.X), !!X?.y
			for _, a := range v.Args {
				visit(a)
			}
		}
	}
	visit(test)
	return names
}

func (w *compatWalker) guardRoot(segs []string) string {
	if len(segs) > 1 && w.globalAlias(segs[0]) {
		return segs[1]
	}
	return segs[0]
}

// guarded runs fn with the names checked by test treated as guarded. Callers
// walk the test inside fn, so reading globalThis.X in a check is not a use.
func (w *compatWalker) guarded(test ast.Expr, fn func()) {
	names := w.guardNames(test)
	for _, n := range names {
		w.guards[n]++
	}
	fn()
	for _, n := range names {
		w.guards[n]--
	}
}

func (w *compatWalker) stmts(list []ast.Stmt) {
	for _, s := range list {
		w.stmt(s)
	}
}

func (w *compatWalker) stmt(s ast.Stmt) {
	switch v := s.(type) {
	case nil:
	case *ast.ExportStmt:
		if v.Declaration != nil {
			w.stmt(v.Declaration)
		} else if v.Default && v.Local != "" {
			w.ref([]string{v.Local}, v.Position, false)
		}
	case *ast.VarStmt:
		for _, d := range v.Decls {
			if d == nil {
				continue
			}
			w.pattern(d.Target)
			w.expr(d.Init)
		}
	case *ast.FnDecl:
		w.fn(v.Params, v.Body, "")
	case *ast.ClassDecl:
		w.class(v)
	case *ast.ReturnStmt:
		w.expr(v.Value)
	case *ast.ThrowStmt:
		w.expr(v.Value)
	case *ast.ExprStmt:
		w.expr(v.Expression)
	case *ast.IfStmt:
		w.guarded(v.Test, func() {
			w.expr(v.Test)
			w.stmts(v.Consequent)
			w.stmts(v.Alternate)
		})
	case *ast.BlockStmt:
		w.stmts(v.Body)
	case *ast.ForStmt:
		w.stmt(v.Init)
		w.expr(v.Test)
		w.expr(v.Update)
		w.stmts(v.Body)
	case *ast.ForInStmt:
		if v.Keyword != "" {
			w.pattern(v.Left)
		} else {
			w.expr(v.Left)
		}
		w.expr(v.Right)
		w.stmts(v.Body)
	case *ast.WhileStmt:
		w.guarded(v.Test, func() {
			w.expr(v.Test)
			w.stmts(v.Body)
		})
	case *ast.DoWhileStmt:
		w.stmts(v.Body)
		w.expr(v.Test)
	case *ast.SwitchStmt:
		w.expr(v.Discriminant)
		for _, c := range v.Cases {
			if c != nil {
				w.expr(c.Test)
				w.stmts(c.Body)
			}
		}
	case *ast.TryStmt:
		w.stmts(v.Body)
		if c := v.Catch; c != nil {
			var names []string
			if c.ParamTarget != nil {
				names = patternNames(c.ParamTarget)
			} else if c.Param != "" {
				names = []string{c.Param}
			}
			w.push(append(names, hoistedNames(c.Body)...))
			w.pattern(c.ParamTarget)
			w.stmts(c.Body)
			w.pop()
		}
		w.stmts(v.Finally)
	case *ast.LabeledStmt:
		w.stmt(v.Body)
	case *ast.WithStmt:
		w.out.Complete = false
		w.expr(v.Object)
		w.stmts(v.Body)
	}
}

// fn walks a function body in its own scope.
func (w *compatWalker) fn(params []*ast.Param, body []ast.Stmt, selfName string) {
	names := []string{selfName, "arguments"}
	for _, p := range params {
		if p == nil {
			continue
		}
		if p.Target != nil {
			names = append(names, patternNames(p.Target)...)
		} else if p.Name != "{...}" {
			names = append(names, p.Name)
		}
	}
	w.push(append(names, hoistedNames(body)...))
	for _, p := range params {
		if p != nil {
			w.pattern(p.Target)
			w.expr(p.Default)
		}
	}
	w.stmts(body)
	w.pop()
}

func (w *compatWalker) class(c *ast.ClassDecl) {
	if c == nil {
		return
	}
	w.expr(c.SuperClass)
	w.push([]string{c.Name})
	for _, m := range c.Members {
		if m == nil {
			continue
		}
		if m.Computed {
			w.expr(m.KeyExpr)
		}
		w.expr(m.Init)
		if m.Kind != ast.ClassField && m.Kind != ast.ClassAccessor {
			w.fn(m.Params, m.Body, "")
		}
	}
	w.pop()
}

// pattern walks the expressions inside a binding pattern (defaults and
// computed keys); the bound names themselves are not references.
func (w *compatWalker) pattern(e ast.Expr) {
	switch v := e.(type) {
	case nil, *ast.Identifier:
	case *ast.ObjectExpr:
		for _, p := range v.Properties {
			if p == nil {
				continue
			}
			if p.Computed {
				w.expr(p.KeyExpr)
			}
			w.pattern(p.Value)
		}
	case *ast.ArrayExpr:
		for _, el := range v.Elements {
			w.pattern(el)
		}
	case *ast.UnaryExpr:
		w.pattern(v.Arg)
	case *ast.BinaryExpr:
		if v.Op == "=" {
			w.pattern(v.Left)
			w.expr(v.Right)
			return
		}
		w.expr(v)
	default:
		w.expr(v)
	}
}

func (w *compatWalker) expr(e ast.Expr) {
	switch v := e.(type) {
	case nil:
	case *ast.Identifier:
		w.ref([]string{v.Name}, v.Position, false)
	case *ast.MemberExpr:
		if segs, root, ok := memberChain(v); ok {
			w.ref(segs, root.Position, false)
			return
		}
		if v.Computed {
			if id, ok := v.Object.(*ast.Identifier); ok && w.globalAlias(id.Name) {
				w.out.Complete = false
			}
		}
		w.expr(v.Object)
		if v.Computed {
			w.expr(v.Property)
		}
	case *ast.CallExpr:
		if id, ok := v.Callee.(*ast.Identifier); ok && id.Name == "eval" && !w.bound("eval") {
			w.out.Complete = false
		}
		w.expr(v.Callee)
		for _, a := range v.Args {
			w.expr(a)
		}
	case *ast.NewExpr:
		if id, ok := v.Callee.(*ast.Identifier); ok && id.Name == "Function" && !w.bound("Function") {
			w.out.Complete = false
		}
		w.expr(v.Callee)
		for _, a := range v.Args {
			w.expr(a)
		}
	case *ast.BinaryExpr:
		switch v.Op {
		case "&&", "||", "??":
			w.guarded(v.Left, func() {
				w.expr(v.Left)
				w.expr(v.Right)
			})
		default:
			w.expr(v.Left)
			w.expr(v.Right)
		}
	case *ast.UnaryExpr:
		if v.Op == "typeof" {
			if segs, root, ok := memberChain(v.Arg); ok {
				w.ref(segs, root.Position, true)
				return
			}
		}
		w.expr(v.Arg)
	case *ast.ConditionalExpr:
		w.guarded(v.Test, func() {
			w.expr(v.Test)
			w.expr(v.Consequent)
			w.expr(v.Alternate)
		})
	case *ast.TypeAssertion:
		w.expr(v.Expr)
	case *ast.ParenExpr:
		w.expr(v.Expr)
	case *ast.AwaitExpr:
		w.expr(v.Arg)
	case *ast.ArrowFn:
		w.fn(v.Params, v.Body, v.Name)
	case *ast.ObjectExpr:
		for _, p := range v.Properties {
			if p == nil {
				continue
			}
			if p.Computed {
				w.expr(p.KeyExpr)
			}
			w.expr(p.Value)
		}
	case *ast.ArrayExpr:
		for _, el := range v.Elements {
			w.expr(el)
		}
	case *ast.TemplateExpr:
		for _, p := range v.Parts {
			w.expr(p)
		}
	case *ast.TaggedTemplateExpr:
		w.expr(v.Tag)
		if v.Quasi != nil {
			w.expr(v.Quasi)
		}
	case *ast.DynamicImport:
		w.expr(v.Arg)
		w.expr(v.Options)
	case *ast.ClassExpr:
		w.class(v.Class)
	case *ast.JSXElement:
		w.jsxElement(v)
	case *ast.JSXFragment:
		w.expr(v.Key)
		w.jsxChildren(v.Children)
	}
}

func (w *compatWalker) jsxElement(el *ast.JSXElement) {
	if el == nil {
		return
	}
	if el.Opening != nil {
		for _, a := range el.Opening.Attributes {
			if a != nil {
				w.expr(a.Value)
			}
		}
	}
	w.jsxChildren(el.Children)
}

func (w *compatWalker) jsxChildren(children []ast.JSXChild) {
	for _, c := range children {
		switch v := c.(type) {
		case *ast.JSXExprContainer:
			w.expr(v.Expression)
		case *ast.JSXElementChild:
			w.jsxElement(v.Element)
		case *ast.JSXFragmentChild:
			if v.Fragment != nil {
				w.expr(v.Fragment)
			}
		}
	}
}

// patternNames returns the names a binding pattern declares.
func patternNames(e ast.Expr) []string {
	switch v := e.(type) {
	case *ast.Identifier:
		return []string{v.Name}
	case *ast.ObjectExpr:
		var names []string
		for _, p := range v.Properties {
			if p == nil {
				continue
			}
			if p.Value != nil {
				names = append(names, patternNames(p.Value)...)
			} else if p.Key != "" {
				names = append(names, p.Key)
			}
		}
		return names
	case *ast.ArrayExpr:
		var names []string
		for _, el := range v.Elements {
			names = append(names, patternNames(el)...)
		}
		return names
	case *ast.UnaryExpr:
		return patternNames(v.Arg)
	case *ast.BinaryExpr:
		if v.Op == "=" {
			return patternNames(v.Left)
		}
	}
	return nil
}

// hoistedNames returns every name a statement list declares outside nested
// functions, including block-scoped ones (treating a block binding as visible
// in the whole function can only hide a reference, never invent one).
func hoistedNames(list []ast.Stmt) []string {
	var names []string
	var visit func(s ast.Stmt)
	visit = func(s ast.Stmt) {
		switch v := s.(type) {
		case *ast.ImportStmt:
			names = append(names, v.Default, v.Namespace)
			for _, n := range v.Named {
				names = append(names, n.Local)
			}
		case *ast.ExportStmt:
			if v.Declaration != nil {
				visit(v.Declaration)
			}
		case *ast.VarStmt:
			for _, d := range v.Decls {
				if d == nil {
					continue
				}
				names = append(names, d.Name, d.RestName)
				names = append(names, d.Names...)
				names = append(names, patternNames(d.Target)...)
			}
		case *ast.FnDecl:
			names = append(names, v.Name)
		case *ast.ClassDecl:
			names = append(names, v.Name)
		case *ast.BlockStmt:
			for _, c := range v.Body {
				visit(c)
			}
		case *ast.IfStmt:
			for _, c := range v.Consequent {
				visit(c)
			}
			for _, c := range v.Alternate {
				visit(c)
			}
		case *ast.ForStmt:
			visit(v.Init)
			for _, c := range v.Body {
				visit(c)
			}
		case *ast.ForInStmt:
			if v.Keyword != "" {
				names = append(names, patternNames(v.Left)...)
			}
			for _, c := range v.Body {
				visit(c)
			}
		case *ast.WhileStmt:
			for _, c := range v.Body {
				visit(c)
			}
		case *ast.DoWhileStmt:
			for _, c := range v.Body {
				visit(c)
			}
		case *ast.SwitchStmt:
			for _, cs := range v.Cases {
				if cs != nil {
					for _, c := range cs.Body {
						visit(c)
					}
				}
			}
		case *ast.TryStmt:
			for _, c := range v.Body {
				visit(c)
			}
			for _, c := range v.Finally {
				visit(c)
			}
		case *ast.LabeledStmt:
			visit(v.Body)
		case *ast.WithStmt:
			for _, c := range v.Body {
				visit(c)
			}
		}
	}
	for _, s := range list {
		visit(s)
	}
	return names
}
