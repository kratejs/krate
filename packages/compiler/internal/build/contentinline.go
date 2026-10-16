package build

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/content"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// InlineContent rewrites `getCollection("name")` calls (and the `getCollection`
// import) into a literal array expression parsed from the collection's baked
// data. After inlining, existing build-time folding (`collectLocalVars`,
// `.map()` resolution, member-chain const folding) renders collection content
// into static HTML with no runtime work.
// It also rewrites direct collection imports - `import { blog } from
// "krate/content"` - but those are less common; the primary API is
// getCollection.
func (b *Builder) InlineContent(prog *ast.Program) {
	if prog == nil || len(b.contentCollections) == 0 {
		return
	}
	// Collect simple literal local bindings (e.g. `const slug = "hello"` after
	// route-param substitution) so collection chains that reference them can be
	// evaluated. Without this, `find((p) => p.slug === slug)` can't fold.
	bindings := collectLiteralBindings(prog)
	for _, stmt := range prog.Body {
		inlineContentStmt(stmt, b.contentCollections, bindings)
	}
}

// collectLiteralBindings maps local/module identifiers bound to a literal
// expression to their JS source, so collection chain evaluation can reference
// them. Only simple literals (string/number/bool/null) and literal-only
// templates are collected.
func collectLiteralBindings(prog *ast.Program) map[string]string {
	out := map[string]string{}
	var walkStmts func(stmts []ast.Stmt)
	var walkStmt func(stmt ast.Stmt)
	walkStmt = func(stmt ast.Stmt) {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d.Name == "" || d.Init == nil {
					continue
				}
				if js, ok := literalJS(d.Init); ok {
					out[d.Name] = js
				}
			}
		case *ast.FnDecl:
			walkStmts(s.Body)
		case *ast.ExportStmt:
			if s.Declaration != nil {
				walkStmt(s.Declaration)
			}
		case *ast.ReturnStmt:
			// An expression-bodied arrow is `Body: [ReturnStmt]`; descend so a
			// nested block's consts are still found (best-effort, flat scope).
		case *ast.IfStmt:
			walkStmts(s.Consequent)
			walkStmts(s.Alternate)
		case *ast.BlockStmt:
			walkStmts(s.Body)
		}
	}
	walkStmts = func(stmts []ast.Stmt) {
		for _, s := range stmts {
			walkStmt(s)
		}
	}
	walkStmts(prog.Body)
	return out
}

// literalJS returns the JS source for a simple literal expression.
func literalJS(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.Literal:
		// GenerateExprJS renders literals to valid JS source (escapes intact).
		return irtree.GenerateExprJS(e, nil), true
	case *ast.TemplateExpr:
		// Only literal-only templates (no interpolated identifiers).
		for _, p := range e.Parts {
			if _, ok := p.(*ast.Literal); !ok {
				return "", false
			}
		}
		js := irtree.GenerateExprJS(e, nil)
		return js, js != ""
	}
	return "", false
}

// inlineContentStmt walks a statement, replacing getCollection call chains with
// literal arrays.
func inlineContentStmt(stmt ast.Stmt, collections map[string][]content.Entry, bindings map[string]string) {
	switch s := stmt.(type) {
	case *ast.ExportStmt:
		if s.Declaration != nil {
			inlineContentStmt(s.Declaration, collections, bindings)
		}
	case *ast.FnDecl:
		for _, inner := range s.Body {
			inlineContentStmt(inner, collections, bindings)
		}
	case *ast.VarStmt:
		for _, d := range s.Decls {
			if d.Init != nil {
				d.Init = inlineContentExpr(d.Init, collections, bindings)
			}
		}
	case *ast.ReturnStmt:
		if s.Value != nil {
			s.Value = inlineContentExpr(s.Value, collections, bindings)
		}
	case *ast.ExprStmt:
		if s.Expression != nil {
			s.Expression = inlineContentExpr(s.Expression, collections, bindings)
		}
	case *ast.IfStmt:
		if s.Test != nil {
			s.Test = inlineContentExpr(s.Test, collections, bindings)
		}
		for _, inner := range s.Consequent {
			inlineContentStmt(inner, collections, bindings)
		}
		for _, inner := range s.Alternate {
			inlineContentStmt(inner, collections, bindings)
		}
	case *ast.BlockStmt:
		for _, inner := range s.Body {
			inlineContentStmt(inner, collections, bindings)
		}
	case *ast.ForStmt:
		if s.Test != nil {
			s.Test = inlineContentExpr(s.Test, collections, bindings)
		}
		for _, inner := range s.Body {
			inlineContentStmt(inner, collections, bindings)
		}
	}
}

