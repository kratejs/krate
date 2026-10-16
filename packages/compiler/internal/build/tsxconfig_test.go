package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestWriteTsxTsconfigIncludesAliasesAndContent(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Content = map[string]any{
		"blog": map[string]any{"dir": "src/content/blog", "schema": map[string]any{"title": "string"}},
	}
	// A project path alias.
	cfg.PathAliases = []config.PathAlias{{Prefix: "@/*", Targets: []string{"./src/*"}}}
	cfg.TSBaseDir = root

	b := New(root, cfg)
	path, err := b.writeTsxTsconfig()
	if err != nil {
		t.Fatalf("writeTsxTsconfig: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		CompilerOptions struct {
			BaseURL string              `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("tsconfig is not valid JSON: %v\n%s", err, data)
	}
	if doc.CompilerOptions.BaseURL != ".." {
		t.Errorf("baseUrl = %q, want ..", doc.CompilerOptions.BaseURL)
	}
	if _, ok := doc.CompilerOptions.Paths["krate/content"]; !ok {
		t.Errorf("expected krate/content path, got %v", doc.CompilerOptions.Paths)
	}
	if _, ok := doc.CompilerOptions.Paths["@krate/content"]; !ok {
		t.Errorf("expected @krate/content path, got %v", doc.CompilerOptions.Paths)
	}
	if got := doc.CompilerOptions.Paths["@/*"]; len(got) != 1 || got[0] != "./src/*" {
		t.Errorf("@/* path = %v, want [./src/*]", got)
	}
	// The content target must be inside .krate/gen.
	if got := doc.CompilerOptions.Paths["krate/content"][0]; !strings.Contains(got, ".krate/gen/content.ts") {
		t.Errorf("krate/content target = %q", got)
	}
}

func TestWriteTsxTsconfigNoContent(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Resolve(root)

	b := New(root, cfg)
	path, err := b.writeTsxTsconfig()
	if err != nil {
		t.Fatalf("writeTsxTsconfig: %v", err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "krate/content") {
		t.Errorf("tsconfig should not reference krate/content without collections:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(root, ".krate", "tsconfig.json")); err != nil {
		t.Errorf("expected .krate/tsconfig.json: %v", err)
	}
}
