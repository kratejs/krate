package markdown

import (
	"strings"
	"testing"
)

func TestSafeURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"https", "https://example.com", true},
		{"http", "http://example.com", true},
		{"relative", "/about", true},
		{"relative dot", "./docs/guide", true},
		{"anchor", "#section", true},
		{"protocol relative", "//cdn.example.com/x.js", true},
		{"mailto", "mailto:a@b.com", true},
		{"tel", "tel:+15551234567", true},
		{"ftp", "ftp://example.com", true},
		{"empty", "", true},
		{"javascript", "javascript:alert(1)", false},
		{"javascript uppercase", "JaVaScRiPt:alert(1)", false},
		{"javascript entity encoded", "javascript&#58;alert(1)", false},
		{"data", "data:text/html,<script>", false},
		{"vbscript", "vbscript:msgbox(1)", false},
		{"file", "file:///etc/passwd", false},
		{"relative with colon", "docs/guide:3", true},
		{"scheme lookalike in query", "https://x.com/?next=a:b", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeURL(tt.url); got != tt.want {
				t.Errorf("safeURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

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

func TestRenderInlineBlockedLinkPreservesLabel(t *testing.T) {
	cfg := DefaultConfig()
	got := renderInline("[click me](javascript:alert(1))", cfg)
	if !strings.Contains(got, "click me") {
		t.Errorf("expected label text preserved, got %q", got)
	}
	if strings.Contains(got, "<a") {
		t.Errorf("expected no anchor for javascript: URL, got %q", got)
	}
}
