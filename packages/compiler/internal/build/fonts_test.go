package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestInjectFontDisplay(t *testing.T) {
	in := "@font-face { font-family: Inter; src: url('/f.woff2'); }\n@font-face { font-family: Mono; font-display: block; src: url('/m.woff2'); }"
	out := injectFontDisplay(in, "swap")
	if strings.Count(out, "font-display") != 2 {
		t.Fatalf("expected one injected + one existing font-display, got: %s", out)
	}
	if !strings.Contains(out, "font-display:swap;") {
		t.Fatalf("swap not injected: %s", out)
	}
	if !strings.Contains(out, "font-display: block") {
		t.Fatalf("existing font-display must be preserved: %s", out)
	}
	if injectFontDisplay(in, "") != in {
		t.Fatal("blank display must be a no-op")
	}
}

func TestInjectFontDisplayAfterUnterminatedDeclaration(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"minified, no final semicolon",
			`@font-face{font-family:Inter;src:url(/f.woff2) format("woff2")}`,
			`@font-face{font-family:Inter;src:url(/f.woff2) format("woff2");font-display:swap;}`},
		{"spaced, no final semicolon",
			"@font-face { font-family: Inter; src: url('/f.woff2') format('woff2') }",
			"@font-face { font-family: Inter; src: url('/f.woff2') format('woff2') ;font-display:swap;}"},
		{"final semicolon is not doubled",
			"@font-face{font-family:Inter;src:url(/f.woff2);}",
			"@font-face{font-family:Inter;src:url(/f.woff2);font-display:swap;}"},
		{"trailing comment after an unterminated declaration",
			"@font-face{src:url(/f.woff2) /* v2 */}",
			"@font-face{src:url(/f.woff2) /* v2 */;font-display:swap;}"},
		{"empty block",
			"@font-face{}",
			"@font-face{font-display:swap;}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := injectFontDisplay(tc.in, "swap"); got != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestCollectFontURLs(t *testing.T) {
	in := "@font-face { src: url('/fonts/a.woff2') format('woff2'), url(\"/fonts/a.woff\") format('woff'); }\n@font-face { src: url(data:font/woff2;base64,AAAA); }\n@font-face { src: url('/fonts/a.woff2'); }"
	urls := collectFontURLs(in)
	want := []string{"/fonts/a.woff2", "/fonts/a.woff"}
	if len(urls) != len(want) {
		t.Fatalf("urls = %v, want %v", urls, want)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Fatalf("urls = %v, want %v", urls, want)
		}
	}
}

func TestFontPreloadHTML(t *testing.T) {
	got := fontPreloadHTML([]string{"/fonts/inter.woff2", "/fonts/x.woff"}, "/docs")
	if !strings.Contains(got, `<link rel="preload" as="font" type="font/woff2" href="/docs/fonts/inter.woff2" crossorigin>`) {
		t.Fatalf("woff2 preload missing/incorrect:\n%s", got)
	}
	if !strings.Contains(got, `type="font/woff" href="/docs/fonts/x.woff"`) {
		t.Fatalf("woff preload missing/incorrect:\n%s", got)
	}
}

func TestBuildInjectsFontDisplayInPageCSS(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.module.css",
		"@font-face { font-family: Inter; src: url('/fonts/inter.woff2'); }\n.body { color: rgb(1, 2, 3); }\n")
	writeFileRel(t, root, "src/pages/index.tsx",
		"import styles from './index.module.css';\n"+
			"export default function Page() { return <div class={styles.body}>hi</div>; }\n")

	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	css := readOnlyPageCSS(t, cfg.OutDir)
	if !strings.Contains(css, "font-display:swap") && !strings.Contains(css, "font-display: swap") {
		t.Fatalf("font-display not injected into page CSS:\n%s", css)
	}
}

func TestCollectFontPreloadsPicksFirstWoff2PerFace(t *testing.T) {
	in := `@font-face { font-family: A; font-weight: 400; src: url("fonts/a.woff") format("woff"), url("fonts/a.woff2") format("woff2"), url("fonts/a.ttf") format("truetype"); }
@font-face { font-family: A; font-weight: 700; src: url('./fonts/a-bold.woff2'), url('./fonts/a-bold.woff'); }
@font-face { font-family: B; /* no-preload */ src: url('/b.woff2'); }
@font-face { font-family: C; unicode-range: U+0400-045F; src: url('/c-cyrillic.woff2'); }
@font-face { font-family: C; unicode-range: U+0000-00FF, U+0131; src: url('/c-latin.woff2?v=2'); }
@font-face { font-family: D; src: url('/d.woff'); }
@font-face { font-family: E; src: url('/e.woff2'); }`
	got := collectFontPreloads(in, 0)
	want := []string{"/fonts/a.woff2", "/fonts/a-bold.woff2", "/c-latin.woff2?v=2", "/d.woff"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("preloads = %v, want %v (capped at 4, relative urls resolved against the stylesheet root)", got, want)
	}
	if got := collectFontPreloads(in, 2); len(got) != 2 {
		t.Errorf("limit not honored: %v", got)
	}
}

func TestBuildPreloadsFontsFromPageImportedCSS(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/blog/index.css",
		"@font-face { font-family: Inter; src: url('fonts/inter.woff2') format('woff2'), url('fonts/inter.woff') format('woff'); }\nbody { font-family: Inter; }\n")
	writeFileRel(t, root, "src/pages/blog/post.tsx",
		"import './index.css';\nexport default function Page() { return <p>hi</p>; }\n")
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "blog", "post", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	if !strings.Contains(html, `rel=preload as=font type=font/woff2 href=/fonts/inter.woff2`) && !strings.Contains(html, `href="/fonts/inter.woff2"`) {
		t.Errorf("font from page-imported CSS is not preloaded (and must resolve against the stylesheet, not the page):\n%s", html)
	}
	if strings.Contains(html, "inter.woff ") || strings.Contains(html, `inter.woff"`) || strings.Contains(html, "inter.woff>") {
		t.Errorf("the woff fallback must not be preloaded:\n%s", html)
	}
}
