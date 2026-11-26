package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestRelocateImports(t *testing.T) {
	t.Parallel()
	src := filepath.Join("/p", "src", "content", "docs", "guides")
	gen := filepath.Join("/p", ".krate", "gen", "docs", "guides")
	cases := map[string]string{
		`import Card from "../../components/card"`:          `import Card from "../../../../src/content/components/card"`,
		`import './local.css'`:                              `import '../../../../src/content/docs/guides/local.css'`,
		`import { a } from "./sibling"`:                     `import { a } from "../../../../src/content/docs/guides/sibling"`,
		`import W from "@/components/w"`:                    `import W from "@/components/w"`,
		`import W from "~/w"`:                               `import W from "~/w"`,
		`import W from "react"`:                             `import W from "react"`,
		`import W from "https://example.com/w.js"`:          `import W from "https://example.com/w.js"`,
		`export { a } from "../x"`:                          `export { a } from "../../../../src/content/docs/x"`,
		`export * from "./y"`:                               `export * from "../../../../src/content/docs/guides/y"`,
		`export const lazy = () => import("../z")`:          `export const lazy = () => import("../../../../src/content/docs/z")`,
		`export const note = "text from './not-an-import'"`: `export const note = "text from './not-an-import'"`,
		"import {\n  a,\n  b,\n} from '../multi'":           "import {\n  a,\n  b,\n} from '../../../../src/content/docs/multi'",
	}
	for in, want := range cases {
		if got := relocateImports(in, src, gen); got != want {
			t.Errorf("relocateImports(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

// writeDocsFiles adds files under root and returns it.
func docsWithFiles(t *testing.T, files map[string]string, extra map[string]interface{}) map[string]string {
	t.Helper()
	_, out := docsSite(t, files, "", extra)
	return out
}

func TestDocsMDXRelativeImportsAtTwoDepths(t *testing.T) {
	files := map[string]string{
		"top.mdx":         "---\ntitle: Top\n---\n\nimport Card from \"../components/card\"\n\n<Card />\n",
		"deep/nested.mdx": "---\ntitle: Nested\n---\n\nimport Card from \"../../components/card\"\n\n<Card />\n",
		"deep/plain.md":   "---\ntitle: Plain\n---\n\nimport Card from \"../../components/card\"\n\nbody\n",
	}
	root, out := docsSite(t, files, "", nil)
	want := func(genRel, spec string) {
		t.Helper()
		src := out[genRel]
		genDir := filepath.Join(root, ".krate", "gen", filepath.Dir(filepath.FromSlash(genRel)))
		rel, _ := filepath.Rel(genDir, filepath.Join(root, "src", "content", "components", "card"))
		rel = filepath.ToSlash(rel)
		if !strings.Contains(src, `import Card from "`+rel+`"`) {
			t.Errorf("%s: want import of %q (spec %s) in:\n%s", genRel, rel, spec, src)
		}
		if strings.Contains(src, `from "../components/card"`) || strings.Contains(src, `from "../../components/card"`) {
			t.Errorf("%s: raw relative specifier left in place", genRel)
		}
	}
	want("docs/top.tsx", "../components/card")
	want("docs/deep/nested.tsx", "../../components/card")
	want("docs/deep/plain.tsx", "../../components/card")
}

func TestDocsThemeScriptOptions(t *testing.T) {
	page := map[string]string{"index.md": "---\ntitle: Home\n---\n\nHi\n"}

	// Default: one script, follows the OS.
	out := docsWithFiles(t, page, nil)["docs.tsx"]
	if n := strings.Count(out, "data-krate-once=\"theme-prepaint\""); n != 1 {
		t.Fatalf("want one pre-paint script, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "prefers-color-scheme") || !strings.Contains(out, `default":"system"`) {
		t.Errorf("default should follow the system:\n%s", out)
	}

	// Default theme dark: nothing stored resolves to dark, no OS lookup.
	out = docsWithFiles(t, page, map[string]interface{}{"defaultTheme": "dark"})["docs.tsx"]
	if !strings.Contains(out, "s='dark';}") || strings.Contains(out, "matchMedia") {
		t.Errorf("defaultTheme dark: script should fall back to 'dark':\n%s", out)
	}
	if !strings.Contains(out, `"default":"dark"`) {
		t.Errorf("colorMode prop should carry the default:\n%s", out)
	}

	// Script off: no script, the prop says so.
	out = docsWithFiles(t, page, map[string]interface{}{"themeScript": false})["docs.tsx"]
	if strings.Contains(out, "data-krate-once") || strings.Contains(out, "setAttribute('data-theme'") {
		t.Errorf("themeScript false should emit no script:\n%s", out)
	}
	if !strings.Contains(out, `"prePaint":false`) {
		t.Errorf("colorMode.prePaint should be false:\n%s", out)
	}

}

func TestDocsOptionWarnings(t *testing.T) {
	cfg := &config.Config{Plugins: []config.PluginConfig{{Name: "docs", Options: map[string]interface{}{
		"rootLayout": true, "defaultTheme": "dark", "themeScript": false,
	}}}}
	if w := ConfigWarnings(cfg); len(w) != 0 {
		t.Errorf("valid options warned: %v", w)
	}
	cfg.Plugins[0].Options = map[string]interface{}{"rootLayouts": true, "defaultTheme": "sepia"}
	w := strings.Join(ConfigWarnings(cfg), "\n")
	if !strings.Contains(w, `unknown docs option "rootLayouts"`) || !strings.Contains(w, "defaultTheme") {
		t.Errorf("warnings = %q", w)
	}
}

func TestDocsRootLayoutFlagsGeneratedPages(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "src/content/docs", "index.md")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte("---\ntitle: Home\n---\n\nHi\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "layout.tsx"), []byte("export default function L(){return null}\n"), 0o644)
	for _, on := range []bool{false, true} {
		cfg := &config.Config{Plugins: []config.PluginConfig{{Name: "docs", Options: map[string]interface{}{
			"layout": "layout.tsx", "search": map[string]interface{}{"enabled": false}, "lastUpdated": false, "rootLayout": on,
		}}}}
		var gen []GeneratedPage
		ctx := &BuildHookCtx{Root: root, OutDir: filepath.Join(root, "dist"), Config: cfg, GeneratedPages: &gen}
		if err := (&DocsPlugin{}).beforeBuild(ctx); err != nil {
			t.Fatal(err)
		}
		if len(gen) == 0 {
			t.Fatal("no pages generated")
		}
		for _, g := range gen {
			if g.RootLayout != on {
				t.Errorf("rootLayout=%v: page %s flagged %v", on, g.Route, g.RootLayout)
			}
		}
	}
}
