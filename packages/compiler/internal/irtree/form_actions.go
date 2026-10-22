package irtree

import (
	"net/url"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// actionStateCallee is the runtime helper `useActionState` is lowered to (see
// bundler/react_rewrite.go). Its arguments are (action, getState, setState,
// setPending, initialState, permalink); the IR builder reads the action,
// initial state and permalink to render the no-JS form fallback.
const actionStateCallee = "__krateActionState"

// isFormActionAttr reports the React 19 action props: `action` on a form and
// `formAction` on a submit button or input.
func isFormActionAttr(name string) bool {
	switch name {
	case "action", "formAction", "formaction":
		return true
	}
	return false
}

// actionStateCall returns the lowered useActionState call a local binding was
// initialized with, or nil.
func (b *builder) actionStateCall(name string) *ast.CallExpr {
	for _, stmt := range b.localFnBody {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, decl := range vs.Decls {
			if decl == nil || decl.IsDestructuring || decl.Name != name {
				continue
			}
			call, ok := decl.Init.(*ast.CallExpr)
			if !ok {
				return nil
			}
			if id, ok := call.Callee.(*ast.Identifier); ok && id.Name == actionStateCallee {
				return call
			}
			return nil
		}
	}
	return nil
}

// isFunctionAction reports whether an action prop value is a function the
// runtime must invoke (a client function, a useActionState dispatch, or any
// value only known at hydration time) rather than a URL.
func (b *builder) isFunctionAction(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.ArrowFn:
		return true
	case *ast.TypeAssertion:
		return b.isFunctionAction(e.Expr)
	case *ast.ParenExpr:
		return b.isFunctionAction(e.Expr)
	case *ast.Identifier:
		if b.actionStateCall(e.Name) != nil {
			return true
		}
		if _, ok := b.actionURLs[e.Name]; ok {
			return false
		}
		if b.isSignalName(e.Name) {
			return false
		}
		if v, ok := b.localProps[e.Name]; ok && !v.ContainsUnknown() && v.Kind() != jsvalue.KindFunction {
			return false
		}
		return true
	case *ast.MemberExpr:
		if _, ok := b.actionAttrURL(e); ok {
			return false
		}
		if obj, ok := e.Object.(*ast.Identifier); ok && obj.Name == "props" && !e.Computed {
			if prop, ok := e.Property.(*ast.Identifier); ok {
				if b.localFuncProps[prop.Name] {
					return true
				}
				if v, ok := b.localProps[prop.Name]; ok && !v.ContainsUnknown() {
					return v.Kind() == jsvalue.KindFunction
				}
			}
			return true
		}
		return false
	case *ast.CallExpr:
		// `save.bind(null, id)` and action factories; signal reads are URLs.
		if id, ok := e.Callee.(*ast.Identifier); ok && b.isSignalName(id.Name) {
			return false
		}
		if mem, ok := e.Callee.(*ast.MemberExpr); ok && !mem.Computed {
			if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "bind" {
				return true
			}
		}
		return false
	}
	return false
}

// formActionBinding binds a function-valued action prop at hydration: the
// runtime's document submit listener reads it from the element. The value is
// wrapped in a getter so it resolves when the form is submitted.
func (b *builder) formActionBinding(attr *ast.JSXAttr, id SlotID) (RefBinding, bool) {
	var js string
	switch v := attr.Value.(type) {
	case *ast.ArrowFn:
		js = renderArrowFn(v, b.sigMap())
	case *ast.Identifier:
		js = v.Name
	default:
		js = generateExprJS(attr.Value, b.sigMap())
	}
	if strings.TrimSpace(js) == "" {
		return RefBinding{}, false
	}
	cb := "function(el){__krateBindFormAction(el,function(){return (" + js + ");});}"
	return RefBinding{ElementSlotID: id, Callback: cb}, true
}

// actionStateURL renders the native POST target of a form whose action is a
// useActionState dispatch wrapping a server action. The initial state travels
// as the leading argument in `__krate_args` so the server can call
// action(initialState, formData) without JavaScript; a literal permalink
// becomes the post-submit redirect.
func (b *builder) actionStateURL(name string) (string, bool) {
	call := b.actionStateCall(name)
	if call == nil || len(call.Args) == 0 {
		return "", false
	}
	target, ok := call.Args[0].(*ast.Identifier)
	if !ok {
		return "", false
	}
	base, ok := b.actionURLs[target.Name]
	if !ok {
		return "", false
	}
	q := url.Values{}
	initial := "null"
	if len(call.Args) >= 5 {
		if v := b.evalValue(call.Args[4]); !v.ContainsUnknown() {
			if s, ok := jsvalue.JSONStringify(v); ok {
				initial = s
			}
		} else if b.locateOnly == 0 {
			b.warnCodeAt(call.Args[4].Pos(), diag.CodeActionStateInitial, "useActionState initial state is not known at build time; forms submitted without JavaScript receive null as the previous state",
				"use a literal initial state (or JSON-serializable constant) so the no-JS submission gets the same previous state")
		}
	}
	q.Set("__krate_args", "["+initial+"]")
	if len(call.Args) >= 6 {
		if lit, ok := call.Args[5].(*ast.Literal); ok && lit.Kind == ast.StringLit {
			permalink := strings.Trim(lit.Value, "\"'`")
			if strings.HasPrefix(permalink, "/") && !strings.HasPrefix(permalink, "//") {
				q.Set("__krate_return", permalink)
			}
		}
	}
	return base + "?" + q.Encode(), true
}

// renderActionAttr renders a resolved server-action URL as the element's
// native action attribute plus the enhancement marker.
func renderActionAttr(tag, attrName, actionURL string) string {
	name := "action"
	if tag != "form" || attrName != "action" {
		name = "formaction"
	}
	return " " + name + `="` + escape.HTMLAttr(actionURL) + `" data-krate-action`
}

// warnCodeAt records a positioned warning with a stable diagnostic code.
func (b *builder) warnCodeAt(pos ast.Pos, code diag.Code, msg, hint string) {
	b.warnings = append(b.warnings, b.diagAt(pos, diag.SeverityWarning, msg, hint).WithCode(code))
}
