package parser

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

func parseFor(path, src string) []error {
	p := New(lexer.ForFile(path, src).Tokenize())
	p.Filename = path
	p.ParseProgram()
	return p.Errors()
}

func TestTSFilesNeverOpenJSX(t *testing.T) {
	srcs := map[string]string{
		"comparison chain": "export function bad(c: {default: number, min: number, max: number}) {\n  return c.default < c.min || c.default > c.max;\n}\n",
		"type assertion":   "const n = <number>JSON.parse(s);\nconst el = <HTMLElement>document.body;\n",
		"generic arrow":    "export const id = <T,>(x: T) => x;\nexport const first = <T>(xs: T[]) => xs[0];\n",
		"less than ident":  "let a = 1, b = 2;\nconst r = a <b> c;\n",
	}
	for name, src := range srcs {
		for _, path := range []string{"a.ts", "a.mts", "a.cts"} {
			if errs := parseFor(path, src); len(errs) > 0 {
				t.Errorf("%s (%s): unexpected errors: %v", name, path, errs)
			}
		}
	}
}

func TestTSXKeepsJSX(t *testing.T) {
	src := "export const a = <div>{x < y}</div>;\n"
	if errs := parseFor("a.tsx", src); len(errs) > 0 {
		t.Fatalf("tsx: %v", errs)
	}
	if errs := parseFor("a.ts", src); len(errs) == 0 {
		t.Fatal("expected .ts to reject JSX syntax")
	}
}
