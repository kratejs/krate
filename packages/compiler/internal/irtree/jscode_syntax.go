package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// Rendering for ES syntax the parser represents structurally: destructuring
// patterns, classes, function expressions, accessors and computed keys.

// renderPattern renders a binding or assignment pattern (cover-grammar
// ObjectExpr/ArrayExpr with `=` defaults and `...` rest elements).
func renderPattern(e ast.Expr, signals map[string]ast.Expr) string {
	switch v := e.(type) {
	case nil:
		return ""
	case *ast.Identifier:
		return v.Name
	case *ast.ObjectExpr:
		parts := make([]string, 0, len(v.Properties))
		for _, prop := range v.Properties {
			switch {
			case prop.Spread:
				parts = append(parts, "..."+renderPattern(prop.Value, signals))
			case prop.Shorthand:
				parts = append(parts, renderPattern(prop.Value, signals))
			default:
				parts = append(parts, renderPropKey(prop, signals)+":"+renderPattern(prop.Value, signals))
			}
		}
		return "{" + strings.Join(parts, ",") + "}"
	case *ast.ArrayExpr:
		parts := make([]string, 0, len(v.Elements))
		for _, el := range v.Elements {
			parts = append(parts, renderPattern(el, signals))
		}
		s := strings.Join(parts, ",")
		if n := len(v.Elements); n > 0 && v.Elements[n-1] == nil {
			s += ","
		}
		return "[" + s + "]"
	case *ast.UnaryExpr:
		if v.Op == "..." {
			return "..." + renderPattern(v.Arg, signals)
		}
	case *ast.BinaryExpr:
		if v.Op == "=" {
			return renderPattern(v.Left, signals) + "=" + generateExprJS(v.Right, signals)
		}
	case *ast.TypeAssertion:
		return renderPattern(v.Expr, signals)
	}
	return generateExprJS(e, signals)
}

// renderPropKey renders an object property key (computed, quoted, numeric or
// identifier).
func renderPropKey(prop *ast.ObjectProp, signals map[string]ast.Expr) string {
	switch {
	case prop.Computed && prop.KeyExpr != nil:
		return "[" + generateExprJS(prop.KeyExpr, signals) + "]"
	case prop.KeyNumeric:
		return prop.Key
	}
	return jsObjectKey(prop.Key)
}

func renderMemberKey(m *ast.ClassMember, signals map[string]ast.Expr) string {
	switch {
	case m.Computed && m.KeyExpr != nil:
		return "[" + generateExprJS(m.KeyExpr, signals) + "]"
	case m.Private, m.KeyNumeric:
		return m.Name
	}
	return jsObjectKey(m.Name)
}

// renderFunctionExpr renders a non-arrow function expression.
func renderFunctionExpr(fn *ast.ArrowFn, signals map[string]ast.Expr) string {
	var b strings.Builder
	if fn.Async {
		b.WriteString("async ")
	}
	b.WriteString("function")
	if fn.Generator {
		b.WriteByte('*')
	}
	if fn.Name != "" {
		b.WriteByte(' ')
		b.WriteString(fn.Name)
	}
	b.WriteByte('(')
	b.WriteString(renderParamsJS(fn.Params, signals))
	b.WriteString(") ")
	b.WriteString(renderFnBodyJS(fnBodyStmts(fn), signals, 0))
	return "(" + b.String() + ")"
}

func fnBodyStmts(fn *ast.ArrowFn) []ast.Stmt {
	if fn.Expression {
		if e := arrowBodyExpr(fn); e != nil {
			return []ast.Stmt{&ast.ReturnStmt{Value: e}}
		}
	}
	return fn.Body
}

func renderParamsJS(params []*ast.Param, signals map[string]ast.Expr) string {
	parts := make([]string, 0, len(params))
	for _, p := range params {
		parts = append(parts, renderParam(p))
	}
	return strings.Join(parts, ", ")
}

