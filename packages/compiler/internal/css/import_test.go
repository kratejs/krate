package css

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInlineImports(t *testing.T) {
	// Create temp directory with test CSS files
	dir := t.TempDir()

	// Write main.css with an @import
	mainCSS := `@import "base.css";
.container { color: red; }`
	os.WriteFile(filepath.Join(dir, "main.css"), []byte(mainCSS), 0644)

	// Write base.css
	baseCSS := `body { margin: 0; }
p { line-height: 1.5; }`
	os.WriteFile(filepath.Join(dir, "base.css"), []byte(baseCSS), 0644)

	// Test inlining
	result := InlineImports(mainCSS, dir)
	if result == mainCSS {
		t.Errorf("expected @import to be inlined, got unchanged input: %s", result)
	}
	if !contains(result, "body { margin: 0; }") {
		t.Errorf("expected base.css content inlined, got: %s", result)
	}
	if !contains(result, ".container { color: red; }") {
		t.Errorf("expected original content preserved, got: %s", result)
	}
}

func TestInlineImportsCircular(t *testing.T) {
	dir := t.TempDir()

	// Create circular imports: a.css imports b.css, b.css imports a.css
	aCSS := `@import "b.css";
.a { color: red; }`
	os.WriteFile(filepath.Join(dir, "a.css"), []byte(aCSS), 0644)

	bCSS := `@import "a.css";
.b { color: blue; }`
	os.WriteFile(filepath.Join(dir, "b.css"), []byte(bCSS), 0644)

	result := InlineImports(aCSS, dir)
	// Should not infinite loop — circular import is skipped
	if !contains(result, ".a { color: red; }") {
		t.Errorf("expected .a content, got: %s", result)
	}
	if !contains(result, ".b { color: blue; }") {
		t.Errorf("expected .b content, got: %s", result)
	}
}

func TestInlineImportsUrlSyntax(t *testing.T) {
	dir := t.TempDir()

	mainCSS := `@import url("base.css");
.container { color: red; }`
	os.WriteFile(filepath.Join(dir, "main.css"), []byte(mainCSS), 0644)

	baseCSS := `body { margin: 0; }`
	os.WriteFile(filepath.Join(dir, "base.css"), []byte(baseCSS), 0644)

	result := InlineImports(mainCSS, dir)
	if !contains(result, "body { margin: 0; }") {
		t.Errorf("expected url() syntax to be inlined, got: %s", result)
	}
}

func TestInlineImportsNotFound(t *testing.T) {
	dir := t.TempDir()

	mainCSS := `@import "nonexistent.css";
.container { color: red; }`

	// Should leave @import as-is when file not found
	result := InlineImports(mainCSS, dir)
	if !contains(result, "@import \"nonexistent.css\"") {
		t.Errorf("expected @import preserved when file not found, got: %s", result)
	}
}

func TestInlineImportsNested(t *testing.T) {
	dir := t.TempDir()

	// main imports level1, level1 imports level2
	mainCSS := `@import "level1.css";
.main { color: red; }`
	os.WriteFile(filepath.Join(dir, "main.css"), []byte(mainCSS), 0644)

	level1CSS := `@import "level2.css";
.level1 { color: green; }`
	os.WriteFile(filepath.Join(dir, "level1.css"), []byte(level1CSS), 0644)

	level2CSS := `.level2 { color: blue; }`
	os.WriteFile(filepath.Join(dir, "level2.css"), []byte(level2CSS), 0644)

	result := InlineImports(mainCSS, dir)
	if !contains(result, ".level2 { color: blue; }") {
		t.Errorf("expected level2.css inlined, got: %s", result)
	}
	if !contains(result, ".level1 { color: green; }") {
		t.Errorf("expected level1.css inlined, got: %s", result)
	}
	if !contains(result, ".main { color: red; }") {
		t.Errorf("expected main content preserved, got: %s", result)
	}
}

