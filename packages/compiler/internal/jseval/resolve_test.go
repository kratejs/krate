package jseval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeKratePackage creates node_modules/<pkg> under dir with the given
// package.json and files (relative to the package dir).
func writeKratePackage(t *testing.T, dir, pkg, pkgJSON string, files map[string]string) {
	t.Helper()
	pkgDir := filepath.Join(dir, "node_modules", filepath.FromSlash(pkg))
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		p := filepath.Join(pkgDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestEvalResolvesKratePackageMain covers the `main` field shape (as @krate/core
// ships) - the exact case that failed through a Windows junction.
func TestEvalResolvesKratePackageMain(t *testing.T) {
	dir := t.TempDir()
	writeKratePackage(t, dir, "@krate/core",
		`{"name":"@krate/core","main":"./dist/index.js"}`,
		map[string]string{"dist/index.js": "export const defineConfig = (c) => c;\nexport default defineConfig;\n"},
	)
	out, err := eval(t, dir, `import { defineConfig } from '@krate/core';
globalThis.__krateEvalResult = defineConfig({ a: 1 });`, nil)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("output %s: %v", out, err)
	}
	if m["a"].(float64) != 1 {
		t.Fatalf("defineConfig result not folded: %s", out)
	}
}

// TestEvalResolvesKratePackageExports covers the `exports` condition shape (as
// @krate/plugin and @krate/runtime ship).
func TestEvalResolvesKratePackageExports(t *testing.T) {
	dir := t.TempDir()
	writeKratePackage(t, dir, "@krate/plugin",
		`{"name":"@krate/plugin","exports":{".":{"types":"./dist/index.d.ts","import":"./dist/index.js"}}}`,
		map[string]string{"dist/index.js": "export const helper = 7;\n"},
	)
	out, err := eval(t, dir, `import { helper } from '@krate/plugin';
globalThis.__krateEvalResult = { n: helper + 1 };`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"n":8}` {
		t.Fatalf("exports entry not resolved: %s", out)
	}
}

// TestEvalResolvesKrateSubpath covers a subpath export (as @krate/components
// ships, e.g. `@krate/components/button`).
func TestEvalResolvesKrateSubpath(t *testing.T) {
	dir := t.TempDir()
	writeKratePackage(t, dir, "@krate/components",
		`{"name":"@krate/components","exports":{".":"./src/index.ts","./button":"./src/button/button.ts"}}`,
		map[string]string{
			"src/index.ts":         "export const root = 1;\n",
			"src/button/button.ts": "export const Button = 'btn';\n",
		},
	)
	out, err := eval(t, dir, `import { Button } from '@krate/components/button';
globalThis.__krateEvalResult = { tag: Button };`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"tag":"btn"}` {
		t.Fatalf("subpath export not resolved: %s", out)
	}
}

// TestEvalKratePackageMissingError is a clear error when the package is absent.
func TestEvalKratePackageMissingError(t *testing.T) {
	_, err := eval(t, t.TempDir(), `import { x } from '@krate/nope';
globalThis.__krateEvalResult = { x };`, nil)
	if err == nil {
		t.Fatal("expected an error for a missing @krate package")
	}
}
