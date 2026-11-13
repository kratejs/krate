package icons

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// Local icons/ folder resolution

func TestResolveLocalIcon(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "icons", "menu.svg"),
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><path d="M0 0h32v32H0z"/></svg>`)

	ic, err := ResolveIcon(root, "menu")
	if err != nil {
		t.Fatalf("ResolveIcon: %v", err)
	}
	if !strings.Contains(ic.Inner, `<path d="M0 0h32v32H0z" />`) {
		t.Errorf("expected inner path, got %q", ic.Inner)
	}
	if ic.ViewBox != "0 0 32 32" {
		t.Errorf("expected local viewBox preserved, got %q", ic.ViewBox)
	}
}

func TestResolveLocalIconRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	// A name that would escape the icons/ directory must be rejected, not read.
	for _, name := range []string{"../secret", "..\\secret", "a/b", "a", "..", ".", "/etc/passwd"} {
		if _, err := ResolveIcon(root, name); err == nil {
			t.Errorf("expected traversal name %q to be rejected", name)
		}
	}
}

func TestSanitizeSVGHardening(t *testing.T) {
	drop := map[string]string{
		"set href":          `<a><set attributeName="href" to="javascript:alert(1)"/></a>`,
		"set xlink:href":    `<a><set attributeName="xlink:href" to="https://evil.test/"/></a>`,
		"animate values":    `<a><animate attributeName="x" values="javascript:alert(1)"/></a>`,
		"animate onbegin":   `<animate attributeName="onbegin" to="alert(1)"/>`,
		"style import":      `<style>@import url(https://evil.test/x.css);</style>`,
		"style external":    `<style>.a{fill:url(https://evil.test/x.svg#a)}</style>`,
		"style attr":        `<path style="fill:url(https://evil.test/x)" d="M0 0"/>`,
		"external image":    `<image href="https://evil.test/x.png"/>`,
		"external image xl": `<image xlink:href="//evil.test/x.png" width="1"/>`,
		"nested iframe":     `<svg><iframe src="https://evil.test/"></iframe></svg>`,
		"nested foreign":    `<foreignObject><foreignObject></foreignObject><div onclick="x()">hi</div></foreignObject>`,
		"object":            `<object data="https://evil.test/x.swf"></object>`,
		"embed":             `<embed src="https://evil.test/x.swf"/>`,
	}
	for name, in := range drop {
		out := sanitizeSVG(in)
		for _, bad := range []string{"evil.test", "javascript:", "<iframe", "<object", "<embed", "<image", "<set", "onbegin", "onclick", "<div", "@import", "<foreignobject"} {
			if strings.Contains(strings.ToLower(out), bad) {
				t.Errorf("%s: output still contains %q:\n%s", name, bad, out)
			}
		}
	}
	keep := map[string]string{
		"style fragment": `<style>.a{fill:url(#g)}</style>`,
		"style plain":    `<style>.a{fill:red}</style>`,
		"animate":        `<animate attributeName="opacity" values="0;1" dur="1s"/>`,
		"local image":    `<image href="#sprite"/>`,
		"gradient":       `<path style="fill:url(#g)" d="M0 0"/>`,
	}
	for name, in := range keep {
		out := sanitizeSVG(in)
		if !strings.Contains(out, strings.Fields(strings.TrimPrefix(in, "<"))[0]) || strings.TrimSpace(out) == "" {
			t.Errorf("%s: legitimate markup dropped: %q -> %q", name, in, out)
		}
	}
	if out := sanitizeSVG(`<style>.a{fill:url(#g)}</style><path d="M0 0"/>`); !strings.Contains(out, "<path") || !strings.Contains(out, "url(#g)") {
		t.Errorf("fragment style should survive, got %q", out)
	}
}
