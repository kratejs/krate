package tsstrip

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func topFn(t *testing.T, prog *ast.Program, name string) *ast.FnDecl {
	t.Helper()
	for _, stmt := range prog.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok && exp.Declaration != nil {
			stmt = exp.Declaration
		}
		if fn, ok := stmt.(*ast.FnDecl); ok && fn.Name == name {
			return fn
		}
	}
	t.Fatalf("no function %s", name)
	return nil
}

func parseOK(t *testing.T, path, src string) *ast.Program {
	t.Helper()
	prog, errs := Parse(path, src, Options{})
	if len(errs) > 0 {
		t.Fatalf("Parse: %v", errs)
	}
	return prog
}

func TestTierDecoratorOnFunction(t *testing.T) {
	prog := parseOK(t, "a.tsx", "@server\nexport default function Page() { return <p/>; }\n")
	fn := topFn(t, prog, "Page")
	if fn.Tier != "server" || prog.Tier != "server" {
		t.Errorf("fn tier %q, file tier %q", fn.Tier, prog.Tier)
	}
	if fn.Position.Line != 2 {
		t.Errorf("position moved: %+v", fn.Position)
	}
}

func TestTierDecoratorForms(t *testing.T) {
	tests := []struct {
		name, src string
		fileTier  string
	}{
		{"same line", "@static export default function P() { return <p/>; }\n", "static"},
		{"anonymous default", "@static\nexport default function () { return <p/>; }\n", "static"},
		{"default by name", "@server\nfunction P() { return <p/>; }\nexport default P;\n", "server"},
		{"sole component export", "@runtime\nexport const Live = () => <p/>;\nexport const config = {};\n", "runtime"},
		{"not exported", "@server\nfunction Helper() { return <p/>; }\nexport default function P() { return <Helper/>; }\n", ""},
		{"named next to a default", "@server\nexport function A() { return <p/>; }\nexport default function P() { return <A/>; }\n", ""},
		{"comment between", "@client // interactive\n/** Docs. */\nexport default function P() { return <p/>; }\n", "client"},
		{"other decorator between", "@server\n@log({ a: 1 })\nexport default class Box {}\nfunction log(o: any) { return (c: any) => c; }\n", "server"},
		{"crlf", "@server\r\nexport default function P() { return <p/>; }\r\n", "server"},
		{"async runtime", "@runtime\nexport default async function P() { return <p/>; }\n", "runtime"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if prog := parseOK(t, "a.tsx", tt.src); prog.Tier != tt.fileTier {
				t.Errorf("file tier = %q, want %q", prog.Tier, tt.fileTier)
			}
			if got := ModuleTier("a.tsx", tt.src); got != tt.fileTier {
				t.Errorf("ModuleTier = %q, want %q", got, tt.fileTier)
			}
		})
	}
}

func TestTierDecoratorOnConstAndClass(t *testing.T) {
	prog := parseOK(t, "a.tsx", "@static\nconst Badge = () => <p/>;\n@server\nclass Box {}\n")
	var constTier, classTier string
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			constTier = s.Decls[0].Tier
		case *ast.ClassDecl:
			classTier = s.Tier
		}
	}
	if constTier != "static" || classTier != "server" {
		t.Errorf("const %q, class %q", constTier, classTier)
	}
}

// The same text in a string, in JSX text or on a class member is not a tier
// decorator and must reach the output unchanged.
func TestTierDecoratorLookalikes(t *testing.T) {
	for name, src := range map[string]string{
		"template": "export const t = `\n@server\nfunction x() {}\n`;\n",
		"jsx text": "export const e = <main>\n@server\nfunction text</main>;\n",
		"comment":  "/*\n@server\nfunction x() {}\n*/\nexport const a = 1;\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := BlankTierDecorators("a.tsx", src); got != src {
				t.Errorf("source changed:\n%s", got)
			}
			parseOK(t, "a.tsx", src)
		})
	}
}

// A project's own decorator named like a tier keeps its meaning.
func TestTierDecoratorYieldsToUserBinding(t *testing.T) {
	for name, src := range map[string]string{
		"class":  "import { server } from './dec';\n@server\nexport class A {}\n",
		"member": "import { server } from './dec';\nexport class A {\n  @server\n  m() {}\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := BlankTierDecorators("a.ts", src); got != src {
				t.Errorf("source changed:\n%s", got)
			}
			res, err := Strip("a.ts", src, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(res.Code, "__decorateElement") {
				t.Errorf("decorator was not kept:\n%s", res.Code)
			}
		})
	}
}

