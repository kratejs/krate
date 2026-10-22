package irtree

import (
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// localFold is the result of folding a component body's locals.
type localFold struct {
	// locals maps each local to its folded value (Unknown when it cannot be
	// resolved at build time).
	locals valueMap
	// useIds maps useId() locals to their per-instance literal.
	useIds map[string]string
	// inits holds each local's declaration initializer.
	inits map[string]ast.Expr
	// order lists locals in declaration order.
	order []string
	// leaked marks locals with no build-time value.
	leaked map[string]bool
	// changed marks locals (and reassigned parameters) whose value changes
	// after their declaration: a reassignment, update or content mutation.
	// Their initializer alone does not describe the value a later read sees,
	// so it must not be re-folded, and the folded value (or, when leaked, the
	// absence of one) replaces any same-named prop binding.
	changed map[string]bool
}

// collectLocalVars resolves top-level local variable declarations in a
// component body to build-time constants (derived from props and other
// locals). It folds sequential declarations and simple reassignments of a
// known local (`x = ...`, `x += ...`, `x++`) across declaration, expression,
// block and `if` statements. Any other statement that may change a local (a
// member write, a mutating method call, a branch with an unknown test, a loop)
// drops that local's build-time value, so a read is deferred to hydration
// instead of folding a value the statement would have changed. The result
// resolves SSR initial values and drives the `var x = ...` declarations
// emitted into the hydration bundle.
func collectLocalVars(body []ast.Stmt, sigMap map[string]ast.Expr, props valueMap, nextUseId func() string) *localFold {
	f := &localFolder{
		res: &localFold{
			locals:  make(valueMap),
			useIds:  make(map[string]string),
			inits:   make(map[string]ast.Expr),
			leaked:  make(map[string]bool),
			changed: make(map[string]bool),
		},
		working:   make(valueMap, len(props)),
		sigMap:    sigMap,
		nextUseId: nextUseId,
		muts:      newMutationWalker(body),
	}
	for k, v := range props {
		f.working[k] = v
	}
	f.stmts(body)
	return f.res
}

// localFolder carries the state of one collectLocalVars walk.
type localFolder struct {
	res       *localFold
	working   valueMap
	sigMap    map[string]ast.Expr
	nextUseId func() string
	muts      *mutationWalker
}

func (f *localFolder) record(name string) {
	for _, n := range f.res.order {
		if n == name {
			return
		}
	}
	f.res.order = append(f.res.order, name)
}

// taint drops the build-time value of every component local in ms. A content
// mutation cannot change a primitive, so primitive locals keep their value.
func (f *localFolder) taint(ms mutationSet) {
	for name, kind := range ms {
		if _, isLocal := f.res.locals[name]; !isLocal {
			continue
		}
		if kind == mutContents {
			if v, ok := f.working[name]; ok && isPrimitiveValue(v) {
				continue
			}
		}
		f.res.locals[name] = jsvalue.Dynamic("local " + name + " changes during render")
		delete(f.working, name)
		f.res.leaked[name] = true
		f.res.changed[name] = true
	}
}

func (f *localFolder) stmts(stmts []ast.Stmt) {
	for _, stmt := range stmts {
		f.stmt(stmt)
	}
}

func (f *localFolder) stmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.VarStmt:
		for _, decl := range s.Decls {
			f.decl(decl)
		}
	case *ast.ExprStmt:
		if !f.assign(s.Expression) {
			f.taint(f.muts.exprMutations(s.Expression))
		}
	case *ast.IfStmt:
		f.taint(f.muts.exprMutations(s.Test))
		test := evalConstValue(s.Test, f.sigMap, f.working)
		if truthy, known := test.Truthy(); known {
			if truthy {
				f.stmts(s.Consequent)
			} else {
				f.stmts(s.Alternate)
			}
			return
		}
		f.taint(f.muts.stmtMutations(s))
	case *ast.BlockStmt:
		f.stmts(s.Body)
	case *ast.FnDecl, *ast.ReturnStmt:
		// Function declarations run later; the render return is the read.
	default:
		f.taint(f.muts.stmtMutations(stmt))
	}
}

