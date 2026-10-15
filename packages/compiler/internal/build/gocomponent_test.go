package build

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/gohost"
)

// TestGoComponentInPage imports a Go component from a `.go` file into a TSX page
// and asserts the Go-rendered HTML is inlined at build time.
func TestGoComponentInPage(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	root := t.TempDir()
	writeFileRel(t, root, "src/components/badge.server.go",
		"package components\n\ntype BadgeProps struct {\n\tLabel string `json:\"label\"`\n}\n\n"+
			"func Badge(p BadgeProps) string { return \"<span class=\\\"badge\\\">\" + p.Label + \"</span>\" }\n")
	writeFileRel(t, root, "src/pages/index.tsx",
		"import { Badge } from \"../components/badge.server.go\";\n\n"+
			"export default function Page() {\n\treturn <main><Badge label=\"Hi\" /><Badge label=\"Yo\" /></main>;\n}\n")

	cfg := config.Default()
	cfg.Go.ComponentDirs = []string{"src/components"}
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(data)
	for _, want := range []string{"badge>Hi</span>", "badge>Yo</span>"} {
		if !strings.Contains(html, want) {
			t.Errorf("Go component output %q missing from page:\n%s", want, html)
		}
	}
	if strings.Contains(html, "krate-gocomp") {
		t.Errorf("unsubstituted Go marker leaked:\n%s", html)
	}
}

// a component file, scaffold + compile the host, generate the .d.go.ts types,
// and render a component via the host's build-time `render` batch mode.
func TestGoComponentHostRender(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	root := t.TempDir()
	writeFileRel(t, root, "src/components/card.server.go",
		"package components\n\ntype CardProps struct {\n\tTitle string `json:\"title\"`\n\tCount int    `json:\"count\"`\n}\n\n"+
			"func Card(p CardProps) string { return \"<h1>\" + p.Title + \"=\" + itoa(p.Count) + \"</h1>\" }\n\n"+
			"func itoa(n int) string {\n\tif n == 0 {\n\t\treturn \"0\"\n\t}\n\treturn \"x\"\n}\n")

	cfg := config.Default()
	cfg.Go.ComponentDirs = []string{"src/components"}
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	if err := b.BuildAllGoAPI(); err != nil {
		t.Fatalf("BuildAllGoAPI: %v", err)
	}

	bin := goAPIServerBinPath(root)
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("host binary missing: %v", err)
	}

	// Build-time batch render.
	req := `{"id":"1","name":"Card","props":"{\"title\":\"Hi\",\"count\":2}"}` + "\n"
	cmd := exec.Command(bin, "render")
	cmd.Stdin = strings.NewReader(req)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("render: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "<h1>Hi=x</h1>") {
		t.Fatalf("unexpected render output: %s", out)
	}

	// TypeScript declarations generated under .krate/types (not next to the .go).
	types, err := os.ReadFile(filepath.Join(root, ".krate", "types", "src", "components", "card.server.go.krate.d.ts"))
	if err != nil {
		t.Fatalf("types not generated: %v", err)
	}
	for _, want := range []string{"declare module \"*card.server.go\"", "export interface CardProps", "title: string;", "count: number;", "export declare const Card"} {
		if !strings.Contains(string(types), want) {
			t.Errorf("types missing %q:\n%s", want, types)
		}
	}
}

// TestGoComponentMultiFilePackage verifies a component package spanning multiple
// files (a component calling a same-package helper) compiles and renders.
func TestGoComponentMultiFilePackage(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	root := t.TempDir()
	writeFileRel(t, root, "src/components/format.go",
		"package components\n\nfunc shout(s string) string { return s + \"!\" }\n")
	writeFileRel(t, root, "src/components/widget.server.go",
		"package components\n\ntype WidgetProps struct {\n\tName string `json:\"name\"`\n}\n\n"+
			"func Widget(p WidgetProps) string { return \"<b>\" + shout(p.Name) + \"</b>\" }\n")
	writeFileRel(t, root, "src/pages/index.tsx",
		"import { Widget } from \"../components/widget.server.go\";\n\nexport default function Page() { return <main><Widget name=\"go\" /></main>; }\n")

	cfg := config.Default()
	cfg.Go.ComponentDirs = []string{"src/components"}
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "go!</b>") {
		t.Errorf("multi-file component not rendered:\n%s", data)
	}
}

