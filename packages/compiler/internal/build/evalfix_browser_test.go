package build

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// evalFixCheck is one browser expectation for a built page: the joined text of
// Selector before hydration (Static, defaulting to Hydrated) and after it.
// Click, when set, is clicked after hydration and AfterClick is then expected.
type evalFixCheck struct {
	Path       string  `json:"path"`
	Selector   string  `json:"selector"`
	Static     *string `json:"static,omitempty"`
	Hydrated   string  `json:"hydrated"`
	Click      string  `json:"click,omitempty"`
	AfterClick *string `json:"afterClick,omitempty"`
}

func strp(s string) *string { return &s }

// evalFixBrowser builds pages (path under src/pages -> source) and checks the
// static and hydrated text of each in a real browser.
func evalFixBrowser(t *testing.T, files map[string]string, checks []evalFixCheck) {
	t.Helper()
	browser := requireBrowserTest(t)
	root := codeThemeProject(t, nil)
	for rel, src := range files {
		writeFileRel(t, root, rel, src)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Tailwind.Enabled = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	enc, err := json.Marshal(checks)
	if err != nil {
		t.Fatal(err)
	}
	runBrowserScript(t, "evalfix_browser.mjs", cfg.OutDir, browser, string(enc))
}
