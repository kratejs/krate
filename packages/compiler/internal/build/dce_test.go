package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestPruneModuleCSS(t *testing.T) {
	scoped := map[string]bool{"used_abc123": true, "unused_def456": true, "wrap_ghi789": true}
	used := map[string]bool{"used_abc123": true}
	in := strings.Join([]string{
		".used_abc123 { color: red; }",
		".unused_def456 { color: blue; }",
		".wrap_ghi789 .unused_def456 { color: green; }",
		".unused_def456:hover { color: pink; }",
		"@media (min-width: 600px) { .unused_def456 { color: black; } }",
		".global-class { color: gray; }",
	}, "\n")

	out := pruneModuleCSS(in, scoped, used)
	if !strings.Contains(out, ".used_abc123 { color: red; }") {
		t.Errorf("used rule was removed:\n%s", out)
	}
	if strings.Contains(out, ".unused_def456 { color: blue; }") {
		t.Errorf("unused module rule was kept:\n%s", out)
	}
	// Compound, pseudo, at-rule and non-module selectors must survive.
	for _, keep := range []string{
		".wrap_ghi789 .unused_def456",
		".unused_def456:hover",
		"@media (min-width: 600px)",
		".global-class",
	} {
		if !strings.Contains(out, keep) {
			t.Errorf("conservative rule %q was removed:\n%s", keep, out)
		}
	}
}

func TestBuildPrunesUnusedModuleCSS(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.module.css", ".used { color: rgb(1, 2, 3); }\n.unused { color: rgb(9, 9, 9); }\n")
	writeFileRel(t, root, "src/pages/index.tsx",
		"import styles from './index.module.css';\n"+
			"export default function Page() { return <div class={styles.used}>hi</div>; }\n")

	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	css := readOnlyPageCSS(t, cfg.OutDir)
	if !strings.Contains(css, "rgb(1, 2, 3)") && !strings.Contains(css, "rgb(1,2,3)") {
		t.Fatalf("used module class missing from output:\n%s", css)
	}
	if strings.Contains(css, "rgb(9, 9, 9)") || strings.Contains(css, "rgb(9,9,9)") {
		t.Fatalf("unused module class was not pruned:\n%s", css)
	}
}

func TestBuildKeepsModuleCSSWhenDCEOff(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.module.css", ".used { color: rgb(1, 2, 3); }\n.unused { color: rgb(9, 9, 9); }\n")
	writeFileRel(t, root, "src/pages/index.tsx",
		"import styles from './index.module.css';\n"+
			"export default function Page() { return <div class={styles.used}>hi</div>; }\n")

	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	off := false
	cfg.Dce.CSS = &off
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	css := readOnlyPageCSS(t, cfg.OutDir)
	if !strings.Contains(css, "rgb(9, 9, 9)") && !strings.Contains(css, "rgb(9,9,9)") {
		t.Fatalf("--no-dce must keep unused module classes:\n%s", css)
	}
}

// readOnlyPageCSS returns the concatenated contents of page stylesheets in
// outDir (styles.<hash>.css), excluding the global Tailwind sheet.
func readOnlyPageCSS(t *testing.T, outDir string) string {
	t.Helper()
	var b strings.Builder
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "styles.") || !strings.HasSuffix(name, ".css") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String()
}
