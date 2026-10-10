package irtree

import (
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

// Helpers

func findReturnStmt(body []ast.Stmt) *ast.ReturnStmt {
	for _, stmt := range body {
		if ret, ok := stmt.(*ast.ReturnStmt); ok {
			return ret
		}
	}
	return nil
}

// collectLocalVars resolves top-level local variable declarations in a component
// body to build-time constants (derived from props and other locals). Handles
// sequential `var x = <expr>` declarations and simple reassignments/mutations
// (`x = ...`, `x += ...`) across `var`/expression/`if` statements. The result
// is used to resolve SSR initial values AND to emit `var x = <const>` decls into
// the hydration bundle so bindings referencing locals don't throw ReferenceError.
func collectLocalVars(body []ast.Stmt, sigMap map[string]ast.Expr, props map[string]string, nextUseId func() string) (locals, useIds map[string]string, inits map[string]ast.Expr, order []string, leaked map[string]bool) {
	locals = make(map[string]string)
	useIds = make(map[string]string)
	inits = make(map[string]ast.Expr)
	leaked = make(map[string]bool)
	working := make(map[string]string, len(props))
	for k, v := range props {
		working[k] = v
	}
	applyLocalStmts(body, locals, working, sigMap, nextUseId, useIds, inits, &order, leaked)
	return locals, useIds, inits, order, leaked
}

func applyLocalStmts(stmts []ast.Stmt, locals, working map[string]string, sigMap map[string]ast.Expr, nextUseId func() string, useIds map[string]string, inits map[string]ast.Expr, order *[]string, leaked map[string]bool) {
	record := func(name string) {
		if order == nil {
			return
		}
		for _, n := range *order {
			if n == name {
				return
			}
		}
		*order = append(*order, name)
	}
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			for _, decl := range s.Decls {
				name := decl.Name
				if name == "" && len(decl.Names) > 0 {
					name = decl.Names[0]
				}
				if name == "" || name == "children" || decl.IsDestructuring {
					continue
				}
				if decl.Init != nil {
					// useId() resolves to a per-instance build-time literal so
					// SSR and hydration agree without runtime state.
					if isUseIdCall(decl.Init) {
						idLit := ""
						if nextUseId != nil {
							idLit = nextUseId()
						}
						locals[name] = idLit
						working[name] = idLit
						if useIds != nil {
							useIds[name] = idLit
						}
						record(name)
						continue
					}
					// A local whose initializer can't be folded (references an
					// unknown/unresolvable value, e.g. `tocItems.length` when
					// tocItems is not statically known) must still be RECORDED so
					// it is emitted as a runtime read - bindings/handlers may
					// reference it. Dropping it entirely produced `X is not
					// defined` at hydration. Its folded value is left unknown ("")
					// and it is kept out of `working` so dependents also emit as
					// runtime reads rather than folding against a wrong value.
					if operandLeaks(decl.Init, sigMap, working) {
						if _, exists := locals[name]; !exists {
							locals[name] = ""
							inits[name] = decl.Init
							leaked[name] = true
							record(name)
						}
						continue
					}
					v := evalConstWithSignals(decl.Init, sigMap, working)
					locals[name] = v
					inits[name] = decl.Init
					working[name] = v
					record(name)
				}
			}
		case *ast.ExprStmt:
			applyLocalAssignment(s.Expression, locals, working, sigMap)
		case *ast.IfStmt:
			test := evalConstWithSignals(s.Test, sigMap, working)
			if isTruthyValue(test) {
				applyLocalStmts(s.Consequent, locals, working, sigMap, nextUseId, useIds, inits, order, leaked)
				continue
			}
			if isFalsyValue(test) {
				applyLocalStmts(s.Alternate, locals, working, sigMap, nextUseId, useIds, inits, order, leaked)
				continue
			}
		case *ast.BlockStmt:
			applyLocalStmts(s.Body, locals, working, sigMap, nextUseId, useIds, inits, order, leaked)
		}
	}
}

