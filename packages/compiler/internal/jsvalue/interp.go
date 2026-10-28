package jsvalue

import (
	"fmt"
	"math"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

// Scope is a lexical scope of variable bindings.
type Scope struct {
	parent *Scope
	vars   map[string]Value
}

// NewScope returns a child scope of parent (nil for a root scope).
func NewScope(parent *Scope) *Scope {
	return &Scope{parent: parent, vars: map[string]Value{}}
}

// Lookup resolves name through the scope chain.
func (s *Scope) Lookup(name string) (Value, bool) {
	for c := s; c != nil; c = c.parent {
		if v, ok := c.vars[name]; ok {
			return v, true
		}
	}
	return Value{}, false
}

// Define binds name in this scope.
func (s *Scope) Define(name string, v Value) {
	s.vars[name] = v
}

// Assign updates the nearest binding of name; ok=false when unbound.
func (s *Scope) Assign(name string, v Value) bool {
	for c := s; c != nil; c = c.parent {
		if _, ok := c.vars[name]; ok {
			c.vars[name] = v
			return true
		}
	}
	return false
}

// Names returns the names bound directly in this scope.
func (s *Scope) Names() []string {
	out := make([]string, 0, len(s.vars))
	for k := range s.vars {
		out = append(out, k)
	}
	return out
}

// Hooks customize evaluation for a host (the SSR evaluator, the IR const
// folder). Every hook is optional; a hook returning ok=false falls through to
// the default behavior.
type Hooks struct {
	// Ident resolves an identifier that is not bound in scope.
	Ident func(name string) (Value, bool)
	// Member intercepts a member read before default evaluation.
	Member func(m *ast.MemberExpr) (Value, bool)
	// Call intercepts a call before default evaluation.
	Call func(c *ast.CallExpr) (Value, bool)
	// New intercepts a `new X(...)` expression.
	New func(n *ast.NewExpr) (Value, bool)
	// JSX renders a JSX element or fragment in the current scope.
	JSX func(expr ast.Expr) Value
}

// AwaitReason is the Unsupported reason for an `await` expression: the
// build-time evaluator cannot wait for a Promise. Callers report it with the
// dedicated server component diagnostic.
const AwaitReason = "`await` cannot be evaluated at build time"

// maxSteps bounds total evaluation work so a runaway loop cannot hang a build.
const maxSteps = 2_000_000

// Interp evaluates expressions to Values.
type Interp struct {
	Hooks Hooks
	// Scope is the current lexical scope. It is swapped while a closure runs,
	// so hooks that call back into Eval see the closure's bindings.
	Scope *Scope
	steps int
	depth int
}

// New returns an interpreter with a fresh root scope.
func New(hooks Hooks) *Interp {
	return &Interp{Hooks: hooks, Scope: NewScope(nil)}
}

// maxCallDepth bounds closure recursion.
const maxCallDepth = 200

// Eval evaluates an expression in the current scope.
func (in *Interp) Eval(expr ast.Expr) Value {
	if in.Scope == nil {
		in.Scope = NewScope(nil)
	}
	v, _ := in.eval(expr)
	return v
}

// EvalIn evaluates expr in scope (restoring the previous scope afterwards).
func (in *Interp) EvalIn(scope *Scope, expr ast.Expr) Value {
	saved := in.Scope
	in.Scope = scope
	defer func() { in.Scope = saved }()
	return in.Eval(expr)
}

// eval returns the value plus whether an optional chain short-circuited, so
// `a?.b.c` yields undefined for the whole chain when a is nullish.
func (in *Interp) eval(expr ast.Expr) (Value, bool) {
	if expr == nil {
		return Undefined(), false
	}
	in.steps++
	if in.steps > maxSteps {
		return Unsupported("evaluation step limit exceeded"), false
	}
	switch e := expr.(type) {
	case *ast.Literal:
		return literalValue(e), false
	case *ast.Identifier:
		return in.ident(e.Name), false
	case *ast.TemplateExpr:
		return in.template(e), false
	case *ast.TypeAssertion:
		return in.eval(e.Expr)
	case *ast.ParenExpr:
		// Parentheses end an optional chain: (a?.b).c reads c of undefined.
		v, _ := in.eval(e.Expr)
		return v, false
	case *ast.TaggedTemplateExpr:
		return Unsupported("tagged template literals are not supported"), false
	case *ast.ClassExpr:
		return Unsupported("class expressions are not supported"), false
	case *ast.AwaitExpr:
		return Unsupported(AwaitReason), false
	case *ast.MetaProperty:
		return Dynamic(e.Meta + "." + e.Property), false
	case *ast.UnaryExpr:
		return in.unary(e), false
	case *ast.BinaryExpr:
		return in.binary(e), false
	case *ast.ConditionalExpr:
		test := in.Eval(e.Test)
		t, known := test.Truthy()
		if !known {
			return test, false
		}
		if t {
			return in.Eval(e.Consequent), false
		}
		return in.Eval(e.Alternate), false
	case *ast.ArrayExpr:
		return in.array(e), false
	case *ast.ObjectExpr:
		return in.object(e), false
	case *ast.ArrowFn:
		return in.Closure("", e.Params, e.Body), false
	case *ast.MemberExpr:
		return in.member(e)
	case *ast.CallExpr:
		return in.call(e)
	case *ast.NewExpr:
		if in.Hooks.New != nil {
			if v, ok := in.Hooks.New(e); ok {
				return v, false
			}
		}
		if name := calleeName(e.Callee); errorConstructors[name] {
			msg := ""
			if len(e.Args) > 0 {
				m := in.Eval(e.Args[0])
				if m.IsUnknown() {
					return m, false
				}
				msg = m.ToString()
			}
			return errorObject(name, msg), false
		}
		return Unsupported("new " + calleeName(e.Callee) + "(...)"), false
	case *ast.JSXElement, *ast.JSXFragment:
		if in.Hooks.JSX != nil {
			return in.Hooks.JSX(expr), false
		}
		return Dynamic("JSX"), false
	}
	return Unsupported(strings.TrimPrefix(fmt.Sprintf("%T", expr), "*ast.") + " expressions are not supported"), false
}

func literalValue(e *ast.Literal) Value {
	switch e.Kind {
	case ast.StringLit:
		return String(e.Value)
	case ast.NumberLit:
		if d, ok := parseBigIntLiteral(e.Value); ok {
			return BigInt(d)
		}
		f, ok := ParseNumberLiteral(e.Value)
		if !ok {
			return Unsupported("numeric literal " + e.Value)
		}
		return Number(f)
	case ast.BoolLit:
		// constSourceToAST historically encodes null/undefined as BoolLit.
		switch e.Value {
		case "true":
			return Bool(true)
		case "null":
			return Null()
		case "undefined":
			return Undefined()
		}
		return Bool(false)
	case ast.NullLit:
		if e.Value == "undefined" {
			return Undefined()
		}
		return Null()
	}
	return Unsupported("regular expression literals are not supported")
}

func (in *Interp) ident(name string) Value {
	if v, ok := in.Scope.Lookup(name); ok {
		return v
	}
	if in.Hooks.Ident != nil {
		if v, ok := in.Hooks.Ident(name); ok {
			return v
		}
	}
	switch name {
	case "undefined":
		return Undefined()
	case "NaN":
		return Number(math.NaN())
	case "Infinity":
		return Number(math.Inf(1))
	}
	if g, ok := globalObject(name); ok {
		return g
	}
	return Dynamic("unresolved identifier " + name)
}

func (in *Interp) template(e *ast.TemplateExpr) Value {
	var b strings.Builder
	for i, raw := range e.Raw {
		b.WriteString(escape.TemplateRaw(raw))
		if i < len(e.Parts) {
			v := in.Eval(e.Parts[i])
			if v.IsUnknown() {
				return v
			}
			b.WriteString(v.ToPrimitive().ToString())
		}
	}
	return String(b.String())
}

func (in *Interp) unary(e *ast.UnaryExpr) Value {
	switch e.Op {
	case "++", "--":
		cur := in.Eval(e.Arg)
		if cur.IsUnknown() {
			return cur
		}
		old := cur.ToNumber()
		next := old + 1
		if e.Op == "--" {
			next = old - 1
		}
		if r := in.assignTo(e.Arg, Number(next)); r.IsUnknown() {
			return r
		}
		if e.Postfix {
			return Number(old)
		}
		return Number(next)
	case "...":
		return Unsupported("spread outside an array literal or call")
	case "delete":
		return Unsupported("delete")
	case "typeof":
		// typeof on an unbound identifier is "undefined" in JS, but here an
		// unbound name may be a runtime value the build cannot see.
		return UnaryOp("typeof", in.Eval(e.Arg))
	case "await":
		return Unsupported("await")
	}
	return UnaryOp(e.Op, in.Eval(e.Arg))
}

func (in *Interp) binary(e *ast.BinaryExpr) Value {
	switch e.Op {
	case "&&", "||", "??":
		left := in.Eval(e.Left)
		if left.IsUnknown() {
			return left
		}
		switch e.Op {
		case "&&":
			if !left.ToBoolean() {
				return left
			}
		case "||":
			if left.ToBoolean() {
				return left
			}
		case "??":
			if !left.IsNullish() {
				return left
			}
		}
		return in.Eval(e.Right)
	case "=":
		return in.assignTo(e.Left, in.Eval(e.Right))
	case "+=", "-=", "*=", "/=", "%=", "**=", "<<=", ">>=", ">>>=", "&=", "|=", "^=":
		cur := in.Eval(e.Left)
		next := BinaryOp(strings.TrimSuffix(e.Op, "="), cur, in.Eval(e.Right))
		if next.IsUnknown() {
			return next
		}
		return in.assignTo(e.Left, next)
	case "&&=", "||=", "??=":
		cur := in.Eval(e.Left)
		if cur.IsUnknown() {
			return cur
		}
		keep := false
		switch e.Op {
		case "&&=":
			keep = !cur.ToBoolean()
		case "||=":
			keep = cur.ToBoolean()
		default:
			keep = !cur.IsNullish()
		}
		if keep {
			return cur
		}
		return in.assignTo(e.Left, in.Eval(e.Right))
	case ",":
		in.Eval(e.Left)
		return in.Eval(e.Right)
	case "instanceof":
		return Unsupported("instanceof")
	}
	left := in.Eval(e.Left)
	right := in.Eval(e.Right)
	return BinaryOp(e.Op, left, right)
}

// assignTo stores v into an identifier or member target.
func (in *Interp) assignTo(target ast.Expr, v Value) Value {
	if v.IsUnknown() {
		// Poison the target so later reads do not see a stale value.
		if id, ok := target.(*ast.Identifier); ok {
			in.Scope.Assign(id.Name, v)
		}
		return v
	}
	switch t := target.(type) {
	case *ast.Identifier:
		if !in.Scope.Assign(t.Name, v) {
			return Unsupported("assignment to undeclared variable " + t.Name)
		}
		return v
	case *ast.MemberExpr:
		obj := in.Eval(t.Object)
		key, ok := in.propertyKey(t)
		if !ok {
			return Unsupported("computed member assignment")
		}
		switch obj.kind {
		case KindObject:
			obj.Set(key, v)
			return v
		case KindArray:
			if isArrayIndex(key) {
				idx := int(StringToNumber(key))
				for len(obj.arr.Elems) <= idx {
					obj.arr.Elems = append(obj.arr.Elems, Undefined())
				}
				obj.arr.Elems[idx] = v
				return v
			}
		case KindUnknown:
			return obj
		}
		return Unsupported("assignment to property " + key)
	case *ast.TypeAssertion:
		return in.assignTo(t.Expr, v)
	case *ast.ParenExpr:
		return in.assignTo(t.Expr, v)
	}
	return Unsupported("assignment target")
}

func (in *Interp) array(e *ast.ArrayExpr) Value {
	elems := make([]Value, 0, len(e.Elements))
	for _, el := range e.Elements {
		if el == nil {
			elems = append(elems, Undefined())
			continue
		}
		if u, ok := el.(*ast.UnaryExpr); ok && u.Op == "..." {
			src := in.Eval(u.Arg)
			items, ok := iterate(src)
			if !ok {
				return unknownOr(src, "spread of a non-iterable value")
			}
			elems = append(elems, items...)
			continue
		}
		elems = append(elems, in.Eval(el))
	}
	return NewArray(elems...)
}

func unknownOr(v Value, reason string) Value {
	if v.IsUnknown() {
		return v
	}
	return Unsupported(reason)
}

// iterate returns the elements produced by iterating v (arrays and strings).
func iterate(v Value) ([]Value, bool) {
	switch v.kind {
	case KindArray:
		return v.Elems(), true
	case KindString:
		var out []Value
		for _, r := range v.s {
			out = append(out, String(string(r)))
		}
		return out, true
	}
	return nil, false
}

func (in *Interp) object(e *ast.ObjectExpr) Value {
	obj := NewObject()
	for _, p := range e.Properties {
		if p == nil {
			continue
		}
		if p.Spread {
			src := in.Eval(p.Value)
			switch src.kind {
			case KindObject:
				for _, k := range src.Keys() {
					v, _ := src.Get(k)
					obj.Set(k, v)
				}
			case KindArray:
				for i, v := range src.Elems() {
					obj.Set(NumberToString(float64(i)), v)
				}
			case KindUndefined, KindNull, KindBool, KindNumber:
			case KindString:
				for i, r := range []rune(src.s) {
					obj.Set(NumberToString(float64(i)), String(string(r)))
				}
			default:
				return unknownOr(src, "object spread")
			}
			continue
		}
		var v Value
		switch {
		case p.Value == nil && p.Shorthand:
			v = in.ident(p.Key)
		case p.Value == nil:
			v = Undefined()
		default:
			v = in.Eval(p.Value)
		}
		obj.Set(p.Key, v)
	}
	return obj
}

// Closure builds a function value from params/body capturing the current
// scope. Bodies may be expression bodies (a single return) or blocks.
func (in *Interp) Closure(name string, params []*ast.Param, body []ast.Stmt) Value {
	captured := in.Scope
	return Func(name, func(args []Value) Value {
		in.depth++
		defer func() { in.depth-- }()
		if in.depth > maxCallDepth {
			return Unsupported("call depth limit exceeded")
		}
		scope := NewScope(captured)
		saved := in.Scope
		in.Scope = scope
		defer func() { in.Scope = saved }()
		for i, p := range params {
			if p == nil {
				continue
			}
			var arg Value
			if p.IsRest {
				if i < len(args) {
					arg = ArrayOf(args[i:])
				} else {
					arg = NewArray()
				}
			} else if i < len(args) {
				arg = args[i]
			} else {
				arg = Undefined()
			}
			if arg.kind == KindUndefined && p.Default != nil {
				arg = in.Eval(p.Default)
			}
			if r := in.BindPattern(scope, p.Name, p.Pattern, arg); r.IsUnknown() && r.IsUnsupported() {
				return r
			}
		}
		v, _ := in.ExecBlock(body)
		return v
	})
}

// BindPattern binds a parameter or declaration target (a plain name, or an
// object/array destructuring pattern given as source text) to v in scope.
// Returns Unsupported for patterns it cannot parse.
func (in *Interp) BindPattern(scope *Scope, name, pattern string, v Value) Value {
	if pattern == "" {
		if name == "" || name == "{...}" || name == "[...]" {
			return Unsupported("destructuring pattern without source")
		}
		scope.Define(name, v)
		return v
	}
	return in.destructure(scope, strings.TrimSpace(pattern), v)
}

func (in *Interp) destructure(scope *Scope, pattern string, v Value) Value {
	if pattern == "" {
		return v
	}
	// A top-level `= default` applies to plain names and nested patterns.
	if target, def, hasDef := cutTopLevel(pattern, '='); hasDef {
		if v.kind == KindUndefined {
			expr := ParseExprSource(strings.TrimSpace(def))
			if expr == nil {
				return Unsupported("default value " + def)
			}
			v = in.Eval(expr)
		}
		return in.destructure(scope, strings.TrimSpace(target), v)
	}
	if pattern[0] != '{' && pattern[0] != '[' {
		name := strings.TrimSpace(pattern)
		if !isIdent(name) {
			return Unsupported("destructuring target " + name)
		}
		scope.Define(name, v)
		return v
	}
	closeCh := byte('}')
	if pattern[0] == '[' {
		closeCh = ']'
	}
	if pattern[len(pattern)-1] != closeCh {
		return Unsupported("destructuring pattern " + pattern)
	}
	inner := pattern[1 : len(pattern)-1]
	parts := splitTopLevel(inner)
	if pattern[0] == '[' {
		elems, ok := iterate(v)
		if !ok {
			if v.IsUnknown() {
				elems = nil
			} else {
				return Unsupported("array destructuring of a non-iterable value")
			}
		}
		for i, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if strings.HasPrefix(part, "...") {
				var rest Value
				switch {
				case v.IsUnknown():
					rest = v
				case i < len(elems):
					rest = ArrayOf(elems[i:])
				default:
					rest = NewArray()
				}
				if r := in.destructure(scope, strings.TrimSpace(part[3:]), rest); r.IsUnsupported() {
					return r
				}
				continue
			}
			el := Undefined()
			if v.IsUnknown() {
				el = v
			} else if i < len(elems) {
				el = elems[i]
			}
			if r := in.destructure(scope, part, el); r.IsUnsupported() {
				return r
			}
		}
		return v
	}
	used := map[string]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "...") {
			rest := NewObject()
			if v.IsUnknown() {
				rest = v
			} else {
				for _, k := range v.Keys() {
					if !used[k] {
						pv, _ := v.Get(k)
						rest.Set(k, pv)
					}
				}
			}
			if r := in.destructure(scope, strings.TrimSpace(part[3:]), rest); r.IsUnsupported() {
				return r
			}
			continue
		}
		key, target, hasColon := cutTopLevel(part, ':')
		key = strings.TrimSpace(key)
		if !hasColon {
			// `name` or `name = default`.
			k, _, _ := cutTopLevel(key, '=')
			key = strings.TrimSpace(k)
			target = part
		}
		key = strings.Trim(key, `'"`)
		used[key] = true
		pv := in.propertyOf(v, key)
		if r := in.destructure(scope, strings.TrimSpace(target), pv); r.IsUnsupported() {
			return r
		}
	}
	return v
}

