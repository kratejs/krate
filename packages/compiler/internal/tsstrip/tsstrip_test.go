package tsstrip

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func mustStrip(t *testing.T, path, src string) *Result {
	t.Helper()
	r, err := Strip(path, src, Options{})
	if err != nil {
		t.Fatalf("Strip(%s): %v", path, err)
	}
	return r
}

func TestStripLowersTypeScript(t *testing.T) {
	cases := []struct {
		name, path, src string
		want            []string
		notWant         []string
	}{
		{
			name:    "enum",
			path:    "a.ts",
			src:     "enum Color { Red = \"red\", Blue = \"blue\" }\nexport const c = Color.Red;\n",
			want:    []string{"var Color", `Color2["Red"] = "red"`},
			notWant: []string{"enum "},
		},
		{
			name:    "const enum",
			path:    "a.ts",
			src:     "const enum Dir { Up = 1 }\nexport const d = Dir.Up;\n",
			want:    []string{"const d = 1 /* Up */;"},
			notWant: []string{"enum "},
		},
		{
			name:    "namespace",
			path:    "a.ts",
			src:     "namespace NS { export const x = 1; }\nexport const y = NS.x;\n",
			want:    []string{"var NS", "NS2.x = 1"},
			notWant: []string{"namespace "},
		},
		{
			name:    "parameter properties",
			path:    "a.ts",
			src:     "export class P { constructor(private a: number, public readonly b = 2) {} }\n",
			want:    []string{"this.a = a", "this.b = b"},
			notWant: []string{"private", "readonly", ": number"},
		},
		{
			name:    "nested generics",
			path:    "a.tsx",
			src:     "const lookup: Map<string, Set<string>> = new Map<string, Set<string>>();\nexport { lookup };\n",
			want:    []string{"const lookup = /* @__PURE__ */ new Map();"},
			notWant: []string{"Set<"},
		},
		{
			name:    "generic arrow in tsx",
			path:    "a.tsx",
			src:     "export const id = <T,>(x: T): T => x;\n",
			want:    []string{"const id = (x) => x;"},
			notWant: []string{"<T"},
		},
		{
			name:    "satisfies and as const",
			path:    "a.ts",
			src:     "type C = { a: number };\nexport const c = { a: 1 } satisfies C;\nexport const t = [1, 2] as const;\n",
			want:    []string{"const c = { a: 1 };", "const t = [1, 2];"},
			notWant: []string{"satisfies", "as const", "type C"},
		},
		{
			name:    "conditional and mapped types",
			path:    "a.ts",
			src:     "type IsStr<T> = T extends string ? true : false;\ntype RO<T> = { readonly [K in keyof T]: T[K] };\ninterface I { a: IsStr<'x'> }\nexport const v = 1 >> 1;\n",
			want:    []string{"const v = 1 >> 1;"},
			notWant: []string{"type ", "interface"},
		},
		{
			name:    "decorators",
			path:    "a.ts",
			src:     "function log(v: any, c: any) { return v; }\nexport class K { @log m() { return 1; } }\n",
			want:    []string{"__decorateElement", "log"},
			notWant: []string{"@log"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := mustStrip(t, c.path, c.src)
			for _, w := range c.want {
				if !strings.Contains(r.Code, w) {
					t.Errorf("missing %q in:\n%s", w, r.Code)
				}
			}
			for _, w := range c.notWant {
				if strings.Contains(r.Code, w) {
					t.Errorf("unexpected %q in:\n%s", w, r.Code)
				}
			}
		})
	}
}