func TestInlineImportsMaxDepth(t *testing.T) {
	dir := t.TempDir()

	// Create a chain of imports deeper than 10
	css := `.deep { color: red; }`
	os.WriteFile(filepath.Join(dir, "level10.css"), []byte(css), 0644)

	for i := 9; i >= 1; i-- {
		css = `@import "level` + itoa(i+1) + `.css";`
		os.WriteFile(filepath.Join(dir, "level"+itoa(i)+".css"), []byte(css), 0644)
	}

	mainCSS := `@import "level1.css";
.main {}`
	result := InlineImports(mainCSS, dir)
	// Should stop at depth 10 — the deep import should still be inlined
	if !contains(result, ".deep { color: red; }") {
		t.Errorf("expected deep content inlined, got: %s", result)
	}
}

func TestInlineImportsTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Dir(dir)

	// Place a decoy outside the project root.
	decoy := filepath.Join(root, "secret.css")
	os.WriteFile(decoy, []byte(".secret { display: none; }"), 0644)

	mainCSS := `@import "../../../` + filepath.Base(dir) + `/../secret.css";
.ok { color: red; }`

	result := InlineImports(mainCSS, dir)
	if contains(result, ".secret") {
		t.Errorf("import escaped the project root:\n%s", result)
	}
	if !contains(result, ".ok { color: red; }") {
		t.Errorf("original content lost:\n%s", result)
	}
}

func TestInlineImportsTraversalFile(t *testing.T) {
	dir := t.TempDir()

	// Create a file outside the base dir.
	outside := filepath.Join(filepath.Dir(dir), "outside.css")
	os.WriteFile(outside, []byte(".outside { color: blue; }"), 0644)

	mainCSS := `@import "../outside.css";
.ok {}`
	result := InlineImports(mainCSS, dir)
	if contains(result, ".outside") {
		t.Errorf("import escaped the base dir:\n%s", result)
	}
}

func TestInlineImportsAbsoluteAndURLRejected(t *testing.T) {
	dir := t.TempDir()
	decoy := filepath.Join(dir, "abs.css")
	os.WriteFile(decoy, []byte(".abs { color: red; }"), 0644)

	// A file outside the root, referenced by absolute path.
	outside := filepath.Join(filepath.Dir(dir), "outside.css")
	os.WriteFile(outside, []byte(".outside { color: blue; }"), 0644)

	mainCSS := `@import "https://evil.example/x.css";
@import "file:///etc/passwd";
@import "` + filepath.Join(dir, "abs.css") + `";
@import "` + outside + `";
.ok {}`
	result := InlineImports(mainCSS, dir)
	if !contains(result, ".abs { color: red; }") {
		t.Errorf("absolute import inside the root should be inlined:\n%s", result)
	}
	if contains(result, ".outside") {
		t.Errorf("absolute import outside the root should be rejected:\n%s", result)
	}
	if !contains(result, "@import \"https://evil.example/x.css\"") {
		t.Errorf("remote import should be left as-is:\n%s", result)
	}
	if !contains(result, ".ok") {
		t.Errorf("original content lost:\n%s", result)
	}
}

func TestInlineImportsNestedRelativeWithinRoot(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b")
	os.MkdirAll(sub, 0755)

	// main at root imports a nested file, which climbs back up two levels to a
	// shared file — still inside the project root.
	os.WriteFile(filepath.Join(dir, "shared.css"), []byte(".shared { color: green; }"), 0644)
	os.WriteFile(filepath.Join(sub, "nested.css"), []byte(`@import "../../shared.css";
.nested {}`), 0644)

	mainCSS := `@import "a/b/nested.css";
.main {}`
	result := InlineImports(mainCSS, dir)
	if !contains(result, ".shared { color: green; }") {
		t.Errorf("expected nested ../ import within root to inline, got:\n%s", result)
	}
	if !contains(result, ".nested") || !contains(result, ".main") {
		t.Errorf("expected nested and main content, got:\n%s", result)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
