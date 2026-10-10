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

// TestMinifyHTMLPreservesRegionMarkers guards the comment stripper: suspense
// AND runtime region splice markers must survive minification or the Go server
// has nothing to splice region HTML into at serve time.
func TestMinifyHTMLPreservesRegionMarkers(t *testing.T) {
	in := `<div id=root><!--suspense:1-1--><span>loading</span><!--/suspense:1-1--><!--region:region-1.Widget_c0--><!--/region:region-1.Widget_c0--><p>static</p></div>`
	out := minifyHTML(in)
	for _, marker := range []string{
		"<!--suspense:1-1-->", "<!--/suspense:1-1-->",
		"<!--region:region-1.Widget_c0-->", "<!--/region:region-1.Widget_c0-->",
	} {
		if !strings.Contains(out, marker) {
			t.Errorf("minifyHTML dropped splice marker %q:\n  out: %s", marker, out)
		}
	}
	if !strings.Contains(out, "<p>static</p>") {
		t.Errorf("minifyHTML dropped page content:\n  out: %s", out)
	}
}

// TestRemoveOptionalQuotesPreservesCSP guards the quote stripper against
// corrupting quoted attribute values that contain quotes and spaces, such as a
// Content-Security-Policy: the inner 'self' quotes must survive unminified and
// the attribute's own opening/closing quotes must stay.
func TestRemoveOptionalQuotesPreservesCSP(t *testing.T) {
	in := `<meta http-equiv="Content-Security-Policy" content="default-src 'self'; script-src 'self' 'sha256-abc';">`
	out := removeOptionalQuotes(in)
	want := `content="default-src 'self'; script-src 'self' 'sha256-abc';"`
	if !strings.Contains(out, want) {
		t.Errorf("CSP content value corrupted:\n  in : %s\n  out: %s", in, out)
	}
	if !strings.Contains(out, "'self'") {
		t.Errorf("inner CSP quotes stripped:\n  out: %s", out)
	}
}

// TestRemoveOptionalQuotesKeepsQuotedSpaces ensures a quoted value containing
// spaces keeps both its quotes and doesn't confuse the tag scanner.
func TestRemoveOptionalQuotesKeepsQuotedSpaces(t *testing.T) {
	in := `<div class="foo bar" title="a b" hidden></div>`
	out := removeOptionalQuotes(in)
	if !strings.Contains(out, `class="foo bar"`) || !strings.Contains(out, `title="a b"`) {
		t.Errorf("quoted space values corrupted:\n  in : %s\n  out: %s", in, out)
	}
	if !strings.Contains(out, `hidden>`) {
		t.Errorf("trailing attribute/tag lost after quoted space value:\n  out: %s", out)
	}
}

// TestRemoveOptionalQuotesStillDropsSimple ensures simple values are still
// safely unquoted.
func TestRemoveOptionalQuotesStillDropsSimple(t *testing.T) {
	in := `<img src="x.png" alt="hi" width="200">`
	out := removeOptionalQuotes(in)
	if !strings.Contains(out, `src=x.png alt=hi width=200>`) {
		t.Errorf("expected simple attribute quotes dropped:\n  out: %s", out)
	}
}

// TestRemoveOptionalQuotesKeepsEmpty preserves empty values (dropping them
// would merge adjacent attributes).
func TestRemoveOptionalQuotesKeepsEmpty(t *testing.T) {
	in := `<img onerror="" onload="">`
	out := removeOptionalQuotes(in)
	if !strings.Contains(out, `onerror="" onload=""`) {
		t.Errorf("empty attribute values must keep quotes:\n  out: %s", out)
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
		requireE2E(t, "runtime dist not available in this environment")
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
