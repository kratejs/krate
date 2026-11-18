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

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEvalImportMetaDirnameAndFilename(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, sub, "helper.ts", "export const here = { dir: import.meta.dirname, file: import.meta.filename, text: 'import.meta.dirname stays', tpl: `${import.meta.filename}!` };\n")
	out, err := eval(t, dir, `import { here } from './sub/helper';
globalThis.__krateEvalResult = { here };`, nil)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Here struct{ Dir, File, Text, Tpl string }
	}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("output %s: %v", out, err)
	}
	if m.Here.Dir != filepath.ToSlash(sub) || m.Here.File != filepath.ToSlash(filepath.Join(sub, "helper.ts")) || m.Here.Text != "import.meta.dirname stays" || m.Here.Tpl != filepath.ToSlash(filepath.Join(sub, "helper.ts"))+"!" {
		t.Fatalf("helper meta wrong: %s", out)
	}
}
