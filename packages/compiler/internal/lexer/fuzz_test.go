package lexer

import "testing"

func FuzzLexer(f *testing.F) {
	f.Add([]byte("import x from \"m\";"))
	f.Add([]byte("export default function Page() { return <p>hi</p>; }"))
	f.Add([]byte("<div><Head>Title</Head><main>{items.map(i => <li key={i}>{i}</li>)}</main></div>"))
	f.Add([]byte("const t = `hello ${name}!`;"))
	f.Add([]byte("function outer(a) { return function inner(b) { return a + b; }; }"))
	f.Add([]byte(`const re = /(foo|bar)+/gi;`))
	f.Add([]byte("function f(x: number): string { return x.toString(); }"))
	f.Add([]byte("const s = \"unterminated"))
	f.Add([]byte("const r = /unterminated"))
	f.Add([]byte(""))
	f.Add([]byte{0xff, 0xfe, 0x00, 0x41})

	f.Fuzz(func(t *testing.T, data []byte) {
		_ = New(string(data)).Tokenize()
	})
}
