package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	text = toolText(t, serve(t, svc, call("edit_file", map[string]any{
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

func TestPolicyDenyWins(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
	})
	writeFile(t, svc.root, ".krate/mcp.json", `{"allow": ["src/**"], "deny": ["src/generated/**"]}`)
	svc.policy = loadPolicy(svc.root)
	writeFile(t, svc.root, "src/generated/gen.ts", "export const x = 1;\n")

	text := toolText(t, serve(t, svc, call("edit_file", map[string]any{
		"route": "src/generated/gen.ts", "content": "export const x = 2;\n", "apply": true,
	}))[0])
	if !strings.Contains(text, "refusing") {
		t.Fatalf("deny rule should win over allow, got %s", text)
	}
}
