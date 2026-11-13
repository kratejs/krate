package jseval

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// nodeProject has a server-only package that throws like the real one, and a
// CommonJS dependency that requires the fs built-in.
func nodeProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, "node_modules/server-only/package.json", `{"name":"server-only","main":"index.js"}`)
	writeTree(t, dir, "node_modules/server-only/index.js", `throw new Error("This module cannot be imported from a Client Component module");`)
	writeTree(t, dir, "node_modules/cjsdep/package.json", `{"name":"cjsdep","main":"index.js"}`)
	writeTree(t, dir, "node_modules/cjsdep/index.js", `const fs = require("fs"); module.exports = { has: typeof fs.readFileSync };`)
	return dir
}

func writeTree(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, filepath.FromSlash(name))), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, filepath.FromSlash(name), body)
}

const nodeSource = `import "server-only";
import dep from "cjsdep";
console.log(dep.has);
`

func TestBundleRunsServerOnlyAndCommonJSBuiltins(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	dir := nodeProject(t)
	js, err := Bundle(Options{Source: nodeSource, ResolveDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "out.mjs")
	if err := os.WriteFile(path, []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, path).CombinedOutput()
	if err != nil {
		t.Fatalf("node failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "function" {
		t.Fatalf("output = %q", out)
	}
}

func TestExplainDynamicRequire(t *testing.T) {
	err := explainDynamicRequire(errors.New(`Error: Dynamic require of "fs" is not supported`))
	if !strings.Contains(err.Error(), `needs the Node built-in "fs"`) {
		t.Fatalf("error lacks guidance: %v", err)
	}
	plain := errors.New("boom")
	if explainDynamicRequire(plain) != plain {
		t.Fatal("unrelated errors must pass through")
	}
}