// propertyOf reads key from v for destructuring (Unknown propagates).
func (in *Interp) propertyOf(v Value, key string) Value {
	if v.IsUnknown() {
		return v
	}
	return GetProperty(v, key)
}

// cutTopLevel splits s at the first sep outside brackets/quotes.
func cutTopLevel(s string, sep byte) (before, after string, found bool) {
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		default:
			if c == sep && depth == 0 {
				// `=>` and `==` are not assignment separators.
				if sep == '=' && i+1 < len(s) && (s[i+1] == '>' || s[i+1] == '=') {
					continue
				}
				return s[:i], s[i+1:], true
			}
		}
	}
	return s, "", false
}

func splitTopLevel(s string) []string {
	var out []string
	for {
		before, after, found := cutTopLevel(s, ',')
		out = append(out, before)
		if !found {
			return out
		}
		s = after
	}
}

// ExecBlock runs statements, returning the value of the first executed return
// (undefined when none). returned=false when the block completed without a
// return statement.
func (in *Interp) ExecBlock(stmts []ast.Stmt) (Value, bool) {
	v, ctl := in.exec(stmts)
	if ctl == ctlReturn {
		return v, true
	}
	if ctl == ctlError {
		return v, true
	}
	return Undefined(), false
}

type control int

const (
	ctlNormal control = iota
	ctlReturn
	ctlBreak
	ctlContinue
	ctlError
)

