package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installPreact11 copies the Preact 11 fixture (a .mjs layout without the
// dist/*.module.js files Preact 10 has) over the project's preact.
func installPreact11(t *testing.T, root string) {
	t.Helper()
	dst := filepath.Join(root, "node_modules", "preact")
	if err := os.RemoveAll(dst); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(t, filepath.Join("testdata", "preact11"), dst); err != nil {
		t.Fatal(err)
	}
}

// With Preact 11 installed (its files are .mjs and its exports map names
// them), a React island builds and is server-rendered. Before, the server
// render looked for Preact 10's file names, failed, and blamed a missing
// Preact; and the vendored preact-render-to-string rendered nothing at all.
func TestIslandServerRendersUnderPreact11(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, false)
	installPreact11(t, root)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	if strings.Contains(out, "KR8008") || strings.Contains(out, "not installed") {
		t.Fatalf("the island fell back to client-only:\n%s", out)
	}
	html := readBuilt(t, cfg.OutDir+"/index.html")
	if !strings.Contains(html, "data-krate-island-ssr") || !strings.Contains(html, `<span class="badge">hello</span>`) {
		t.Errorf("island markup missing from the server render:\n%s", html)
	}
}

// preactEntry follows the package's exports map and falls back to Preact 10's
// file names when the map is absent.
func TestPreactEntryFollowsExports(t *testing.T) {
	root := t.TempDir()
	v11 := filepath.Join(root, "v11")
	if err := copyTree(t, filepath.Join("testdata", "preact11"), v11); err != nil {
		t.Fatal(err)
	}
	v10 := filepath.Join(root, "v10")
	writeProjectFile(t, root, "v10/package.json", `{"name":"preact","version":"10.29.8"}`)
	cases := []struct{ dir, sub, legacy, want string }{
		{v11, ".", "dist/preact.module.js", v11 + "/dist/preact.mjs"},
		{v11, "./compat", "compat/dist/compat.module.js", v11 + "/compat/dist/compat.mjs"},
		{v11, "./hooks", "hooks/dist/hooks.module.js", v11 + "/hooks/dist/hooks.mjs"},
		{v11, "./jsx-runtime", "jsx-runtime/dist/jsxRuntime.module.js", v11 + "/jsx-runtime/dist/jsxRuntime.mjs"},
		{v10, "./compat", "compat/dist/compat.module.js", v10 + "/compat/dist/compat.module.js"},
	}
	for _, c := range cases {
		if got := preactEntry(c.dir, c.sub, c.legacy); got != filepath.ToSlash(c.want) {
			t.Errorf("preactEntry(%s, %s) = %s, want %s", filepath.Base(c.dir), c.sub, got, c.want)
		}
	}
}

// A package that fails to bundle while Preact IS installed must not be
// reported as a missing Preact.
func TestIslandBundleErrorDoesNotBlameInstalledPreact(t *testing.T) {
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	writeProjectFile(t, root, "node_modules/fake-lib/index.js", `
		import { jsx } from 'react/jsx-runtime';
		import missing from 'preact-extras-not-installed';
		export function Badge({ label }) { return jsx('span', { children: missing(label) }); }
	`)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	text := out
	if err != nil {
		text += err.Error()
	}
	if strings.Contains(text, "which is not installed in this project") {
		t.Errorf("blamed a missing Preact although it is installed:\n%s", text)
	}
	if !strings.Contains(text, "preact-extras-not-installed") {
		t.Errorf("the unresolved package is not named:\n%s", text)
	}
}
