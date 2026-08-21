package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMinifyJSKeepsRegexLiterals guards against the minifier misreading the
// escaped slash + closing delimiter in a regex like /^(https?:)?\/\// as a //
// line comment (which would truncate the file and break the runtime).
func TestMinifyJSKeepsRegexLiterals(t *testing.T) {
	in := `function f(src){if(/^(https?:)?\/\//.test(src)||src.startsWith('/')){return new URL(src,"http://x").href;}}`
	out := minifyJSBase(in)
	if !strings.Contains(out, "/^(https?:)?\\/\\//.test(src)") && !strings.Contains(out, "\\/\\//.test") {
		t.Errorf("regex literal corrupted by minifier:\n  in : %s\n  out: %s", in, out)
	}
	if strings.Contains(out, "return") == false {
		t.Errorf("minifier truncated the input after the regex literal:\n  out: %s", out)
	}
}

// TestMinifyJSKeepsRUNTIME_CHUNK_RE guards the shared-runtime regex too.
func TestMinifyJSKeepsRUNTIME_CHUNK_RE(t *testing.T) {
	in := `var RUNTIME_CHUNK_RE=/\/chunks\/runtime\.[^/]+\.js$/;`
	out := minifyJSBase(in)
	if !strings.Contains(out, "/\\/chunks\\/runtime\\.[^/]+\\.js$/") {
		t.Errorf("RUNTIME_CHUNK_RE corrupted by minifier:\n  in : %s\n  out: %s", in, out)
	}
}

// TestMinifiedRuntimeIsValidJS runs the full runtime through the minifier and
// verifies the result parses and keeps its window-exposed exports intact.
func TestMinifiedRuntimeIsValidJS(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(pkgDir, "..", "..", "..", ".."))
	rt := loadRuntimeFromDisk(root)
	if rt == "" {
		t.Skip("runtime dist not available in this environment")
	}
	min := minifyJSBase(rt)
	// The runtime exposes its API on window; the window.* property names must
	// survive identifier mangling (properties are not renamed).
	for _, name := range []string{"reconcileTrees", "initRouter", "createSignal", "createEffect"} {
		if !strings.Contains(min, "window."+name) && !strings.Contains(min, name) {
			t.Fatalf("minified runtime lost core export %q (%d bytes)", name, len(min))
		}
	}
	if len(min) >= len(rt) {
		t.Errorf("esbuild minification did not shrink the runtime (%d -> %d bytes)", len(rt), len(min))
	}

	// Real syntax validation with node if available.
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Log("node not available; skipping syntax check")
		return
	}
	tmp := filepath.Join(t.TempDir(), "runtime.min.js")
	if err := os.WriteFile(tmp, []byte(min), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(nodePath, "--check", tmp).CombinedOutput()
	if err != nil {
		t.Fatalf("minified runtime failed node --check: %v\n%s", err, string(out))
	}
}