// maxLoopIterations bounds statically evaluated loops.
const maxLoopIterations = 10000

func (in *Interp) exec(stmts []ast.Stmt) (Value, control) {
	for _, stmt := range stmts {
		v, ctl := in.execStmt(stmt)
		if ctl != ctlNormal {
			return v, ctl
		}
	}
	return Undefined(), ctlNormal
}

func (in *Interp) execStmt(stmt ast.Stmt) (Value, control) {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		if s.Value == nil {
			return Undefined(), ctlReturn
		}
		return in.Eval(s.Value), ctlReturn
	case *ast.ExprStmt:
		v := in.Eval(s.Expression)
		if v.IsUnsupported() {
			return v, ctlError
		}
		return Undefined(), ctlNormal
	case *ast.VarStmt:
		for _, d := range s.Decls {
			if r := in.Declare(d); r.IsUnsupported() {
				return r, ctlError
			}
		}
		return Undefined(), ctlNormal
	case *ast.BlockStmt:
		saved := in.Scope
		in.Scope = NewScope(saved)
		v, ctl := in.exec(s.Body)
		in.Scope = saved
		return v, ctl
	case *ast.IfStmt:
		test := in.Eval(s.Test)
		t, known := test.Truthy()
		if !known {
			return unknownOr(test, "if"), ctlError
		}
		saved := in.Scope
		in.Scope = NewScope(saved)
		defer func() { in.Scope = saved }()
		if t {
			return in.exec(s.Consequent)
		}
		return in.exec(s.Alternate)
	case *ast.ForStmt:
		return in.execFor(s)
	case *ast.ForInStmt:
		return in.execForIn(s)
	case *ast.BreakStmt:
		if s.Label != "" {
			return Unsupported("labeled break"), ctlError
		}
		return Undefined(), ctlBreak
	case *ast.ContinueStmt:
		if s.Label != "" {
			return Unsupported("labeled continue"), ctlError
		}
		return Undefined(), ctlContinue
	case *ast.FnDecl:
		in.Scope.Define(s.Name, in.Closure(s.Name, s.Params, s.Body))
		return Undefined(), ctlNormal
	case *ast.ThrowStmt:
		return in.throw(s), ctlError
	case *ast.EmptyStmt, *ast.DebuggerStmt:
		return Undefined(), ctlNormal
	case *ast.LabeledStmt:
		v, ctl := in.execStmt(s.Body)
		if ctl == ctlBreak {
			return Undefined(), ctlNormal
		}
		return v, ctl
	case *ast.WhileStmt:
		return in.execWhile(s.Test, s.Body, false)
	case *ast.DoWhileStmt:
		return in.execWhile(s.Test, s.Body, true)
	case *ast.SwitchStmt:
		return in.execSwitch(s)
	case *ast.TryStmt:
		// Only a throw statement raises a catchable error at build time; other
		// failures are Unknown values that propagate.
		v, ctl := in.execStmt(&ast.BlockStmt{Body: s.Body})
		if ctl == ctlError && v.Thrown() != nil && s.Catch != nil {
			v, ctl = in.execCatch(s.Catch, v.Thrown())
		}
		if ctl != ctlNormal {
			if ctl != ctlError && len(s.Finally) > 0 {
				if fv, fctl := in.execStmt(&ast.BlockStmt{Body: s.Finally}); fctl != ctlNormal {
					return fv, fctl
				}
			}
			return v, ctl
		}
		if len(s.Finally) > 0 {
			return in.execStmt(&ast.BlockStmt{Body: s.Finally})
		}
		return Undefined(), ctlNormal
	case *ast.ImportStmt, *ast.ClassDecl:
		return Unsupported(strings.TrimPrefix(fmt.Sprintf("%T", stmt), "*ast.") + " inside a function body"), ctlError
	}
	return Unsupported(strings.TrimPrefix(fmt.Sprintf("%T", stmt), "*ast.") + " statements are not supported"), ctlError
}