// inlineContentExpr rewrites getCollection(...) chains within an expression.
func inlineContentExpr(expr ast.Expr, collections map[string][]content.Entry, bindings map[string]string) ast.Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		// getCollection("name")
		if lit := getCollectionName(e); lit != "" {
			return literalArrayExpr(collections[lit])
		}
		// `...map(fn)` rooted at getCollection: fold the receiver chain
		// (filter/sort/etc.) to a literal array so the existing `.map()` list
		// resolution sees real items. The `.map` itself is left intact.
		if arr := foldContentMapReceiver(e, collections, bindings); arr != nil {
			if mem, ok := e.Callee.(*ast.MemberExpr); ok {
				mem.Object = arr
			}
			return e
		}
		// Any other chain rooted at getCollection (e.g. `.filter().sort()`)
		// folds wholesale to a literal array.
		if arr := foldCollectionChain(e, collections, bindings); arr != nil {
			return arr
		}
		e.Callee = inlineContentExpr(e.Callee, collections, bindings)
		for i, arg := range e.Args {
			e.Args[i] = inlineContentExpr(arg, collections, bindings)
		}
		return e
	case *ast.MemberExpr:
		if arr := foldCollectionChain(e, collections, bindings); arr != nil {
			return arr
		}
		e.Object = inlineContentExpr(e.Object, collections, bindings)
		return e
	case *ast.ArrayExpr:
		for i, el := range e.Elements {
			e.Elements[i] = inlineContentExpr(el, collections, bindings)
		}
		return e
	case *ast.ObjectExpr:
		for _, prop := range e.Properties {
			if prop.Value != nil {
				prop.Value = inlineContentExpr(prop.Value, collections, bindings)
			}
		}
		return e
	case *ast.BinaryExpr:
		e.Left = inlineContentExpr(e.Left, collections, bindings)
		e.Right = inlineContentExpr(e.Right, collections, bindings)
		return e
	case *ast.ConditionalExpr:
		e.Test = inlineContentExpr(e.Test, collections, bindings)
		e.Consequent = inlineContentExpr(e.Consequent, collections, bindings)
		e.Alternate = inlineContentExpr(e.Alternate, collections, bindings)
		return e
	case *ast.UnaryExpr:
		e.Arg = inlineContentExpr(e.Arg, collections, bindings)
		return e
	case *ast.TemplateExpr:
		for i, part := range e.Parts {
			e.Parts[i] = inlineContentExpr(part, collections, bindings)
		}
		return e
	case *ast.ArrowFn:
		// Do not descend into nested function bodies for argument rewriting;
		// the map callback contains an element binding, not getCollection.
		return e
	case *ast.JSXElement:
		inlineContentJSX(e, collections, bindings)
		return e
	case *ast.JSXFragment:
		inlineContentJSXChildren(e.Children, collections, bindings)
		return e
	}
	return expr
}

func inlineContentJSX(el *ast.JSXElement, collections map[string][]content.Entry, bindings map[string]string) {
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Value == nil {
			continue
		}
		attr.Value = inlineContentExpr(attr.Value, collections, bindings)
	}
	inlineContentJSXChildren(el.Children, collections, bindings)
}

func inlineContentJSXChildren(children []ast.JSXChild, collections map[string][]content.Entry, bindings map[string]string) {
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXExprContainer:
			c.Expression = inlineContentExpr(c.Expression, collections, bindings)
		case *ast.JSXElementChild:
			inlineContentJSX(c.Element, collections, bindings)
		case *ast.JSXFragmentChild:
			inlineContentJSXChildren(c.Fragment.Children, collections, bindings)
		}
	}
}

