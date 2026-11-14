package tsstrip

import (
	"strings"
	"testing"
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
