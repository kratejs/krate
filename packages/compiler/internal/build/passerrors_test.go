package build

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// One build reports every failing pass: a server action validation error
// (KR7004), a metadata route failure (KR7130) and a page that cannot bundle.
func TestBuildReportsErrorsFromEveryPass(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
		t.Skip("node is not installed")
	}
	ensureHandlerDist(t)
	root := t.TempDir()
	files := map[string]string{
		"src/pages/index.tsx":  "export default function Page() { return <main>ok</main>; }\n",
		"src/pages/broken.tsx": "import { nope } from \"./does-not-exist\";\nexport default function Page() { return <main>{nope}</main>; }\n",
		"src/pages/robots.ts":  "export default function robots() { throw new Error(\"boom from robots\"); }\n",
		"src/actions.ts":       "\"use server\";\nexport const notAFunction = 42;\n",
	}
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	err := New(root, cfg).BuildAll()
	if err == nil {
		t.Fatal("expected the build to fail")
	}
	var codes []diag.Code
	var texts []string
	for _, e := range flattenErrors(err) {
		texts = append(texts, e.Error())
		var d diag.Diagnostic
		if errors.As(e, &d) {
			codes = append(codes, d.Code)
		}
	}
	all := strings.Join(texts, "\n")
	has := func(c diag.Code) bool {
		for _, got := range codes {
			if got == c {
				return true
			}
		}
		return false
	}
	if !has(diag.CodeActionExportNotFunction) {
		t.Errorf("missing the server action error (KR7004):\n%s", all)
	}
	if !has(diag.CodeMetadataRoute) {
		t.Errorf("missing the metadata route error (KR7130):\n%s", all)
	}
	if !strings.Contains(all, "does-not-exist") {
		t.Errorf("missing the page bundling error:\n%s", all)
	}
}
