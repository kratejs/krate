package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/gohost"
)

// TestGoComponentSSRPageE2E boots the real SSR sidecar against an SSR page that
// imports a server-tier and a runtime-tier Go component, and verifies the
// end-to-end chain:
//
//   - the server-tier component is inlined from its build-time baked HTML (no
//     host call);
//   - the runtime-tier component comes back as a krate-go marker, which the Go
//     host then renders (with the request context forwarded).
func TestGoComponentSSRPageE2E(t *testing.T) {
	ensureRuntimeDist(t)
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node not available: %v", err)
	}
	if _, err := exec.LookPath("go"); err != nil {
		requireE2E(t, "go toolchain not available: %v", err)
	}
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	writeFileRel(t, root, "src/components/badge.server.go",
		"package components\n\ntype BadgeProps struct {\n\tLabel string `json:\"label\"`\n}\n\n"+
			"func Badge(p BadgeProps) string { return \"<span class=\\\"badge\\\">\" + p.Label + \"</span>\" }\n")
	writeFileRel(t, root, "src/components/clock.runtime.go",
		"package components\n\nimport kr \"github.com/kratejs/krate/packages/compiler/goext/krate\"\n\n"+
			"type ClockProps struct {\n\tZone string `json:\"zone\"`\n}\n\n"+
			"func Clock(p ClockProps, r kr.Request) string { return \"<time>\" + p.Zone + \"-\" + r.Param(\"id\") + \"</time>\" }\n")
	writeFileRel(t, root, "src/pages/ssr.tsx",
		"// @server\nimport { Badge } from \"../components/badge.server.go\";\n"+
			"import { Clock } from \"../components/clock.runtime.go\";\n\n"+
			"export const config = { ssr: true };\n\n"+
			"export default function Page() { return <main><Badge label=\"Hi\" /><Clock zone=\"UTC\" /></main>; }\n")

	cfg := config.Default()
	cfg.Go.ComponentDirs = []string{"src/components"}
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	outDir := cfg.OutDir

	nodeModules := filepath.Join(root, "node_modules")
	_ = os.MkdirAll(nodeModules, 0755)
	if err := linkRuntimePackage(t, filepath.Join(nodeModules, "@krate")); err != nil {
		t.Fatal(err)
	}

	staged := stageServerRenderer(repoRoot, outDir)
	if staged == "" {
		t.Fatal("stageServerRenderer returned empty path")
	}
	port := freePort(t)
	cmd := exec.Command(node, staged)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("KRATE_SSR_PORT=%d", port),
		fmt.Sprintf("KRATE_MANIFEST=%s", filepath.Join(outDir, "server-manifest.json")),
		fmt.Sprintf("KRATE_ROOT=%s", root),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting renderer: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	base := fmt.Sprintf("http://localhost:%d", port)
	if !waitHealth(t, base) {
		t.Fatal("renderer did not become ready")
	}

	frames := postRegionsList(t, base, "/ssr", "/ssr/42", map[string]string{"id": "42"}, []map[string]string{{"id": "page", "kind": "page"}})
	page := frames.regions["page"]
	if page == "" {
		t.Fatalf("no page region HTML returned: %+v (errors=%v)", frames, frames.errors)
	}
	if !strings.Contains(page, `class="badge"`) || !strings.Contains(page, "Hi") {
		t.Errorf("server-tier Go component not baked into the SSR page:\n%s", page)
	}
	if !strings.Contains(page, "<!--krate-go:Clock:") {
		t.Errorf("runtime-tier Go component did not emit a marker:\n%s", page)
	}

	// Render the runtime marker via the Go host, forwarding the request context.
	sup := newGoAPISupervisor(goAPIServerBinPath(root), gohost.RoutesManifestPath(root), freePort(t))
	if err := sup.Start(); err != nil {
		t.Fatalf("starting Go host: %v", err)
	}
	defer sup.Close()

	final := resolveGoMarkers(page, `{"method":"GET","params":{"id":"42"}}`, sup.Render)
	if strings.Contains(final, "krate-go:") {
		t.Errorf("Go marker not resolved:\n%s", final)
	}
	if !strings.Contains(final, "<time>UTC-42</time>") {
		t.Errorf("runtime Go component not rendered with request context:\n%s", final)
	}
}
