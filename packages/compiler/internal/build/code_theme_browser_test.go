package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// codeThemeProject writes a small project whose pages show highlighted code
// through the Code component and a bare highlighted block, links the workspace
// packages, and returns its root.
func codeThemeProject(t *testing.T, pages map[string]string) string {
	t.Helper()
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for rel, src := range pages {
		writeFileRel(t, root, filepath.Join("src", "pages", rel), src)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	// The build finds @krate/runtime's dist through node_modules relative to
	// the working directory.
	t.Chdir(root)
	for _, pkg := range []string{"runtime", "components", "core"} {
		if err := os.Symlink(filepath.Join(repoRoot, "packages", pkg), filepath.Join(root, "node_modules", "@krate", pkg)); err != nil {
			t.Skipf("cannot link workspace packages: %v", err)
		}
	}
	return root
}

func requireBrowserTest(t *testing.T) string {
	t.Helper()
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser tests")
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	return browser
}

func runBrowserScript(t *testing.T, script, outDir, browser string, extra ...string) {
	t.Helper()
	// The working directory may have moved into a test project.
	_, here, _, _ := runtime.Caller(0)
	abs := filepath.Join(filepath.Dir(here), "testdata", script)
	args := append([]string{abs, outDir, browser}, extra...)
	out, err := exec.Command("node", args...).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("%s failed: %v\n%s", script, err, out)
	}
	t.Logf("%s", out)
}

const codeThemePageSrc = `import { Code } from "@krate/components";
export default function Page() {
	return (
		<main>
			<Code lang="ts" title="a.ts">{` + "`const a: number = 1;\\nfunction f(x) { return x + \"s\"; }`" + `}</Code>
			<SyntaxHighlight lang="ts">{` + "`const b = 2;`" + `}</SyntaxHighlight>
		</main>
	);
}
`

// TestCodeThemeBrowser checks in a real browser that highlighted code follows
// the colour mode (system preference and data-theme, switched at runtime).
func TestCodeThemeBrowser(t *testing.T) {
	browser := requireBrowserTest(t)
	root := codeThemeProject(t, map[string]string{"index.tsx": codeThemePageSrc})
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	runBrowserScript(t, "code_theme.mjs", cfg.OutDir, browser)
}
