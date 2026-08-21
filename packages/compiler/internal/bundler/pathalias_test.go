package bundler

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathAliasExactMatch(t *testing.T) {
	dir := t.TempDir()
	// Create target file
	utilsDir := filepath.Join(dir, "src", "utils")
	os.MkdirAll(utilsDir, 0755)
	os.WriteFile(filepath.Join(utilsDir, "helper.ts"), []byte("export const x = 1"), 0644)

	aliases := []pathAlias{
		{prefix: "@utils", targets: []string{"./src/utils/index.ts"}},
	}
	// Also create index.ts
	os.WriteFile(filepath.Join(utilsDir, "index.ts"), []byte("export const x = 1"), 0644)

	result := resolvePathAlias("@utils", aliases, dir)
	if result == "" {
		t.Fatal("expected to resolve @utils, got empty string")
	}
	if filepath.Clean(result) != filepath.Clean(filepath.Join(dir, "src", "utils", "index.ts")) {
		t.Errorf("resolved to unexpected path: %s", result)
	}
}

func TestResolvePathAliasWildcard(t *testing.T) {
	dir := t.TempDir()
	// Create target file — alias "./*" resolves relative to tsBaseDir (dir)
	compDir := filepath.Join(dir, "components")
	os.MkdirAll(compDir, 0755)
	os.WriteFile(filepath.Join(compDir, "Button.tsx"), []byte("export default function Button() {}"), 0644)

	aliases := []pathAlias{
		{prefix: "@/*", targets: []string{"./*"}},
	}

	result := resolvePathAlias("@/components/Button", aliases, dir)
	if result == "" {
		t.Fatal("expected to resolve @/components/Button, got empty string")
	}
	expected := filepath.Clean(filepath.Join(dir, "components", "Button.tsx"))
	if filepath.Clean(result) != expected {
		t.Errorf("resolved to %s, want %s", result, expected)
	}
}

func TestResolvePathAliasWithExtension(t *testing.T) {
	dir := t.TempDir()
	compDir := filepath.Join(dir, "components")
	os.MkdirAll(compDir, 0755)
	os.WriteFile(filepath.Join(compDir, "Button.tsx"), []byte("export default function Button() {}"), 0644)

	aliases := []pathAlias{
		{prefix: "@/*", targets: []string{"./*"}},
	}

	result := resolvePathAlias("@/components/Button", aliases, dir)
	if result == "" {
		t.Fatal("expected to resolve @/components/Button with extension")
	}
}

func TestResolvePathAliasDirectoryIndex(t *testing.T) {
	dir := t.TempDir()
	// Create directory with index.tsx — alias resolves relative to tsBaseDir (dir)
	compDir := filepath.Join(dir, "components", "Card")
	os.MkdirAll(compDir, 0755)
	os.WriteFile(filepath.Join(compDir, "index.tsx"), []byte("export default function Card() {}"), 0644)

	aliases := []pathAlias{
		{prefix: "@/*", targets: []string{"./*"}},
	}

	result := resolvePathAlias("@/components/Card", aliases, dir)
	if result == "" {
		t.Fatal("expected to resolve @/components/Card to directory index")
	}
	expected := filepath.Clean(filepath.Join(compDir, "index.tsx"))
	if filepath.Clean(result) != expected {
		t.Errorf("resolved to %s, want %s", result, expected)
	}
}

func TestResolvePathAliasNoMatch(t *testing.T) {
	dir := t.TempDir()
	aliases := []pathAlias{
		{prefix: "@/*", targets: []string{"./*"}},
	}

	result := resolvePathAlias("lodash", aliases, dir)
	if result != "" {
		t.Errorf("expected empty for non-matching alias, got %s", result)
	}
}

func TestResolvePathAliasFileNotFound(t *testing.T) {
	dir := t.TempDir()
	aliases := []pathAlias{
		{prefix: "@/*", targets: []string{"./*"}},
	}

	result := resolvePathAlias("@/nonexistent/File", aliases, dir)
	if result != "" {
		t.Errorf("expected empty for nonexistent file, got %s", result)
	}
}