// getCollectionName returns the literal collection name when expr is a
// `getCollection("name")` call, else "".
func getCollectionName(call *ast.CallExpr) string {
	id, ok := call.Callee.(*ast.Identifier)
	if !ok || id.Name != "getCollection" || len(call.Args) != 1 {
		return ""
	}
	if lit, ok := call.Args[0].(*ast.Literal); ok && lit.Kind == ast.StringLit {
		return lit.Value
	}
	return ""
}

// foldContentMapReceiver detects a `.map(...)` call whose receiver is a chain
// rooted at `getCollection("name")`, evaluates the receiver chain (everything
// before `.map`) in QuickJS, and returns the resulting array as an AST
// ArrayExpr. Returns nil when the chain isn't rooted at getCollection or the
// evaluation fails.
func foldContentMapReceiver(call *ast.CallExpr, collections map[string][]content.Entry, bindings map[string]string) ast.Expr {
	mem, ok := call.Callee.(*ast.MemberExpr)
	if !ok {
		return nil
	}
	if prop, ok := mem.Property.(*ast.Identifier); !ok || prop.Name != "map" {
		return nil
	}
	return foldCollectionChain(mem.Object, collections, bindings)
}

// foldCollectionChain evaluates a chain rooted at `getCollection("name")`
// (including the getCollection call itself) in QuickJS and returns the result
// as a literal AST expression. Returns nil when the expression isn't rooted at
// getCollection, contains unsupported constructs, or evaluation fails.
func foldCollectionChain(expr ast.Expr, collections map[string][]content.Entry, bindings map[string]string) ast.Expr {
	base := rootCollectionName(expr)
	if base == "" {
		return nil
	}
	entries, ok := collections[base]
	if !ok {
		return nil
	}
	chainJS, ok := renderContentChain(expr, base, entries, bindings)
	if !ok {
		return nil
	}
	out, err := jsruntime.EvaluateExpr("JSON.stringify(" + chainJS + ")")
	if err != nil || out == "" || out == "undefined" {
		return nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		return nil
	}
	return arrayASTFromJSON(decoded)
}

// rootCollectionName returns the collection name if expr is (or is chained off)
// a direct `getCollection("name")` call.
func rootCollectionName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.CallExpr:
		if name := getCollectionName(e); name != "" {
			return name
		}
		if mem, ok := e.Callee.(*ast.MemberExpr); ok {
			return rootCollectionName(mem.Object)
		}
	case *ast.MemberExpr:
		return rootCollectionName(e.Object)
	}
	return ""
}

// renderContentChain renders an expression chain rooted at `getCollection(base)`
// as JavaScript, substituting the baked entries array for the root call.
// Returns ok=false on any construct it can't faithfully render.
func renderContentChain(expr ast.Expr, base string, entries []content.Entry, bindings map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.CallExpr:
		if getCollectionName(e) == base {
			return content.CollectionJS(entries), true
		}
		mem, ok := e.Callee.(*ast.MemberExpr)
		if !ok {
			return "", false
		}
		prop, ok := mem.Property.(*ast.Identifier)
		if !ok {
			return "", false
		}
		recv, ok := renderContentChain(mem.Object, base, entries, bindings)
		if !ok {
			return "", false
		}
		var args []string
		for _, a := range e.Args {
			js := renderArgWithBindings(a, bindings)
			if js == "" {
				return "", false
			}
			args = append(args, js)
		}
		return recv + "." + prop.Name + "(" + strings.Join(args, ", ") + ")", true
	case *ast.MemberExpr:
		recv, ok := renderContentChain(e.Object, base, entries, bindings)
		if !ok {
			return "", false
		}
		prop, ok := e.Property.(*ast.Identifier)
		if !ok {
			return "", false
		}
		return recv + "." + prop.Name, true
	}
	return "", false
}

