package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListFilesSkipsHeavyDirs(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":     "export default function Page() { return <h1>Hi</h1>; }",
		"src/components/ui/a.tsx": "export function A() { return <i/>; }",
		"node_modules/pkg/x.js":   "module.exports = 1;",
		"dist/index.html":         "<html></html>",
	})
	text := toolText(t, serve(t, svc, call("list_files", map[string]any{}))[0])
	var out struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	joined := strings.Join(out.Files, ",")
	if !strings.Contains(joined, "src/pages/index.tsx") || !strings.Contains(joined, "src/components/ui/a.tsx") {
		t.Fatalf("expected project files, got %v", out.Files)
	}
	if strings.Contains(joined, "node_modules") || strings.Contains(joined, "dist/") {
		t.Fatalf("heavy dirs should be skipped, got %v", out.Files)
	}
}

func TestListFilesGlob(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":     "export default function Page() { return <h1>Hi</h1>; }",
		"src/components/ui/a.tsx": "export function A() { return <i/>; }",
		"src/styles/main.css":     ".x{}",
	})
	text := toolText(t, serve(t, svc, call("list_files", map[string]any{"glob": "src/components/**/*.tsx"}))[0])
	if !strings.Contains(text, "src/components/ui/a.tsx") || strings.Contains(text, "src/styles/main.css") {
		t.Fatalf("glob filter wrong: %s", text)
	}
}

func TestReadFile(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
	})
	text := toolText(t, serve(t, svc, call("read_file", map[string]any{"path": "src/pages/index.tsx"}))[0])
	if !strings.Contains(text, `"language": "tsx"`) || !strings.Contains(text, "export default function Page()") {
		t.Fatalf("read_file wrong: %s", text)
	}
}

func TestPolicyDeniesEnvByDefault(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
		".env":                "SECRET=1\n",
	})
	writeFile(t, svc.root, ".env", "SECRET=1\n")

	text := toolText(t, serve(t, svc, call("read_file", map[string]any{"path": ".env"}))[0])
	if !strings.Contains(text, "refusing") {
		t.Fatalf("expected .env read refusal, got %s", text)
	}
	text = toolText(t, serve(t, svc, call("edit_page", map[string]any{
		"route": ".env", "content": "SECRET=2\n", "apply": true,
	}))[0])
	if !strings.Contains(text, "refusing") {
		t.Fatalf("expected .env write refusal, got %s", text)
	}
	data, _ := os.ReadFile(filepath.Join(svc.root, ".env"))
	if string(data) != "SECRET=1\n" {
		t.Fatalf("denied write still modified .env: %q", data)
	}
}

func TestPolicyAllowOverridesDefaultDeny(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
	})
	writeFile(t, svc.root, ".krate/mcp.json", `{"allow": ["config/public.env"]}`)
	svc.policy = loadPolicy(svc.root)
	writeFile(t, svc.root, "config/public.env", "PUBLIC=1\n")

	text := toolText(t, serve(t, svc, call("edit_page", map[string]any{
		"route": "config/public.env", "content": "PUBLIC=2\n", "apply": true,
	}))[0])
	if strings.Contains(text, "refusing") {
		t.Fatalf("allowed path was refused: %s", text)
	}
	data, _ := os.ReadFile(filepath.Join(svc.root, "config", "public.env"))
	if string(data) != "PUBLIC=2\n" {
		t.Fatalf("allowed write did not apply: %q", data)
	}
}

func TestPolicyDenyWins(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
	})
	writeFile(t, svc.root, ".krate/mcp.json", `{"allow": ["src/**"], "deny": ["src/generated/**"]}`)
	svc.policy = loadPolicy(svc.root)
	writeFile(t, svc.root, "src/generated/gen.ts", "export const x = 1;\n")

	text := toolText(t, serve(t, svc, call("edit_page", map[string]any{
		"route": "src/generated/gen.ts", "content": "export const x = 2;\n", "apply": true,
	}))[0])
	if !strings.Contains(text, "refusing") {
		t.Fatalf("deny rule should win over allow, got %s", text)
	}
}

func TestCreateFileRefusesExisting(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
	})
	text := toolText(t, serve(t, svc, call("create_file", map[string]any{
		"path": "src/pages/index.tsx", "content": "x",
	}))[0])
	if !strings.Contains(text, "already exists") {
		t.Fatalf("expected exists refusal, got %s", text)
	}
}

