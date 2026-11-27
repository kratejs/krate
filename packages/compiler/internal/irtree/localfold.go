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
	// callables holds local helper functions exposed to the component's const
	// interpreter. They are emitted as runtime functions separately, not as
	// folded values.
	callables map[string]jsvalue.Value
	// destructs lists the destructuring declarations in source order. Their
	// names cannot each be initialized from the whole right-hand side, so a
	// declaration the client must evaluate is replayed as written (see
	// builder.replayedDestructs).
	destructs []*ast.VarDecl
	// jsxInits holds, for each local whose value was built from JSX by
	// statements, the equivalent initializer: an expression with the loop
	// variables and block locals it captured replaced by their values.
	jsxInits map[string]ast.Expr
	// notes lists the statements folding gave up on and why.
	notes []foldNote
}

// collectLocalVarsWithBuilder resolves top-level local variable declarations in a
// component body to build-time constants (derived from props and other
// locals). It folds sequential declarations and simple reassignments of a
// known local (`x = ...`, `x += ...`, `x++`) across declaration, expression,
// block and `if` statements. Any other statement that may change a local (a
// member write, a mutating method call, a branch with an unknown test, a loop)
// drops that local's build-time value, so a read is deferred to hydration
// instead of folding a value the statement would have changed. The result
// resolves SSR initial values and drives the `var x = ...` declarations
// emitted into the hydration bundle.
//
// owner, when set, gives the fold the module helpers it can call.
func collectLocalVarsWithBuilder(body []ast.Stmt, sigMap map[string]ast.Expr, props valueMap, nextUseId func() string, owner *builder) *localFold {
	f := &localFolder{
		res: &localFold{
			locals:    make(valueMap),
			useIds:    make(map[string]string),
			inits:     make(map[string]ast.Expr),
			leaked:    make(map[string]bool),
			changed:   make(map[string]bool),
			callables: make(map[string]jsvalue.Value),
			jsxInits:  make(map[string]ast.Expr),
		},
		jsxLocals: make(map[string]jsvalue.Value),
		working:   make(valueMap, len(props)),
		sigMap:    sigMap,
		nextUseId: nextUseId,
		muts:      newMutationWalker(body),
		owner:     owner,
	}
	for k, v := range props {
		f.working[k] = v
	}
	if owner != nil {
		f.in = owner.constInterp(f.working)
	} else {
		f.in = newConstInterp(sigMap, f.working)
	}
	f.in.Volatile = owner != nil && owner.bakesOnly()
	f.preloadFunctions(body)
	f.stmts(body)
	return f.res
}

// eval evaluates through the folder's persistent interpreter. Local helper
// functions are bound in its scope, so calls to pure local functions fold with
// the same rules as built-ins.
func (f *localFolder) eval(expr ast.Expr) jsvalue.Value {
	if f.in == nil {
		return evalConstValue(expr, f.sigMap, f.working)
	}
	v := f.in.Eval(expr)
	if !v.ContainsUnknown() || f.owner == nil || f.owner.evalJS == nil {
		return v
	}
	saved := f.owner.localProps
	f.owner.localProps = f.working
	defer func() { f.owner.localProps = saved }()
	if v, ok := f.owner.evalPureWithJS(expr); ok {
		return v
	}
	return v
}

// preloadFunctions exposes top-level function declarations and const-arrow
// helpers to the constant interpreter. Functions are not folded themselves;
// calls to them only become known when their body can be evaluated.
func (f *localFolder) preloadFunctions(body []ast.Stmt) {
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			if s.Name != "" {
				fn := f.in.Closure(s.Name, s.Params, s.Body)
				f.in.Scope.Define(s.Name, fn)
				f.res.callables[s.Name] = fn
				f.working[s.Name] = fn
			}
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d == nil || d.Name == "" || d.Init == nil {
					continue
				}
				if arrow, ok := d.Init.(*ast.ArrowFn); ok {
					fn := f.in.Closure(d.Name, arrow.Params, arrow.Body)
					f.in.Scope.Define(d.Name, fn)
					f.res.callables[d.Name] = fn
					f.working[d.Name] = fn
				}
			}
		}
	}
}

// localFolder carries the state of one collectLocalVars walk.
type localFolder struct {
	res       *localFold
	working   valueMap
	sigMap    map[string]ast.Expr
	nextUseId func() string
	muts      *mutationWalker
	in        *jsvalue.Interp
	owner     *builder
	// aliases links locals that may share an object (`const q = p`,
	// `const list = [p]`): a change through one may be visible through the
	// other, so they lose their build-time value together.
	aliases map[string]map[string]bool
	// jsxLocals holds locals whose value contains JSX built by statements
	// (`items.push(<li/>)`). They have no folded value; the interpreter still
	// sees them, and the builder renders them from res.jsxInits.
	jsxLocals map[string]jsvalue.Value
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
			// The shared object changed: every alias sees it.
			for _, alias := range f.aliasGroup(name) {
				f.drop(alias)
			}
			continue
		}
		f.drop(name)
	}
}

func (f *localFolder) drop(name string) {
	if _, isLocal := f.res.locals[name]; !isLocal {
		return
	}
	f.res.locals[name] = jsvalue.Dynamic("local " + name + " changes during render")
	delete(f.working, name)
	f.res.leaked[name] = true
	f.res.changed[name] = true
}

