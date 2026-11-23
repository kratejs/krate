package build

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestServerActionPropOfIslandInBrowser: a server action passed as a prop to a
// "use client" island and used as `<form action={run}>` through useActionState
// receives the FormData (not `{}`), keeps its bound arguments, and the SPA
// router does not turn the submit into a GET navigation.
func TestServerActionPropOfIslandInBrowser(t *testing.T) {
	t.Parallel()
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the island action e2e")
	}
	if !canStopInProcessServer {
		t.Skip("the in-process server needs a unix host")
	}
	ensureRuntimeDist(t)
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	root := t.TempDir()
	if err := copyTree(t, filepath.Join("testdata", "action_island_prop"), root); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "packages")); err != nil {
		t.Fatal(err)
	}
	withPreact(t, root)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "action_island_prop.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, base, browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "Cannot find module") {
			requireE2E(t, "browser unavailable: %s", out)
		}
		t.Fatalf("island action e2e failed: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var got struct {
		Plain  string   `json:"plain"`
		Bound  string   `json:"bound"`
		URL    string   `json:"url"`
		Direct string   `json:"direct"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &got); err != nil {
		t.Fatalf("bad output: %v\n%s", err, out)
	}
	if got.Plain != "saved ada-plain" || got.Bound != "tag:ada-bound" || got.URL != "/" || got.Direct != "/done/?n=x" || len(got.Errors) != 0 {
		t.Fatalf("got %+v\n%s", got, out)
	}
}