func TestCreateFileDryRunThenApply(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
	})
	dry := toolText(t, serve(t, svc, call("create_file", map[string]any{
		"path": "src/components/a.tsx", "content": "export function A() { return <i/>; }\n",
	}))[0])
	if !strings.Contains(dry, "Dry run") {
		t.Fatalf("expected dry run: %s", dry)
	}
	if _, err := os.Stat(filepath.Join(svc.root, "src", "components", "a.tsx")); err == nil {
		t.Fatal("dry run wrote the file")
	}
	applied := toolText(t, serve(t, svc, call("create_file", map[string]any{
		"path": "src/components/a.tsx", "content": "export function A() { return <i/>; }\n", "apply": true,
	}))[0])
	if !strings.Contains(applied, "Created") {
		t.Fatalf("apply failed: %s", applied)
	}
	if _, err := os.Stat(filepath.Join(svc.root, "src", "components", "a.tsx")); err != nil {
		t.Fatal("apply did not write the file")
	}
}

func TestDeleteAndMoveFile(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":    "export default function Page() { return <h1>Hi</h1>; }",
		"src/components/old.tsx": "export function Old() { return <i/>; }\n",
	})
	moved := toolText(t, serve(t, svc, call("move_file", map[string]any{
		"from": "src/components/old.tsx", "to": "src/components/new.tsx", "apply": true,
	}))[0])
	if !strings.Contains(moved, "Moved") {
		t.Fatalf("move failed: %s", moved)
	}
	if _, err := os.Stat(filepath.Join(svc.root, "src", "components", "old.tsx")); err == nil {
		t.Fatal("old path still exists after move")
	}
	if _, err := os.Stat(filepath.Join(svc.root, "src", "components", "new.tsx")); err != nil {
		t.Fatal("new path missing after move")
	}
	deleted := toolText(t, serve(t, svc, call("delete_file", map[string]any{
		"path": "src/components/new.tsx", "apply": true,
	}))[0])
	if !strings.Contains(deleted, "Deleted") {
		t.Fatalf("delete failed: %s", deleted)
	}
	if _, err := os.Stat(filepath.Join(svc.root, "src", "components", "new.tsx")); err == nil {
		t.Fatal("file still exists after delete")
	}
}

func TestCreateComponent(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
	})
	dry := toolText(t, serve(t, svc, call("create_component", map[string]any{
		"name": "StatusBadge", "kind": "client", "withCss": true,
	}))[0])
	if !strings.Contains(dry, "Dry run") || !strings.Contains(dry, "status-badge/status-badge.tsx") {
		t.Fatalf("unexpected dry run: %s", dry)
	}
	if _, err := os.Stat(filepath.Join(svc.root, "src", "components", "status-badge", "status-badge.tsx")); err == nil {
		t.Fatal("dry run wrote the component")
	}
	applied := toolText(t, serve(t, svc, call("create_component", map[string]any{
		"name": "StatusBadge", "kind": "client", "withCss": true, "apply": true,
	}))[0])
	if !strings.Contains(applied, "Created") {
		t.Fatalf("apply failed: %s", applied)
	}
	src, err := os.ReadFile(filepath.Join(svc.root, "src", "components", "status-badge", "status-badge.tsx"))
	if err != nil {
		t.Fatalf("component not written: %v", err)
	}
	if !strings.Contains(string(src), "export function StatusBadge") {
		t.Fatalf("component scaffold wrong:\n%s", src)
	}
	if _, err := os.Stat(filepath.Join(svc.root, "src", "components", "status-badge", "status-badge.css")); err != nil {
		t.Fatalf("companion CSS not written: %v", err)
	}
}

func TestCreateComponentRejectsBadName(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
	})
	text := toolText(t, serve(t, svc, call("create_component", map[string]any{"name": "bad-name"}))[0])
	if !strings.Contains(text, "invalid component name") {
		t.Fatalf("expected name rejection, got %s", text)
	}
}

func TestGlobMatchPolicy(t *testing.T) {
	cases := []struct {
		pat, path string
		want      bool
	}{
		{".env", "config/.env", true},
		{".env.*", ".env.local", true},
		{"src/**", "src/a/b/c.tsx", true},
		{"src/**", "other/a.tsx", false},
		{".git", ".git/config", true},
		{".git/**", ".git/refs/heads/main", true},
		{"*.pem", "certs/server.pem", true},
		{"*.pem", "certs/server.pem.bak", false},
	}
	for _, c := range cases {
		if got := globMatch(c.pat, c.path); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pat, c.path, got, c.want)
		}
	}
}
