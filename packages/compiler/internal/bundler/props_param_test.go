package bundler

import (
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
)

func TestSingleParamComponentsGetThePropsName(t *testing.T) {
	dir := writeModules(t, map[string]string{
		"page.tsx": "function Box(p) { return <i title={p.title}>{p.p}</i>; }\n" +
			"const Arrow = (q) => <b>{q.x}</b>;\n" +
			"function Keep(props, ref) { return <u>{props.x}</u>; }\n" +
			"function Busy(p) { const props = 1; return <u>{p.x}{props}</u>; }\n" +
			"function helper(z) { return z.a; }\n" +
			"function Layout(children) { return <div>{children}</div>; }\n" +
			"export default function P() { return <main><Box title=\"t\" p=\"v\" /><Arrow x=\"1\" /><Keep x=\"2\" /><Busy x=\"3\" /></main>; }\n",
	})
	bundle, err := New(dir).Bundle(filepath.Join(dir, "page.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	prog := moduleNamed(t, bundle, "page.tsx").Program
	paramOf := map[string]string{}
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			if len(s.Params) > 0 {
				paramOf[s.Name] = s.Params[0].Name
			}
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if a, ok := d.Init.(*ast.ArrowFn); ok && len(a.Params) == 1 {
					paramOf[d.Name] = a.Params[0].Name
				}
			}
		}
	}
	want := map[string]string{"Box": "props", "Arrow": "props", "Keep": "props", "Busy": "p", "helper": "z", "Layout": "children"}
	for name, param := range want {
		if paramOf[name] != param {
			t.Errorf("%s: parameter %q, want %q", name, paramOf[name], param)
		}
	}
	// The property name survives while the reference follows the parameter.
	var members []string
	for _, stmt := range prog.Body {
		if fn, ok := stmt.(*ast.FnDecl); ok && fn.Name == "Box" {
			visitStmts(fn.Body, false, func(n any, _ bool) bool {
				if m, ok := n.(*ast.MemberExpr); ok {
					if obj, ok := m.Object.(*ast.Identifier); ok {
						if prop, ok := m.Property.(*ast.Identifier); ok {
							members = append(members, obj.Name+"."+prop.Name)
						}
					}
				}
				return true
			})
		}
	}
	if len(members) != 2 || members[0] != "props.title" || members[1] != "props.p" {
		t.Errorf("members = %v", members)
	}
}
