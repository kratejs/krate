package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A symlink inside the project that points at a protected file must not be a
// way around the default deny list.
func TestPolicyDeniesSymlinkAliasOfProtectedFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
		".env":                "SECRET=1\n",
	})
	if err := os.Symlink(filepath.Join(svc.root, ".env"), filepath.Join(svc.root, "notes.txt")); err != nil {
		t.Fatal(err)
	}

	text := toolText(t, serve(t, svc, call("read_file", map[string]any{"path": "notes.txt"}))[0])
	if strings.Contains(text, "SECRET=1") {
		t.Fatalf("read_file served .env through a symlink: %s", text)
	}
	text = toolText(t, serve(t, svc, call("read_files", map[string]any{"paths": []string{"notes.txt"}}))[0])
	if strings.Contains(text, "SECRET=1") {
		t.Fatalf("read_files served .env through a symlink: %s", text)
	}
	text = toolText(t, serve(t, svc, call("edit_file", map[string]any{
		"route": "notes.txt", "content": "SECRET=2\n", "apply": true,
	}))[0])
	data, _ := os.ReadFile(filepath.Join(svc.root, ".env"))
	if string(data) != "SECRET=1\n" {
		t.Fatalf("edit_file wrote .env through a symlink (%s): %q", text, data)
	}
	text = toolText(t, serve(t, svc, call("search_project", map[string]any{"query": "SECRET"}))[0])
	if strings.Contains(text, "notes.txt") {
		t.Fatalf("search_project matched .env through a symlink: %s", text)
	}
}

// The default deny and generated-tree lists must not be sidestepped by
// changing the case of a name: macOS and Windows filesystems are
// case-insensitive.
func TestPolicyDefaultsIgnoreCase(t *testing.T) {
	p := &Policy{}
	for _, rel := range []string{".ENV", ".Env.local", "ID_RSA", "Server.PEM", ".GIT/config", ".Krate/mcp.json", "Config/.ENV"} {
		if !p.denied(rel) {
			t.Errorf("%q is readable; the default deny list is case-sensitive", rel)
		}
	}
	for _, rel := range []string{"Node_Modules/pkg/x.js", "DIST/index.html", ".KRATE/types/x.d.ts"} {
		if p.writable(rel) {
			t.Errorf("%q is writable; the generated-tree list is case-sensitive", rel)
		}
	}
	custom := &Policy{Deny: []string{"src/secret/**"}, ReadOnly: []string{"krate.config.ts"}}
	if !custom.denied("src/Secret/key.ts") {
		t.Error("a deny rule is case-sensitive")
	}
	if custom.writable("Krate.Config.ts") {
		t.Error("a read-only rule is case-sensitive")
	}
	allowed := &Policy{Allow: []string{"config/public.env"}}
	if allowed.denied("config/public.env") {
		t.Error("an exact allow entry stopped working")
	}
	if !allowed.denied("config/PUBLIC.ENV") {
		t.Error("an allow entry must not widen to other cases")
	}
}
