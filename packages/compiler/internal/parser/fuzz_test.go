package parser_test

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// FuzzParse checks that the parser never panics or hangs, and that for any
// input that parses cleanly, printing the AST yields source that re-parses
// without errors and prints identically (printing is idempotent).
func FuzzParse(f *testing.F) {
	f.Add("import x from \"m\";")
	f.Add("export default function Page() { return <p>hi</p>; }")
	f.Add("<div><Head>Title</Head><main>{items.map(i => <li key={i}>{i}</li>)}</main></div>")
	f.Add("const t = `hello ${name}!`;")
	f.Add("function outer(a) { return function inner(b) { return a + b; }; }")
	f.Add(`const re = /(foo|bar)+/gi;`)
	f.Add("function f(x: number): string { return x.toString(); }")
	f.Add("const s = \"unterminated")
	f.Add("const r = /unterminated")
	f.Add(string([]byte{0xff, 0xfe, 0x00, 0x41}))
	f.Add("\x00\xff\x80\x00")
	for _, tc := range shapeCases {
		f.Add(tc.src)
	}

	f.Fuzz(func(t *testing.T, src string) {
		// Keep generated inputs bounded so one pathological case cannot stall
		// the fuzz worker past its deadline.
		if len(src) > 16*1024 {
			t.Skip()
		}
		p := parser.New(lexer.New(src).Tokenize())
		p.SetSource(src)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			return
		}
		out := astprint.Print(prog)
		p2 := parser.New(lexer.New(out).Tokenize())
		prog2 := p2.ParseProgram()
		if errs := p2.Errors(); len(errs) > 0 {
			t.Fatalf("printed output does not re-parse:\nsource: %q\nprinted: %q\nerror: %v", src, out, errs[0])
		}
		if again := astprint.Print(prog2); again != out {
			t.Fatalf("printing is not idempotent:\nsource: %q\nfirst:  %q\nsecond: %q", src, out, again)
		}
	})
}
