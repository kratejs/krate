package build

import (
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