// renderArgWithBindings renders a callback argument (e.g. a filter/find
// predicate) to JS, substituting known literal local bindings first so chains
// like `.find((p) => p.slug === slug)` can be evaluated. If the argument
// contains any unknown identifier, returns "" so the caller bails rather than
// emitting a reference that would throw.
func renderArgWithBindings(arg ast.Expr, bindings map[string]string) string {
	substituted := substituteBindings(arg, bindings)
	if hasUnknownIdentifier(substituted) {
		return ""
	}
	return irtree.GenerateExprJS(substituted, nil)
}

// substituteBindings replaces bare identifier references with their literal
// source. It skips identifiers that are declared as parameters inside an arrow
// (so `p` in `(p) => p.slug` is not replaced).
func substituteBindings(expr ast.Expr, bindings map[string]string) ast.Expr {
	if len(bindings) == 0 {
		return expr
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		if js, ok := bindings[e.Name]; ok {
			if lit := literalFromJS(js); lit != nil {
				return lit
			}
		}
		return e
	case *ast.ArrowFn:
		shadow := map[string]bool{}
		for _, p := range e.Params {
			if p != nil {
				shadow[p.Name] = true
			}
		}
		local := make(map[string]string, len(bindings))
		for k, v := range bindings {
			if !shadow[k] {
				local[k] = v
			}
		}
		for _, stmt := range e.Body {
			substituteBindingsStmt(stmt, local)
		}
		return e
	case *ast.MemberExpr:
		e.Object = substituteBindings(e.Object, bindings)
		return e
	case *ast.CallExpr:
		e.Callee = substituteBindings(e.Callee, bindings)
		for i, a := range e.Args {
			e.Args[i] = substituteBindings(a, bindings)
		}
		return e
	case *ast.BinaryExpr:
		e.Left = substituteBindings(e.Left, bindings)
		e.Right = substituteBindings(e.Right, bindings)
		return e
	case *ast.UnaryExpr:
		e.Arg = substituteBindings(e.Arg, bindings)
		return e
	case *ast.ConditionalExpr:
		e.Test = substituteBindings(e.Test, bindings)
		e.Consequent = substituteBindings(e.Consequent, bindings)
		e.Alternate = substituteBindings(e.Alternate, bindings)
		return e
	case *ast.TemplateExpr:
		for i, part := range e.Parts {
			e.Parts[i] = substituteBindings(part, bindings)
		}
		return e
	case *ast.ArrayExpr:
		for i, el := range e.Elements {
			e.Elements[i] = substituteBindings(el, bindings)
		}
		return e
	case *ast.ObjectExpr:
		for _, p := range e.Properties {
			if p.Value != nil {
				p.Value = substituteBindings(p.Value, bindings)
			}
		}
		return e
	}
	return expr
}

func substituteBindingsStmt(stmt ast.Stmt, bindings map[string]string) {
	if rs, ok := stmt.(*ast.ReturnStmt); ok && rs.Value != nil {
		rs.Value = substituteBindings(rs.Value, bindings)
	}
}

// literalFromJS parses a JS literal source string back into an AST literal.
func literalFromJS(js string) ast.Expr {
	prog := parseInlineSource("(" + js + ")")
	if prog == nil {
		return nil
	}
	for _, stmt := range prog.Body {
		if es, ok := stmt.(*ast.ExprStmt); ok {
			switch es.Expression.(type) {
			case *ast.Literal, *ast.TemplateExpr, *ast.ArrayExpr, *ast.ObjectExpr:
				return es.Expression
			}
		}
	}
	return nil
}

