package build

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestServerlessNodeBrowser drives the node target (no Go server) with a real
// browser: hydration and soft navigation with an intercepting route, a server
// action that redirects, and the no-JS flash round trip. Runs when
// KRATE_BROWSER is set.
func TestServerlessNodeBrowser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the node target browser test")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	run := func(t *testing.T, fixture, script string) string {
		t.Helper()
		root, cfg := serverlessProject(t, fixture)
		if _, err := BuildServerBundle(root, cfg, "node"); err != nil {
			t.Fatalf("BuildServerBundle: %v", err)
		}
		base := startNodeBundle(t, root, cfg)
		abs, err := filepath.Abs(filepath.Join("testdata", script))
		if err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("node", abs, base, browser).CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
				requireE2E(t, "%s browser is not installed: %s", browser, out)
			}
			t.Fatalf("%s failed: %v\n%s", script, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	last := func(out string) string {
		if i := strings.LastIndex(out, "\n"); i >= 0 {
			return out[i+1:]
		}
		return out
	}

	t.Run("soft navigation and intercepting route", func(t *testing.T) {
		out := run(t, "router_gallery", "router_e2e.mjs")
		t.Logf("%s", out)
	})
	t.Run("segment pages", func(t *testing.T) {
		// Nested 404 on hard load and soft navigation with layout state kept,
		// the server error page, client boundaries with reset().
		out := run(t, "segments_app", "segments_e2e.mjs")
		t.Logf("%s", out)
	})
	t.Run("server action redirect", func(t *testing.T) {
		out := last(run(t, "action_redirect", "action_redirect.mjs"))
		var res struct {
			FormRedirect struct{ Path, Page string } `json:"formRedirect"`
			ActionState  struct{ Path, Page string } `json:"actionState"`
			Errors       []string                    `json:"errors"`
		}
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("bad output: %v\n%s", err, out)
		}
		if res.FormRedirect.Page != "about" || res.ActionState.Page != "account" || len(res.Errors) > 0 {
			t.Errorf("action redirect through the node target: %s", out)
		}
	})
	t.Run("no-JS flash state", func(t *testing.T) {
		out := last(run(t, "action_nojs", "action_nojs.mjs"))
		var res struct {
			AfterPost struct {
				Banner []string `json:"banner"`
				InForm int      `json:"inForm"`
			} `json:"afterPost"`
			FlashCookieLeft bool `json:"flashCookieLeft"`
			Hydrated        struct {
				Error   string `json:"error"`
				Notices int    `json:"notices"`
			} `json:"hydrated"`
		}
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("bad output: %v\n%s", err, out)
		}
		if len(res.AfterPost.Banner) != 1 || !strings.Contains(res.AfterPost.Banner[0], "user is required") || res.AfterPost.InForm != 1 || res.FlashCookieLeft || res.Hydrated.Error != "user is required" {
			t.Errorf("flash state through the node target: %s", out)
		}
	})
}
