package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// An unclosed block in one stylesheet must be reported with its file and line,
// and must not swallow the rules of the stylesheet joined after it.
func TestBuildReportsUnbalancedCSSAndKeepsSheetsApart(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/a.css", ".a { color: rgb(1, 2, 3);\n.a2 { color: rgb(4, 5, 6) }\n")
	writeFileRel(t, root, "src/pages/b.css", ".b { color: rgb(7, 8, 9) }\n")
	writeFileRel(t, root, "src/pages/index.tsx",
		"import './a.css';\nimport './b.css';\nexport default function Page() { return <p class=\"b\">hi</p>; }\n")
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	var err error
	out := captureStderr(t, func() { err = New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if !strings.Contains(out, "src/pages/a.css:1:4") || !strings.Contains(out, "KR7141") {
		t.Errorf("no warning with file and line:\n%s", out)
	}
	sheet := readOnlyPageCSS(t, cfg.OutDir)
	idx := strings.Index(sheet, ".b {")
	if idx < 0 {
		idx = strings.Index(sheet, ".b{")
	}
	if idx < 0 {
		t.Fatalf("b.css rule missing:\n%s", sheet)
	}
	before := sheet[:idx]
	if strings.Count(before, "{") != strings.Count(before, "}") {
		t.Errorf("a.css is still open where b.css starts:\n%s", sheet)
	}
	if _, err := os.Stat(filepath.Join(cfg.OutDir, "index.html")); err != nil {
		t.Error(err)
	}
}
