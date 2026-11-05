package irtree

import (
	"strconv"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// Module-level statements that change a module value.
//
// A module constant is normally folded from its initializer alone. That is
// wrong once top-level code changes it afterwards:
//
//	const squares = [];
//	for (let i = 0; i < 3; i++) squares.push(i * i);
//
// RunModule finds the top-level statements that can change a module binding
// (assignments, `++`, property writes, `push` and other method calls, loops,
// `Object.assign`, and calls to module functions that do any of these) and
// runs the module's top level in source order with the compile-time
// interpreter. The bindings' final values replace the initializer folds. A
// statement the interpreter cannot run in full leaves the bindings it may
// have changed unknown: they are blocked, and everything read from them falls
// back to the runtime path like any other value the build cannot know.

// ModuleBlockedPrefix starts the reason of a blocked module binding.
const ModuleBlockedPrefix = "module value changed by top-level code: "

// ModuleRun is what running the top-level code of one or more modules left
// behind.
type ModuleRun struct {
	// Values holds the final value of every binding the run computed in full.
	Values map[string]jsvalue.Value
	// Blocked names the bindings whose value cannot be known at build time,
	// with the reason.
	Blocked map[string]string
}

// MergeModuleRuns combines runs; for a name in several, the earlier run wins.
func MergeModuleRuns(runs ...*ModuleRun) *ModuleRun {
	var out *ModuleRun
	for _, r := range runs {
		if r == nil {
			continue
		}
		if out == nil {
			out = &ModuleRun{Values: map[string]jsvalue.Value{}, Blocked: map[string]string{}}
		}
		for k, v := range r.Values {
			if _, ok := out.Values[k]; !ok && out.Blocked[k] == "" {
				out.Values[k] = v
			}
		}
		for k, reason := range r.Blocked {
			if _, ok := out.Blocked[k]; !ok {
				if _, known := out.Values[k]; !known {
					out.Blocked[k] = reason
				}
			}
		}
	}
	return out
}

// RunModule runs the top-level code of prog that can change a module binding.
// outer holds the initializers of the other modules' bindings, which the run
// reads lazily. It returns nil when no top-level code can change a binding,
// in which case initializer folding is exact.
func RunModule(prog *ast.Program, outer map[string]ast.Expr) *ModuleRun {
	if prog == nil {
		return nil
	}
	m := analyzeModule(prog)
	if !m.needsRun() {
		return nil
	}
	return m.run(outer)
}

// moduleItem is one top-level statement that runs code or declares a value.
type moduleItem struct {
	stmt ast.Stmt
	decl *ast.VarDecl // set for one declarator of a variable statement
}

// exposure is what a piece of code may change.
type exposure struct {
	// roots are the binding names it may write to or call a mutating method on.
	roots map[string]bool
	// inPlace are the roots changed by a call the interpreter evaluates on a
	// copy (sort, reverse), whose effect a run would miss.
	inPlace map[string]bool
}

func newExposure() *exposure {
	return &exposure{roots: map[string]bool{}, inPlace: map[string]bool{}}
}

func (e *exposure) add(o *exposure) {
	for k := range o.roots {
		e.roots[k] = true
	}
	for k := range o.inPlace {
		e.inPlace[k] = true
	}
}

type callable struct {
	params []*ast.Param
	body   []ast.Stmt
}

type moduleAnalysis struct {
	prog      *ast.Program
	items     []moduleItem
	data      map[string]bool   // declared value bindings
	foreign   map[string]string // imported binding -> exported name
	callables map[string]*callable
	alias     map[string]string // binding -> binding it is another name for
	fnExp     map[string]*exposure
	active    map[string]bool
}

func analyzeModule(prog *ast.Program) *moduleAnalysis {
	m := &moduleAnalysis{
		prog:      prog,
		data:      map[string]bool{},
		foreign:   map[string]string{},
		callables: map[string]*callable{},
		alias:     map[string]string{},
		fnExp:     map[string]*exposure{},
		active:    map[string]bool{},
	}
	for _, stmt := range prog.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok {
			if exp.Declaration == nil {
				continue
			}
			stmt = exp.Declaration
		}
		switch s := stmt.(type) {
		case *ast.ImportStmt:
			if s.Default != "" {
				m.foreign[s.Default] = "default"
			}
			if s.Namespace != "" {
				m.foreign[s.Namespace] = s.Namespace
			}
			for _, n := range s.Named {
				local := n.Local
				if local == "" {
					local = n.Remote
				}
				m.foreign[local] = n.Remote
			}
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d == nil {
					continue
				}
				m.items = append(m.items, moduleItem{stmt: s, decl: d})
				if arrow, ok := d.Init.(*ast.ArrowFn); ok && d.Name != "" {
					m.callables[d.Name] = &callable{params: arrow.Params, body: arrow.Body}
					continue
				}
				for _, name := range varDeclBindingNames(d) {
					m.data[name] = true
				}
				if d.Name != "" && !d.IsDestructuring {
					if root := rootIdentifier(d.Init); root != "" {
						m.alias[d.Name] = root
					}
				}
			}
		case *ast.FnDecl:
			if s.Name != "" {
				m.callables[s.Name] = &callable{params: s.Params, body: s.Body}
			}
			m.items = append(m.items, moduleItem{stmt: s})
		case *ast.ClassDecl:
			m.items = append(m.items, moduleItem{stmt: s})
		case *ast.ExportStmt, *ast.EmptyStmt, *ast.DebuggerStmt:
		case *ast.ExprStmt:
			// Directive prologues ("use client") are not code.
			if _, ok := s.Expression.(*ast.Literal); ok {
				continue
			}
			m.items = append(m.items, moduleItem{stmt: s})
		default:
			m.items = append(m.items, moduleItem{stmt: s})
		}
	}
	return m
}