// link records that a and b may share an object.
func (f *localFolder) link(a, b string) {
	if a == b {
		return
	}
	if f.aliases == nil {
		f.aliases = make(map[string]map[string]bool)
	}
	for _, p := range [][2]string{{a, b}, {b, a}} {
		if f.aliases[p[0]] == nil {
			f.aliases[p[0]] = make(map[string]bool)
		}
		f.aliases[p[0]][p[1]] = true
	}
}

// aliasGroup returns name and every local transitively linked to it.
func (f *localFolder) aliasGroup(name string) []string {
	seen := map[string]bool{name: true}
	queue := []string{name}
	for i := 0; i < len(queue); i++ {
		for other := range f.aliases[queue[i]] {
			if !seen[other] {
				seen[other] = true
				queue = append(queue, other)
			}
		}
	}
	return queue
}

// linkReferences links name to every object-valued local its initializer or
// assigned value references.
func (f *localFolder) linkReferences(name string, expr ast.Expr) {
	scanJSIdents(generateExprJS(expr, nil), func(ref string) {
		if _, isLocal := f.res.locals[ref]; !isLocal || ref == name {
			return
		}
		if v, ok := f.working[ref]; ok && isPrimitiveValue(v) {
			return
		}
		f.link(name, ref)
	})
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
			f.run(s)
		}
	case *ast.IfStmt:
		f.taint(f.muts.exprMutations(s.Test))
		test := f.eval(s.Test)
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
	case *ast.ForStmt, *ast.WhileStmt, *ast.DoWhileStmt, *ast.ForInStmt,
		*ast.SwitchStmt, *ast.TryStmt, *ast.LabeledStmt:
		f.run(stmt)
	default:
		f.taint(f.muts.stmtMutations(stmt))
	}
}

func (f *localFolder) decl(decl *ast.VarDecl) {
	if decl.Init != nil {
		if _, isFn := decl.Init.(*ast.ArrowFn); !isFn {
			muts := f.muts.exprMutations(decl.Init)
			if len(muts) > 0 && !decl.IsDestructuring && decl.Name != "" && decl.Name != "children" && f.declare(decl, muts) {
				return
			}
			f.taint(muts)
		}
	}
	if decl.Init == nil {
		return
	}
	if decl.IsDestructuring {
		if call, ok := decl.Init.(*ast.CallExpr); ok {
			if id, ok := call.Callee.(*ast.Identifier); ok && isHookCallee(id.Name) {
				return
			}
		}
		// The destructured `[get, set] = createCSS*(...)` form is compile-time
		// state like the handle form: never a runtime local.
		if isCSSFactoryCall(decl.Init) {
			return
		}
		f.destructuredDecl(decl)
		return
	}
	name := decl.Name
	if name == "" && len(decl.Names) > 0 {
		name = decl.Names[0]
	}
	if name == "" || name == "children" {
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
	f.bindLocal(name, decl.Init, f.eval(decl.Init))
}

// bindLocal records a local declared with init and folded to v.
func (f *localFolder) bindLocal(name string, init ast.Expr, v jsvalue.Value) {
	if !isPrimitiveValue(v) {
		f.linkReferences(name, init)
	}
	if v.ContainsUnknown() || v.IsFunction() || v.IsJSX() || v.ContainsJSXNode() {
		if _, exists := f.res.locals[name]; !exists {
			f.res.locals[name] = jsvalue.Dynamic("local " + name)
			f.res.inits[name] = init
			f.res.leaked[name] = true
			f.record(name)
		}
		return
	}
	f.res.locals[name] = v
	f.res.inits[name] = init
	f.working[name] = v
	f.record(name)
}

// destructuredDecl folds object/array destructuring by evaluating the
// initializer once and binding the parser's pattern through the JS interpreter.
// This keeps `const { name } = user` and `const [first] = items` available to
// later JSX reads.
func (f *localFolder) destructuredDecl(decl *ast.VarDecl) {
	f.res.destructs = append(f.res.destructs, decl)
	v := f.eval(decl.Init)
	names := decl.Names
	leak := func(name string) {
		if name == "" || name == "children" {
			return
		}
		f.res.locals[name] = jsvalue.Dynamic("local " + name)
		f.res.inits[name] = decl.Init
		f.res.leaked[name] = true
		f.record(name)
	}
	if v.ContainsUnknown() || v.IsFunction() || v.IsJSX() {
		for _, name := range names {
			if _, exists := f.res.locals[name]; !exists {
				leak(name)
			}
		}
		return
	}
	scope := jsvalue.NewScope(nil)
	saved := f.in.Scope
	f.in.Scope = scope
	result := f.in.BindPattern(scope, decl.Name, decl.Pattern, v)
	f.in.Scope = saved
	if result.IsUnsupported() {
		for _, name := range names {
			if _, exists := f.res.locals[name]; !exists {
				leak(name)
			}
		}
		return
	}
	for _, name := range names {
		if name == "" || name == "children" {
			continue
		}
		val, ok := scope.Lookup(name)
		if !ok || val.ContainsUnknown() || val.IsFunction() || val.IsJSX() {
			leak(name)
			continue
		}
		f.res.locals[name] = val
		f.res.inits[name] = decl.Init
		f.working[name] = val
		f.record(name)
	}
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
	rhs := f.eval(bin.Right)
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
	if !isPrimitiveValue(next) {
		f.linkReferences(id.Name, bin.Right)
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
