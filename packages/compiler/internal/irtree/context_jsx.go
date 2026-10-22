package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// Context JSX: `<Ctx.Provider value>`, React 19 `<Ctx value>` and
// `<Ctx.Consumer>{value => ...}</Ctx.Consumer>`.

// isContextBinding reports whether name is a createContext binding (in this
// module or an imported one) rather than a component.
func (b *builder) isContextBinding(name string) bool {
	if name == "" {
		return false
	}
	if _, ok := b.contextDefaults[name]; !ok {
		return false
	}
	return b.functions[name] == nil
}

// providerContext returns the context a provider element names:
// `<Ctx.Provider>` or, for a known context binding, React 19's `<Ctx>`.
func (b *builder) providerContext(name string) (string, bool) {
	if ctx, ok := strings.CutSuffix(name, ".Provider"); ok {
		return ctx, true
	}
	if b.isContextBinding(name) {
		return name, true
	}
	return "", false
}

// consumerContext returns the context of a `<Ctx.Consumer>` element.
func (b *builder) consumerContext(name string) (string, bool) {
	ctx, ok := strings.CutSuffix(name, ".Consumer")
	if !ok || !b.isContextBinding(ctx) {
		return "", false
	}
	return ctx, true
}

// contextReadExpr is the expression a context read evaluates to at this point
// of the build: the active dynamic provider value (the getter prop is called),
// else the folded provider value or default as a literal.
func (b *builder) contextReadExpr(ctx string) ast.Expr {
	if v, ok := b.contextValues[ctx]; ok && v != nil {
		if mem, ok := v.(*ast.MemberExpr); ok {
			if prop, ok := mem.Property.(*ast.Identifier); ok && strings.HasPrefix(prop.Name, contextPropPrefix) {
				return &ast.CallExpr{Callee: v}
			}
		}
		return v
	}
	if v, ok := b.localProps[contextMarkerPrefix+ctx]; ok {
		if expr := valueToExpr(v); expr != nil {
			return expr
		}
		// Not representable as a literal (it holds functions): keep the read,
		// which the build-time evaluator folds.
		return &ast.CallExpr{Callee: &ast.MemberExpr{
			Object:   &ast.Identifier{Name: ctx},
			Property: &ast.Identifier{Name: "useContext"},
		}}
	}
	return &ast.Identifier{Name: "undefined"}
}

// valueToExpr converts a serializable build-time value to an expression.
func valueToExpr(v jsvalue.Value) ast.Expr {
	if v.Kind() == jsvalue.KindUndefined {
		return &ast.Identifier{Name: "undefined"}
	}
	src, ok := jsvalue.JSSource(v)
	if !ok {
		return nil
	}
	prog := parseExprAsProgram("(" + src + ")")
	if prog == nil || len(prog.Body) != 1 {
		return nil
	}
	if es, ok := prog.Body[0].(*ast.ExprStmt); ok {
		if p, ok := es.Expression.(*ast.ParenExpr); ok {
			return p.Expr
		}
		return es.Expression
	}
	return nil
}

// buildContextConsumer lowers `<Ctx.Consumer>{(v) => body}</Ctx.Consumer>` by
// substituting the nearest provider value for the render-prop parameter and
// building the body in place, so the rendered output binds to the provider
// value exactly like a `useContext` read would.
func (b *builder) buildContextConsumer(el *ast.JSXElement, ctx, parentID string) []SlotNode {
	var arrow *ast.ArrowFn
	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXText:
			if strings.TrimSpace(c.Value) != "" {
				arrow = nil
			}
		case *ast.JSXExprContainer:
			if fn, ok := c.Expression.(*ast.ArrowFn); ok && arrow == nil {
				arrow = fn
				continue
			}
			arrow = nil
		default:
			arrow = nil
		}
	}
	if arrow == nil || len(arrow.Params) > 1 {
		b.warnCompat(el.Position, diag.Code("KR8004"),
			"<"+el.Opening.Name+"> needs a single render function child, e.g. {(value) => <span>{value}</span>}",
			"pass one arrow function as the only child, or read the value with useContext("+ctx+") in a component")
		return nil
	}
	body := arrowBodyExpr(arrow)
	if body == nil {
		return nil
	}
	value := b.contextReadExpr(ctx)
	if len(arrow.Params) == 1 && arrow.Params[0] != nil {
		p := arrow.Params[0]
		switch {
		case p.Pattern != "":
			names, ok := plainObjectPatternNames(p.Pattern)
			if !ok {
				b.warnCompat(el.Position, diag.Code("KR8004"),
					"<"+el.Opening.Name+"> render function destructures its value with renames, defaults or nesting",
					"take the value as one parameter, e.g. {(value) => ... value.field ...}")
				return nil
			}
			for _, name := range names {
				member := &ast.MemberExpr{Object: value, Property: &ast.Identifier{Name: name}}
				field := ast.Expr(member)
				if v := memberASTOf(value, member.Property); v != nil {
					field = v
				}
				body = substituteCallbackLocals(arrow, body, name, field, "", nil)
				body = substituteListParam(body, name, field)
			}
		case p.Name != "":
			body = substituteCallbackLocals(arrow, body, p.Name, value, "", nil)
			body = substituteListParam(body, p.Name, value)
		}
	}
	return b.exprChild(body, parentID, true)
}

// plainObjectPatternNames returns the names of a `{ a, b }` pattern with no
// renames, defaults, rest or nesting.
func plainObjectPatternNames(pattern string) ([]string, bool) {
	p := strings.TrimSpace(pattern)
	if len(p) < 2 || p[0] != '{' || p[len(p)-1] != '}' {
		return nil, false
	}
	var names []string
	for _, part := range strings.Split(p[1:len(p)-1], ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.ContainsAny(part, ":={}[].") {
			return nil, false
		}
		names = append(names, part)
	}
	return names, len(names) > 0
}

// warnCompat records a coded React compatibility warning, once per position.
func (b *builder) warnCompat(pos ast.Pos, code diag.Code, msg, hint string) {
	key := string(code) + ":" + itoa(pos.Line) + ":" + itoa(pos.Col) + ":" + b.srcFnName()
	if b.eventDiagSeen == nil {
		b.eventDiagSeen = make(map[string]bool)
	}
	if b.eventDiagSeen[key] || b.locateOnly > 0 {
		return
	}
	b.eventDiagSeen[key] = true
	b.warnings = append(b.warnings, b.diagAt(pos, diag.SeverityWarning, msg, hint).WithCode(code))
}