func TestTierDecoratorErrors(t *testing.T) {
	tests := []struct {
		name, src, want string
		line            int
	}{
		{"not a declaration", "const a = 1;\n@server\nconsole.log(a);\n", "must be placed on a function", 2},
		{"async", "@static\nexport default async function P() {}\n", "cannot be used on an async", 1},
		{"conflict", "@server\n@static\nexport default function P() {}\n", "conflicting tier decorators", 2},
		{"runtime helper", "@runtime\nfunction Live() {}\nexport default function P() {}\n", "@runtime must be on the default export", 1},
		{"runtime one of many", "@runtime\nexport function A() {}\nexport function B() {}\n", "@runtime must be on the default export", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errs := Parse("a.tsx", tt.src, Options{})
			if len(errs) == 0 {
				t.Fatal("expected a diagnostic")
			}
			d, ok := errs[0].(diag.Diagnostic)
			if !ok || d.Code != diag.CodeTierDecorator {
				t.Fatalf("got %T %v", errs[0], errs[0])
			}
			if !strings.Contains(d.Message, tt.want) || d.Line != tt.line {
				t.Errorf("diagnostic: %+v", d)
			}
		})
	}
}

// Decorators in plain JS are lowered like TypeScript's, which the parser alone
// cannot do.
func TestJSDecoratorsAreLowered(t *testing.T) {
	src := "function log(v, c) { return v; }\nexport class A { @log m() { return 1; } }\nexport const E = () => <p/>;\n"
	for _, path := range []string{"a.js", "a.jsx", "a.mjs"} {
		prog, errs := Parse(path, src, Options{})
		if len(errs) > 0 || prog == nil {
			t.Fatalf("%s: %v", path, errs)
		}
	}
}

// A tier name that the file also binds only means a user decorator on a class;
// on a function or const it is still the tier decorator.
func TestTierDecoratorWithBoundNameOnFunction(t *testing.T) {
	for name, src := range map[string]string{
		"imported": "import { client } from './x';\n@client\nexport function S() { return <p>{client}</p>; }\n",
		"const":    "const client = 1;\n@client\nexport default function S() { return <p>{client}</p>; }\n",
		"arrow":    "import { server } from './x';\n@server\nexport const S = () => <p>{server}</p>;\n",
	} {
		t.Run(name, func(t *testing.T) {
			prog := parseOK(t, "a.tsx", src)
			found := false
			for _, stmt := range prog.Body {
				if exp, ok := stmt.(*ast.ExportStmt); ok {
					switch d := exp.Declaration.(type) {
					case *ast.FnDecl:
						found = found || d.Tier != ""
					case *ast.VarStmt:
						found = found || (len(d.Decls) > 0 && d.Decls[0].Tier != "")
					}
				}
			}
			if !found {
				t.Error("tier was not applied")
			}
		})
	}
}

func TestTierDecoratorOnDefaultExpressionExplainsHowToName(t *testing.T) {
	for name, src := range map[string]string{
		"object": "@client\nexport default { title: 'x' };\n",
		"member": "@client\nexport default Themes.dark(View);\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, errs := Parse("a.tsx", src, Options{})
			if len(errs) == 0 {
				t.Fatal("expected a diagnostic")
			}
			d, ok := errs[0].(diag.Diagnostic)
			if !ok || d.Code != diag.CodeTierDecorator {
				t.Fatalf("got %T %v", errs[0], errs[0])
			}
			if !strings.Contains(d.Message, "export default") || !strings.Contains(d.Hint, "export default View") {
				t.Errorf("diagnostic: %+v", d)
			}
		})
	}
}

// The inline default forms take the decorator as the file's tier.
func TestTierDecoratorOnInlineDefaultComponent(t *testing.T) {
	for name, src := range map[string]string{
		"arrow":        "@static\nexport default () => <div/>;\n",
		"arrow block":  "@static\nexport default (props: { a: number }) => { return <div>{props.a}</div>; };\n",
		"function":     "@static\nexport default (function () { return <div/>; });\n",
		"memo":         "@static\nexport default memo(function V() { return <div/>; });\n",
		"forwardRef":   "@static\nexport default forwardRef((props, ref) => <div ref={ref}/>);\n",
		"memo forward": "@static\nexport default memo(forwardRef((props, ref) => <div ref={ref}/>));\n",
		"hoc":          "@static\nexport default withTheme(View);\n",
		"hoc in memo":  "@static\nexport default memo(withTheme(View));\n",
		"memo of name": "@static\nexport default memo(View);\n",
	} {
		t.Run(name, func(t *testing.T) {
			prog := parseOK(t, "a.tsx", src)
			if prog.Tier != "static" {
				t.Errorf("file tier %q, want static", prog.Tier)
			}
		})
	}
	t.Run("async needs runtime", func(t *testing.T) {
		_, errs := Parse("a.tsx", "@static\nexport default async () => <div/>;\n", Options{})
		if len(errs) != 1 || !strings.Contains(errs[0].Error(), "async") {
			t.Fatalf("got %v", errs)
		}
		prog := parseOK(t, "a.tsx", "@runtime\nexport default async () => <div/>;\n")
		if prog.Tier != "runtime" {
			t.Errorf("file tier %q", prog.Tier)
		}
	})
}