// TestGoComponentRuntimeRegion verifies a runtime-tier Go component becomes a
// request-time region (marker in the shell, runtime "go" in the manifest).
func TestGoComponentRuntimeRegion(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	root := t.TempDir()
	writeFileRel(t, root, "src/components/clock.runtime.go",
		"package components\n\ntype ClockProps struct {\n\tZone string `json:\"zone\"`\n}\n\n"+
			"func Clock(p ClockProps) string { return \"<time>\" + p.Zone + \"</time>\" }\n")
	writeFileRel(t, root, "src/pages/index.tsx",
		"import { Clock } from \"../components/clock.runtime.go\";\n\nexport default function Page() { return <main><Clock zone=\"UTC\" /></main>; }\n")

	cfg := config.Default()
	cfg.Go.ComponentDirs = []string{"src/components"}
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "<!--region:") {
		t.Errorf("expected a region marker for the runtime Go component:\n%s", html)
	}
	man, err := os.ReadFile(filepath.Join(cfg.OutDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(man), `"runtime": "go"`) {
		t.Errorf("manifest missing runtime go region:\n%s", man)
	}
}

// TestGoComponentGorenderEndpoint exercises the host's /__krate/gorender endpoint
// used for runtime region dispatch.
func TestGoComponentGorenderEndpoint(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	root := t.TempDir()
	writeFileRel(t, root, "src/components/wrap.runtime.go",
		"package components\n\nimport \"strings\"\n\ntype WrapProps struct {\n\tItems    []string `json:\"items\"`\n\tChildren string   `json:\"children\"`\n}\n\n"+
			"func Wrap(p WrapProps) string { return \"<x>\" + strings.Join(p.Items, \",\") + \"|\" + p.Children + \"</x>\" }\n")

	cfg := config.Default()
	cfg.Go.ComponentDirs = []string{"src/components"}
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAllGoAPI(); err != nil {
		t.Fatalf("BuildAllGoAPI: %v", err)
	}
	bin := goAPIServerBinPath(root)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), fmt.Sprintf("KRATE_GOAPI_PORT=%d", port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start host: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	url := fmt.Sprintf("http://127.0.0.1:%d/__krate/gorender", port)
	body := `{"regions":[{"id":"r1","name":"Wrap","props":"{\"items\":[\"a\",\"b\"],\"children\":\"<i>c</i>\"}"}]}`
	var respBody string
	for i := 0; i < 50; i++ {
		resp, err := http.Post(url, "application/json", strings.NewReader(body))
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			respBody = string(b)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(respBody, "<x>a,b|<i>c</i></x>") {
		t.Fatalf("gorender response: %q", respBody)
	}
}

// TestInitGoWorkspace verifies `krate go:init` writes go.mod/go.work and vendors
// the helper module (no Go toolchain required).
func TestInitGoWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "components"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := gohost.Init(root); err != nil {
		t.Fatalf("InitGo: %v", err)
	}
	for _, p := range []string{
		filepath.Join("go.mod"),
		filepath.Join("go.work"),
		filepath.Join(".krate", "goext", "go.mod"),
		filepath.Join(".krate", "goext", "goext", "krate", "krate.go"),
		filepath.Join(".krate", "goext", "goext", "runtime", "runtime.go"),
	} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
}