// tracked reports whether code changing name changes a module value the build
// folds: a binding this module declares or one it imports.
func (m *moduleAnalysis) tracked(name string) bool {
	if m.data[name] {
		return true
	}
	_, ok := m.foreign[name]
	return ok
}

// isCode reports whether a top-level item is a statement that runs code, as
// opposed to a declaration.
func (it moduleItem) isCode() bool {
	switch it.stmt.(type) {
	case *ast.VarStmt, *ast.FnDecl, *ast.ClassDecl:
		return false
	}
	return true
}

// trackedExposure returns what the item may change, restricted to tracked
// bindings and widened over bindings that are another name for the same value.
func (m *moduleAnalysis) trackedExposure(it moduleItem) *exposure {
	raw := newExposure()
	switch {
	case it.isCode():
		m.expose(it.stmt, nil, raw, false)
	case it.decl != nil && it.decl.Init != nil:
		// An initializer can change a binding only through a call to a module
		// function or a mutating method; calls to unknown functions are not
		// followed (their effect on arguments is not assumed).
		m.expose(it.decl.Init, nil, raw, true)
	}
	out := newExposure()
	widen := func(name string, into map[string]bool) {
		for _, n := range m.sameValue(name) {
			if m.tracked(n) {
				into[n] = true
			}
		}
	}
	for name := range raw.roots {
		widen(name, out.roots)
	}
	for name := range raw.inPlace {
		widen(name, out.inPlace)
		widen(name, out.roots)
	}
	return out
}

// sameValue returns name and the bindings that are another name for the same
// object (`const y = x`), in either direction.
func (m *moduleAnalysis) sameValue(name string) []string {
	group := map[string]bool{name: true}
	for changed := true; changed; {
		changed = false
		for a, b := range m.alias {
			if group[a] && !group[b] {
				group[b], changed = true, true
			}
			if group[b] && !group[a] {
				group[a], changed = true, true
			}
		}
	}
	out := make([]string, 0, len(group))
	for n := range group {
		out = append(out, n)
	}
	return out
}

func (m *moduleAnalysis) needsRun() bool {
	for _, it := range m.items {
		if len(m.trackedExposure(it).roots) > 0 {
			return true
		}
	}
	return false
}

