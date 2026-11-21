package scene3d

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

const header = `import { Canvas, OrbitControls, Environment } from "@krate/3d";
import { createSignal, For } from "@krate/runtime";
`

type result struct {
	t       *testing.T
	out     string
	orig    string
	changed bool
	diags   []diag.Diagnostic
	err     error
}

func compile(t *testing.T, body string, opts ...func(*Options)) result {
	t.Helper()
	src := header + body
	prog, errs := tsstrip.Parse("page.tsx", src, tsstrip.Options{})
	if len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	o := Options{File: "page.tsx", Source: src}
	for _, f := range opts {
		f(&o)
	}
	orig := astprint.Print(prog)
	changed, diags, err := Rewrite(prog, o)
	return result{t: t, out: astprint.Print(prog), orig: orig, changed: changed, diags: diags, err: err}
}

func page(scene string, pre string) string {
	return pre + "\nexport default function Page() {\n  const [rotY, setRotY] = createSignal(0);\n  return (" + scene + ");\n}\n"
}

func (r result) has(sub ...string) {
	r.t.Helper()
	for _, s := range sub {
		if !strings.Contains(r.out, s) {
			r.t.Errorf("output does not contain %q:\n%s", s, r.out)
		}
	}
}

func (r result) lacks(sub ...string) {
	r.t.Helper()
	for _, s := range sub {
		if strings.Contains(r.out, s) {
			r.t.Errorf("output contains %q:\n%s", s, r.out)
		}
	}
}

func (r result) count(sub string) int { return strings.Count(r.out, sub) }

func (r result) fallback(reason string) {
	r.t.Helper()
	if r.err != nil {
		r.t.Fatalf("unexpected error: %v", r.err)
	}
	if r.changed {
		r.t.Errorf("expected the Canvas to stay untouched:\n%s", r.out)
	}
	if len(r.diags) != 1 {
		r.t.Fatalf("want one fallback diagnostic, got %v", r.diags)
	}
	d := r.diags[0]
	if d.Code != diag.CodeScene3DFallback || !d.IsWarning() || d.Hint == "" || d.Line == 0 {
		r.t.Errorf("bad diagnostic: %+v", d)
	}
	if !strings.Contains(d.Message, reason) {
		r.t.Errorf("diagnostic %q does not mention %q", d.Message, reason)
	}
	if r.out != r.orig {
		r.t.Errorf("the tree changed:\n%s", r.out)
	}
}

// matrixOf extracts the nth 16-number array argument of staticMesh calls.
func staticMatrices(out string) [][16]float64 {
	var res [][16]float64
	rest := out
	for {
		i := strings.Index(rest, "staticMesh(")
		if i < 0 {
			return res
		}
		rest = rest[i:]
		j := strings.Index(rest, "[")
		k := strings.Index(rest, "]")
		var m [16]float64
		nums := strings.Split(rest[j+1:k], ",")
		for n, s := range nums {
			fmt.Sscan(strings.TrimSpace(s), &m[n])
		}
		res = append(res, m)
		rest = rest[k:]
	}
}

func approx(a, b float64) bool {
	d := a - b
	return d < 1e-5 && d > -1e-5
}

func parseExpr(src string) (ast.Expr, []error) {
	prog, errs := tsstrip.Parse("e.ts", "const __x = "+src+";", tsstrip.Options{})
	if len(errs) > 0 {
		return nil, errs
	}
	return prog.Body[0].(*ast.VarStmt).Decls[0].Init, nil
}
