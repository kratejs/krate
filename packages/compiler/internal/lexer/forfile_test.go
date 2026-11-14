package lexer

import "testing"

func TestForFileJSXByExtension(t *testing.T) {
	src := "x = <div>a</div>"
	hasJSX := func(path string) bool {
		for _, tok := range ForFile(path, src).Tokenize() {
			if tok.Kind == LT && tok.JSX {
				return true
			}
		}
		return false
	}
	for path, want := range map[string]bool{
		"a.ts": false, "a.MTS": false, "a.cts": false,
		"a.tsx": true, "a.jsx": true, "a.js": true, "a.mjs": true,
	} {
		if got := hasJSX(path); got != want {
			t.Errorf("%s: JSX opened = %v, want %v", path, got, want)
		}
	}
}
