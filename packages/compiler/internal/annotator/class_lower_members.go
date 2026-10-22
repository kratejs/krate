package annotator

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// classMemberLocal is the local binding used for a lowered class method,
// accessor, or instance field. Class lowering turns `this.foo` into this
// identifier so rendered JSX and handlers close over ordinary locals.
// Private names (#foo) get their own prefix so they cannot collide with a
// public member of a similar spelling.
func classMemberLocal(name string) string {
	var b strings.Builder
	b.WriteString("__krate_member_")
	if rest, ok := strings.CutPrefix(name, "#"); ok {
		b.WriteString("priv_")
		name = rest
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// classAccessorLocal names the local function lowered from a getter ("get")
// or setter ("set").
func classAccessorLocal(kind, name string) string {
	return classMemberLocal(name) + "__" + kind
}

// instanceLifecycles maps the React instance lifecycle methods Krate lowers to
// the ClassSpec key the runtime calls them by, and the arity they receive.
var instanceLifecycles = map[string]struct {
	key   string
	arity int
}{
	"componentDidMount":                {"componentDidMount", 0},
	"componentDidUpdate":               {"componentDidUpdate", 3},
	"componentWillUnmount":             {"componentWillUnmount", 0},
	"shouldComponentUpdate":            {"shouldComponentUpdate", 3},
	"getSnapshotBeforeUpdate":          {"getSnapshotBeforeUpdate", 2},
	"componentDidCatch":                {"componentDidCatch", 2},
	"componentWillReceiveProps":        {"componentWillReceiveProps", 2},
	"UNSAFE_componentWillReceiveProps": {"componentWillReceiveProps", 2},
	"componentWillUpdate":              {"componentWillUpdate", 3},
	"UNSAFE_componentWillUpdate":       {"componentWillUpdate", 3},
}

// classModel is the analysis of one React class component: what each member
// lowers to, and the first construct that blocks lowering.
type classModel struct {
	class *ast.ClassDecl
	name  string
	pure  bool

	render *ast.ClassMember
	ctor   *ast.ClassMember
	// lifecycles holds instance lifecycle methods (or arrow fields), keyed by
	// source name.
	lifecycles map[string]*ast.ClassMember
	// Static members with React meaning.
	gdsfp, gdsfe *ast.ClassMember
	defaultProps ast.Expr
	contextType  ast.Expr
	displayName  ast.Expr

	methods []*ast.ClassMember
	fields  []*ast.ClassMember
	getters map[string]*ast.ClassMember
	setters map[string]*ast.ClassMember
	// members are the instance names lowered to locals: methods, fields and
	// every `this.x = ...` target.
	members map[string]bool
	// renderMembers are methods and getters reached from render(); they read
	// the committed state signal so the build can fold their output.
	renderMembers map[string]bool

	stateInit ast.Expr
	// ctorProps is the constructor's props parameter name when it is not `props`.
	ctorProps string

	issue *classIssue
	// notes are non-blocking diagnostics (lowered with a caveat).
	notes []classIssue
}

// classIssue explains why (or with what caveat) a React class component is
// lowered: the offending member, where it is, and what to do instead.
type classIssue struct {
	code   diag.Code
	member string
	pos    ast.Pos
	reason string
	hint   string
}

func (m *classModel) isBoundary() bool {
	return m.gdsfe != nil || m.lifecycles["componentDidCatch"] != nil
}

func (m *classModel) fail(code diag.Code, member string, pos ast.Pos, reason, hint string) {
	if m.issue == nil {
		m.issue = &classIssue{code: code, member: member, pos: pos, reason: reason, hint: hint}
	}
}

// analyzeClass classifies every member of a React class component.
func analyzeClass(class *ast.ClassDecl) *classModel {
	m := &classModel{
		class:         class,
		name:          class.Name,
		pure:          isPureBase(class.SuperClass),
		lifecycles:    map[string]*ast.ClassMember{},
		getters:       map[string]*ast.ClassMember{},
		setters:       map[string]*ast.ClassMember{},
		members:       map[string]bool{},
		renderMembers: map[string]bool{},
	}
	for _, mem := range class.Members {
		if mem == nil {
			continue
		}
		m.classifyMember(mem)
	}
	if m.render == nil && m.issue == nil {
		m.fail(diag.CodeClassNoRender, "", class.Position, "it has no render() method",
			"add a render() method returning JSX, or convert it to a function component.")
	}
	if m.issue != nil {
		return m
	}
	if m.ctor != nil {
		m.analyzeConstructor()
	}
	// Every `this.x = ...` in any member body creates an instance field.
	for _, body := range m.bodies() {
		collectThisWrites(body, m.members)
	}
	for name := range m.getters {
		delete(m.members, name)
	}
	for name := range m.setters {
		delete(m.members, name)
	}
	m.findRenderMembers()
	m.checkThisReads()
	m.checkStatics()
	if m.lifecycles["componentDidCatch"] != nil && m.gdsfe == nil {
		mem := m.lifecycles["componentDidCatch"]
		m.notes = append(m.notes, classIssue{code: diag.CodeBoundaryNoDerivedState, member: "componentDidCatch", pos: mem.Position,
			reason: "componentDidCatch without static getDerivedStateFromError",
			hint:   "add `static getDerivedStateFromError(error) { return { hasError: true }; }` so the boundary renders a fallback; until componentDidCatch calls setState the failed subtree stays unmounted with its last DOM."})
	}
	return m
}

func isPureBase(super ast.Expr) bool {
	switch s := super.(type) {
	case *ast.Identifier:
		return s.Name == "PureComponent"
	case *ast.MemberExpr:
		if p, ok := s.Property.(*ast.Identifier); ok {
			return p.Name == "PureComponent"
		}
	}
	return false
}

func (m *classModel) classifyMember(mem *ast.ClassMember) {
	if mem.Computed {
		m.fail(diag.CodeClassComputedMember, "["+exprLabel(mem.KeyExpr)+"]", mem.Position,
			"computed member names are not supported",
			"give the member a plain name, or move the dynamic lookup into a helper method.")
		return
	}
	if mem.Kind == ast.ClassStaticBlock {
		m.fail(diag.CodeClassStaticMember, "static {}", mem.Position, "static initialization blocks are not supported",
			"move the initialization to module scope next to the component.")
		return
	}
	if mem.Kind == ast.ClassConstructor {
		m.ctor = mem
		return
	}
	if mem.Static {
		m.classifyStatic(mem)
		return
	}
	switch mem.Kind {
	case ast.ClassMethod:
		m.classifyMethod(mem, mem.Name)
	case ast.ClassField, ast.ClassAccessor:
		if mem.Name == "state" && mem.Kind == ast.ClassField {
			m.stateInit = mem.Init
			return
		}
		if fn, ok := mem.Init.(*ast.ArrowFn); ok && (mem.Name == "render" || instanceLifecycleName(mem.Name)) {
			// `componentDidMount = () => {...}` is called by React like a method.
			lifted := &ast.ClassMember{Position: mem.Position, Kind: ast.ClassMethod, Name: mem.Name,
				Async: fn.Async, Params: fn.Params, Body: arrowBody(fn)}
			m.classifyMethod(lifted, mem.Name)
			return
		}
		if isLegacyUnsafeMount(mem.Name) {
			m.classifyMethod(mem, mem.Name)
			return
		}
		m.fields = append(m.fields, mem)
		m.members[mem.Name] = true
	case ast.ClassGetter:
		m.getters[mem.Name] = mem
	case ast.ClassSetter:
		m.setters[mem.Name] = mem
	}
}

func instanceLifecycleName(name string) bool {
	_, ok := instanceLifecycles[name]
	return ok
}

func isLegacyUnsafeMount(name string) bool {
	return name == "componentWillMount" || name == "UNSAFE_componentWillMount"
}

func (m *classModel) classifyMethod(mem *ast.ClassMember, name string) {
	switch {
	case name == "render":
		m.render = mem
	case isLegacyUnsafeMount(name):
		m.fail(diag.CodeClassWillMount, name, mem.Position, name+" is not supported",
			"initialize state in the constructor (or a `state = {...}` field) and move side effects to componentDidMount.")
	case instanceLifecycleName(name):
		m.lifecycles[name] = mem
	default:
		m.methods = append(m.methods, mem)
		m.members[name] = true
	}
}

func (m *classModel) classifyStatic(mem *ast.ClassMember) {
	switch mem.Kind {
	case ast.ClassField:
		switch mem.Name {
		case "defaultProps":
			m.defaultProps = mem.Init
			return
		case "propTypes":
			return
		case "displayName":
			m.displayName = mem.Init
			return
		case "contextType":
			m.contextType = mem.Init
			return
		case "contextTypes", "childContextTypes":
			m.fail(diag.CodeClassLegacyContext, "static "+mem.Name, mem.Position, "legacy context (static "+mem.Name+") is not supported",
				"use `static contextType = MyContext` and read this.context, or a function component with useContext.")
			return
		case "getDerivedStateFromProps", "getDerivedStateFromError":
			if fn, ok := mem.Init.(*ast.ArrowFn); ok {
				lifted := &ast.ClassMember{Position: mem.Position, Kind: ast.ClassMethod, Name: mem.Name, Static: true,
					Params: fn.Params, Body: arrowBody(fn)}
				m.classifyStatic(lifted)
				return
			}
		}
	case ast.ClassMethod:
		switch mem.Name {
		case "getDerivedStateFromProps":
			m.gdsfp = mem
			return
		case "getDerivedStateFromError":
			m.gdsfe = mem
			return
		}
	}
	m.fail(diag.CodeClassStaticMember, "static "+mem.Name, mem.Position, "static member "+mem.Name+" is not supported",
		"move it to a module-level constant or function next to the component; only defaultProps, propTypes, displayName, contextType, getDerivedStateFromProps and getDerivedStateFromError are lowered.")
}

// arrowBody returns an arrow function's statements, turning an expression body
// into a return.
func arrowBody(fn *ast.ArrowFn) []ast.Stmt {
	if fn.Expression && len(fn.Body) == 1 {
		if es, ok := fn.Body[0].(*ast.ExprStmt); ok {
			return []ast.Stmt{&ast.ReturnStmt{Position: es.Position, Value: es.Expression}}
		}
	}
	return fn.Body
}

// analyzeConstructor extracts `this.state = ...` and the props parameter name.
func (m *classModel) analyzeConstructor() {
	if len(m.ctor.Params) > 0 && m.ctor.Params[0] != nil {
		if n := m.ctor.Params[0].Name; n != "" && n != "props" && m.ctor.Params[0].Pattern == "" {
			m.ctorProps = n
		}
	}
	if m.stateInit != nil {
		return
	}
	for _, stmt := range m.ctor.Body {
		if init := thisStateAssignment(stmt); init != nil {
			m.stateInit = init
		}
	}
}

// thisStateAssignment returns x for a top-level `this.state = x;`.
func thisStateAssignment(stmt ast.Stmt) ast.Expr {
	es, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return nil
	}
	assign, ok := es.Expression.(*ast.BinaryExpr)
	if !ok || assign.Op != "=" {
		return nil
	}
	if name, _, ok := thisMember(assign.Left); ok && name == "state" {
		return assign.Right
	}
	return nil
}

// bodies returns every member body that is lowered (instance code).
func (m *classModel) bodies() [][]ast.Stmt {
	var out [][]ast.Stmt
	if m.render != nil {
		out = append(out, m.render.Body)
	}
	if m.ctor != nil {
		out = append(out, m.ctor.Body)
	}
	for _, mem := range m.lifecycles {
		out = append(out, mem.Body)
	}
	for _, mem := range m.methods {
		out = append(out, mem.Body)
	}
	for _, mem := range m.getters {
		out = append(out, mem.Body)
	}
	for _, mem := range m.setters {
		out = append(out, mem.Body)
	}
	for _, mem := range m.fields {
		if mem.Init != nil {
			out = append(out, []ast.Stmt{&ast.ExprStmt{Expression: mem.Init}})
		}
	}
	return out
}

// collectThisWrites adds every `this.x` assignment or update target to members.
func collectThisWrites(stmts []ast.Stmt, members map[string]bool) {
	inspectStmts(stmts, func(e ast.Expr) bool {
		if isOwnThisFunction(e) {
			return false
		}
		switch x := e.(type) {
		case *ast.BinaryExpr:
			if isAssignOp(x.Op) {
				if name, _, ok := thisMember(x.Left); ok && !reservedThis(name) {
					members[name] = true
				}
			}
		case *ast.UnaryExpr:
			if x.Op == "++" || x.Op == "--" {
				if name, _, ok := thisMember(x.Arg); ok && !reservedThis(name) {
					members[name] = true
				}
			}
		}
		return true
	})
}

func isAssignOp(op string) bool {
	switch op {
	case "=", "+=", "-=", "*=", "/=", "%=", "**=", "<<=", ">>=", ">>>=", "&=", "|=", "^=", "&&=", "||=", "??=":
		return true
	}
	return false
}

// reservedThis are instance properties the runtime instance provides.
func reservedThis(name string) bool {
	switch name {
	case "props", "state", "context", "setState", "forceUpdate":
		return true
	}
	return false
}

// findRenderMembers marks the methods called and getters read from render(),
// transitively.
func (m *classModel) findRenderMembers() {
	methods := map[string]*ast.ClassMember{}
	for _, mem := range m.methods {
		methods[mem.Name] = mem
	}
	var visit func([]ast.Stmt)
	visit = func(body []ast.Stmt) {
		inspectStmts(body, func(e ast.Expr) bool {
			if isOwnThisFunction(e) {
				return false
			}
			switch x := e.(type) {
			case *ast.CallExpr:
				if name, _, ok := thisMember(x.Callee); ok {
					if mem := methods[name]; mem != nil && !m.renderMembers[name] {
						m.renderMembers[name] = true
						visit(mem.Body)
					}
				}
			case *ast.MemberExpr:
				if name, _, ok := thisMember(x); ok {
					if g := m.getters[name]; g != nil && !m.renderMembers[name] {
						m.renderMembers[name] = true
						visit(g.Body)
					}
				}
			}
			return true
		})
	}
	if m.render != nil {
		visit(m.render.Body)
	}
}

// checkThisReads reports the first `this.x` read that is neither a member nor
// an instance property the runtime provides (this.refs, this.isMounted, ...).
func (m *classModel) checkThisReads() {
	for _, body := range m.bodies() {
		var found *ast.MemberExpr
		inspectStmts(body, func(e ast.Expr) bool {
			if found != nil || isOwnThisFunction(e) {
				return false
			}
			if name, mem, ok := thisMember(e); ok {
				if !reservedThis(name) && !m.members[name] && m.getters[name] == nil && m.setters[name] == nil {
					found = mem
				}
				return false
			}
			return true
		})
		if found != nil {
			name := found.Property.(*ast.Identifier).Name
			hint := "declare " + name + " as a class field or method, or read it from this.props/this.state."
			if name == "refs" {
				hint = "string refs are not supported; use createRef() in a field (`input = createRef()`) and ref={this.input}."
			}
			m.fail(diag.CodeClassUnknownThis, "this."+name, found.Position,
				"this."+name+" is not a field, method, props, state, context, setState or forceUpdate", hint)
			return
		}
	}
}

// checkStatics reports `this` inside static methods: there it is the class,
// which no longer exists after lowering.
func (m *classModel) checkStatics() {
	for _, mem := range []*ast.ClassMember{m.gdsfp, m.gdsfe} {
		if mem == nil {
			continue
		}
		var found ast.Expr
		inspectStmts(mem.Body, func(e ast.Expr) bool {
			if found != nil || isOwnThisFunction(e) {
				return false
			}
			if _, ok := e.(*ast.ThisExpr); ok {
				found = e
			}
			return true
		})
		if found != nil {
			m.fail(diag.CodeClassStaticThis, "static "+mem.Name, found.Pos(), "`this` inside static "+mem.Name+" is not supported",
				"static lifecycles receive everything as arguments; use the props/state (or error) parameters instead of this.")
		}
	}
}

// exprLabel renders a short label for an expression in diagnostics.
func exprLabel(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Identifier:
		return x.Name
	case *ast.Literal:
		return x.Value
	}
	return "..."
}
