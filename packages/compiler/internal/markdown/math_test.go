package markdown

import (
	"strings"
	"testing"
)

func TestWrapMath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Math = true
	src := "Euler: $e^{i\\pi}+1=0$ ok.\n\n$$\\int_0^1 x\\,dx$$\n\nUse `$5` here.\n"
	got := RenderToHTML(src, cfg)
	if !strings.Contains(got, "krate-math-inline") {
		t.Errorf("inline math not wrapped:\n%s", got)
	}
	if !strings.Contains(got, "krate-math-block") {
		t.Errorf("block math not wrapped:\n%s", got)
	}
	// Inline code must be left untouched.
	if strings.Contains(got, `<code><span class="krate-math`) {
		t.Errorf("math wrapped inside inline code:\n%s", got)
	}
	if !strings.Contains(got, "$5") {
		t.Errorf("inline code content changed:\n%s", got)
	}
}

func TestWrapMathDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Math = false
	got := RenderToHTML("Cost is $5 today.", cfg)
	if strings.Contains(got, "krate-math") {
		t.Errorf("math should not be wrapped when disabled:\n%s", got)
	}
}

func TestWrapMathSkipsInlineCodeAfterText(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Math = true
	got := RenderToHTML("Use `$x$` now.\n", cfg)
	if strings.Contains(got, "<code><span") || strings.Contains(got, "<code>$x$</code>") == false {
		t.Errorf("math should not be wrapped inside inline code after text:\n%s", got)
	}
}

func TestWrapMathSkipsCodeBlock(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Math = true
	got := RenderToHTML("```\n$x$\n```\n", cfg)
	if strings.Contains(got, "krate-math") {
		t.Errorf("math wrapped inside a fenced code block:\n%s", got)
	}
}