// throw evaluates a throw statement into the Unknown that carries the error.
func (in *Interp) throw(s *ast.ThrowStmt) Value {
	t := &ThrownError{Line: s.Position.Line, Col: s.Position.Col}
	v := in.Eval(s.Value)
	if prior := v.Thrown(); prior != nil {
		return v
	}
	switch {
	case v.kind == KindString:
		t.Message = v.s
	case v.kind == KindObject:
		if n, ok := v.Get("name"); ok && n.kind == KindString {
			t.Name = n.s
		}
		if m, ok := v.Get("message"); ok && m.kind == KindString {
			t.Message = m.s
		}
	case v.IsUnknown():
		t.Message = "(" + v.Reason() + ")"
	default:
		if src, ok := JSSource(v); ok {
			t.Message = src
		}
	}
	return Throw(t)
}

// execCatch runs a catch clause with its parameter bound to the error object.
func (in *Interp) execCatch(c *ast.CatchClause, t *ThrownError) (Value, control) {
	saved := in.Scope
	in.Scope = NewScope(saved)
	defer func() { in.Scope = saved }()
	if c.Param != "" {
		if r := in.BindPattern(in.Scope, c.Param, patternOf(c.Param), errorObject(t.Name, t.Message)); r.IsUnsupported() {
			return r, ctlError
		}
	}
	return in.exec(c.Body)
}