func (f *localFolder) decl(decl *ast.VarDecl) {
	if decl.Init != nil {
		if _, isFn := decl.Init.(*ast.ArrowFn); !isFn {
			f.taint(f.muts.exprMutations(decl.Init))
		}
	}
	name := decl.Name
	if name == "" && len(decl.Names) > 0 {
		name = decl.Names[0]
	}
	if name == "" || name == "children" || decl.IsDestructuring || decl.Init == nil {
		return
	}
	// A first-class scope handle is compile-time state, never a runtime local.
	// (A `Ctx.useContext()` read that is NOT a scope handle still folds to the
	// context default here.)
	if isCSSFactoryCall(decl.Init) {
		return
	}
	// useId() resolves to a per-instance build-time literal so SSR and
	// hydration agree without runtime state.
	if isUseIdCall(decl.Init) {
		idLit := ""
		if f.nextUseId != nil {
			idLit = f.nextUseId()
		}
		f.res.locals[name] = jsvalue.String(idLit)
		f.working[name] = jsvalue.String(idLit)
		f.res.useIds[name] = idLit
		f.record(name)
		return
	}
	// A local whose initializer can't be folded (it references an unknown
	// value, e.g. `tocItems.length` when tocItems is not statically known) is
	// still RECORDED so it is emitted as a runtime read: bindings and handlers
	// may reference it. Its folded value stays Unknown and it is kept out of
	// `working` so dependents also emit as runtime reads. Functions and JSX
	// have no literal form and stay runtime too.
	v := evalConstValue(decl.Init, f.sigMap, f.working)
	if v.ContainsUnknown() || v.IsFunction() || v.IsJSX() {
		if _, exists := f.res.locals[name]; !exists {
			f.res.locals[name] = jsvalue.Dynamic("local " + name)
			f.res.inits[name] = decl.Init
			f.res.leaked[name] = true
			f.record(name)
		}
		return
	}
	f.res.locals[name] = v
	f.res.inits[name] = decl.Init
	f.working[name] = v
	f.record(name)
}

// assign folds a simple reassignment of a known local (`x = 1`, `x += 2`,
// `x++`) and reports whether it did. Anything else is left to the caller,
// which drops the build-time value of whatever the expression may change.
func (f *localFolder) assign(expr ast.Expr) bool {
	if u, ok := expr.(*ast.UnaryExpr); ok && (u.Op == "++" || u.Op == "--") {
		id, ok := u.Arg.(*ast.Identifier)
		if !ok || !isKnownLocal(id.Name, f.res.locals) {
			return false
		}
		cur, exists := f.working[id.Name]
		if !exists || cur.IsUnknown() {
			return false
		}
		next := jsvalue.BinaryOp(u.Op[:1], jsvalue.Number(cur.ToNumber()), jsvalue.Number(1))
		if next.IsUnknown() {
			return false
		}
		f.set(id.Name, next)
		return true
	}
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok {
		return false
	}
	id, ok := bin.Left.(*ast.Identifier)
	if !ok {
		return false
	}
	// A reassigned parameter (in working but not a declared local) becomes a
	// folded local when its new value is known.
	cur, exists := f.working[id.Name]
	if !exists && !isKnownLocal(id.Name, f.res.locals) {
		return false
	}
	if len(f.muts.exprMutations(bin.Right)) > 0 {
		return false
	}
	rhs := evalConstValue(bin.Right, f.sigMap, f.working)
	if rhs.ContainsUnknown() {
		return false
	}
	var next jsvalue.Value
	switch bin.Op {
	case "=":
		next = rhs
	case "+=", "-=", "*=", "/=", "%=":
		if !exists || cur.IsUnknown() {
			return false
		}
		next = jsvalue.BinaryOp(bin.Op[:1], cur, rhs)
		if next.IsUnknown() {
			return false
		}
	default:
		return false
	}
	f.set(id.Name, next)
	return true
}

func (f *localFolder) set(name string, v jsvalue.Value) {
	f.res.locals[name] = v
	f.working[name] = v
	f.res.changed[name] = true
}

func isKnownLocal(name string, locals valueMap) bool {
	_, ok := locals[name]
	return ok
}
