package jsscan

import (
	"strings"
	"testing"
)

func TestCodeOnly(t *testing.T) {
	src := "a(\"x\"); // c\n/* d */ b(`t${ c(1) }u`); /re/.test(s)"
	got := CodeOnly(src)
	if len(got) != len(src) || strings.Count(got, "\n") != 1 {
		t.Fatalf("length or lines changed: %q", got)
	}
	for _, drop := range []string{`"x"`, "// c", "/* d */", "`t", "u`", "/re/"} {
		if strings.Contains(got, drop) {
			t.Errorf("%q is not code but survived: %q", drop, got)
		}
	}
	for _, keep := range []string{"a(", "b(", "c(1)", ".test(s)"} {
		if !strings.Contains(got, keep) {
			t.Errorf("%q is code but was dropped: %q", keep, got)
		}
	}
}

func TestPairs(t *testing.T) {
	src := `f("(", [1, {a: 2}]) // )`
	p := Pairs(src, Mask(src))
	open, closeAt := 1, strings.Index(src, ") //")
	if p[open] != closeAt || p[closeAt] != open {
		t.Errorf("outer parens pair %d with %d, want %d", open, p[open], closeAt)
	}
	if str := strings.Index(src, `"("`) + 1; p[str] != -1 {
		t.Errorf("a bracket inside a string must not pair")
	}
	if c := strings.Index(src, "{"); src[p[c]] != '}' {
		t.Errorf("brace pairs with %d", p[c])
	}
}
