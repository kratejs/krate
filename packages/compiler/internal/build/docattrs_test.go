package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
)

type docAttrsPlugin struct{}

func (docAttrsPlugin) Name() string { return "doc-attrs" }
func (docAttrsPlugin) Order() int   { return 50 }
func (docAttrsPlugin) Hooks() plugin.PluginHooks {
	return plugin.PluginHooks{AfterPage: func(ctx *plugin.PageHookCtx) error {
		ctx.HeadHTML += `<html class="from-plugin" data-plugin="1"></html><body data-plugin="b"></body>`
		return nil
	}}
}

// TestPluginHooksSetDocumentAttributes: a hook that appends <html> and <body>
// elements to headHTML sets the attributes of the document elements; the
// page's own attribute of the same name wins.
func TestPluginHooksSetDocumentAttributes(t *testing.T) {
	saved := plugin.DefaultRegistry
	plugin.DefaultRegistry = plugin.NewRegistry()
	defer func() { plugin.DefaultRegistry = saved }()
	if err := plugin.Register(docAttrsPlugin{}); err != nil {
		t.Fatal(err)
	}
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <main><Head><html className="page" /></Head><h1>x</h1></main>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{"class=page", "data-plugin=1", "data-plugin=b"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "from-plugin") {
		t.Errorf("the page's class must win over the plugin's:\n%s", html)
	}
}

func TestSplitDocTagsMergesAttributeByAttribute(t *testing.T) {
	head := `<meta name="a" content="b"><html lang="fr" class="a"></html><html class="b"></html><body data-x="1"></body>`
	rest, htmlA, body := splitDocTags(head, true)
	if rest != `<meta name="a" content="b">` {
		t.Errorf("rest = %q", rest)
	}
	if htmlA["lang"] != "fr" || htmlA["class"] != "b" || body["data-x"] != "1" {
		t.Errorf("html=%v body=%v", htmlA, body)
	}
	// First wins when the page's tags come before its layouts'.
	_, first, _ := splitDocTags(head, false)
	if first["class"] != "a" {
		t.Errorf("first-wins class = %q", first["class"])
	}
}

func TestSetStartTagAttrsEscapesAndKeeps(t *testing.T) {
	doc := `<!DOCTYPE html><html lang="en"><head></head><body class="x" data-a="1"><div id="root"></div></body></html>`
	out := setStartTagAttrs(doc, "html", docAttrs{"class": `dark" onload="alert(1)`, "data-flag": ""})
	if !strings.Contains(out, `<html class="dark&quot; onload=&quot;alert(1)" data-flag lang="en">`) {
		t.Errorf("html tag: %s", out)
	}
	out = setStartTagAttrs(out, "body", docAttrs{"class": "y"})
	if !strings.Contains(out, `<body class="y" data-a="1">`) {
		t.Errorf("body tag: %s", out)
	}
	if got := setStartTagAttrs(doc, "html", nil); got != doc {
		t.Errorf("no attributes must leave the document alone")
	}
}

func TestAttrMapConfig(t *testing.T) {
	var s config.SiteConfig
	if err := (&s.HTMLAttributes).UnmarshalJSON([]byte(`{"class":"dark","data-n":3,"hidden":true,"x":false,"y":null}`)); err != nil {
		t.Fatal(err)
	}
	if len(s.HTMLAttributes) != 3 || s.HTMLAttributes["class"] != "dark" || s.HTMLAttributes["data-n"] != "3" {
		t.Errorf("attrs = %v", s.HTMLAttributes)
	}
	if v, ok := s.HTMLAttributes["hidden"]; !ok || v != "" {
		t.Errorf("true must be a bare attribute: %v", s.HTMLAttributes)
	}
	if err := (&s.BodyAttributes).UnmarshalJSON([]byte(`{"on click":"x"}`)); err == nil {
		t.Errorf("an invalid attribute name must be rejected")
	}
}

// TestDocumentAttributesInPrerenderedPages: site config sets the base, a layout
// and the page refine it from <Head>, and the page's lang export wins.
func TestDocumentAttributesInPrerenderedPages(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/_layout.tsx": `export default function Layout({ children }: { children: any }) {
  return <div><Head><html lang="fr" className="dark" /><body className="font-sans" /></Head>{children}</div>;
}`,
		"src/pages/index.tsx": `export default function Home() {
  return <main><Head><html className="light" data-v={"v" + 2} /></Head><h1>home</h1></main>;
}`,
		"src/pages/about.tsx": `export const lang = "de";
export default function About() { return <h1>about</h1>; }`,
	})
	b.Cfg.Site.HTMLAttributes = config.AttrMap{"data-site": "yes", "class": "base"}
	b.Cfg.Site.BodyAttributes = config.AttrMap{"data-body": "b", "class": "base"}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(root, "dist", rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	home := read("index.html")
	for _, want := range []string{"lang=fr", "class=light", "data-site=yes", "data-v=v2", "data-body=b", "class=font-sans"} {
		if !strings.Contains(home, want) {
			t.Errorf("home is missing %q:\n%s", want, home)
		}
	}
	if strings.Contains(home, "<html></html>") || strings.Count(home, "<html") != 1 || strings.Count(home, "<body") != 1 {
		t.Errorf("html/body tags must not leak into the head:\n%s", home)
	}
	if about := read("about/index.html"); !strings.Contains(about, "lang=de") || !strings.Contains(about, "class=dark") {
		t.Errorf("the lang export wins and the layout's class stays:\n%s", about)
	}
}

// TestDocumentAttributesAtRequestTime: an async page sets <html> and <body>
// attributes from data it awaits, and the response carries them.
func TestDocumentAttributesAtRequestTime(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/components/theme.tsx": `export function Theme({ mode }: { mode: string }) {
  return <Head><html lang="fr" className={mode} /><body data-mode={mode} /></Head>;
}`,
		"src/pages/index.tsx": `import { Theme } from "../components/theme";
export default async function Home() {
  const mode = await Promise.resolve("night");
  return <main><Theme mode={mode} /><p>{mode}</p></main>;
}`,
	})
	b.Cfg.Site.HTMLAttributes = config.AttrMap{"data-site": "yes"}
	e2eEnv(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	status, _, html := getBody(t, base+"/")
	if status != 200 {
		t.Fatalf("status %d:\n%s", status, html)
	}
	for _, want := range []string{`<html class="night" data-site="yes" lang="fr">`, `data-mode="night"`} {
		if !strings.Contains(html, want) {
			t.Errorf("response is missing %q:\n%s", want, html)
		}
	}
	if strings.Count(html, "<html") != 1 || strings.Contains(html, "krate-head") {
		t.Errorf("html/body tags must not remain in the document body:\n%s", html)
	}
}

// TestConfigDocAttrsUseJSXNames loads a real krate.config.ts, so the loader's
// key handling is covered, and builds with JSX attribute names.
func TestConfigDocAttrsUseJSXNames(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"krate.config.ts":     `export default { site: { htmlAttributes: { className: 'dark', lang: 'en' }, bodyAttributes: { className: 'min-h-screen' } } };`,
		"src/pages/index.tsx": `export default function Home() { return <h1>home</h1>; }`,
	})
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	b = New(root, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<html class=dark lang=en>`, `<body class=min-h-screen>`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q:\n%s", want, data)
		}
	}
}
