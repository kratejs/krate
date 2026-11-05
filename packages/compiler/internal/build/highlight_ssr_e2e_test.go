package build

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/syntaxhighlight"
)

const ssrCodePageSrc = `export const config = { ssr: true };
export default function P() {
	const code = "const t: number = " + (Date.now() % 7) + ";\nlet s = 'a<b';";
	return (
		<main>
			<SyntaxHighlight lang="ts">{code}</SyntaxHighlight>
			<SyntaxHighlight lang="ts" highlight="2">{code}</SyntaxHighlight>
			<SyntaxHighlight lang="cobol">{code}</SyntaxHighlight>
		</main>
	);
}
`

// TestSSRCodeHighlightingE2E renders a request-time page whose code is only
// known per request, through the real Node renderer, and checks the code is
// highlighted with the same markup the compiler writes (no JavaScript for it in
// the browser), line marking included, and that an unsupported language falls
// back to plain text.
func TestSSRCodeHighlightingE2E(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node not available: %v", err)
	}
	ensureRuntimeDist(t)
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeFileRel(t, root, filepath.Join("src", "pages", "ssr.tsx"), ssrCodePageSrc)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	t.Chdir(root)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.OutDir, ".krate", "highlight.json")); err != nil {
		t.Fatalf("the build wrote no highlight tables for the server page: %v", err)
	}
	staged := stageServerRenderer(repoRoot, cfg.OutDir)
	if staged == "" {
		t.Fatal("stageServerRenderer returned an empty path")
	}
	// Ports 5700-5719 are this feature's; fall back to any free one.
	port := 5711
	if l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		port = freePort(t)
	} else {
		l.Close()
	}
	cmd := exec.Command(node, staged)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("KRATE_SSR_PORT=%d", port),
		fmt.Sprintf("KRATE_MANIFEST=%s", filepath.Join(cfg.OutDir, ".krate", "server-manifest.json")),
		fmt.Sprintf("KRATE_ROOT=%s", root),
		fmt.Sprintf("KRATE_OUT_DIR=%s", cfg.OutDir),
	)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the renderer: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	base := fmt.Sprintf("http://localhost:%d", port)
	if !waitHealth(t, base) {
		t.Fatal("the renderer did not become ready")
	}
	html := postRegionsList(t, base, "/ssr", "ssr", nil, []map[string]string{{"id": "page", "kind": "page"}}).regions["page"]
	if html == "" {
		t.Fatal("the page region is empty")
	}
	// The server page's code is whatever Date.now() gave, so rebuild the
	// expected markup from the rendered text.
	if !strings.Contains(html, `class="language-ts"`) {
		t.Fatalf("no highlighted block in:\n%.600s", html)
	}
	for _, want := range []string{
		`<span class="line"><span class="cl"><span class="kr">const</span>`,
		`<span class="s1">&#39;a&lt;b&#39;</span>`,
		`<span class="line hl">`,
		`data-highlight-lines="2"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%.1200s", want, html)
		}
	}
	// One block is highlighted per ts element, none for the unknown language.
	if got := strings.Count(html, `<span class="line`); got < 4 {
		t.Errorf("expected line wrappers for two highlighted blocks, got %d", got)
	}
	cobol := html[strings.Index(html, "language-cobol"):]
	if strings.Contains(cobol, `class="line`) || strings.Contains(cobol[:min(len(cobol), 400)], "<span") {
		t.Errorf("an unsupported language should be plain text:\n%.400s", cobol)
	}
	// The same code the compiler would write at build time.
	want := syntaxhighlight.HighlightThemeLines("const t: number = 0;\nlet s = 'a<b';", "ts", syntaxhighlight.DefaultTheme, "")
	if !strings.Contains(want, `<span class="kr">const</span>`) {
		t.Fatalf("test setup: unexpected compiler output %q", want)
	}
}