// isContextRead reports whether expr is a context read: either the React form
// `useContext(Ctx)` or the Krate member form `Ctx.useContext()`. Such reads fold
// to the context default at build time and must not be emitted as runtime calls
// (the context object has no runtime binding in the generated IIFE).
func isContextRead(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	if id, ok := call.Callee.(*ast.Identifier); ok {
		return id.Name == "useContext"
	}
	if mem, ok := call.Callee.(*ast.MemberExpr); ok {
		if prop, ok := mem.Property.(*ast.Identifier); ok {
			return prop.Name == "useContext"
		}
	}
	return false
}

// isUseIdCall reports whether expr is the rewritten `__krate_useId()` marker
// (React's useId) that the IR builder resolves to a per-instance literal.
func isUseIdCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Callee.(*ast.Identifier)
	return ok && id.Name == "__krate_useId"
}

func applyLocalAssignment(expr ast.Expr, locals, working map[string]string, sigMap map[string]ast.Expr) {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok {
		return
	}
	id, ok := bin.Left.(*ast.Identifier)
	if !ok {
		return
	}
	name := id.Name
	cur, exists := working[name]
	if !exists && !isKnownLocal(name, locals) {
		return
	}
	rhs := evalConstWithSignals(bin.Right, sigMap, working)
	if rhs == "" {
		return
	}
	switch bin.Op {
	case "=":
		locals[name] = rhs
		working[name] = rhs
	case "+=", "-=", "*=", "/=", "%=":
		if cur == "" {
			return
		}
		combined := cur + " " + bin.Op[:1] + " " + rhs
		if bin.Op == "+=" {
			combined = cur + rhs
		}
		locals[name] = combined
		working[name] = combined
	}
}

func isKnownLocal(name string, locals map[string]string) bool {
	_, ok := locals[name]
	return ok
}

// isTruthyValue reports whether a resolved const string is truthy in JS terms.
func isTruthyValue(v string) bool {
	return v != "" && v != "false" && v != "null" && v != "undefined" && v != "0"
}

// evalBinaryValue computes the value of a binary operation between two resolved
// const operands. Arithmetic/comparison ops use numeric evaluation when both
// operands are numeric; string concat uses raw concatenation.
func evalBinaryValue(op, left, right string) string {
	lNum, lOk := parseNumeric(left)
	rNum, rOk := parseNumeric(right)
	switch op {
	case "+":
		if lOk && rOk {
			return trimFloat(lNum + rNum)
		}
		return left + right
	case "-":
		if lOk && rOk {
			return trimFloat(lNum - rNum)
		}
	case "*":
		if lOk && rOk {
			return trimFloat(lNum * rNum)
		}
	case "/":
		if lOk && rOk && rNum != 0 {
			return trimFloat(lNum / rNum)
		}
	case "%":
		if lOk && rOk && rNum != 0 {
			return trimFloat(float64(int(lNum) % int(rNum)))
		}
	case "<":
		if lOk && rOk {
			return strconv.FormatBool(lNum < rNum)
		}
		return strconv.FormatBool(left < right)
	case "<=":
		if lOk && rOk {
			return strconv.FormatBool(lNum <= rNum)
		}
		return strconv.FormatBool(left <= right)
	case ">":
		if lOk && rOk {
			return strconv.FormatBool(lNum > rNum)
		}
		return strconv.FormatBool(left > right)
	case ">=":
		if lOk && rOk {
			return strconv.FormatBool(lNum >= rNum)
		}
		return strconv.FormatBool(left >= right)
	}
	return ""
}

