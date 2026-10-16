package parser

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

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

	f.Fuzz(func(t *testing.T, src string) {
		toks := lexer.New(src).Tokenize()
		p := New(toks)
		p.SetSource(src)
		p.ParseProgram()
		p.Errors()
	})
}