// run executes the module's top level and collects the result.
func (m *moduleAnalysis) run(outer map[string]ast.Expr) *ModuleRun {
	res := &ModuleRun{Values: map[string]jsvalue.Value{}, Blocked: map[string]string{}}
	fold := lazyOuterFold(outer)
	in := jsvalue.New(jsvalue.Hooks{})
	in.StrictStatements = true
	scope := in.Scope
	in.Hooks.Ident = func(name string) (jsvalue.Value, bool) {
		if m.data[name] {
			return jsvalue.Dynamic("used before its declaration: " + name), true
		}
		if m.foreign[name] != "" || outer[name] != nil {
			return fold(name)
		}
		return jsvalue.Value{}, false
	}
	block := func(name, reason string) {
		res.Blocked[name] = reason
		if _, ok := scope.Lookup(name); ok {
			scope.Assign(name, jsvalue.Dynamic(ModuleBlockedPrefix+reason))
		} else {
			scope.Define(name, jsvalue.Dynamic(ModuleBlockedPrefix+reason))
		}
		if remote := m.foreign[name]; remote != "" && remote != name {
			res.Blocked[remote] = reason
		}
	}
	// Function declarations are hoisted.
	for _, it := range m.items {
		if fn, ok := it.stmt.(*ast.FnDecl); ok && fn.Name != "" {
			scope.Define(fn.Name, in.Closure(fn.Name, fn.Params, fn.Body))
		}
	}
	for _, it := range m.items {
		switch s := it.stmt.(type) {
		case *ast.FnDecl:
			continue
		case *ast.ClassDecl:
			if s.Name != "" {
				scope.Define(s.Name, jsvalue.Dynamic("class "+s.Name))
			}
			continue
		}
		exp := m.trackedExposure(it)
		if it.decl != nil {
			v := in.Declare(it.decl)
			if v.IsUnknown() {
				for _, name := range varDeclBindingNames(it.decl) {
					scope.Define(name, v)
				}
				// A call that failed halfway may have changed what it was given.
				for name := range exp.roots {
					block(name, moduleStatementReason(it.decl.Init, name))
				}
			}
			continue
		}
		if len(exp.roots) == 0 {
			// It cannot change a tracked binding; running it adds nothing.
			continue
		}
		failed := len(exp.inPlace) > 0
		if !failed {
			_, failed = in.ExecStmt(it.stmt)
		}
		if failed {
			for name := range exp.roots {
				block(name, moduleStatementReason(it.stmt, name))
			}
		}
	}
	for name := range m.data {
		if _, blocked := res.Blocked[name]; blocked {
			continue
		}
		v, ok := scope.Lookup(name)
		if !ok || v.ContainsUnknown() || v.IsFunction() || v.IsJSX() {
			continue
		}
		res.Values[name] = v
	}
	return res
}

// moduleStatementReason describes the statement that left name unknown.
func moduleStatementReason(node any, name string) string {
	pos := ast.Pos{}
	switch n := node.(type) {
	case ast.Stmt:
		pos = n.Pos()
	case ast.Expr:
		pos = n.Pos()
	}
	if pos.Line > 0 {
		return "`" + name + "` is changed by the statement at line " + strconv.Itoa(pos.Line) + ", which cannot be run at build time"
	}
	return "`" + name + "` is changed by a top-level statement that cannot be run at build time"
}

// lazyOuterFold returns a resolver that folds another module's binding from
// its initializer.
func lazyOuterFold(outer map[string]ast.Expr) func(string) (jsvalue.Value, bool) {
	in := jsvalue.New(jsvalue.Hooks{})
	state := map[string]int{}
	values := map[string]jsvalue.Value{}
	resolve := func(name string) (jsvalue.Value, bool) {
		switch state[name] {
		case 1:
			return jsvalue.Dynamic("cyclic module constant " + name), true
		case 2:
			return values[name], true
		}
		init := outer[name]
		if init == nil {
			return jsvalue.Value{}, false
		}
		state[name] = 1
		v := in.EvalIn(jsvalue.NewScope(nil), init)
		state[name] = 2
		values[name] = v
		return v, true
	}
	in.Hooks.Ident = resolve
	return resolve
}

// Exposure analysis.

// syncCallbackMethods run their function arguments before returning.
var syncCallbackMethods = map[string]bool{
	"map": true, "filter": true, "find": true, "findIndex": true, "findLast": true, "findLastIndex": true,
	"some": true, "every": true, "flatMap": true, "forEach": true, "reduce": true, "reduceRight": true,
}

