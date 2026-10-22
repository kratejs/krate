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
	f.Add([]byte("#"))
	f.Add([]byte(""))

	f.Add([]byte{0xff, 0xfe, 0x00, 0x41})

	f.Fuzz(func(t *testing.T, data []byte) {
		src := string(data)
		toks := New(src).Tokenize()
		prevEnd := 0
		for _, tk := range toks {
			if tk.Offset < prevEnd || tk.End < tk.Offset || tk.End > len(src) {
				t.Fatalf("token %v spans [%d,%d) after %d", tk, tk.Offset, tk.End, prevEnd)
			}
			if !tk.Escaped && src[tk.Offset:tk.End] != tk.Value {
				t.Fatalf("token %v: Value differs from src[Offset:End] = %q", tk, src[tk.Offset:tk.End])
			}
			if tk.Line < 1 || tk.Col < 1 {
				t.Fatalf("token %v has position %d:%d", tk, tk.Line, tk.Col)
			}
			prevEnd = tk.End
		}
		if toks[len(toks)-1].Kind != EOF {
			t.Fatal("token stream does not end in EOF")
		}
	})
}
