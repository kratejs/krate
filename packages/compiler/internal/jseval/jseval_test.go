package jseval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func eval(t *testing.T, dir, source string, env map[string]string) (json.RawMessage, error) {
	t.Helper()
	return Eval(Options{Source: source, ResolveDir: dir, Env: env})
}

func TestEvalBasicValue(t *testing.T) {
	out, err := eval(t, t.TempDir(), `globalThis.__krateEvalResult = { a: 1, b: "x" };`, nil)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("output %s: %v", out, err)
	}
	if m["a"].(float64) != 1 || m["b"] != "x" {
		t.Fatalf("unexpected value: %s", out)
	}
}

func TestEvalResolvesImports(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "helper.ts", "export const value = 41;\n")
	out, err := eval(t, dir, `import { value } from './helper';
globalThis.__krateEvalResult = { n: value + 1 };`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "42") {
		t.Fatalf("imported value not folded: %s", out)
	}
}

// TestEvalPreservesImportMetaURL verifies each source file keeps its own
// import.meta.url (rather than collapsing onto the bundle location), which
// plugin/theme factories rely on to locate their real source on disk.
func TestEvalPreservesImportMetaURL(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "helper.ts", "export const url = import.meta.url;\n")
	out, err := eval(t, dir, `import { url } from './helper';
globalThis.__krateEvalResult = { url };`, nil)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(m["url"], "file://") || !strings.Contains(m["url"], "helper.ts") {
		t.Fatalf("import.meta.url not preserved: %q", m["url"])
	}
}

func TestRewriteImportMetaURLLeavesStringsAndComments(t *testing.T) {
	src := "const a = import.meta.url;\n" +
		"const s = \"import.meta.url\";\n" +
		"const t = 'import.meta.url';\n" +
		"const c = `x import.meta.url ${import.meta.url} y`;\n" +
		"// import.meta.url\n" +
		"/* import.meta.url */\n" +
		"const w = `${ { k: import.meta.url } }`;\n" +
		"const n = notimport.meta.urlX;\n"
	want := "const a = \"file:///p/x.ts\";\n" +
		"const s = \"import.meta.url\";\n" +
		"const t = 'import.meta.url';\n" +
		"const c = `x import.meta.url ${\"file:///p/x.ts\"} y`;\n" +
		"// import.meta.url\n" +
		"/* import.meta.url */\n" +
		"const w = `${ { k: \"file:///p/x.ts\" } }`;\n" +
		"const n = notimport.meta.urlX;\n"
	if got := rewriteImportMetaURL(src, `"file:///p/x.ts"`); got != want {
		t.Fatalf("rewrite mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestEvalStringContainingImportMetaURL guards against the naive text rewrite
// that corrupted a double-quoted string holding the literal text (as in the
// generated docs content module).
func TestEvalStringContainingImportMetaURL(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "content.ts", "export const sample = \"code: import.meta.url\";\nexport const url = import.meta.url;\n")
	out, err := eval(t, dir, `import { sample, url } from './content';
globalThis.__krateEvalResult = { sample, url };`, nil)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["sample"] != "code: import.meta.url" {
		t.Fatalf("string literal rewritten: %q", m["sample"])
	}
	if !strings.HasPrefix(m["url"], "file://") || !strings.Contains(m["url"], "content.ts") {
		t.Fatalf("import.meta.url not preserved: %q", m["url"])
	}
}

func TestEvalExposesEnv(t *testing.T) {
	out, err := eval(t, t.TempDir(), `globalThis.__krateEvalResult = process.env.KRATE_TEST;`, map[string]string{"KRATE_TEST": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"hello"` {
		t.Fatalf("env not exposed: %s", out)
	}
}

func TestEvalThrowingSource(t *testing.T) {
	if _, err := eval(t, t.TempDir(), `throw new Error("boom")`, nil); err == nil {
		t.Fatal("expected an error for a throwing entry")
	}
}

func TestEvalNoResult(t *testing.T) {
	if _, err := eval(t, t.TempDir(), `const x = 1;`, nil); err == nil {
		t.Fatal("expected an error when the entry sets no result")
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