func parseNumeric(s string) (float64, bool) {
	if s == "" || !isNumericLiteral(s) {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func trimFloat(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// isFalsyValue reports whether a resolved const string is falsy in JS terms.
func isFalsyValue(v string) bool {
	return v == "" || v == "false" || v == "null" || v == "undefined" || v == "0"
}

// jsLiteralFor renders a resolved const value as a JS literal. A genuine JS
// numeric literal or keyword is emitted bare; array/object source produced by
// evalConst is emitted verbatim; everything else is quoted as a string so
// values like "1.2.3", "2024-01-01", "Hello world", "0", or a string that
// happens to look like an identifier ("menu") are not emitted as invalid or
// incorrectly-typed JS. Identifier *references* are handled from the AST by the
// caller (see isReferenceInit), not inferred from the value's shape.
func jsLiteralFor(v string) string {
	switch v {
	case "true", "false", "null", "undefined", "NaN", "Infinity", "-Infinity":
		return v
	}
	if v == "" {
		return "''"
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return v
	}
	// Array/object JS source produced by evalConst (e.g. a folded list).
	t := strings.TrimSpace(v)
	if strings.HasPrefix(t, "[") || strings.HasPrefix(t, "{") {
		return v
	}
	return "'" + escape.JSString(v) + "'"
}

// isReferenceInit reports whether a local's initializer is a bare reference to
// another binding (an identifier or member expression) whose runtime value must
// be assigned by name, e.g. `const Comp = Slot` or `const fmt = helpers.date`.
// Such locals are emitted with generateExprJS; string-valued locals are folded
// and quoted instead (the folded value alone cannot distinguish the two).
func isReferenceInit(expr ast.Expr) bool {
	switch expr.(type) {
	case *ast.Identifier, *ast.MemberExpr:
		return true
	}
	return false
}

// isStringLiteral reports whether expr is a string literal. A folded string
// value cannot be told apart from the null/undefined keyword (both resolve to
// the same text), so callers deciding whether to omit a nullish value consult
// the AST kind: `{"null"}` is real text, `{null}` is not.
func isStringLiteral(expr ast.Expr) bool {
	lit, ok := expr.(*ast.Literal)
	return ok && lit.Kind == ast.StringLit
}

func isNumericLiteral(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' && r != '-' && r != 'e' && r != 'E' && r != '+' {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// declaredLocalNames returns the set of identifiers already declared in the
// component's hydration scope: signal names, extra vars, and local functions.
// Locals collected by collectLocalVars that collide with these are skipped so
// the hydration bundle doesn't redeclare (and clobber) them.
func declaredLocalNames(node *ComponentNode, fn *ast.FnDecl) map[string]bool {
	declared := make(map[string]bool)
	for _, sig := range node.Signals {
		declared[sig.Name] = true
	}
	for _, ev := range node.ExtraVars {
		name := extraVarName(ev)
		if name != "" {
			declared[name] = true
		}
	}
	// Pre-signal vars (e.g. useRef's `{current:...}` object) are declared before
	// the signal decls. Treat them as declared so collectLocalVars does not
	// re-emit a duplicate `var` that clobbers the object with a string literal.
	for _, ev := range node.PreSignalVars {
		name := extraVarName(ev)
		if name != "" {
			declared[name] = true
		}
	}
	for _, stmt := range fn.Body {
		if fd, ok := stmt.(*ast.FnDecl); ok {
			declared[fd.Name] = true
		}
	}
	return declared
}

func extraVarName(ev string) string {
	s := strings.TrimSpace(ev)
	if !strings.HasPrefix(s, "var ") {
		return ""
	}
	s = strings.TrimPrefix(s, "var ")
	if i := strings.IndexAny(s, "= ;,("); i > 0 {
		return s[:i]
	}
	return s
}

func deriveInstanceID(id string) string {
	return strings.ReplaceAll(id, ".", "_")
}

// nextElementTag returns a unique tag identifier for an element under a given parent.
// When multiple sibling elements share the same tag name, appends _1, _2, etc.
func (b *builder) nextElementTag(tagName, parentID string) string {
	sanitized := sanitizeTagName(tagName)
	key := parentID + "." + sanitized
	count := b.elementCounts[key]
	b.elementCounts[key]++
	if count == 0 {
		return sanitized
	}
	return sanitized + "_" + itoa(count)
}

func isOnEvent(name string) bool {
	return len(name) > 2 && name[0] == 'o' && name[1] == 'n' && name[2] >= 'A' && name[2] <= 'Z'
}

// isReactDirectiveAttr reports whether a JSX attribute is a React-only
// directive with no HTML representation.
func isReactDirectiveAttr(name string) bool {
	switch name {
	case "key", "suppressHydrationWarning":
		return true
	}
	return false
}

// reactEventAliases maps JSX event prop names whose DOM event differs from a
// simple lowercasing of the prop. onFocus/onBlur map to focusin/focusout
// (which bubble, so delegation works); onChange maps to input (React's
// per-keystroke semantics for text controls).
var reactEventAliases = map[string]string{
	"onDoubleClick": "dblclick",
	"onFocus":       "focusin",
	"onBlur":        "focusout",
	"onChange":      "input",
}

// nonBubblingEvents are DOM events with no bubbling phase. They cannot be
// served by the delegated (bubble-phase) listener and are attached directly.
var nonBubblingEvents = map[string]bool{
	"mouseenter": true, "mouseleave": true,
	"pointerenter": true, "pointerleave": true,
	"scroll": true, "load": true, "error": true,
}

// reactEventName resolves a JSX event prop to its DOM event name plus whether it
// needs a capture-phase / direct listener. `<button onClickCapture>` strips the
// Capture suffix and attaches a capture listener; non-bubbling events are
// attached directly; everything else is delegated.
func reactEventName(attrName string) (event string, capture, direct bool) {
	name := attrName
	if strings.HasSuffix(name, "Capture") {
		name = strings.TrimSuffix(name, "Capture")
		capture = true
	}
	if mapped, ok := reactEventAliases[name]; ok {
		event = mapped
	} else if len(name) > 2 {
		event = strings.ToLower(name[2:])
	}
	direct = capture || nonBubblingEvents[event]
	return event, capture, direct
}

// componentNeedsClient reports whether a signal-less component must be built
// as a client component (through the tree path) instead of being flattened by
// the SSR evaluator. That is the case when it receives a function prop (which
// can only run as a live handler), a signal-valued prop (which must stay
// reactive), or its return JSX contains event handlers (e.g. a reusable
// <Button onClick={props.onClick}> wrapper).
func (b *builder) componentNeedsClient(fn *ast.FnDecl, attrs map[string]ast.Expr) bool {
	if hasLifecycleCall(fn.Body) {
		return true
	}
	for _, expr := range attrs {
		switch e := expr.(type) {
		case *ast.ArrowFn:
			return true
		case *ast.Identifier:
			if b.localFnBody != nil {
				if findLocalFunction(e.Name, b.localFnBody) != nil {
					return true
				}
			}
		}
		// Signal-valued props: <Child value={count()} /> means the child
		// must stay reactive even if it has no local signals.
		if b.referencesSignal(expr) {
			return true
		}
	}
	if ret := findReturnStmt(fn.Body); ret != nil {
		return hasEventHandlerExpr(ret.Value)
	}
	return false
}

// hasLifecycleCall reports whether a component body contains a top-level
// createEffect / onMount / onCleanup call. Signal-less components that register
// lifecycle callbacks must still be client-rendered so those callbacks are
// emitted into the hydration bundle - otherwise they'd be SSR-evaluated and the
// effects silently dropped.
func hasLifecycleCall(body []ast.Stmt) bool {
	var walk func(stmts []ast.Stmt) bool
	walk = func(stmts []ast.Stmt) bool {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.ExprStmt:
				if call, ok := s.Expression.(*ast.CallExpr); ok {
					if id, ok := call.Callee.(*ast.Identifier); ok {
						switch id.Name {
						case "createEffect", "onMount", "onCleanup":
							return true
						}
					}
				}
			case *ast.BlockStmt:
				if walk(s.Body) {
					return true
				}
			case *ast.IfStmt:
				if walk(s.Consequent) || walk(s.Alternate) {
					return true
				}
			case *ast.ForStmt:
				if walk(s.Body) {
					return true
				}
			case *ast.WhileStmt:
				if walk(s.Body) {
					return true
				}
			case *ast.DoWhileStmt:
				if walk(s.Body) {
					return true
				}
			case *ast.SwitchStmt:
				for _, c := range s.Cases {
					if walk(c.Body) {
						return true
					}
				}
			case *ast.TryStmt:
				if walk(s.Body) {
					return true
				}
				if s.Catch != nil && walk(s.Catch.Body) {
					return true
				}
				if walk(s.Finally) {
					return true
				}
			}
		}
		return false
	}
	return walk(body)
}

// hasEventHandlerExpr reports whether a JSX expression tree contains any on*
// event handler attributes. Used to keep signal-less wrapper components
// interactive instead of flattening their handlers into dead static markup.
func hasEventHandlerExpr(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.JSXElement:
		for _, attr := range e.Opening.Attributes {
			if isOnEvent(attr.Name) {
				return true
			}
		}
		return hasEventHandlerChildren(e.Children)
	case *ast.JSXFragment:
		return hasEventHandlerChildren(e.Children)
	case *ast.ConditionalExpr:
		return hasEventHandlerExpr(e.Consequent) || hasEventHandlerExpr(e.Alternate)
	case *ast.BinaryExpr:
		return hasEventHandlerExpr(e.Left) || hasEventHandlerExpr(e.Right)
	case *ast.TypeAssertion:
		return hasEventHandlerExpr(e.Expr)
	}
	return false
}

func hasEventHandlerChildren(children []ast.JSXChild) bool {
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXExprContainer:
			if hasEventHandlerExpr(c.Expression) {
				return true
			}
		case *ast.JSXElementChild:
			if hasEventHandlerExpr(c.Element) {
				return true
			}
		case *ast.JSXFragmentChild:
			if hasEventHandlerExpr(c.Fragment) {
				return true
			}
		}
	}
	return false
}