// patternOf returns the destructuring source of a catch parameter, or "".
func patternOf(param string) string {
	if strings.HasPrefix(param, "{") || strings.HasPrefix(param, "[") {
		return param
	}
	return ""
}

// errorObject models an Error instance as a plain {name, message} object.
func errorObject(name, message string) Value {
	if name == "" {
		name = "Error"
	}
	o := NewObject()
	o.Set("name", String(name))
	o.Set("message", String(message))
	return o
}

// errorConstructors are the built-in Error types `new X(msg)` builds at build time.
var errorConstructors = map[string]bool{
	"Error": true, "TypeError": true, "RangeError": true, "SyntaxError": true,
	"ReferenceError": true, "EvalError": true, "URIError": true,
}

// Declare evaluates a variable declaration into the current scope.
func (in *Interp) Declare(d *ast.VarDecl) Value {
	v := Undefined()
	if d.Init != nil {
		v = in.Eval(d.Init)
	}
	if d.IsDestructuring || d.Pattern != "" {
		return in.BindPattern(in.Scope, "", d.Pattern, v)
	}
	if d.Name == "" {
		return Unsupported("declaration without a name")
	}
	in.Scope.Define(d.Name, v)
	return v
}

func (in *Interp) execFor(s *ast.ForStmt) (Value, control) {
	saved := in.Scope
	in.Scope = NewScope(saved)
	defer func() { in.Scope = saved }()
	if s.Init != nil {
		if v, ctl := in.execStmt(s.Init); ctl == ctlError {
			return v, ctl
		}
	}
	for iter := 0; ; iter++ {
		if iter >= maxLoopIterations {
			return Unsupported("loop iteration limit exceeded"), ctlError
		}
		if s.Test != nil {
			test := in.Eval(s.Test)
			t, known := test.Truthy()
			if !known {
				return unknownOr(test, "for"), ctlError
			}
			if !t {
				break
			}
		}
		body := in.Scope
		in.Scope = NewScope(body)
		v, ctl := in.exec(s.Body)
		in.Scope = body
		switch ctl {
		case ctlReturn, ctlError:
			return v, ctl
		case ctlBreak:
			return Undefined(), ctlNormal
		}
		if s.Update != nil {
			if u := in.Eval(s.Update); u.IsUnknown() {
				return u, ctlError
			}
		}
	}
	return Undefined(), ctlNormal
}

