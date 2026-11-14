package markdown

import (
	"strings"
	"testing"
)

func TestRenderInlineSchemeAllowlist(t *testing.T) {
	cfg := DefaultConfig()
	tests := []struct {
		name  string
		input string
		allow bool
	}{
		{"https link", "[a](https://example.com)", true},
		{"relative link", "[a](/about)", true},
		{"anchor link", "[a](#top)", true},
		{"javascript link", "[a](javascript:alert(1))", false},
		{"data link", "[a](data:text/html,<b>hi</b>)", false},
		{"safe image", "![pic](/img.png)", true},
		{"javascript image", "![pic](javascript:alert(1))", false},
		{"data image", "![pic](data:image/svg+xml;base64,PHN2Zz4=)", false},
		{"javascript autolink", "<javascript:alert(5)>", false},
		{"uppercase javascript autolink", "<JAVASCRIPT:alert(6)>", false},
		{"data autolink", "<data:text/html,hi>", false},
		{"vbscript autolink", "<vbscript:msgbox(1)>", false},
		{"https autolink", "<https://example.com>", true},
		{"mailto autolink", "<me@example.com>", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderInline(tt.input, cfg)
			scriptable := strings.Contains(got, "<a href=") || strings.Contains(got, "<img")
			if tt.allow && !scriptable {
				t.Errorf("expected rendered link/image for %q, got %q", tt.input, got)
			}
			if !tt.allow && scriptable {
				t.Errorf("expected scheme-neutralized output for %q, got %q", tt.input, got)
			}
		})
	}
}

func renderInline(s string, cfg Config) string { return RenderToHTML(s, cfg) }