func isAttrBinding(attr *ast.JSXAttr) bool {
	if attr.Spread || attr.Value == nil {
		return false
	}
	if isOnEvent(attr.Name) || isReactDirectiveAttr(attr.Name) {
		return false
	}
	switch attr.Value.(type) {
	case *ast.Identifier, *ast.CallExpr, *ast.MemberExpr, *ast.ConditionalExpr, *ast.BinaryExpr, *ast.ArrowFn:
		return true
	}
	return false
}

// funcPropAliasOf returns "Y" when the local variable `name` is declared as
// `var name = props.Y` and Y is one of the function props. Returns "" otherwise.
func funcPropAliasOf(body []ast.Stmt, name string, funcProps map[string]bool) string {
	if len(funcProps) == 0 {
		return ""
	}
	for _, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, decl := range vs.Decls {
			if decl.Name != name || decl.Init == nil {
				continue
			}
			mem, ok := decl.Init.(*ast.MemberExpr)
			if !ok {
				continue
			}
			if id, ok := mem.Object.(*ast.Identifier); !ok || id.Name != "props" {
				continue
			}
			if pid, ok := mem.Property.(*ast.Identifier); ok && funcProps[pid.Name] {
				return pid.Name
			}
		}
	}
	return ""
}

// isStaticResolvable reports whether an expression can be fully resolved at
// build time from literals, props, and local bindings - i.e. it contains no
// signal reads and every identifier is a known prop/local. Statically
// resolvable attribute values are emitted directly into the SSR HTML with no
// hydration binding, avoiding runtime evaluation of build-time-only locals
// like for-loop counters.
func (b *builder) isStaticResolvable(expr ast.Expr) bool {
	if expr == nil {
		return true
	}
	switch e := expr.(type) {
	case *ast.Literal:
		return true
	case *ast.Identifier:
		if e.Name == "props" {
			return false
		}
		if _, inSig := b.sigMap()[e.Name]; inSig {
			return false
		}
		_, inProps := b.localProps[e.Name]
		return inProps
	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.Identifier); ok {
			if _, inSig := b.sigMap()[id.Name]; inSig {
				return false
			}
			if id.Name == "String" && len(e.Args) == 1 {
				return b.isStaticResolvable(e.Args[0])
			}
			// cn/clsx fold to a static class string when every argument is
			// resolvable, so no hydration binding is needed.
			if id.Name == "cn" || id.Name == "clsx" {
				for _, arg := range e.Args {
					if !b.isStaticResolvable(arg) {
						return false
					}
				}
				return true
			}
			// A known cva factory call folds when its selection is static.
			if b.cvaFactories != nil {
				if _, ok := b.cvaFactories[id.Name]; ok {
					if len(e.Args) == 0 {
						return true
					}
					if obj, ok := e.Args[0].(*ast.ObjectExpr); ok {
						return b.isStaticResolvable(obj)
					}
				}
			}
		}
		return false
	case *ast.MemberExpr:
		// A class helper's member read (e.g. cn(...) as part of an expression)
		// is not static; only props.X resolves.
		if id, ok := e.Object.(*ast.Identifier); ok && id.Name == "props" {
			if _, ok := e.Property.(*ast.Identifier); ok {
				return true
			}
		}
		return false
	case *ast.BinaryExpr:
		return b.isStaticResolvable(e.Left) && b.isStaticResolvable(e.Right)
	case *ast.UnaryExpr:
		return b.isStaticResolvable(e.Arg)
	case *ast.ConditionalExpr:
		return b.isStaticResolvable(e.Test) && b.isStaticResolvable(e.Consequent) && b.isStaticResolvable(e.Alternate)
	case *ast.TemplateExpr:
		for _, p := range e.Parts {
			if !b.isStaticResolvable(p) {
				return false
			}
		}
		return true
	case *ast.ObjectExpr:
		for _, prop := range e.Properties {
			if prop == nil || prop.Spread || prop.Value == nil {
				return false
			}
			if !b.isStaticResolvable(prop.Value) {
				return false
			}
		}
		return true
	case *ast.ArrayExpr:
		for _, el := range e.Elements {
			if el != nil && !b.isStaticResolvable(el) {
				return false
			}
		}
		return true
	case *ast.TypeAssertion:
		return b.isStaticResolvable(e.Expr)
	}
	return false
}