func TestStripExperimentalDecorators(t *testing.T) {
	src := "function log(t: any, k: string) {}\nexport class K { @log m() { return 1; } }\n"
	r, err := Strip("a.ts", src, Options{ExperimentalDecorators: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Code, "__decorateClass") {
		t.Errorf("experimentalDecorators should use legacy lowering:\n%s", r.Code)
	}
}

func TestStripImportElision(t *testing.T) {
	src := "import type { A } from './a';\n" +
		"import { type B, C } from './b';\n" +
		"import D from './d';\n" +
		"import './side-effect';\n" +
		"export type { A };\n" +
		"export const x: B = 1 as unknown as B;\n"
	r := mustStrip(t, "m.ts", src)
	for _, w := range []string{`import { C } from "./b";`, `import D from "./d";`, `import "./side-effect";`} {
		if !strings.Contains(r.Code, w) {
			t.Errorf("value import %q must be kept (verbatimModuleSyntax):\n%s", w, r.Code)
		}
	}
	if strings.Contains(r.Code, "./a") || strings.Contains(r.Code, "type B") {
		t.Errorf("type-only imports must be removed:\n%s", r.Code)
	}
}

func TestStripPreservesDirectivesAndJSXText(t *testing.T) {
	src := "\"use client\";\n" +
		"export default function P(props: { n: number }) {\n" +
		"  return (\n" +
		"    <div class=\"a\">\n" +
		"      Hello   world &nbsp; &amp; {props.n}{\" \"}\n" +
		"      <b>  x  </b>\n" +
		"      é ☃\n" +
		"    </div>\n" +
		"  );\n" +
		"}\n"
	r := mustStrip(t, "p.tsx", src)
	if !strings.HasPrefix(r.Code, "\"use client\";") {
		t.Errorf("directive lost:\n%s", r.Code)
	}
	for _, w := range []string{
		"\n      Hello   world &nbsp; &amp; {props.n}{\" \"}\n      <b>  x  </b>\n      é ☃\n    </div>",
	} {
		if !strings.Contains(r.Code, w) {
			t.Errorf("JSX text not preserved verbatim; want %q in:\n%s", w, r.Code)
		}
	}
}

func TestStripCRLF(t *testing.T) {
	src := "const a: number = 1;\r\nexport default function P() {\r\n  return <p>\r\n    hi {a}\r\n  </p>;\r\n}\r\n"
	prog, errs := Parse("p.tsx", src, Options{})
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	var fn *ast.FnDecl
	for _, s := range prog.Body {
		if e, ok := s.(*ast.ExportStmt); ok {
			if f, ok := e.Declaration.(*ast.FnDecl); ok {
				fn = f
			}
		}
	}
	if fn == nil {
		t.Fatalf("no exported function in %#v", prog.Body)
	}
	if fn.Position.Line != 2 {
		t.Errorf("function line = %d, want 2", fn.Position.Line)
	}
}

func TestStripSyntaxErrorDiagnostic(t *testing.T) {
	src := "// header\nexport const ok = 1;\nconst x: = 5;\n"
	_, err := Strip("bad.ts", src, Options{})
	if err == nil {
		t.Fatal("expected a syntax error")
	}
	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("error is not a diag.Diagnostic: %T %v", err, err)
	}
	if d.File != "bad.ts" || d.Line != 3 || d.Col != 10 {
		t.Errorf("diagnostic at %s:%d:%d, want bad.ts:3:10 (%s)", d.File, d.Line, d.Col, d.Message)
	}
	if d.Source != "const x: = 5;" || d.Hint == "" {
		t.Errorf("source/hint = %q / %q", d.Source, d.Hint)
	}
	if !strings.Contains(err.Error(), "bad.ts:3:10") {
		t.Errorf("formatted error lacks location: %s", err)
	}
	// The error must be cached too, and still reported.
	if _, err2 := Strip("bad.ts", src, Options{}); err2 == nil {
		t.Error("cached syntax error was swallowed")
	}
}

func TestStripSyntaxErrorMultibyteColumn(t *testing.T) {
	src := "const s = \"é☃\"; const x: = 5;\n"
	_, err := Strip("bad.ts", src, Options{})
	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("want diagnostic, got %v", err)
	}
	// Column counts runes: `const s = "é☃"; const x: ` is 25 runes.
	if d.Line != 1 || d.Col != 26 {
		t.Errorf("got %d:%d, want 1:26", d.Line, d.Col)
	}
}

// TestPositionsMapToOriginal checks that AST positions after stripping match
// the positions Krate's parser assigns when it parses the same code directly.
func TestPositionsMapToOriginal(t *testing.T) {
	// Each line of plain is the same as typed with the types removed, at the
	// same columns, so the expected positions come from parsing plain as-is.
	typed := "import { h } from './h';\n" +
		"interface Props { title: string; items: Map<string, Set<string>> }\n" +
		"type Alias = Props;\n" +
		"export default function Page(props: Props) {\n" +
		"  const label: string = props.title as string;\n" +
		"  return (\n" +
		"    <section class=\"s\">\n" +
		"      <h1>{label}</h1>\n" +
		"      <Card x={1} />\n" +
		"    </section>\n" +
		"  );\n" +
		"}\n"
	plain := "import { h } from './h';\n" +
		"\n" +
		"\n" +
		"export default function Page(props) {\n" +
		"  const label = props.title;\n" +
		"  return (\n" +
		"    <section class=\"s\">\n" +
		"      <h1>{label}</h1>\n" +
		"      <Card x={1} />\n" +
		"    </section>\n" +
		"  );\n" +
		"}\n"
	got, errs := Parse("p.tsx", typed, Options{})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	want, errs := Parse("p.jsx", plain, Options{})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	gotJSX := jsxPositions(got)
	wantJSX := jsxPositions(want)
	if len(gotJSX) == 0 || len(gotJSX) != len(wantJSX) {
		t.Fatalf("jsx elements: got %v want %v", gotJSX, wantJSX)
	}
	for i := range gotJSX {
		if gotJSX[i] != wantJSX[i] {
			t.Errorf("jsx element %d at %+v, want %+v", i, gotJSX[i], wantJSX[i])
		}
	}
}