// pureGlobals are global namespaces whose functions do not change their arguments.
var pureGlobals = map[string]bool{
	"Math": true, "JSON": true, "console": true, "Number": true, "String": true, "Boolean": true,
	"Array": true, "Date": true, "Intl": true, "Symbol": true, "BigInt": true, "RegExp": true,
	"parseInt": true, "parseFloat": true, "isNaN": true, "isFinite": true, "structuredClone": true,
	"encodeURIComponent": true, "decodeURIComponent": true, "encodeURI": true, "decodeURI": true,
}

// pureObjectFuncs are Object functions that only read their arguments.
var pureObjectFuncs = map[string]bool{
	"keys": true, "values": true, "entries": true, "fromEntries": true, "isFrozen": true, "is": true,
	"getOwnPropertyNames": true, "getOwnPropertyDescriptor": true, "getOwnPropertyDescriptors": true,
	"hasOwn": true, "create": true, "getPrototypeOf": true, "freeze": true,
}

// pureConstructors build a new value from their arguments.
var pureConstructors = map[string]bool{
	"Set": true, "Map": true, "Array": true, "Date": true, "Error": true, "TypeError": true, "RangeError": true,
	"RegExp": true, "URL": true, "URLSearchParams": true, "Intl": true, "WeakMap": true, "WeakSet": true,
}

// expose records into exp what the code in node may change. skip names are
// local to the code being analyzed. declInit relaxes the rules for a variable
// initializer: arguments to functions the module does not define are not
// assumed to be changed.
func (m *moduleAnalysis) expose(node any, skip map[string]bool, exp *exposure, declInit bool) {
	addRoot := func(name string) {
		if name != "" && !skip[name] {
			exp.roots[name] = true
		}
	}
	addTargets := func(e ast.Expr) {
		for _, name := range assignTargetRoots(e) {
			addRoot(name)
		}
	}
	addArgs := func(args []ast.Expr) {
		for _, a := range args {
			for _, name := range identifiersIn(a) {
				addRoot(name)
			}
		}
	}
	WalkAST(node, func(n any) bool {
		switch x := n.(type) {
		case *ast.ArrowFn, *ast.FnDecl, *ast.ClassDecl, *ast.ClassExpr:
			// A function body runs only when called; calls are followed below.
			return false
		case *ast.BinaryExpr:
			if isAssignOp(x.Op) {
				addTargets(x.Left)
			}
		case *ast.UnaryExpr:
			switch x.Op {
			case "++", "--", "delete":
				addTargets(x.Arg)
			}
		case *ast.ForInStmt:
			if x.Keyword == "" {
				addTargets(x.Left)
			}
		case *ast.NewExpr:
			if id, ok := x.Callee.(*ast.Identifier); ok && pureConstructors[id.Name] {
				break
			}
			if !declInit {
				addArgs(x.Args)
			}
		case *ast.CallExpr:
			m.exposeCall(x, skip, exp, declInit, addRoot, addArgs)
		}
		return true
	})
}

func (m *moduleAnalysis) exposeCall(call *ast.CallExpr, skip map[string]bool, exp *exposure, declInit bool, addRoot func(string), addArgs func([]ast.Expr)) {
	callee := unwrapExpr(call.Callee)
	switch c := callee.(type) {
	case *ast.ArrowFn:
		// An immediately invoked function runs now.
		m.exposeFunction(c.Params, c.Body, skip, exp, declInit)
		addArgs(call.Args)
	case *ast.Identifier:
		if cal := m.callables[c.Name]; cal != nil && !skip[c.Name] {
			exp.add(m.functionExposure(c.Name))
			addArgs(call.Args)
			return
		}
		if pureGlobals[c.Name] || skip[c.Name] {
			return
		}
		if !declInit {
			addArgs(call.Args)
		}
	case *ast.MemberExpr:
		method := memberName(c)
		root := rootIdentifier(c.Object)
		if root != "" && pureGlobals[root] {
			return
		}
		if root == "Object" {
			switch {
			case method == "assign" || method == "defineProperty" || method == "defineProperties" || method == "setPrototypeOf":
				if len(call.Args) > 0 {
					for _, name := range identifiersIn(call.Args[0]) {
						addRoot(name)
					}
				}
			case pureObjectFuncs[method]:
			default:
				if !declInit {
					addArgs(call.Args)
				}
			}
			return
		}
		if pureMethods[method] {
			if syncCallbackMethods[method] {
				for _, a := range call.Args {
					if arrow, ok := a.(*ast.ArrowFn); ok {
						m.exposeFunction(arrow.Params, arrow.Body, skip, exp, declInit)
					}
				}
			}
			return
		}
		addRoot(root)
		if method == "sort" || method == "reverse" {
			if root != "" && !skip[root] {
				exp.inPlace[root] = true
			}
		}
		if !declInit {
			addArgs(call.Args)
		}
	default:
		if !declInit {
			addArgs(call.Args)
		}
	}
}