func stmtReferencesProps(stmt ast.Stmt) bool {
	if stmt == nil {
		return false
	}
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		return referencesProps(s.Expression)
	case *ast.VarStmt:
		for _, decl := range s.Decls {
			if referencesProps(decl.Init) {
				return true
			}
		}
		return false
	case *ast.ReturnStmt:
		return referencesProps(s.Value)
	case *ast.IfStmt:
		if referencesProps(s.Test) {
			return true
		}
		for _, b := range s.Consequent {
			if stmtReferencesProps(b) {
				return true
			}
		}
		for _, b := range s.Alternate {
			if stmtReferencesProps(b) {
				return true
			}
		}
		return false
	case *ast.BlockStmt:
		for _, b := range s.Body {
			if stmtReferencesProps(b) {
				return true
			}
		}
		return false
	case *ast.ForStmt:
		for _, b := range s.Body {
			if stmtReferencesProps(b) {
				return true
			}
		}
		return false
	case *ast.WhileStmt:
		if referencesProps(s.Test) {
			return true
		}
		for _, b := range s.Body {
			if stmtReferencesProps(b) {
				return true
			}
		}
		return false
	case *ast.DoWhileStmt:
		if referencesProps(s.Test) {
			return true
		}
		for _, b := range s.Body {
			if stmtReferencesProps(b) {
				return true
			}
		}
		return false
	case *ast.TryStmt:
		for _, b := range s.Body {
			if stmtReferencesProps(b) {
				return true
			}
		}
		if s.Catch != nil {
			for _, b := range s.Catch.Body {
				if stmtReferencesProps(b) {
					return true
				}
			}
		}
		for _, b := range s.Finally {
			if stmtReferencesProps(b) {
				return true
			}
		}
		return false
	case *ast.ThrowStmt:
		return referencesProps(s.Value)
	case *ast.SwitchStmt:
		if referencesProps(s.Discriminant) {
			return true
		}
		for _, c := range s.Cases {
			if referencesProps(c.Test) {
				return true
			}
			for _, b := range c.Body {
				if stmtReferencesProps(b) {
					return true
				}
			}
		}
		return false
	default:
		return false
	}
}

