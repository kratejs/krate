package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func buildDedupFixture(t *testing.T, minify bool) string {
	t.Helper()
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.module.css",
		".box {\n  color: rgb(1, 2, 3);\n  color: rgb(4, 5, 6);\n}\n")
	writeFileRel(t, root, "src/pages/index.tsx",
		"import styles from './index.module.css';\n"+
			"export default function Page() { return <div class={styles.box}>hi</div>; }\n")

	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = minify
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return readOnlyPageCSS(t, cfg.OutDir)
}

func TestBuildDedupesDeclarationsWithoutMinify(t *testing.T) {
	css := buildDedupFixture(t, false)
	if strings.Contains(css, "rgb(1, 2, 3)") || strings.Contains(css, "rgb(1,2,3)") {
		t.Fatalf("duplicate declaration not folded when minify is off:\n%s", css)
	}
	if !strings.Contains(css, "rgb(4, 5, 6)") && !strings.Contains(css, "rgb(4,5,6)") {
		t.Fatalf("kept (last) value missing:\n%s", css)
	}
	// Formatting is preserved: the rule body still spans multiple lines.
	if !strings.Contains(css, "\n") {
		t.Fatalf("formatting was collapsed when minify is off:\n%s", css)
	}
}

func TestBuildDedupesDeclarationsWithMinify(t *testing.T) {
	css := buildDedupFixture(t, true)
	// rgb(1,2,3)→#010203 and rgb(4,5,6)→#040506; the first must be gone.
	if strings.Contains(css, "010203") || strings.Contains(css, "rgb(1") {
		t.Fatalf("duplicate declaration not folded when minify is on:\n%s", css)
	}
	if !strings.Contains(css, "#040506") {
		t.Fatalf("kept (last) value missing/mangled:\n%s", css)
	}
}
