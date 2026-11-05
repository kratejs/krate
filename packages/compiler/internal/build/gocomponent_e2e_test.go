package build

import (
	"encoding/json"
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
		fmt.Sprintf("KRATE_MANIFEST=%s", ServerManifestPath(outDir)),
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

// TestGoComponentRegionCacheVariesOnWhatItReadsE2E drives the real generated Go
// host: a component that reads its Request through the accessors is cached per
// the cookies it read, one that reads the fields (or hands the Request on) is
// cached per whole request, and none of them ever serves one visitor's render
// to another.
func TestGoComponentRegionCacheVariesOnWhatItReadsE2E(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		requireE2E(t, "go toolchain not available: %v", err)
	}
	root := t.TempDir()
	writeFileRel(t, root, "src/components/vary.runtime.go",
		"package components\n\nimport (\n\t\"fmt\"\n\t\"sync/atomic\"\n\n\tkr \"github.com/kratejs/krate/packages/compiler/goext/krate\"\n)\n\n"+
			"var renders int64\n\ntype P struct{}\n\n"+
			"func Greet(p P, r kr.Request) string {\n\tn := atomic.AddInt64(&renders, 1)\n\treturn fmt.Sprintf(\"<p>greet %s n%d</p>\", r.Cookie(\"sid\"), n)\n}\n\n"+
			"func RawCookie(p P, r kr.Request) string {\n\tn := atomic.AddInt64(&renders, 1)\n\treturn fmt.Sprintf(\"<p>raw %s n%d</p>\", r.Headers[\"Cookie\"], n)\n}\n\n"+
			"func describe(r kr.Request) string { return r.Headers[\"X-Forwarded-For\"] }\n\n"+
			"func Passed(p P, r kr.Request) string {\n\tn := atomic.AddInt64(&renders, 1)\n\treturn fmt.Sprintf(\"<p>passed %s n%d</p>\", describe(r), n)\n}\n\n"+
			"func Plain(p P) string {\n\tn := atomic.AddInt64(&renders, 1)\n\treturn fmt.Sprintf(\"<p>plain n%d</p>\", n)\n}\n")
	writeFileRel(t, root, "src/pages/index.tsx", "export default function Page() { return <main>x</main>; }\n")

	cfg := config.Default()
	cfg.Go.ComponentDirs = []string{"src/components"}
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	sup := newGoAPISupervisor(goAPIServerBinPath(root), gohost.RoutesManifestPath(root), freePort(t))
	if err := sup.Start(); err != nil {
		t.Fatalf("starting Go host: %v", err)
	}
	defer sup.Close()

	ctx := func(hdr map[string]string) string {
		b, _ := json.Marshal(map[string]any{"method": "GET", "url": "/p", "params": map[string]string{}, "query": map[string]string{}, "headers": hdr})
		return string(b)
	}
	render := func(name string, hdr map[string]string) string {
		t.Helper()
		out, err := sup.Render([]goRenderRegion{{ID: "r", Name: name, Props: "{}", Request: ctx(hdr), Revalidate: 60}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out["r"]
	}
	alice := map[string]string{"Cookie": "sid=alice", "User-Agent": "a"}
	bob := map[string]string{"Cookie": "sid=bob", "User-Agent": "b"}

	// Recorded accessor: per cookie, shared across everything else.
	a1 := render("Greet", alice)
	if !strings.Contains(a1, "greet alice") {
		t.Fatalf("alice: %s", a1)
	}
	if b := render("Greet", bob); !strings.Contains(b, "greet bob") || strings.Contains(b, "alice") {
		t.Fatalf("bob was served another visitor's render: %s", b)
	}
	if again := render("Greet", map[string]string{"Cookie": "sid=alice; ads=" + "123", "User-Agent": "other", "Referer": "/x"}); again != a1 {
		t.Errorf("a cookie the component never reads must not split the cache:\n got %s\nwant %s", again, a1)
	}
	if anon := render("Greet", nil); strings.Contains(anon, "alice") || strings.Contains(anon, "bob") {
		t.Errorf("a visitor without a cookie got someone's render: %s", anon)
	}

	// Fields read directly: keyed on the whole request.
	r1 := render("RawCookie", alice)
	if r2 := render("RawCookie", alice); r2 != r1 {
		t.Errorf("an identical request should hit: %s vs %s", r2, r1)
	}
	if other := render("RawCookie", bob); strings.Contains(other, "alice") {
		t.Errorf("bob was served alice's raw-cookie render: %s", other)
	}
	// Request passed to a helper: also unrecordable.
	p1 := render("Passed", map[string]string{"X-Forwarded-For": "1.1.1.1"})
	if p2 := render("Passed", map[string]string{"X-Forwarded-For": "2.2.2.2"}); p2 == p1 || !strings.Contains(p2, "2.2.2.2") {
		t.Errorf("a different client IP must render again: %s vs %s", p2, p1)
	}
	// No Request: one entry for everybody.
	pl := render("Plain", alice)
	if pl2 := render("Plain", bob); pl2 != pl {
		t.Errorf("a component that cannot see the request should share one entry: %s vs %s", pl2, pl)
	}
}
