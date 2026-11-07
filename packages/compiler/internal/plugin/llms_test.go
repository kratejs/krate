package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestGenerateLLMs(t *testing.T) {
	root := t.TempDir()
	writePluginFile(t, root, "docs/index.md", "---\ntitle: Intro\ndescription: Start here\n---\n\n# Intro\n\nHello world.\n")
	writePluginFile(t, root, "docs/guides/a.md", "---\ntitle: Guide A\n---\n\nGuide body text.\n")

	out := t.TempDir()
	cfg := config.Default()
	cfg.Plugins = []config.PluginConfig{{Name: "llms", Options: map[string]interface{}{
		"contentDir":  "docs",
		"title":       "My Site",
		"description": "All the docs",
		"baseUrl":     "https://example.com",
	}}}
	ctx := &BuildResultHookCtx{Root: root, OutDir: out, Config: cfg}

	if err := generateLLMs(ctx); err != nil {
		t.Fatalf("generateLLMs: %v", err)
	}

	idx, err := os.ReadFile(filepath.Join(out, "llms.txt"))
	if err != nil {
		t.Fatalf("llms.txt missing: %v", err)
	}
	full, err := os.ReadFile(filepath.Join(out, "llms-full.txt"))
	if err != nil {
		t.Fatalf("llms-full.txt missing: %v", err)
	}

	for _, want := range []string{"# My Site", "> All the docs", "Intro", "https://example.com/docs/", "Guide A"} {
		if !strings.Contains(string(idx), want) {
			t.Errorf("llms.txt missing %q:\n%s", want, idx)
		}
	}
	for _, want := range []string{"Guide body text.", "Hello world."} {
		if !strings.Contains(string(full), want) {
			t.Errorf("llms-full.txt missing %q:\n%s", want, full)
		}
	}
}

func TestGenerateLLMsNotConfigured(t *testing.T) {
	cfg := config.Default()
	ctx := &BuildResultHookCtx{Root: t.TempDir(), OutDir: t.TempDir(), Config: cfg}
	if err := generateLLMs(ctx); err != nil {
		t.Fatalf("unconfigured llms plugin should be a no-op: %v", err)
	}
}

func writePluginFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateLLMsBasePath(t *testing.T) {
	cases := []struct {
		name, opt, seo, basePath, want string
	}{
		{"explicit basePath", "https://example.com", "", "/sub", "https://example.com/sub/docs/"},
		{"site url carries the path", "", "https://example.com/sub", "", "https://example.com/sub/docs/"},
		{"no origin", "", "", "/sub", "/sub/docs/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writePluginFile(t, root, "docs/index.md", "---\ntitle: Intro\n---\n\nHi.\n")
			out := t.TempDir()
			cfg := config.Default()
			cfg.SEO.BaseURL = tc.seo
			cfg.BasePath = tc.basePath
			cfg.Plugins = []config.PluginConfig{{Name: "llms", Options: map[string]interface{}{
				"contentDir": "docs", "baseUrl": tc.opt,
			}}}
			if err := generateLLMs(&BuildResultHookCtx{Root: root, OutDir: out, Config: cfg}); err != nil {
				t.Fatal(err)
			}
			for _, n := range []string{"llms.txt", "llms-full.txt"} {
				b, _ := os.ReadFile(filepath.Join(out, n))
				if !strings.Contains(string(b), tc.want) || strings.Contains(string(b), "/sub/sub") {
					t.Errorf("%s: want %q, got:\n%s", n, tc.want, b)
				}
			}
		})
	}
}