// exposeFunction records what calling a function literal may change, leaving
// out its own parameters and locals.
func (m *moduleAnalysis) exposeFunction(params []*ast.Param, body []ast.Stmt, skip map[string]bool, exp *exposure, declInit bool) {
	inner := map[string]bool{}
	for k := range skip {
		inner[k] = true
	}
	for _, name := range functionScopeNames(params, body) {
		inner[name] = true
	}
	for _, st := range body {
		m.expose(st, inner, exp, declInit)
	}
}

// functionExposure returns what calling the module function name may change.
func (m *moduleAnalysis) functionExposure(name string) *exposure {
	if e, ok := m.fnExp[name]; ok {
		return e
	}
	out := newExposure()
	if m.active[name] {
		return out
	}
	m.active[name] = true
	cal := m.callables[name]
	m.exposeFunction(cal.params, cal.body, nil, out, false)
	m.active[name] = false
	m.fnExp[name] = out
	return out
}

// Small AST helpers.

func unwrapExpr(e ast.Expr) ast.Expr {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.Expr
		case *ast.TypeAssertion:
			e = x.Expr
		default:
			return e
		}
	}
}

// rootIdentifier returns the name at the base of a member chain (`a.b[c].d`
// is `a`), or "" when the base is not an identifier.
func rootIdentifier(e ast.Expr) string {
	for {
		switch x := unwrapExpr(e).(type) {
		case *ast.Identifier:
			return x.Name
		case *ast.MemberExpr:
			e = x.Object
		default:
			return ""
		}
	}
}

// memberName returns the method name of a member expression, or "".
func memberName(m *ast.MemberExpr) string {
	if !m.Computed {
		if id, ok := m.Property.(*ast.Identifier); ok {
			return id.Name
		}
		return ""
	}
	if lit, ok := m.Property.(*ast.Literal); ok && lit.Kind == ast.StringLit {
		return lit.Value
	}
	return ""
}

// assignTargetRoots returns the binding names an assignment writes: the base
// of a member target, or every name in a destructuring target.
func assignTargetRoots(e ast.Expr) []string {
	switch x := unwrapExpr(e).(type) {
	case *ast.Identifier:
		return []string{x.Name}
	case *ast.MemberExpr:
		if root := rootIdentifier(x); root != "" {
			return []string{root}
		}
	case *ast.ArrayExpr:
		var out []string
		for _, el := range x.Elements {
			if el != nil {
				out = append(out, assignTargetRoots(el)...)
			}
		}
		return out
	case *ast.ObjectExpr:
		var out []string
		for _, p := range x.Properties {
			if p != nil && p.Value != nil {
				out = append(out, assignTargetRoots(p.Value)...)
			}
		}
		return out
	case *ast.BinaryExpr:
		if x.Op == "=" {
			return assignTargetRoots(x.Left)
		}
	case *ast.UnaryExpr:
		if x.Op == "..." {
			return assignTargetRoots(x.Arg)
		}
	}
	return nil
}

// identifiersIn returns the identifier names read by e, leaving out function
// bodies (they run later, if at all).
func identifiersIn(e ast.Expr) []string {
	var out []string
	WalkAST(e, func(n any) bool {
		switch x := n.(type) {
		case *ast.ArrowFn:
			return false
		case *ast.MemberExpr:
			// Only the base of a member chain names a binding.
			if root := rootIdentifier(x); root != "" {
				out = append(out, root)
				if x.Computed {
					out = append(out, identifiersIn(x.Property)...)
				}
				return false
			}
		case *ast.Identifier:
			out = append(out, x.Name)
		}
		return true
	})
	return out
}

// blocked reports whether name is blocked; a nil run blocks nothing.
func (r *ModuleRun) blocked(name string) (string, bool) {
	if r == nil {
		return "", false
	}
	reason, ok := r.Blocked[name]
	return reason, ok
}
