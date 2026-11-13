package inlineaction

import (
	"reflect"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

var posType = reflect.TypeOf(ast.Pos{})

// walk visits every AST node reachable from v depth first. enter returning
// false skips a node's children (and its exit call); exit runs after the
// children of every node enter accepted.
func walk(v reflect.Value, enter func(n any) bool, exit func(n any)) {
	switch v.Kind() {
	case reflect.Interface:
		if !v.IsNil() {
			walk(v.Elem(), enter, exit)
		}
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		n := v.Interface()
		if !enter(n) {
			return
		}
		walk(v.Elem(), enter, exit)
		if exit != nil {
			exit(n)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walk(v.Index(i), enter, exit)
		}
	case reflect.Struct:
		if v.Type() == posType {
			return
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				walk(v.Field(i), enter, exit)
			}
		}
	}
}

func walkNode(n any, enter func(n any) bool, exit func(n any)) {
	walk(reflect.ValueOf(n), enter, exit)
}

// fnParts returns the parts of a function-like node.
func fnParts(n any) (params []*ast.Param, body []ast.Stmt, async bool, name string, arrow bool, ok bool) {
	switch f := n.(type) {
	case *ast.FnDecl:
		return f.Params, f.Body, f.Async, f.Name, false, true
	case *ast.ArrowFn:
		return f.Params, f.Body, f.Async, f.Name, !f.Function, true
	case *ast.ClassMember:
		if f.Kind == ast.ClassField || f.Kind == ast.ClassAccessor {
			return nil, nil, false, "", false, false
		}
		return f.Params, f.Body, f.Async, "", false, true
	}
	return nil, nil, false, "", false, false
}

// patternNames collects the names a binding pattern (cover grammar) declares.
func patternNames(e ast.Expr, out map[string]bool) {
	switch p := e.(type) {
	case nil:
	case *ast.Identifier:
		out[p.Name] = true
	case *ast.ObjectExpr:
		for _, prop := range p.Properties {
			if prop == nil {
				continue
			}
			if prop.Value == nil {
				if prop.Key != "" {
					out[prop.Key] = true
				}
				continue
			}
			patternNames(prop.Value, out)
		}
	case *ast.ArrayExpr:
		for _, el := range p.Elements {
			patternNames(el, out)
		}
	case *ast.BinaryExpr:
		if p.Op == "=" {
			patternNames(p.Left, out)
		}
	case *ast.UnaryExpr:
		if p.Op == "..." {
			patternNames(p.Arg, out)
		}
	case *ast.TypeAssertion:
		patternNames(p.Expr, out)
	case *ast.ParenExpr:
		patternNames(p.Expr, out)
	}
}

// declaredNames lists every name a function declares for itself: parameters,
// and var/let/const, function, class and catch bindings anywhere in its body
// outside nested functions. It treats them as visible in the whole function,
// which can only over-approximate shadowing.
func declaredNames(params []*ast.Param, body []ast.Stmt, own string) map[string]bool {
	out := map[string]bool{}
	if own != "" {
		out[own] = true
	}
	for _, p := range params {
		if p == nil {
			continue
		}
		if p.Name != "" && !strings.HasPrefix(p.Name, "{") {
			out[p.Name] = true
		}
		patternNames(p.Target, out)
	}
	walkNode(body, func(n any) bool {
		switch x := n.(type) {
		case *ast.FnDecl:
			if x.Name != "" {
				out[x.Name] = true
			}
			return false
		case *ast.ArrowFn:
			return false
		case *ast.ClassMember:
			return false
		case *ast.ClassDecl:
			if x.Name != "" {
				out[x.Name] = true
			}
			return false
		case *ast.ClassExpr:
			return false
		case *ast.VarDecl:
			if x.Name != "" && !x.IsDestructuring {
				out[x.Name] = true
			}
			for _, name := range x.Names {
				out[name] = true
			}
			if x.RestName != "" {
				out[x.RestName] = true
			}
			patternNames(x.Target, out)
		case *ast.CatchClause:
			if x.ParamTarget != nil {
				patternNames(x.ParamTarget, out)
			} else if x.Param != "" {
				out[x.Param] = true
			}
		case *ast.ForInStmt:
			if x.Keyword != "" {
				patternNames(x.Left, out)
			}
		}
		return true
	}, nil)
	return out
}

// finder collects the free names of a function or statement: identifiers it
// reads that no function inside it declares.
type finder struct {
	scopes   []map[string]bool
	free     []string
	seen     map[string]bool
	usesThis bool
	// thisDepth counts enclosing non-arrow functions entered below the root.
	thisDepth int
	funcs     []bool // per scope: whether it is a non-arrow function
}

func newFinder() *finder { return &finder{seen: map[string]bool{}} }

func (f *finder) ref(name string) {
	if name == "" || f.seen[name] {
		return
	}
	for i := len(f.scopes) - 1; i >= 0; i-- {
		if f.scopes[i][name] {
			return
		}
	}
	f.seen[name] = true
	f.free = append(f.free, name)
}

func (f *finder) visit(root any) {
	walkNode(root, func(n any) bool {
		switch x := n.(type) {
		case *ast.Identifier:
			f.ref(x.Name)
			return false
		case *ast.MemberExpr:
			f.visit(x.Object)
			if x.Computed {
				f.visit(x.Property)
			}
			return false
		case *ast.ObjectProp:
			if x.Shorthand && x.Value == nil {
				f.ref(x.Key)
			}
		case *ast.ThisExpr:
			if f.thisDepth == 0 {
				f.usesThis = true
			}
		case *ast.JSXElement:
			if x.Opening != nil {
				name := x.Opening.Name
				if i := strings.IndexAny(name, ".:"); i >= 0 {
					name = name[:i]
				} else if name == "" || name[0] < 'A' || name[0] > 'Z' {
					name = ""
				}
				f.ref(name)
			}
		default:
			if params, body, _, name, arrow, ok := fnParts(n); ok {
				f.scopes = append(f.scopes, declaredNames(params, body, name))
				f.funcs = append(f.funcs, !arrow)
				if !arrow {
					f.thisDepth++
				}
			}
		}
		return true
	}, func(n any) {
		if _, _, _, _, _, ok := fnParts(n); ok {
			f.scopes = f.scopes[:len(f.scopes)-1]
			if f.funcs[len(f.funcs)-1] {
				f.thisDepth--
			}
			f.funcs = f.funcs[:len(f.funcs)-1]
		}
	})
}

// freeNames returns the free names of a node in order of first use, and
// whether it reads `this` outside any nested non-arrow function.
func freeNames(n any) ([]string, bool) {
	f := newFinder()
	f.visit(n)
	return f.free, f.usesThis
}