func (in *Interp) execWhile(test ast.Expr, body []ast.Stmt, doFirst bool) (Value, control) {
	saved := in.Scope
	defer func() { in.Scope = saved }()
	for iter := 0; ; iter++ {
		if iter >= maxLoopIterations {
			return Unsupported("loop iteration limit exceeded"), ctlError
		}
		if !doFirst || iter > 0 {
			t := in.Eval(test)
			truthy, known := t.Truthy()
			if !known {
				return unknownOr(t, "while"), ctlError
			}
			if !truthy {
				return Undefined(), ctlNormal
			}
		}
		in.Scope = NewScope(saved)
		v, ctl := in.exec(body)
		switch ctl {
		case ctlReturn, ctlError:
			return v, ctl
		case ctlBreak:
			return Undefined(), ctlNormal
		}
	}
}

func (in *Interp) execSwitch(s *ast.SwitchStmt) (Value, control) {
	disc := in.Eval(s.Discriminant)
	if disc.IsUnknown() {
		return disc, ctlError
	}
	start := -1
	for i, c := range s.Cases {
		if c.Test == nil {
			continue
		}
		t := in.Eval(c.Test)
		if t.IsUnknown() {
			return t, ctlError
		}
		if StrictEquals(disc, t) {
			start = i
			break
		}
	}
	if start < 0 {
		for i, c := range s.Cases {
			if c.Test == nil {
				start = i
				break
			}
		}
	}
	if start < 0 {
		return Undefined(), ctlNormal
	}
	saved := in.Scope
	in.Scope = NewScope(saved)
	defer func() { in.Scope = saved }()
	for _, c := range s.Cases[start:] {
		v, ctl := in.exec(c.Body)
		switch ctl {
		case ctlBreak:
			return Undefined(), ctlNormal
		case ctlNormal:
			continue
		}
		return v, ctl
	}
	return Undefined(), ctlNormal
}