func jsxPositions(prog *ast.Program) []ast.Pos {
	var out []ast.Pos
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case *ast.JSXElement:
			// Line and column are compared; byte offsets differ between the
			// typed and plain sources.
			out = append(out, ast.Pos{Line: v.Position.Line, Col: v.Position.Col})
			for _, c := range v.Children {
				if ec, ok := c.(*ast.JSXElementChild); ok {
					walk(ec.Element)
				}
			}
		case *ast.ReturnStmt:
			walk(v.Value)
		case *ast.FnDecl:
			for _, s := range v.Body {
				walk(s)
			}
		case *ast.ExportStmt:
			walk(v.Declaration)
		}
	}
	for _, s := range prog.Body {
		walk(s)
	}
	return out
}

func TestParserDiagnosticsRemapped(t *testing.T) {
	// `**` is valid TS (esbuild accepts it) but Krate's parser rejects it, so
	// the parser diagnostic is raised against the stripped code and must be
	// mapped back to the original line.
	src := "interface A {\n  a: number;\n}\nconst x: number = 2 ** 3;\n"
	_, errs := Parse("p.ts", src, Options{})
	if len(errs) == 0 {
		t.Skip("parser accepts **; nothing to remap")
	}
	d, ok := errs[0].(diag.Diagnostic)
	if !ok {
		t.Fatalf("%T", errs[0])
	}
	if d.Line != 4 || d.Source != "const x: number = 2 ** 3;" {
		t.Errorf("parser diagnostic at line %d source %q, want line 4", d.Line, d.Source)
	}
}

func TestDecodeMappings(t *testing.T) {
	// "AAAA" = [0,0,0,0]; "IAAI" = [4,0,0,4]; ";" new line; "AACA" = [0,0,1,0].
	sm, err := decodeMappings("AAAA,IAAI;AACA")
	if err != nil {
		t.Fatal(err)
	}
	if len(sm.lines) != 2 || len(sm.lines[0]) != 2 || len(sm.lines[1]) != 1 {
		t.Fatalf("lines = %+v", sm.lines)
	}
	if s := sm.lines[0][1]; s.genCol != 4 || s.origLn != 0 || s.origCol != 4 {
		t.Errorf("seg = %+v", s)
	}
	if s := sm.lines[1][0]; s.genCol != 0 || s.origLn != 1 || s.origCol != 4 {
		t.Errorf("seg = %+v (orig columns are cumulative across lines)", s)
	}
	// Negative deltas and multi-digit VLQs: "D" = -1, "gB" = 16.
	sm, err = decodeMappings("gBAAgB,DAAD")
	if err != nil {
		t.Fatal(err)
	}
	if s := sm.lines[0][0]; s.genCol != 15 || s.origCol != 15 {
		t.Errorf("seg = %+v", s)
	}
	if _, err := decodeMappings("A!"); err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestPosMapping(t *testing.T) {
	src := "const a: Map<string, Set<string>> = new Map();\nconst b: number = 1, c = a;\n"
	r := mustStrip(t, "m.ts", src)
	// Generated: "const a = /* @__PURE__ */ new Map();\nconst b = 1, c = a;\n"
	genLines := strings.Split(r.Code, "\n")
	col := strings.Index(genLines[1], "c =") + 1
	l, c := r.Pos(2, col)
	if l != 2 || c != strings.Index("const b: number = 1, c = a;", "c =")+1 {
		t.Errorf("Pos(2,%d) = %d:%d", col, l, c)
	}
	// Krate's lexer records a token's end; the end column maps too.
	col = strings.Index(genLines[0], "new Map") + len("new")
	l, c = r.Pos(1, col)
	if l != 1 || c != strings.Index(src, "new Map")+len("new") {
		t.Errorf("Pos(1,%d) = %d:%d", col, l, c)
	}
	if l, c := r.Pos(0, 0); l != 0 || c != 0 {
		t.Errorf("zero position changed: %d:%d", l, c)
	}
}

func TestBOM(t *testing.T) {
	src := "\xef\xbb\xbfconst x: = 1;\n"
	_, err := Strip("b.ts", src, Options{})
	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("want diagnostic: %v", err)
	}
	// Krate's lexer counts the BOM as one column.
	if d.Line != 1 || d.Col != 11 {
		t.Errorf("got %d:%d, want 1:11", d.Line, d.Col)
	}
}

func TestStripConcurrentCache(t *testing.T) {
	src := "enum E { A }\nexport const e: E = E.A;\n"
	var wg sync.WaitGroup
	codes := make([]string, 16)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			prog, errs := Parse("c.ts", src, Options{})
			if len(errs) > 0 || prog == nil {
				t.Errorf("parse failed: %v", errs)
			}
			r, _ := Strip("c.ts", src, Options{})
			codes[i] = r.Code
		}(i)
	}
	wg.Wait()
	for _, c := range codes {
		if c != codes[0] {
			t.Fatal("non-deterministic output")
		}
	}
}

func TestApplies(t *testing.T) {
	for p, want := range map[string]bool{"a.ts": true, "a.tsx": true, "a.mts": true, "a.cts": true, "a.js": false, "a.jsx": false, "a.mdx": false} {
		if Applies(p) != want {
			t.Errorf("Applies(%s) = %v", p, !want)
		}
	}
}
