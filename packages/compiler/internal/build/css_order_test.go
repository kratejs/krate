package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func buildCSSTestApp(t *testing.T, files map[string]string) *config.Config {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		writeFileRel(t, root, rel, body)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return cfg
}

func pageCSSFor(t *testing.T, cfg *config.Config, rel string) string {
	t.Helper()
	html, err := os.ReadFile(filepath.Join(cfg.OutDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, part := range strings.Split(string(html), `href="`)[1:] {
		href := part[:strings.IndexByte(part, '"')]
		if strings.HasSuffix(href, ".css") && !strings.Contains(href, ".tw.") {
			data, err := os.ReadFile(filepath.Join(cfg.OutDir, filepath.FromSlash(strings.TrimPrefix(href, "/"))))
			if err != nil {
				t.Fatal(err)
			}
			sb.Write(data)
		}
	}
	return sb.String()
}

func TestLayoutCSSPrecedesPageCSS(t *testing.T) {
	cfg := buildCSSTestApp(t, map[string]string{
		"src/pages/_layout.tsx":      "import './root.css';\nexport default function L(children) { return <div>{children}</div>; }\n",
		"src/pages/root.css":         ".note { font-weight: 701; }\n",
		"src/pages/blog/_layout.tsx": "import './blog.css';\nexport default function L(children) { return <section>{children}</section>; }\n",
		"src/pages/blog/blog.css":    ".note { font-weight: 702; }\n",
		"src/pages/blog/index.tsx":   "import './a.css';\nexport default function P() { return <p class=\"note\">x</p>; }\n",
		"src/pages/blog/a.css":       ".note { font-weight: 703; }\n",
	})
	css := pageCSSFor(t, cfg, "blog/index.html")
	i1, i2, i3 := strings.Index(css, "701"), strings.Index(css, "702"), strings.Index(css, "703")
	if i1 < 0 || i2 < 0 || i3 < 0 || !(i1 < i2 && i2 < i3) {
		t.Fatalf("want root layout, blog layout, page order, got:\n%s", css)
	}
}

func TestDCEKeepsModuleClassesAppliedOnClient(t *testing.T) {
	cfg := buildCSSTestApp(t, map[string]string{
		"src/pages/t.module.css": ".box { color: rgb(1, 1, 1); }\n.open { color: rgb(2, 2, 2); }\n.dead { color: rgb(3, 3, 3); }\n",
		"src/pages/index.tsx": "import s from './t.module.css';\n" +
			"export default function P() {\n const [open, setOpen] = createSignal(false);\n" +
			" return <button class={open() ? s.open : s.box} onClick={() => setOpen(!open())}>t</button>;\n}\n",
	})
	css := readAllCSS(t, cfg.OutDir)
	for _, want := range []string{"rgb(1, 1, 1)", "rgb(2, 2, 2)"} {
		if !strings.Contains(css, want) {
			t.Errorf("client-applied class missing %s:\n%s", want, css)
		}
	}
	if strings.Contains(css, "rgb(3, 3, 3)") {
		t.Errorf("unused class should still be pruned:\n%s", css)
	}
}

func TestCSPCoversMinifiedInlineStylesAndAttrs(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", "export default function P() {\n"+
		" return <div style={{color: 'red', 'margin-top': '3px'}}><style>{`.a  {  color : blue ;  }\n\n .b{color:green}`}</style><p>x</p></div>;\n}\n")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CSP.Enabled = true
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	meta := doc[strings.Index(doc, "Content-Security-Policy"):]
	meta = meta[:strings.Index(meta, ">")]
	for _, inline := range extractInlineContent(doc, "style") {
		if !strings.Contains(meta, sha256Base64(inline)) {
			t.Errorf("style block %q not hashed in %s", inline, meta)
		}
	}
	attrs, _ := inlineStyleAttrsAndSpecRules(doc, "")
	if len(attrs) == 0 {
		t.Fatalf("no style attribute found in %s", doc)
	}
	if !strings.Contains(meta, "'unsafe-hashes'") {
		t.Errorf("missing unsafe-hashes: %s", meta)
	}
	for _, h := range attrs {
		if !strings.Contains(meta, h) {
			t.Errorf("style attr hash %s missing in %s", h, meta)
		}
	}
}

func TestGenerateCSPMeta_StyleAttrsAndSpeculationRules(t *testing.T) {
	doc := `<div style="color:red"></div><script type="speculationrules">{"prefetch":[]}</script>`
	meta := generateCSPMeta(doc, doc, "", "")
	if !strings.Contains(meta, "'unsafe-hashes' "+sha256Base64("color:red")) {
		t.Errorf("style attr hash missing: %s", meta)
	}
	if !strings.Contains(meta, sha256Base64(`{"prefetch":[]}`)) || !strings.Contains(meta, "'inline-speculation-rules'") {
		t.Errorf("speculation rules not allowed: %s", meta)
	}
}
