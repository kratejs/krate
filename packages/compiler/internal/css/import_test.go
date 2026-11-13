package css

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInlineImportsTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Dir(dir)

	// Place a decoy outside the project root.
	decoy := filepath.Join(root, "secret.css")
	_ = os.WriteFile(decoy, []byte(".secret { display: none; }"), 0644)

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

func TestInlineImportsAbsoluteAndURLRejected(t *testing.T) {
	dir := t.TempDir()
	decoy := filepath.Join(dir, "abs.css")
	_ = os.WriteFile(decoy, []byte(".abs { color: red; }"), 0644)

	// A file outside the root, referenced by absolute path.
	outside := filepath.Join(filepath.Dir(dir), "outside.css")
	_ = os.WriteFile(outside, []byte(".outside { color: blue; }"), 0644)

	mainCSS := `@import "https://evil.example/x.css";
@import "file:///etc/passwd";
@import "` + filepath.ToSlash(filepath.Join(dir, "abs.css")) + `";
@import "` + filepath.ToSlash(outside) + `";
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
