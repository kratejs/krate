package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

var scriptTagRe = regexp.MustCompile(`<script\b[^>]*>`)

// executableInlineScripts lists the <script> tags of html that run code from
// the document itself: no src and not a data block. A strict `script-src
// 'self'` policy blocks exactly these.
func executableInlineScripts(html string) []string {
	var out []string
	for _, tag := range scriptTagRe.FindAllString(html, -1) {
		if strings.Contains(tag, " src=") || strings.Contains(tag, "application/json") || strings.Contains(tag, "importmap") {
			continue
		}
		out = append(out, tag)
	}
	return out
}

func TestRuntimeHeadConfigBlock(t *testing.T) {
	newBuilder := func(mutate func(*config.Config)) *Builder {
		cfg := config.Default()
		mutate(cfg)
		return New(t.TempDir(), cfg)
	}
	t.Run("defaults need no block", func(t *testing.T) {
		head := newBuilder(func(*config.Config) {}).runtimeCommonHead()
		if strings.Contains(head, "__KRATE_CFG__") || strings.Contains(head, "__krate_cfg__") || strings.Contains(head, "<script") {
			t.Errorf("a site on the defaults must not carry a config block:\n%s", head)
		}
	})
	t.Run("view transitions off", func(t *testing.T) {
		head := newBuilder(func(c *config.Config) { c.ViewTransitions = "off" }).runtimeCommonHead()
		want := `<script type="application/json" id="__krate_cfg__">{"viewTransitions":false}</script>`
		if !strings.Contains(head, want) {
			t.Errorf("want %s in:\n%s", want, head)
		}
		if strings.Contains(head, "@view-transition") {
			t.Errorf("off must drop the view-transition style:\n%s", head)
		}
	})
	t.Run("base path", func(t *testing.T) {
		head := newBuilder(func(c *config.Config) { c.BasePath = "/docs" }).runtimeCommonHead()
		if !strings.Contains(head, `id="__krate_cfg__">{"basePath":"/docs"}</script>`) || len(executableInlineScripts(head)) != 0 {
			t.Errorf("base path must travel in the JSON block:\n%s", head)
		}
	})
	t.Run("dev flag", func(t *testing.T) {
		b := newBuilder(func(*config.Config) {})
		b.DevMode = true
		if head := b.runtimeCommonHead(); !strings.Contains(head, `{"dev":true}</script>`) {
			t.Errorf("dev mode must be flagged:\n%s", head)
		}
	})
}

// TestBuiltPagesRunUnderStrictScriptSrc builds a site and checks that its
// prerendered and request-time pages carry no executable inline script, so
// `script-src 'self'` lets them work.
func TestBuiltPagesRunUnderStrictScriptSrc(t *testing.T) {
	root, cfg := serverlessProject(t, "serverless")
	for _, rel := range []string{"index.html", "about/index.html", "ssr/index.html", "404.html"} {
		data, err := os.ReadFile(filepath.Join(cfg.OutDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: %v (root %s)", rel, err, root)
		}
		if bad := executableInlineScripts(string(data)); len(bad) != 0 {
			t.Errorf("%s has executable inline scripts: %v", rel, bad)
		}
	}
}
