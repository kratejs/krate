package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestEventDelegationBubbles verifies nested handlers are both registered with
// the runtime's delegated event system (which bubbles through every ancestor and
// honours stopPropagation) and no per-page delegation code is emitted.
func TestEventDelegationBubbles(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		export default function Page() {
			const [n, setN] = createSignal(0);
			return <div onClick={() => setN(n() + 1)}><button onClick={() => setN(n() + 2)}>x</button></div>;
		}
	`
	_, js := buildReactPage(t, page)
	if got := strings.Count(js, `"onClick",`); got != 2 {
		t.Errorf("expected two onClick handler registrations, got %d:\n%s", got, js)
	}
	if strings.Contains(js, "__krate_del_add") {
		t.Errorf("per-page delegation code should no longer be emitted:\n%s", js)
	}
}

// TestDurableStateScriptEscaped verifies a krate.state.json containing a
// </script> sequence cannot break out of the injected script.
func TestDurableStateScriptEscaped(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", `export default function Page() { return <h1>Hi</h1>; }`)
	writeFileRel(t, root, "krate.state.json", `{"note":"</script><script>alert(1)</script>"}`)

	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html, _ := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if strings.Contains(string(html), "</script><script>alert(1)") {
		t.Errorf("durable state allows script breakout:\n%s", html)
	}
	if !strings.Contains(string(html), "\\u003c/script\\u003e") {
		t.Errorf("durable state < should be escaped as \\u003c:\n%s", html)
	}
}