func renderFnBodyJS(body []ast.Stmt, signals map[string]ast.Expr, level int) string {
	if len(body) == 0 {
		return "{}"
	}
	return "{\n" + renderStmtBlock(body, signals, level+1) + "\n" + indent(level) + "}"
}

// renderClassJS renders a class declaration or expression body.
func renderClassJS(c *ast.ClassDecl, signals map[string]ast.Expr, level int) string {
	if c == nil {
		return "class {}"
	}
	var b strings.Builder
	b.WriteString("class")
	if c.Name != "" {
		b.WriteByte(' ')
		b.WriteString(c.Name)
	}
	if c.SuperClass != nil {
		b.WriteString(" extends ")
		b.WriteString(generateExprJS(c.SuperClass, signals))
	}
	b.WriteString(" {")
	for _, m := range c.Members {
		if m == nil {
			continue
		}
		b.WriteByte('\n')
		b.WriteString(indent(level + 1))
		if m.Static {
			b.WriteString("static ")
		}
		switch m.Kind {
		case ast.ClassStaticBlock:
			b.WriteString(renderFnBodyJS(m.Body, signals, level+1))
			continue
		case ast.ClassField, ast.ClassAccessor:
			if m.Kind == ast.ClassAccessor {
				b.WriteString("accessor ")
			}
			b.WriteString(renderMemberKey(m, signals))
			if m.Init != nil {
				b.WriteString(" = ")
				b.WriteString(generateExprJS(m.Init, signals))
			}
			b.WriteByte(';')
			continue
		case ast.ClassGetter:
			b.WriteString("get ")
		case ast.ClassSetter:
			b.WriteString("set ")
		default:
			if m.Async {
				b.WriteString("async ")
			}
			if m.Generator {
				b.WriteByte('*')
			}
		}
		b.WriteString(renderMemberKey(m, signals))
		b.WriteByte('(')
		b.WriteString(renderParamsJS(m.Params, signals))
		b.WriteString(") ")
		b.WriteString(renderFnBodyJS(m.Body, signals, level+1))
	}
	if len(c.Members) > 0 {
		b.WriteByte('\n')
		b.WriteString(indent(level))
	}
	b.WriteByte('}')
	return b.String()
}

// renderUnaryJS renders a prefix/postfix/keyword unary operator.
func renderUnaryJS(e *ast.UnaryExpr, signals map[string]ast.Expr) string {
	if e.Arg == nil {
		return e.Op
	}
	arg := generateExprJS(e.Arg, signals)
	if e.Postfix {
		return arg + e.Op
	}
	switch e.Op {
	case "typeof":
		return "typeof " + arg
	case "void", "delete", "yield", "yield*":
		// Keyword operators need a space: `typeofx` would be an identifier.
		return "(" + e.Op + " " + arg + ")"
	case "...":
		return "..." + arg
	}
	// Keep `- -x` / `+ +x` from fusing into `--x` / `++x`.
	if strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "+") {
		return e.Op + "(" + arg + ")"
	}
	return e.Op + arg
}

func isGeneratorFn(e ast.Expr) bool {
	fn, ok := e.(*ast.ArrowFn)
	return ok && fn.Generator
}

// generateAccessorMethod renders getters, setters, generator methods and
// methods with computed or numeric keys.
func generateAccessorMethod(prop *ast.ObjectProp, signals map[string]ast.Expr) string {
	fn, ok := prop.Value.(*ast.ArrowFn)
	if !ok {
		return renderPropKey(prop, signals) + ":" + generateExprJS(prop.Value, signals)
	}
	var b strings.Builder
	switch {
	case prop.Getter:
		b.WriteString("get ")
	case prop.Setter:
		b.WriteString("set ")
	default:
		if fn.Async {
			b.WriteString("async ")
		}
		if fn.Generator {
			b.WriteByte('*')
		}
	}
	b.WriteString(renderPropKey(prop, signals))
	b.WriteByte('(')
	b.WriteString(renderParamsJS(fn.Params, signals))
	b.WriteString(") ")
	b.WriteString(renderFnBodyJS(fnBodyStmts(fn), signals, 0))
	return b.String()
}
