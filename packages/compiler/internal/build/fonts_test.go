package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

const fontTestCSS = `
:root { --font-sans: 'Body Sans', sans-serif; --font-mono: 'Fira Thing', ui-monospace, monospace; }
@font-face { font-family: 'Body Sans'; src: url('/fonts/body.woff2') format('woff2'); }
@font-face { font-family: 'Fira Thing'; src: url('/fonts/fira.woff2') format('woff2'); }
@font-face { font-family: 'JetBrains Mono Variable'; src: url('/fonts/jb.woff2') format('woff2'); }
@font-face { font-family: 'Other Sans'; /* no-preload */ src: url('/fonts/other.woff2') format('woff2'); }
`

func TestCollectFontFacesMarksMono(t *testing.T) {
	got := collectFontFaces(fontTestCSS, 0)
	want := map[string]bool{"/fonts/body.woff2": false, "/fonts/fira.woff2": true, "/fonts/jb.woff2": true}
	if len(got) != len(want) {
		t.Fatalf("faces = %+v", got)
	}
	for _, f := range got {
		if m, ok := want[f.URL]; !ok || m != f.Mono {
			t.Errorf("face %+v, want mono=%v", f, m)
		}
	}
}

func TestPageUsesCode(t *testing.T) {
	for html, want := range map[string]bool{
		`<p>hi</p>`:                     false,
		`<pre class="x">a</pre>`:        true,
		`<p>use <CODE>x</CODE></p>`:     true,
		`<link rel="preload">`:          false,
		`<codex-widget></codex-widget>`: false,
	} {
		if got := pageUsesCode(html); got != want {
			t.Errorf("pageUsesCode(%q) = %v, want %v", html, got, want)
		}
	}
}

func fontBuilder(t *testing.T, publicFiles ...string) *Builder {
	t.Helper()
	root := t.TempDir()
	for _, f := range publicFiles {
		writeFileRel(t, root, "public/"+f, "font")
	}
	cfg := config.Default()
	cfg.PublicDir = filepath.Join(root, "public")
	b := New(root, cfg)
	b.globalFonts = collectFontFaces(fontTestCSS, 0)
	return b
}

func TestPageFontPreloadsMonoOnlyWithCode(t *testing.T) {
	b := fontBuilder(t, "fonts/body.woff2", "fonts/fira.woff2", "fonts/jb.woff2")
	plain := b.pageFontPreloads(`<main><p>text</p></main>`, nil)
	if !strings.Contains(plain, "/fonts/body.woff2") || strings.Contains(plain, "fira") || strings.Contains(plain, "jb.woff2") {
		t.Errorf("code-free page preloads:\n%s", plain)
	}
	code := b.pageFontPreloads(`<main><pre>x</pre></main>`, nil)
	for _, u := range []string{"body.woff2", "fira.woff2", "jb.woff2"} {
		if !strings.Contains(code, u) {
			t.Errorf("page with <pre> missing %s:\n%s", u, code)
		}
	}
}

func TestPageFontPreloadsSkipsUnservedFile(t *testing.T) {
	b := fontBuilder(t, "fonts/body.woff2")
	out := b.pageFontPreloads(`<pre>x</pre>`, nil)
	if strings.Contains(out, "fira") || strings.Contains(out, "jb.woff2") || !strings.Contains(out, "body.woff2") {
		t.Errorf("unserved fonts must be skipped:\n%s", out)
	}
	if _, warned := b.fontWarned.Load("/fonts/fira.woff2"); !warned {
		t.Error("no warning recorded for the missing font")
	}
}

func TestThemeFontsHashedAndPreloaded(t *testing.T) {
	root := t.TempDir()
	pkg := "node_modules/fontpkg"
	writeFileRel(t, root, pkg+"/package.json", `{"name":"fontpkg","exports":{"./theme.css":"./src/theme.css"}}`)
	writeFileRel(t, root, pkg+"/src/theme.css", `:root{--font-sans:'Pkg Sans',sans-serif;--font-mono:'Pkg Mono',monospace}
@font-face{font-family:'Pkg Sans';src:url('./fonts/sans.woff2') format('woff2')}
@font-face{font-family:'Pkg Mono';src:url('./fonts/mono.woff2') format('woff2')}
body{font-family:var(--font-sans)}pre{font-family:var(--font-mono)}`)
	writeFileRel(t, root, pkg+"/src/fonts/sans.woff2", "sans-bytes")
	writeFileRel(t, root, pkg+"/src/fonts/mono.woff2", "mono-bytes")
	writeFileRel(t, root, "src/pages/_layout.tsx", "import \"fontpkg/theme.css\";\nexport default function L(p) { return <div>{p.children}</div>; }\n")
	writeFileRel(t, root, "src/pages/index.tsx", "export default function P() { return <main><p>plain</p></main>; }\n")
	writeFileRel(t, root, "src/pages/code.tsx", "export default function P() { return <main><pre>x</pre></main>; }\n")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(cfg.OutDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	re := regexp.MustCompile(`<link rel="?preload"? as="?font[^>]*href="?(/assets/(sans|mono)-[0-9a-z]+\.woff2)`)
	plain := re.FindAllStringSubmatch(read("index.html"), -1)
	if len(plain) != 1 || plain[0][2] != "sans" {
		t.Errorf("plain page font preloads = %v", plain)
	}
	withCode := re.FindAllStringSubmatch(read("code/index.html"), -1)
	if len(withCode) != 2 {
		t.Errorf("code page font preloads = %v", withCode)
	}
	for _, m := range withCode {
		if _, err := os.Stat(filepath.Join(cfg.OutDir, filepath.FromSlash(m[1]))); err != nil {
			t.Errorf("hashed font %s not written: %v", m[1], err)
		}
	}
}
