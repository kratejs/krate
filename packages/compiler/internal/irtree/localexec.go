package irtree

import (
	"sort"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// Statements that build up a value (loops, switch, try, labelled blocks and
// calls that change a local) run through the jsvalue interpreter, which has
// real JS semantics: break, continue, labels, return, nested loops, switch,
// closures, Map/Set and in-place array methods.
//
// Isolation. Build-time values can be shared: a module constant or a prop
// passed from a module constant is one value that every page and component
// reads. JS would let `list.push(x)` change it for everyone; a static build
// must not, because the result would depend on the order pages are built.
// A statement therefore runs on a deep copy of every component local it can
// see and of every other compound value it names, taken with one identity
// memo so two names for the same object stay one object. Nothing is written
// back unless the statement finished; the copy of a local replaces it after
// that. Module values are never changed.

// foldLoopLimit bounds the loop iterations folding runs for one component.
const foldLoopLimit = 100_000

// foldNote records a statement folding gave up on for a reason the author can
// act on (an unsupported construct or a limit), not because a value depends
// on the browser.
type foldNote struct {
	Pos    ast.Pos
	Stmt   string
	Reason string
}

// run executes stmt on private copies and applies its effects, or leaves the
// component state alone and drops what the statement may have changed.
func (f *localFolder) run(stmt ast.Stmt) {
	muts := f.muts.stmtMutations(stmt)
	if len(muts) == 0 {
		return
	}
	if v, applied, _ := f.exec(stmt, muts, nil); !applied {
		f.noteFailure(stmt, v)
		f.taint(muts)
	}
}

// declare runs a declaration whose initializer changes something
// (`const first = list.shift()`) the same way. It reports whether the
// declaration was folded; the caller falls back to dropping what the
// initializer changes otherwise.
func (f *localFolder) declare(decl *ast.VarDecl, muts mutationSet) bool {
	stmt := &ast.VarStmt{Kind: ast.VarLet, Decls: []*ast.VarDecl{decl}}
	failure, applied, declared := f.exec(stmt, muts, []string{decl.Name})
	if !applied {
		f.noteFailure(stmt, failure)
		return false
	}
	f.bindLocal(decl.Name, decl.Init, declared[decl.Name])
	return true
}

// exec runs stmt in the interpreter's root scope with copies of the locals
// bound there. applied reports whether the statement finished and its
// effects were written back.
func (f *localFolder) exec(stmt ast.Stmt, muts mutationSet, declared []string) (failure jsvalue.Value, applied bool, vals map[string]jsvalue.Value) {
	in := f.in
	if in == nil || in.Scope == nil {
		return jsvalue.Dynamic("no interpreter"), false, nil
	}
	root := in.Scope
	cloner := jsvalue.NewCloner()

	type binding struct {
		prev jsvalue.Value
		had  bool
	}
	defined := make(map[string]binding)
	define := func(name string, v jsvalue.Value) {
		if _, done := defined[name]; !done {
			prev, had := rootOnly(root, name)
			defined[name] = binding{prev, had}
		}
		root.Define(name, v)
	}
	defer func() {
		for name, b := range defined {
			if b.had {
				root.Define(name, b.prev)
			} else {
				root.Remove(name)
			}
		}
	}()

	// Locals first: every one is copied, so aliases among them are kept.
	locals := make(map[string]bool)
	for name := range f.res.locals {
		if v, ok := f.working[name]; ok && !v.IsFunction() && !v.IsUnknown() {
			define(name, cloner.Clone(v))
			locals[name] = true
		} else if v, ok := f.jsxLocals[name]; ok {
			define(name, cloner.Clone(v))
			locals[name] = true
		}
	}
	// Other names the statement mentions: props, reassigned parameters and
	// module values it can reach.
	names := make(map[string]bool)
	WalkAST(stmt, func(n any) bool {
		switch x := n.(type) {
		case *ast.Identifier:
			if !locals[x.Name] {
				names[x.Name] = true
			}
		case *ast.MemberExpr:
			// props.name reads the prop through the interpreter's hook.
			if id, ok := x.Object.(*ast.Identifier); ok && id.Name == "props" && !x.Computed {
				if p, ok := x.Property.(*ast.Identifier); ok {
					names[p.Name] = true
				}
			}
		}
		return true
	})
	mentioned := make(map[string]bool)
	originals := make(map[string]jsvalue.Value)
	swapped := make(map[string]jsvalue.Value)
	for _, name := range keysOf(names) {
		if name == "props" {
			continue
		}
		if _, isSignal := f.sigMap[name]; isSignal {
			continue
		}
		if _, isFn := f.res.callables[name]; isFn {
			continue
		}
		v, inWorking := f.working[name]
		ok := inWorking
		if !ok && in.Hooks.Ident != nil {
			v, ok = in.Hooks.Ident(name)
		}
		if !ok || v.IsFunction() || v.IsUnknown() {
			continue
		}
		dup := cloner.Clone(v)
		define(name, dup)
		mentioned[name] = true
		originals[name] = v
		if inWorking {
			swapped[name] = v
			f.working[name] = dup
		}
	}

	for _, name := range declared {
		if _, done := defined[name]; !done {
			defined[name] = binding{}
		}
	}
	savedHook := in.Hooks.JSX
	savedInPlace, savedStrict, savedLimit, savedCap := in.InPlace, in.StrictStatements, in.LoopLimit, in.LoopCap
	in.Hooks.JSX = func(expr ast.Expr) jsvalue.Value { return f.jsxValue(expr, root, locals, mentioned) }
	in.InPlace, in.StrictStatements, in.LoopLimit, in.LoopCap = true, true, foldLoopLimit, foldLoopLimit
	result, fl := in.ExecTop(stmt)
	in.Hooks.JSX = savedHook
	in.InPlace, in.StrictStatements, in.LoopLimit, in.LoopCap = savedInPlace, savedStrict, savedLimit, savedCap
	for name, v := range swapped {
		f.working[name] = v
	}
	if fl == jsvalue.FlowFailed {
		return result, false, nil
	}
	if fl == jsvalue.FlowReturn {
		// The component's own return is built from its JSX, not from here: a
		// return inside a loop is left to the browser.
		return jsvalue.Dynamic("return inside a statement"), false, nil
	}

	vals = make(map[string]jsvalue.Value, len(declared))
	for _, name := range declared {
		vals[name], _ = root.Lookup(name)
	}
	// Read the copies back.
	next := make(map[string]jsvalue.Value, len(defined))
	for name := range defined {
		if v, ok := root.Lookup(name); ok {
			next[name] = v
		}
	}
	for _, name := range keysOf(locals) {
		f.applyLocal(name, next[name])
	}
	for _, name := range keysOf(mentioned) {
		v := next[name]
		if v.IsUnknown() || v.IsFunction() || v.ContainsUnknown() || v.ContainsJSXNode() {
			continue
		}
		// A name not listed as changed may still hold an object that
		// another name (a loop variable, an alias) changed.
		if _, mutated := muts[name]; mutated || !jsvalue.DeepEqual(originals[name], v) {
			f.set(name, v)
		}
	}
	// Other names that held an object the statement copied see the copy.
	for _, name := range f.otherNames(locals, mentioned) {
		cur, ok := f.working[name]
		if !ok && f.owner != nil {
			cur, ok = f.owner.moduleConsts[name]
		}
		if !ok {
			continue
		}
		if dup, ok := cloner.Copy(cur); ok {
			f.set(name, dup)
		}
	}
	return result, true, vals
}

// otherNames lists the props and module constants that are not part of the
// statement's own copies, in a stable order.
func (f *localFolder) otherNames(locals, mentioned map[string]bool) []string {
	seen := make(map[string]bool)
	for name := range f.working {
		if !locals[name] && !mentioned[name] {
			seen[name] = true
		}
	}
	if f.owner != nil {
		for name := range f.owner.moduleConsts {
			if !locals[name] && !mentioned[name] {
				seen[name] = true
			}
		}
	}
	return keysOf(seen)
}

// rootOnly reads a binding made directly in scope s.
func rootOnly(s *jsvalue.Scope, name string) (jsvalue.Value, bool) {
	v, owner, ok := s.LookupScope(name)
	if !ok || owner != s {
		return jsvalue.Value{}, false
	}
	return v, true
}

// applyLocal records the value a local has after a statement ran.
func (f *localFolder) applyLocal(name string, v jsvalue.Value) {
	old, hadOld := f.working[name]
	switch {
	case v.IsUnknown() || v.IsFunction() || v.ContainsUnknown():
		delete(f.jsxLocals, name)
		delete(f.res.jsxInits, name)
		f.drop(name)
	case v.ContainsJSXNode():
		expr := jsvalue.ToAST(v)
		if expr == nil {
			delete(f.jsxLocals, name)
			delete(f.res.jsxInits, name)
			f.drop(name)
			return
		}
		f.jsxLocals[name] = v
		f.res.jsxInits[name] = expr
		f.drop(name)
	default:
		if hadOld && isPrimitiveValue(v) && isPrimitiveValue(old) && jsvalue.StrictEquals(old, v) {
			return
		}
		delete(f.jsxLocals, name)
		delete(f.res.jsxInits, name)
		f.set(name, v)
	}
}

// jsxValue turns a JSX expression met while a statement runs into a value.
// Names bound by the statement (loop variables, block locals) and the
// component locals it changed are replaced by what they hold now, as JS
// evaluates the element at that point. Everything else (props, module
// values, signals, component functions) stays a name for the builder.
func (f *localFolder) jsxValue(expr ast.Expr, root *jsvalue.Scope, locals, mentioned map[string]bool) jsvalue.Value {
	seen := make(map[string]bool)
	var names []string
	WalkAST(expr, func(n any) bool {
		if id, ok := n.(*ast.Identifier); ok && !seen[id.Name] {
			seen[id.Name] = true
			names = append(names, id.Name)
		}
		return true
	})
	out := expr
	for _, name := range names {
		v, owner, ok := f.in.Scope.LookupScope(name)
		if !ok {
			continue
		}
		if owner == root && !locals[name] && !mentioned[name] {
			continue
		}
		if v.IsUnknown() {
			return jsvalue.Dynamic("JSX uses " + name + ", which is not known at build time")
		}
		if v.IsFunction() {
			if owner == root {
				continue
			}
			return jsvalue.Unsupported("JSX uses " + name + ", a function created inside the statement")
		}
		lit := jsvalue.ToAST(v)
		if lit == nil {
			return jsvalue.Unsupported("JSX uses " + name + ", which has no source form")
		}
		sub := &listParamSubstituter{param: name, elem: lit}
		out = sub.sub(out)
	}
	return jsvalue.JSXFromNode(out)
}

// noteFailure records why folding stopped, when the cause is something the
// author can change. A value that only the browser knows (a signal, a prop
// the build does not have) is the normal reason a local stays unfolded and
// is not worth a message.
func (f *localFolder) noteFailure(stmt ast.Stmt, v jsvalue.Value) {
	if !v.IsUnsupported() || v.Thrown() != nil {
		return
	}
	kind, ok := stmtKind(stmt)
	if !ok {
		return
	}
	f.res.notes = append(f.res.notes, foldNote{Pos: stmt.Pos(), Stmt: kind, Reason: v.Reason()})
}

func stmtKind(stmt ast.Stmt) (string, bool) {
	switch s := stmt.(type) {
	case *ast.ForStmt:
		return "for loop", true
	case *ast.ForInStmt:
		if s.IsForOf {
			return "for...of loop", true
		}
		return "for...in loop", true
	case *ast.WhileStmt:
		return "while loop", true
	case *ast.DoWhileStmt:
		return "do...while loop", true
	case *ast.SwitchStmt:
		return "switch statement", true
	case *ast.TryStmt:
		return "try statement", true
	case *ast.LabeledStmt:
		return "labelled statement", true
	case *ast.ExprStmt:
		return "statement", true
	}
	return "", false
}

func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// reportFoldNotes turns what folding gave up on into warnings.
func (b *builder) reportFoldNotes(fold *localFold) {
	for _, n := range fold.notes {
		b.warnCompat(n.Pos, diag.CodeFoldGaveUp,
			"build-time evaluation stopped at this "+n.Stmt+": "+n.Reason,
			"the value it builds is computed in the browser instead of being rendered into the page; rewrite it with plain "+
				"loops, array methods, Map/Set and switch over props, locals and module constants, or keep it under the "+
				"iteration limit of "+itoa(foldLoopLimit))
	}
}