// TestInitGoTsconfigInclude verifies go:init adds .krate/types to the tsconfig.
func TestInitGoTsconfigInclude(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(`{"compilerOptions":{"strict":true},"include":["src"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := gohost.Init(root); err != nil {
		t.Fatalf("InitGo: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "tsconfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), ".krate/types") {
		t.Errorf("tsconfig include not updated:\n%s", data)
	}
}
func TestGoComponentRequestContext(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	root := t.TempDir()
	writeFileRel(t, root, "src/components/echo.runtime.go",
		"package components\n\nimport kr \"github.com/kratejs/krate/packages/compiler/goext/krate\"\n\n"+
			"type EchoProps struct {\n\tPrefix string `json:\"prefix\"`\n}\n\n"+
			"func Echo(p EchoProps, r kr.Request) string { return \"<e>\" + p.Prefix + r.Param(\"id\") + \"</e>\" }\n")

	cfg := config.Default()
	cfg.Go.ComponentDirs = []string{"src/components"}
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAllGoAPI(); err != nil {
		t.Fatalf("BuildAllGoAPI: %v", err)
	}
	bin := goAPIServerBinPath(root)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), fmt.Sprintf("KRATE_GOAPI_PORT=%d", port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start host: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	url := fmt.Sprintf("http://127.0.0.1:%d/__krate/gorender", port)
	body := `{"regions":[{"id":"r1","name":"Echo","props":"{\"prefix\":\"#\"}","request":"{\"params\":{\"id\":\"42\"}}"}]}`
	var respBody string
	for i := 0; i < 50; i++ {
		resp, err := http.Post(url, "application/json", strings.NewReader(body))
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			respBody = string(b)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(respBody, "<e>#42</e>") {
		t.Fatalf("request context not passed: %q", respBody)
	}
}

// TestGoComponentTypedProps verifies array/number props are serialized as typed
// JSON and decode into the Go props struct.
func TestGoComponentTypedProps(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	root := t.TempDir()
	writeFileRel(t, root, "src/components/list.server.go",
		"package components\n\nimport \"strings\"\n\ntype ListProps struct {\n\tItems []string `json:\"items\"`\n\tCount int      `json:\"count\"`\n}\n\n"+
			"func List(p ListProps) string { return \"<ul>\" + strings.Join(p.Items, \",\") + \"#\" + itoa(p.Count) + \"</ul>\" }\n\n"+
			"func itoa(n int) string {\n\tif n == 0 {\n\t\treturn \"0\"\n\t}\n\treturn \"n\"\n}\n")
	writeFileRel(t, root, "src/pages/index.tsx",
		"import { List } from \"../components/list.server.go\";\n\nexport default function Page() { return <main><List items={[\"a\",\"b\"]} count={3} /></main>; }\n")

	cfg := config.Default()
	cfg.Go.ComponentDirs = []string{"src/components"}
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "a,b#n") {
		t.Errorf("typed props not decoded into Go (want a,b#n):\n%s", data)
	}
}

