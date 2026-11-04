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

// TestServerActionRedirectInBrowser submits hydrated forms whose server action
// calls redirect(): the SPA router must swap the page without page errors,
// for both a plain function action and a useActionState dispatch.
func TestServerActionRedirectInBrowser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the action redirect e2e")
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
	if err := copyTree(t, filepath.Join("testdata", "action_redirect"), root); err != nil {
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
	script, err := filepath.Abs(filepath.Join("testdata", "action_redirect.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, base, browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "Cannot find module") {
			requireE2E(t, "browser unavailable: %s", out)
		}
		t.Fatalf("action redirect e2e failed: %v\n%s", err, out)
	}
	last := out
	if i := strings.LastIndex(strings.TrimSpace(string(out)), "\n"); i >= 0 {
		last = []byte(strings.TrimSpace(string(out))[i+1:])
	}
	if strings.Contains(string(last), `"errors":[]`) {
		t.Logf("%s", last)
		return
	}
	t.Fatalf("page errors during the redirect:\n%s", out)
}

// TestNoJSActionStateFeedbackInBrowser posts a useActionState form with
// JavaScript disabled: the 303 lands on the pre-rendered page, which must
// show the validation state the action returned.
func TestNoJSActionStateFeedbackInBrowser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the no-JS action state e2e")
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
	if err := copyTree(t, filepath.Join("testdata", "action_nojs"), root); err != nil {
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
	script, err := filepath.Abs(filepath.Join("testdata", "action_nojs.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, base, browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "Cannot find module") {
			requireE2E(t, "browser unavailable: %s", out)
		}
		t.Fatalf("no-JS e2e failed: %v\n%s", err, out)
	}
	last := strings.TrimSpace(string(out))
	if i := strings.LastIndex(last, "\n"); i >= 0 {
		last = last[i+1:]
	}
	var res struct {
		InitialBanner int `json:"initialBanner"`
		AfterPost     struct {
			Path   string   `json:"path"`
			Banner []string `json:"banner"`
			InForm int      `json:"inForm"`
		} `json:"afterPost"`
		FlashCookieLeft bool `json:"flashCookieLeft"`
		AfterReload     int  `json:"afterReload"`
		AfterValid      struct {
			Path string `json:"path"`
			Page string `json:"page"`
		} `json:"afterValid"`
		Hydrated struct {
			Error   string `json:"error"`
			Notices int    `json:"notices"`
		} `json:"hydrated"`
	}
	if err := json.Unmarshal([]byte(last), &res); err != nil {
		t.Fatalf("bad script output: %v\n%s", err, out)
	}
	if res.InitialBanner != 0 {
		t.Errorf("a fresh page must not show a notice: %s", last)
	}
	if res.AfterPost.Path != "/" || len(res.AfterPost.Banner) != 1 || !strings.Contains(res.AfterPost.Banner[0], "user is required") || res.AfterPost.InForm != 1 {
		t.Errorf("the validation error must be rendered into the form without JavaScript: %s", last)
	}
	if res.FlashCookieLeft || res.AfterReload != 0 {
		t.Errorf("the flash must be single use: %s", last)
	}
	if res.Hydrated.Error != "user is required" || res.Hydrated.Notices != 0 {
		t.Errorf("a hydrated form must start from the flash state and drop the notice: %s", last)
	}
	if strings.TrimSuffix(res.AfterValid.Path, "/") != "/done" || res.AfterValid.Page != "done" {
		t.Errorf("a valid post still redirects: %s", last)
	}
}
