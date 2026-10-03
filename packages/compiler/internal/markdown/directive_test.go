package markdown

import (
	"strings"
	"testing"
)

func TestComponentDirective(t *testing.T) {
	cfg := DefaultConfig()
	_, segs := ParseMDXSegments(":::component Card title=\"Hi\"\nBody **bold** and $x$.\n:::\n", cfg)
	var jsx string
	for _, s := range segs {
		if s.JSX != "" {
			jsx = s.JSX
		}
	}
	if jsx == "" {
		t.Fatalf("no JSX segment emitted: %#v", segs)
	}
	if !strings.HasPrefix(jsx, "<Card title=\"Hi\">") {
		t.Errorf("component tag/attrs wrong: %s", jsx)
	}
	if !strings.Contains(jsx, "krate-directive-body") || !strings.Contains(jsx, "dangerouslySetInnerHTML") {
		t.Errorf("directive body not attached: %s", jsx)
	}
	if !strings.Contains(jsx, "<strong>bold</strong>") {
		t.Errorf("inner markdown not rendered: %s", jsx)
	}
	if !strings.Contains(jsx, "</Card>") {
		t.Errorf("directive not closed: %s", jsx)
	}
}

func TestComponentDirectiveRequiresName(t *testing.T) {
	cfg := DefaultConfig()
	_, segs := ParseMDXSegments(":::component\nnot a directive\n:::\n", cfg)
	for _, s := range segs {
		if strings.Contains(s.JSX, "krate-directive-body") {
			t.Fatalf("nameless :::component should not emit a directive: %#v", segs)
		}
	}
}