// TestGoComponentRuntimeChildren verifies a runtime Go component's static
// children are baked into the region props.
func TestGoComponentRuntimeChildren(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	root := t.TempDir()
	writeFileRel(t, root, "src/components/box.runtime.go",
		"package components\n\ntype BoxProps struct {\n\tChildren string `json:\"children\"`\n}\n\n"+
			"func Box(p BoxProps) string { return \"<box>\" + p.Children + \"</box>\" }\n")
	writeFileRel(t, root, "src/pages/index.tsx",
		"import { Box } from \"../components/box.runtime.go\";\n\nexport default function Page() { return <main><Box>inner</Box></main>; }\n")

	cfg := config.Default()
	cfg.Go.ComponentDirs = []string{"src/components"}
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	man, err := os.ReadFile(filepath.Join(cfg.OutDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(man), `"children"`) || !strings.Contains(string(man), "inner") {
		t.Errorf("runtime children not baked into region props:\n%s", man)
	}
}

// TestGoComponentSSRServerBundle covers the SSR/ISR case: an SSR page importing
// Go components must still compile a server bundle (esbuild has no `.go` loader),
// and the bundle must inline a server-tier component's build-time HTML keyed by
// the same call-site id the page source is annotated with.
func TestGoComponentSSRServerBundle(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	root := t.TempDir()
	writeFileRel(t, root, "src/components/badge.server.go",
		"package components\n\ntype BadgeProps struct {\n\tLabel string `json:\"label\"`\n}\n\n"+
			"func Badge(p BadgeProps) string { return \"<span class=\\\"badge\\\">\" + p.Label + \"</span>\" }\n")
	writeFileRel(t, root, "src/components/clock.runtime.go",
		"package components\n\ntype ClockProps struct {\n\tZone string `json:\"zone\"`\n}\n\n"+
			"func Clock(p ClockProps) string { return \"<time>\" + p.Zone + \"</time>\" }\n")
	pageSrc := "import { Badge } from \"../components/badge.server.go\";\n" +
		"import { Clock } from \"../components/clock.runtime.go\";\n\n" +
		"export const config = { ssr: true };\n\n" +
		"export default function Page() {\n\treturn <main><Badge label=\"Hi\" /><Clock zone=\"UTC\" /></main>;\n}\n"
	writeFileRel(t, root, "src/pages/ssr.tsx", pageSrc)

	cfg := config.Default()
	cfg.Go.ComponentDirs = []string{"src/components"}
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	bundle := filepath.Join(cfg.OutDir, ".krate", "server-bundles", "ssr.server.mjs")
	data, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatalf("SSR server bundle missing (the .go loader error): %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "goComponent") {
		t.Errorf("bundle missing the Go shim call:\n%s", out)
	}
	if !strings.Contains(out, "badge") {
		t.Errorf("bundle missing the baked server-tier HTML (GoBaked):\n%s", out)
	}

	// The baked map key must equal the annotated call-site id, else the shim
	// can't inline and would fall back to a live host render.
	annotated := annotateGoCalls(pageSrc, "src/pages/ssr.tsx", map[string]bool{"Badge": true, "Clock": true})
	const marker = `<Badge __krateGoId="`
	i := strings.Index(annotated, marker)
	if i < 0 {
		t.Fatalf("annotation missing in recomputed source:\n%s", annotated)
	}
	rest := annotated[i+len(marker):]
	id := rest[:strings.Index(rest, `"`)]
	if !strings.Contains(out, id) {
		t.Errorf("baked map key %q not present in bundle (id mismatch):\n%s", id, out)
	}
}

// TestGoComponentNestedServerComponentBaked covers a Go component used inside a
// nested server component (not the page itself): the nested module is annotated
// and its server-tier render is baked into the SSR bundle.
func TestGoComponentNestedServerComponentBaked(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	root := t.TempDir()
	writeFileRel(t, root, "src/components/badge.server.go",
		"package components\n\ntype BadgeProps struct {\n\tLabel string `json:\"label\"`\n}\n\n"+
			"func Badge(p BadgeProps) string { return \"<span class=\\\"badge\\\">\" + p.Label + \"</span>\" }\n")
	writeFileRel(t, root, "src/components/Card.server.tsx",
		"import { Badge } from \"./badge.server.go\";\n\n"+
			"export function Card() { return <section><Badge label=\"Nested\" /></section>; }\n")
	writeFileRel(t, root, "src/pages/ssr.tsx",
		"// @server\nimport { Card } from \"../components/Card.server\";\n\n"+
			"export const config = { ssr: true };\n\n"+
			"export default function Page() { return <main><Card /></main>; }\n")

	cfg := config.Default()
	cfg.Go.ComponentDirs = []string{"src/components"}
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, ".krate", "server-bundles", "ssr.server.mjs"))
	if err != nil {
		t.Fatalf("SSR server bundle missing: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "goComponent") || !strings.Contains(out, "badge") || !strings.Contains(out, "Nested") {
		t.Errorf("nested server component's Go render not baked into the bundle:\n%s", out)
	}
}

// TestResolveGoMarkers verifies the SSR page bundle's Go placeholders are
// decoded and rendered via the host, in place.
func TestResolveGoMarkers(t *testing.T) {
	props := `{"zone":"UTC"}`
	enc := base64.RawURLEncoding.EncodeToString([]byte(props))
	html := `<main>hi <!--krate-go:Clock:30:` + enc + `--> bye</main>`

	var got []goRenderRegion
	render := func(rs []goRenderRegion) (map[string]string, error) {
		got = rs
		return map[string]string{rs[0].ID: "<time>UTC</time>"}, nil
	}
	reqCtx := `{"method":"GET","params":{"id":"7"}}`
	out := resolveGoMarkers(html, reqCtx, render)
	if out != `<main>hi <time>UTC</time> bye</main>` {
		t.Fatalf("got %q", out)
	}
	if len(got) != 1 || got[0].Name != "Clock" || got[0].Props != props {
		t.Fatalf("bad render request: %+v", got)
	}
	if got[0].Request != reqCtx || got[0].Revalidate != 30 {
		t.Fatalf("request context/revalidate not forwarded: %+v", got[0])
	}

	// No markers -> unchanged; render error -> empty render.
	if resolveGoMarkers("<p>x</p>", "", render) != "<p>x</p>" {
		t.Fatal("expected no-op for markerless html")
	}
	fail := func([]goRenderRegion) (map[string]string, error) { return nil, fmt.Errorf("boom") }
	if resolveGoMarkers(html, "", fail) != "" {
		t.Fatal("expected empty output on render failure")
	}
}