func (in *Interp) execForIn(s *ast.ForInStmt) (Value, control) {
	src := in.Eval(s.Right)
	var items []Value
	if s.IsForOf {
		var ok bool
		items, ok = iterate(src)
		if !ok {
			return unknownOr(src, "for...of over a non-iterable value"), ctlError
		}
	} else {
		switch src.kind {
		case KindObject:
			for _, k := range src.Keys() {
				items = append(items, String(k))
			}
		case KindArray:
			for i := range src.Elems() {
				items = append(items, String(NumberToString(float64(i))))
			}
		default:
			return unknownOr(src, "for...in"), ctlError
		}
	}
	name := ""
	switch l := s.Left.(type) {
	case *ast.Identifier:
		name = l.Name
	default:
		return Unsupported("for loop binding"), ctlError
	}
	saved := in.Scope
	defer func() { in.Scope = saved }()
	for _, it := range items {
		in.Scope = NewScope(saved)
		if s.Keyword != "" || !in.Scope.Assign(name, it) {
			in.Scope.Define(name, it)
		}
		v, ctl := in.exec(s.Body)
		switch ctl {
		case ctlReturn, ctlError:
			return v, ctl
		case ctlBreak:
			return Undefined(), ctlNormal
		}
	}
	return Undefined(), ctlNormal
}

func calleeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Identifier:
		return t.Name
	case *ast.MemberExpr:
		if p, ok := t.Property.(*ast.Identifier); ok && !t.Computed {
			return calleeName(t.Object) + "." + p.Name
		}
		return calleeName(t.Object) + "[...]"
	}
	return "expression"
}