// hasUnknownIdentifier reports whether expr references a bare identifier that
// is neither a known JS global nor bound by an enclosing arrow parameter.
func hasUnknownIdentifier(expr ast.Expr) bool {
	found := false
	var walk func(e ast.Expr, bound map[string]bool)
	walk = func(e ast.Expr, bound map[string]bool) {
		if e == nil || found {
			return
		}
		switch x := e.(type) {
		case *ast.Identifier:
			if !bound[x.Name] && !knownGlobalIdent(x.Name) {
				found = true
			}
		case *ast.MemberExpr:
			walk(x.Object, bound)
		case *ast.CallExpr:
			walk(x.Callee, bound)
			for _, a := range x.Args {
				walk(a, bound)
			}
		case *ast.BinaryExpr:
			walk(x.Left, bound)
			walk(x.Right, bound)
		case *ast.UnaryExpr:
			walk(x.Arg, bound)
		case *ast.ConditionalExpr:
			walk(x.Test, bound)
			walk(x.Consequent, bound)
			walk(x.Alternate, bound)
		case *ast.TemplateExpr:
			for _, p := range x.Parts {
				walk(p, bound)
			}
		case *ast.ArrayExpr:
			for _, el := range x.Elements {
				walk(el, bound)
			}
		case *ast.ObjectExpr:
			for _, p := range x.Properties {
				walk(p.Value, bound)
			}
		case *ast.ArrowFn:
			inner := map[string]bool{}
			for k := range bound {
				inner[k] = true
			}
			for _, p := range x.Params {
				if p != nil {
					inner[p.Name] = true
				}
			}
			for _, stmt := range x.Body {
				if rs, ok := stmt.(*ast.ReturnStmt); ok {
					walk(rs.Value, inner)
				}
			}
		}
	}
	walk(expr, nil)
	return found
}

// knownGlobalIdent reports whether name is a JS global we allow in evaluated
// predicates/literals.
func knownGlobalIdent(name string) bool {
	switch name {
	case "undefined", "null", "true", "false", "NaN", "Infinity",
		"JSON", "Math", "String", "Number", "Boolean", "Object", "Array", "Date":
		return true
	}
	return false
}

// arrayASTFromJSON converts a JSON-decoded array into an AST ArrayExpr of
// object/array/scalar literals.
func arrayASTFromJSON(v any) ast.Expr {
	switch t := v.(type) {
	case []any:
		els := make([]ast.Expr, len(t))
		for i, item := range t {
			els[i] = arrayASTFromJSON(item)
		}
		return &ast.ArrayExpr{Elements: els}
	case map[string]any:
		props := make([]*ast.ObjectProp, 0, len(t))
		for k, val := range t {
			props = append(props, &ast.ObjectProp{Key: k, Value: arrayASTFromJSON(val)})
		}
		// Stable key order for deterministic output.
		sortObjectProps(props)
		return &ast.ObjectExpr{Properties: props}
	case string:
		return &ast.Literal{Kind: ast.StringLit, Value: t}
	case bool:
		if t {
			return &ast.Literal{Kind: ast.BoolLit, Value: "true"}
		}
		return &ast.Literal{Kind: ast.BoolLit, Value: "false"}
	case float64:
		if t == float64(int64(t)) {
			return &ast.Literal{Kind: ast.NumberLit, Value: fmt.Sprintf("%d", int64(t))}
		}
		return &ast.Literal{Kind: ast.NumberLit, Value: fmt.Sprintf("%g", t)}
	case nil:
		return &ast.Literal{Kind: ast.NullLit, Value: "null"}
	default:
		return &ast.Literal{Kind: ast.StringLit, Value: fmt.Sprintf("%v", t)}
	}
}

func sortObjectProps(props []*ast.ObjectProp) {
	for i := 1; i < len(props); i++ {
		for j := i; j > 0 && props[j-1].Key > props[j].Key; j-- {
			props[j-1], props[j] = props[j], props[j-1]
		}
	}
}

// literalArrayExpr parses the collection's JS array literal (produced by
// content.CollectionJS) back into an AST ArrayExpr. Parsing the generated
// source reuses the real lexer/parser, so nested objects, arrays, and escaping
// are handled uniformly.
func literalArrayExpr(entries []content.Entry) ast.Expr {
	src := content.CollectionJS(entries)
	prog := parseInlineSource(src)
	if prog == nil {
		return &ast.ArrayExpr{}
	}
	for _, stmt := range prog.Body {
		if es, ok := stmt.(*ast.ExprStmt); ok {
			if arr, ok := es.Expression.(*ast.ArrayExpr); ok {
				return arr
			}
		}
	}
	return &ast.ArrayExpr{}
}

// parseInlineSource parses a snippet of JS source into a program.
func parseInlineSource(src string) *ast.Program {
	toks := lexer.New(src).Tokenize()
	return parser.New(toks).ParseProgram()
}