func TestResolvePathAliasMultipleTargets(t *testing.T) {
	dir := t.TempDir()
	// First target doesn't exist, second does
	os.MkdirAll(filepath.Join(dir, "fallback"), 0755)
	os.WriteFile(filepath.Join(dir, "fallback", "index.ts"), []byte("export const x = 1"), 0644)

	aliases := []pathAlias{
		{prefix: "@app", targets: []string{"./primary/index.ts", "./fallback/index.ts"}},
	}

	result := resolvePathAlias("@app", aliases, dir)
	if result == "" {
		t.Fatal("expected to resolve via fallback target")
	}
}

func TestResolvePathAliasAbsoluteTarget(t *testing.T) {
	dir := t.TempDir()
	// Create file with absolute target
	targetDir := filepath.Join(dir, "absolute")
	os.MkdirAll(targetDir, 0755)
	os.WriteFile(filepath.Join(targetDir, "mod.ts"), []byte("export const x = 1"), 0644)

	aliases := []pathAlias{
		{prefix: "@abs", targets: []string{targetDir + "/mod.ts"}},
	}

	result := resolvePathAlias("@abs", aliases, dir)
	if result == "" {
		t.Fatal("expected to resolve absolute target")
	}
}

func TestResolveImportForModuleWithAliases(t *testing.T) {
	dir := t.TempDir()
	// Create the source file and the alias target
	os.MkdirAll(filepath.Join(dir, "src", "components"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "main.tsx"), []byte("import Button from '@/components/Button'"), 0644)
	os.WriteFile(filepath.Join(dir, "src", "components", "Button.tsx"), []byte("export default function Button() {}"), 0644)

	b := New(dir)
	b.SetPathAliases(
		[]string{"@/*"},
		[][]string{{"./*"}},
		filepath.Join(dir, "src"),
	)

	result := b.resolveImportForModule(filepath.Join(dir, "src", "main.tsx"), "@/components/Button")
	if result == "" {
		t.Fatal("resolveImportForModule failed to resolve path alias")
	}
	expected := filepath.Clean(filepath.Join(dir, "src", "components", "Button.tsx"))
	if filepath.Clean(result) != expected {
		t.Errorf("resolved to %s, want %s", result, expected)
	}
}

func TestResolveImportForModuleNoAlias(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)

	// No aliases configured — should still work for standard resolution
	result := b.resolveImportForModule(filepath.Join(dir, "main.tsx"), "nonexistent")
	if result != "" {
		t.Errorf("expected empty for nonexistent module without aliases, got %s", result)
	}
}

func TestSetPathAliases(t *testing.T) {
	b := New(t.TempDir())
	b.SetPathAliases(
		[]string{"@/*", "@ui/*"},
		[][]string{{"./*"}, {"./ui/*"}},
		"/some/dir",
	)

	if len(b.pathAliases) != 2 {
		t.Fatalf("expected 2 aliases, got %d", len(b.pathAliases))
	}
	if b.pathAliases[0].prefix != "@/*" {
		t.Errorf("alias[0].prefix = %q, want @/*", b.pathAliases[0].prefix)
	}
	if b.pathAliases[1].prefix != "@ui/*" {
		t.Errorf("alias[1].prefix = %q, want @ui/*", b.pathAliases[1].prefix)
	}
	if b.tsBaseDir != "/some/dir" {
		t.Errorf("tsBaseDir = %q, want /some/dir", b.tsBaseDir)
	}
}

func TestSetPathAliasesMismatchedLengths(t *testing.T) {
	b := New(t.TempDir())
	// More prefixes than targets
	b.SetPathAliases(
		[]string{"@/*", "@ui/*"},
		[][]string{{"./*"}},
		"/dir",
	)

	if len(b.pathAliases) != 1 {
		t.Errorf("expected 1 alias (clamped to shorter slice), got %d", len(b.pathAliases))
	}
}
