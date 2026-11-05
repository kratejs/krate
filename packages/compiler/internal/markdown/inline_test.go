package markdown

import (
	"os"
	"path/filepath"
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

func renderInline(s string, cfg Config) string { return RenderToHTML(s, cfg) }

func TestRenderInlineBlockedAutolinkKeepsText(t *testing.T) {
	got := renderInline("see <javascript:alert(5)> now", DefaultConfig())
	if strings.Contains(got, "<a") || strings.Contains(got, "href") {
		t.Errorf("expected no anchor for javascript: autolink, got %q", got)
	}
	if !strings.Contains(got, "javascript:alert(5)") {
		t.Errorf("expected autolink text preserved, got %q", got)
	}
}

func TestRelativeImagesResolveThroughImageURL(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "img"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "img", "a b.png"), []byte("png-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.png"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	registered := map[string]string{}
	cfg := DefaultConfig()
	cfg.ImageURL = LocalImageResolver(filepath.Join(root, "docs"), filepath.Join(root, "docs", "page.md"), "/base", func(abs, u string) {
		registered[abs] = u
	})
	got := RenderToHTML("![a](./img/a%20b.png?x=1) ![b](img/missing.png) ![c](https://e.test/c.png) ![d](/abs.png) ![e](../secret.png)\n", cfg)
	if !strings.Contains(got, `src="/base/assets/a-b-`) {
		t.Errorf("relative image not rewritten:\n%s", got)
	}
	if !strings.Contains(got, `.png?x=1`) {
		t.Errorf("query lost:\n%s", got)
	}
	for _, keep := range []string{`src="img/missing.png"`, `src="https://e.test/c.png"`, `src="/abs.png"`, `src="../secret.png"`} {
		if !strings.Contains(got, keep) {
			t.Errorf("expected %s untouched:\n%s", keep, got)
		}
	}
	if len(registered) != 1 {
		t.Errorf("want exactly one registered asset, got %v", registered)
	}
}