func isStringType(expr ast.Expr, signals map[string]ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Literal:
		return e.Kind == ast.StringLit
	case *ast.BinaryExpr:
		// Logical operators return one of their operands. A string operand
		// (especially a string fallback like props.x || "") means the result
		// may be a string and must be emitted as a quoted JS literal.
		if e.Op != "||" && e.Op != "&&" && e.Op != "??" {
			return false
		}
		return isStringType(e.Left, signals) || isStringType(e.Right, signals)
	case *ast.ConditionalExpr:
		// Only a string when BOTH branches are strings. Array/object branches
		// (e.g. createSignal(type === "single" ? "" : [])) must be emitted as
		// real arrays or Array.isArray() checks fail at runtime.
		return isStringType(e.Consequent, signals) && isStringType(e.Alternate, signals)
	case *ast.TemplateExpr:
		return true
	case *ast.UnaryExpr:
		return e.Op == "typeof" || isStringType(e.Arg, signals)
	case *ast.ArrayExpr, *ast.ObjectExpr:
		return false
	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.Identifier); ok {
			if id.Name == "String" {
				return true
			}
			if initial, ok := signals[id.Name]; ok {
				return isStringType(initial, signals)
			}
		}
		return false
	case *ast.Identifier:
		if initial, ok := signals[e.Name]; ok {
			return isStringType(initial, signals)
		}
		return false
	}
	return false
}

