package build

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// A markdown page with a broken frontmatter value fails the build with the
// file and line instead of shipping a wrong title such as `"bad`.
func TestBuildFailsOnInvalidFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.md", "---\ntitle: \"bad\ndescription: ok\n---\n\n# Hello\n")
	cfg := config.Default()
	cfg.Resolve(root)
	err := New(root, cfg).BuildAll()
	if err == nil {
		t.Fatal("BuildAll succeeded with an unterminated frontmatter quote")
	}
	msg := err.Error()
	if !strings.Contains(msg, "index.md:2:8") || !strings.Contains(msg, "invalid frontmatter") {
		t.Errorf("error lacks file:line:\n%s", msg)
	}
}

func TestBuildKeepsValidFrontmatterEdgeCases(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.md",
		"---\ntitle: \"Hello: World\"\ndescription: >-\n  a long\n  description\ntags: [a, \"b, c\"]\n---\n\n# Hello\n")
	writeFileRel(t, root, "src/pages/empty.md", "---\n---\n\n# Empty\n")
	cfg := config.Default()
	cfg.Resolve(root)
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html := readOut(t, cfg.OutDir, "index.html")
	if !strings.Contains(html, "Hello: World") {
		t.Errorf("title with a quoted colon was lost:\n%.600s", html)
	}
}
