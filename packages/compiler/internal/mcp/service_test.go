package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// newTestService builds a Service over a temp project with one page.
func newTestService(t *testing.T, files map[string]string) *Service {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		writeFile(t, root, rel, content)
	}
	cfg := config.Default()
	cfg.Resolve(root)
	cfg.Minify = false
	return NewService(Options{Root: root, Cfg: cfg})
}

// serve drives the server with newline-delimited JSON requests and returns the
// decoded responses.
func serve(t *testing.T, svc *Service, requests ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	srv := NewServer(&out, "krate", "test")
	svc.Register(srv)

	input := strings.Join(requests, "\n") + "\n"
	if err := srv.Serve(strings.NewReader(input)); err != nil {
		t.Fatalf("serve: %v", err)
	}

	var responses []map[string]any
	dec := json.NewDecoder(&out)
	for {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			break
		}
		responses = append(responses, m)
	}
	return responses
}

func call(name string, args map[string]any) string {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	})
	return string(b)
}

func toolText(t *testing.T, resp map[string]any) string {
	t.Helper()
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in response: %v", resp)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("no content in result: %v", result)
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

func TestEditPageRejectsTraversal(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	text := toolText(t, serve(t, svc, call("edit_file", map[string]any{
		"route": "../../../evil.tsx", "content": "export default function Page() { return null; }\n",
	}))[0])
	if !strings.Contains(text, "invalid path") {
		t.Fatalf("expected traversal rejection, got %s", text)
	}
}

// TestEditPageRejectsSymlinkEscape verifies a symlinked directory inside the
// project pointing outside it cannot be used to write outside the root.
func TestEditPageRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is privileged on Windows CI")
	}
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	outside := t.TempDir()
	link := filepath.Join(svc.root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	text := toolText(t, serve(t, svc, call("edit_file", map[string]any{
		"route": "link/evil.tsx", "content": "export default function Page() { return null; }\n", "apply": true,
	}))[0])
	if !strings.Contains(text, "invalid path") && !strings.Contains(text, "escapes") {
		t.Fatalf("expected symlink escape rejection, got %s", text)
	}
	if _, err := os.Stat(filepath.Join(outside, "evil.tsx")); err == nil {
		t.Fatalf("symlink escape wrote outside the root")
	}
}

func TestApplyPatchDryRunApplyUndo(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Old</h1>; }\n",
	})
	patch := `--- a/src/pages/index.tsx
+++ b/src/pages/index.tsx
@@ -1 +1 @@
-export default function Page() { return <h1>Old</h1>; }
+export default function Page() { return <h1>New</h1>; }
--- /dev/null
+++ b/src/pages/about.tsx
@@ -0,0 +1 @@
+export default function About() { return <h1>About</h1>; }
`
	resps := serve(t, svc, call("apply_patch", map[string]any{"patch": patch}))
	if text := toolText(t, resps[0]); !strings.Contains(text, "+export default function Page()") {
		t.Fatalf("dry-run missing diff: %s", text)
	}
	data, _ := os.ReadFile(filepath.Join(svc.root, "src", "pages", "index.tsx"))
	if !strings.Contains(string(data), "Old") {
		t.Fatal("dry-run modified the source")
	}
	resps = serve(t, svc, call("apply_patch", map[string]any{"patch": patch, "apply": true}))
	if text := toolText(t, resps[0]); !strings.Contains(text, "Applied apply patch") {
		t.Fatalf("apply failed: %s", text)
	}
	data, _ = os.ReadFile(filepath.Join(svc.root, "src", "pages", "index.tsx"))
	if !strings.Contains(string(data), "New") {
		t.Fatal("patch did not apply")
	}
	if _, err := os.Stat(filepath.Join(svc.root, "src", "pages", "about.tsx")); err != nil {
		t.Fatal("created file missing")
	}
	resps = serve(t, svc, call("undo", map[string]any{}))
	if text := toolText(t, resps[0]); !strings.Contains(text, "Undid transaction") {
		t.Fatalf("undo failed: %s", text)
	}
	data, _ = os.ReadFile(filepath.Join(svc.root, "src", "pages", "index.tsx"))
	if !strings.Contains(string(data), "Old") {
		t.Fatal("undo did not restore the source")
	}
	if _, err := os.Stat(filepath.Join(svc.root, "src", "pages", "about.tsx")); !os.IsNotExist(err) {
		t.Fatal("undo did not remove the created file")
	}
}

func TestRunTaskAllowlist(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }\n",
	})
	svc.policy.AllowTasks = []string{"build"}
	text := toolText(t, serve(t, svc, call("run_task", map[string]any{"task": "test"}))[0])
	if !strings.Contains(text, "allowTasks") {
		t.Fatalf("expected allowlist refusal, got: %s", text)
	}
}

func TestReadOnlyModeBlocksWritesAndTasks(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }\n",
	})
	svc.policy.ReadOnlyMode = true
	text := toolText(t, serve(t, svc, call("edit_file", map[string]any{
		"route": "src/pages/index.tsx", "find": "Hi", "replace": "Hello", "apply": true,
	}))[0])
	if !strings.Contains(text, "readOnlyMode") {
		t.Fatalf("expected read-only write refusal, got: %s", text)
	}
	text = toolText(t, serve(t, svc, call("run_task", map[string]any{"task": "build"}))[0])
	if !strings.Contains(text, "readOnlyMode") {
		t.Fatalf("expected read-only task refusal, got: %s", text)
	}
}

func TestAuditLogAndSecretRedaction(t *testing.T) {
	const secret = "super-secret-token-value"
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>" + secret + "</h1>; }\n",
	})
	svc.secretValues = []string{secret}

	text := toolText(t, serve(t, svc, call("read_file", map[string]any{"path": "src/pages/index.tsx"}))[0])
	if strings.Contains(text, secret) {
		t.Fatalf("tool output leaked a secret: %s", text)
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("tool output was not redacted: %s", text)
	}

	auditPath := filepath.Join(svc.root, filepath.FromSlash(svc.auditPath))
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("reading audit log: %v", err)
	}
	log := string(data)
	if !strings.Contains(log, `"event":"tool_call"`) || !strings.Contains(log, `"tool":"read_file"`) {
		t.Fatalf("audit log missing tool call: %s", log)
	}
	if strings.Contains(log, secret) {
		t.Fatalf("audit log leaked a secret: %s", log)
	}
}