// referencesAnyLocal reports whether expr (rendered to JS) references any of
// the given local names, so the caller can emit it as a runtime expression in
// dependency order rather than a folded build-time literal.
func referencesAnyLocal(expr ast.Expr, sigMap map[string]ast.Expr, locals map[string]string) bool {
	js := generateExprJS(expr, sigMap)
	if js == "" {
		return false
	}
	for name := range locals {
		if name == "props" || name == "children" {
			continue
		}
		if codeContainsIdent(js, name) {
			return true
		}
	}
	return false
}

func referencesProps(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		return e.Name == "props"
	case *ast.MemberExpr:
		return referencesProps(e.Object)
	case *ast.CallExpr:
		if referencesProps(e.Callee) {
			return true
		}
		for _, arg := range e.Args {
			if referencesProps(arg) {
				return true
			}
		}
		return false
	case *ast.BinaryExpr:
		return referencesProps(e.Left) || referencesProps(e.Right)
	case *ast.UnaryExpr:
		return referencesProps(e.Arg)
	case *ast.ConditionalExpr:
		return referencesProps(e.Test) || referencesProps(e.Consequent) || referencesProps(e.Alternate)
	case *ast.TemplateExpr:
		for _, p := range e.Parts {
			if referencesProps(p) {
				return true
			}
		}
		return false
	case *ast.ArrayExpr:
		for _, el := range e.Elements {
			if referencesProps(el) {
				return true
			}
		}
		return false
	case *ast.ObjectExpr:
		for _, prop := range e.Properties {
			if referencesProps(prop.Value) {
				return true
			}
		}
		return false
	case *ast.ArrowFn:
		if e.Expression {
			bodyExpr := arrowBodyExpr(e)
			if bodyExpr != nil {
				return referencesProps(bodyExpr)
			}
			return false
		}
		for _, stmt := range e.Body {
			if ref := stmtReferencesProps(stmt); ref {
				return true
			}
		}
		return false
	case *ast.JSXElement:
		if e.Opening != nil {
			for _, attr := range e.Opening.Attributes {
				if attr != nil && referencesProps(attr.Value) {
					return true
				}
			}
		}
		return jsxChildrenReferenceProps(e.Children)
	case *ast.JSXFragment:
		return jsxChildrenReferenceProps(e.Children)
	default:
		return false
	}
}

// jsxChildrenReferenceProps reports whether any JSX child references `props`.
func jsxChildrenReferenceProps(children []ast.JSXChild) bool {
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXExprContainer:
			if referencesProps(c.Expression) {
				return true
			}
		case *ast.JSXElementChild:
			if c.Element != nil && referencesProps(c.Element) {
				return true
			}
		case *ast.JSXFragmentChild:
			if c.Fragment != nil && referencesProps(c.Fragment) {
				return true
			}
		}
	}
	return false
}

// bodyReferencesProps reports whether any statement in a function body
// references `props`, including reads inside rendered JSX.
func bodyReferencesProps(body []ast.Stmt) bool {
	for _, stmt := range body {
		if stmtReferencesProps(stmt) {
			return true
		}
	}
	return false
}
