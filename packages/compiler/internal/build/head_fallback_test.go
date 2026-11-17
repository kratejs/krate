package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestFallbackHeadTags(t *testing.T) {
	seo := config.SEOConfig{SiteName: "Acme", Icon: "icon.svg"}
	got := fallbackHeadTags("", seo)
	if !strings.Contains(got, "<title>Acme</title>") || !strings.Contains(got, `<link rel="icon" href="/icon.svg">`) {
		t.Errorf("fallback tags = %q", got)
	}
	got = fallbackHeadTags(`<title>Own</title><link rel="shortcut icon" href="/x.ico">`, seo)
	if got != "" {
		t.Errorf("a page with a title and icon gets nothing, got %q", got)
	}
	if got := fallbackHeadTags("", config.SEOConfig{}); got != "" {
		t.Errorf("no site name and no icon: got %q", got)
	}
	tmpl := config.SEOConfig{SiteName: "A&B", TitleTemplate: "%s | A&B"}
	if got := fallbackHeadTags("", tmpl); got != "<title>A&amp;B</title>\n" {
		t.Errorf("site name used as is and escaped: %q", got)
	}
}

func TestBuildEmitsFallbackTitleAndIcon(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", "export default function P() { return <p>home</p>; }\n")
	writeFileRel(t, root, "src/pages/about.tsx", "import { Head } from '@krate/runtime';\nexport default function P() { return <><Head><title>About us</title></Head><p>about</p></>; }\n")
	writeFileRel(t, root, "krate.config.json", `{"site":{"name":"Acme","icon":"/brand.svg"}}`)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(cfg.OutDir, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	home := read("index.html")
	if strings.Count(home, "<title>") != 1 || !strings.Contains(home, "<title>Acme</title>") {
		t.Errorf("home needs exactly the fallback title:\n%s", home)
	}
	if !strings.Contains(home, "rel=icon href=/brand.svg") {
		t.Errorf("home needs the configured icon:\n%s", home)
	}
	about := read("about/index.html")
	if strings.Count(about, "<title>") != 1 || !strings.Contains(about, "<title>About us</title>") {
		t.Errorf("about keeps its own title:\n%s", about)
	}
}

func TestBuildFallbackTitleOnPageWithOtherHeadTags(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", "import { Head } from '@krate/runtime';\nexport default function P() { return <><Head><meta name=\"robots\" content=\"all\" /></Head><p>home</p></>; }\n")
	writeFileRel(t, root, "krate.config.json", `{"site":{"name":"Acme"}}`)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if doc := string(data); strings.Count(doc, "<title>") != 1 || !strings.Contains(doc, "<title>Acme</title>") {
		t.Errorf("want the site name as the only title:\n%s", doc)
	}
}
