package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/kratedocs"
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

func TestInitializeAndToolsList(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	resps := serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(resps))
	}
	if resps[0]["result"] == nil {
		t.Fatalf("initialize failed: %v", resps[0])
	}
	result := resps[1]["result"].(map[string]any)
	tools := result["tools"].([]any)
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"list_routes", "read_page", "read_content", "create_content", "edit_content", "create_page", "edit_file", "build", "check", "search_docs", "list_files", "read_file", "create_file", "delete_file", "move_file", "create_component", "apply_patch", "batch_edit", "undo", "format_file", "search_project", "read_files", "get_diagnostics", "find_references", "impact", "list_changes", "get_history", "apply_fix", "run_task"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestListRoutes(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":      "export default function Page() { return <h1>Hi</h1>; }",
		"src/pages/about.tsx":      "export default function Page() { return <h1>About</h1>; }",
		"src/pages/video/[id].tsx": "export default function Page() { return <h1>Video</h1>; }",
	})
	resps := serve(t, svc, call("list_routes", nil))
	text := toolText(t, resps[0])
	if !strings.Contains(text, `"/about"`) {
		t.Errorf("expected /about route, got %s", text)
	}
	if !strings.Contains(text, `"id"`) {
		t.Errorf("expected dynamic param id, got %s", text)
	}
}

func TestReadPage(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/about.tsx": "export default function About() { return <h1>About</h1>; }",
	})
	// The default format is the raw source (no AST round-trip, no absolute path).
	resps := serve(t, svc, call("read_page", map[string]any{"route": "/about"}))
	text := toolText(t, resps[0])
	if !strings.Contains(text, `"content": "export default function About()`) {
		t.Errorf("expected source content, got %s", text)
	}
	if strings.Contains(text, `"kind": "Program"`) {
		t.Errorf("source format should not include the AST, got %s", text)
	}
	if strings.Contains(text, "sourcePath") {
		t.Errorf("sourcePath must not leak across the bridge: %s", text)
	}

	// format "all" returns source, AST, and lossyTypes.
	resps = serve(t, svc, call("read_page", map[string]any{"route": "/about", "format": "all"}))
	text = toolText(t, resps[0])
	if !strings.Contains(text, `"lossyTypes": false`) {
		t.Errorf("expected lossyTypes false, got %s", text)
	}
	if !strings.Contains(text, `"kind": "Program"`) {
		t.Errorf("expected AST document, got %s", text)
	}
	if !strings.Contains(text, `"content":`) {
		t.Errorf("expected source content in all format, got %s", text)
	}

	// An invalid format is rejected.
	text = toolText(t, serve(t, svc, call("read_page", map[string]any{"route": "/about", "format": "bogus"}))[0])
	if !strings.Contains(text, "format must be one of") {
		t.Errorf("expected format validation, got %s", text)
	}
}

func TestCreatePageDryRunThenApply(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})

	// Dry-run: no file written.
	resps := serve(t, svc, call("create_page", map[string]any{"route": "/pricing"}))
	if !strings.Contains(toolText(t, resps[0]), "+++") {
		t.Errorf("expected a diff, got %s", toolText(t, resps[0]))
	}
	if _, err := os.Stat(filepath.Join(svc.root, "src", "pages", "pricing.tsx")); !os.IsNotExist(err) {
		t.Fatal("dry-run must not create the file")
	}

	// Apply.
	resps = serve(t, svc, call("create_page", map[string]any{"route": "/pricing", "apply": true}))
	if _, err := os.Stat(filepath.Join(svc.root, "src", "pages", "pricing.tsx")); err != nil {
		t.Fatalf("apply should create the file: %v", err)
	}
	_ = resps
}

func TestResourceRoutesAndPage(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/about.tsx": "export default function About() { return <h1>About</h1>; }",
	})
	resps := serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"krate://routes"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"krate://page/about"}}`,
	)
	if len(resps) != 3 {
		t.Fatalf("expected 3 responses, got %d", len(resps))
	}
	list := resps[0]["result"].(map[string]any)["resources"].([]any)
	if len(list) == 0 {
		t.Fatal("expected registered resources")
	}
	routes := resps[1]["result"].(map[string]any)["contents"].([]any)
	if len(routes) != 1 {
		t.Fatalf("expected routes contents, got %v", routes)
	}
	page := resps[2]["result"].(map[string]any)["contents"].([]any)
	pageText := page[0].(map[string]any)["text"].(string)
	if !strings.Contains(pageText, `"source": "src/pages/about.tsx"`) {
		t.Fatalf("expected about page resource, got %s", pageText)
	}
}

func TestUnknownMethodAndTool(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	resps := serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"does/not/exist"}`,
		call("nope", nil),
	)
	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(resps))
	}
	for i, r := range resps {
		if r["error"] == nil {
			t.Errorf("response %d should be an error: %v", i, r)
		}
	}
}

func TestNotificationNoResponse(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	resps := serve(t, svc, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if len(resps) != 0 {
		t.Fatalf("notifications must not produce a response, got %v", resps)
	}
}

func TestUnifiedDiffNewFile(t *testing.T) {
	d := unifiedDiff("pricing.tsx", "", "a\nb\n")
	if !strings.Contains(d, "--- /dev/null") || !strings.Contains(d, "+a") || !strings.Contains(d, "+b") {
		t.Fatalf("unexpected diff: %s", d)
	}
}

func mustResp(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	if resp["error"] != nil {
		t.Fatalf("unexpected error response: %v", resp)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in response: %v", resp)
	}
	return result
}

func TestToolAnnotations(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	resp := mustResp(t, serve(t, svc, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)[0])
	want := map[string]bool{
		"list_routes": false, "read_page": false, "read_content": false, "search_docs": false,
		"list_files": false, "read_file": false,
		"create_page": false, "edit_file": true, "apply_patch": true, "batch_edit": true, "undo": true, "format_file": true, "apply_fix": true,
		"create_file": false, "delete_file": true, "move_file": true, "create_component": false,
		"create_content": false, "edit_content": true,
		"build": false, "check": false, "run_task": false,
	}
	for _, tool := range resp["tools"].([]any) {
		m := tool.(map[string]any)
		name := m["name"].(string)
		anns, ok := m["annotations"].(map[string]any)
		if !ok {
			t.Errorf("tool %s missing annotations", name)
			continue
		}
		destructive, _ := anns["destructiveHint"].(bool)
		if destructive != want[name] {
			t.Errorf("tool %s destructiveHint = %v, want %v", name, destructive, want[name])
		}
	}
}

func TestVersionNegotiation(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	resp := mustResp(t, serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
	)[0])
	if got := resp["protocolVersion"]; got != "2024-11-05" {
		t.Errorf("negotiated protocolVersion = %v, want 2024-11-05", got)
	}
	if _, ok := resp["capabilities"].(map[string]any); !ok {
		t.Errorf("missing capabilities: %v", resp)
	}
}

func TestResourceTemplatesAndCompletions(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/about.tsx": "export default function Page() { return <h1>About</h1>; }",
	})
	resps := serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"resources/templates/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"completions/complete","params":{"ref":{"type":"ref/resource","uri":"krate://page/{route}"},"argument":{"name":"route","value":""}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"krate://page/about"}}`,
	)
	tmpls := mustResp(t, resps[0])["resourceTemplates"].([]any)
	if len(tmpls) == 0 {
		t.Fatal("expected registered resource templates")
	}
	first := tmpls[0].(map[string]any)
	if first["uriTemplate"] == nil {
		t.Errorf("template missing uriTemplate: %v", first)
	}

	completion := mustResp(t, resps[1])
	values := completion["completion"].(map[string]any)["values"].([]any)
	found := false
	for _, v := range values {
		if v.(string) == "/about" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected /about in route completions, got %v", values)
	}

	page := mustResp(t, resps[2])["contents"].([]any)
	if !strings.Contains(page[0].(map[string]any)["text"].(string), `"source": "src/pages/about.tsx"`) {
		t.Errorf("unexpected page resource: %v", page)
	}
}

func TestPromptsListAndGet(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	resps := serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"add-page","arguments":{"route":"/pricing"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"completions/complete","params":{"ref":{"type":"ref/prompt","name":"add-page"},"argument":{"name":"template","value":""}}}`,
	)
	list := mustResp(t, resps[0])["prompts"].([]any)
	names := map[string]bool{}
	for _, p := range list {
		names[p.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"add-page", "publish-content", "fix-checks", "explore"} {
		if !names[want] {
			t.Errorf("missing prompt %q", want)
		}
	}

	message := mustResp(t, resps[1])["messages"].([]any)
	text := message[0].(map[string]any)["content"].(map[string]any)["text"].(string)
	if !strings.Contains(text, "create_page") || !strings.Contains(text, "/pricing") {
		t.Errorf("unexpected add-page message: %s", text)
	}

	completion := mustResp(t, resps[2])
	values := completion["completion"].(map[string]any)["values"].([]any)
	if strings.Contains(values[0].(string), "static") {
		t.Logf("template completions: %v", values)
	}
}

func TestEditPageFindReplaceDryRunAndApply(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/about.tsx": "export default function Page() { return <h1>About</h1>; }",
	})

	// Dry-run: no file written.
	text := toolText(t, serve(t, svc, call("edit_file", map[string]any{
		"route": "/about", "find": "About", "replace": "Company",
	}))[0])
	if !strings.Contains(text, "+++") || !strings.Contains(text, "Dry run") {
		t.Fatalf("expected dry-run diff, got %s", text)
	}
	about, _ := os.ReadFile(filepath.Join(svc.root, "src", "pages", "about.tsx"))
	if !strings.Contains(string(about), "About") {
		t.Fatal("dry-run must not modify the file")
	}

	// Apply writes the change.
	text = toolText(t, serve(t, svc, call("edit_file", map[string]any{
		"route": "/about", "find": "About", "replace": "Company", "apply": true,
	}))[0])
	if !strings.Contains(text, "Updated") || !strings.Contains(text, "+++") {
		t.Fatalf("expected update diff, got %s", text)
	}
	about, _ = os.ReadFile(filepath.Join(svc.root, "src", "pages", "about.tsx"))
	if !strings.Contains(string(about), "Company") {
		t.Errorf("expected edited about page, got %s", about)
	}
}

func TestEditPageFullReplaceAndNewFile(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
	})
	// Full-file replace via a project-relative path.
	text := toolText(t, serve(t, svc, call("edit_file", map[string]any{
		"route": "src/pages/index.tsx", "content": "export default function Page() { return <h1>New</h1>; }\n",
	}))[0])
	if !strings.Contains(text, "<h1>Hi</h1>") || !strings.Contains(text, "<h1>New</h1>") {
		t.Fatalf("expected replace diff, got %s", text)
	}

	// content on a missing target creates the file (apply).
	text = toolText(t, serve(t, svc, call("edit_file", map[string]any{
		"route": "src/pages/faq.tsx", "content": "export default function Page() { return <h1>FAQ</h1>; }\n", "apply": true,
	}))[0])
	if !strings.Contains(text, "Created") {
		t.Fatalf("expected create message, got %s", text)
	}
	faq, err := os.ReadFile(filepath.Join(svc.root, "src", "pages", "faq.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(faq), "FAQ") {
		t.Errorf("unexpected faq content: %s", faq)
	}
}

func TestEditPageFindReplaceContextRules(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>a</h1>; return <h1>a</h1>; }",
	})
	// Multiple matches require replaceAll.
	text := toolText(t, serve(t, svc, call("edit_file", map[string]any{
		"route": "/", "find": "<h1>a</h1>", "replace": "<h1>b</h1>",
	}))[0])
	if !strings.Contains(text, "replaceAll: true") {
		t.Fatalf("expected replaceAll hint, got %s", text)
	}

	// replaceAll applies everywhere.
	text = toolText(t, serve(t, svc, call("edit_file", map[string]any{
		"route": "/", "find": "<h1>a</h1>", "replace": "<h1>b</h1>", "replaceAll": true, "apply": true,
	}))[0])
	if !strings.Contains(text, "Updated") {
		t.Fatalf("expected update, got %s", text)
	}

	// A non-matching find text errors.
	text = toolText(t, serve(t, svc, call("edit_file", map[string]any{
		"route": "/", "find": "nope", "replace": "x",
	}))[0])
	if !strings.Contains(text, "not found") {
		t.Fatalf("expected not-found message, got %s", text)
	}
}

func TestEditPageRefusesBrokenSource(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/about.tsx": "export default function Page() { return <h1>About</h1>; }",
	})
	// Replacing "function Page()" with "function Page(" yields unparseable code.
	for _, apply := range []bool{false, true} {
		text := toolText(t, serve(t, svc, call("edit_file", map[string]any{
			"route": "/about", "find": "function Page()", "replace": "function Page(", "apply": apply,
		}))[0])
		if !strings.Contains(text, "refusing to write") {
			t.Fatalf("expected parse refusal (apply=%v), got %s", apply, text)
		}
	}
	about, _ := os.ReadFile(filepath.Join(svc.root, "src", "pages", "about.tsx"))
	if !strings.Contains(string(about), "About") {
		t.Errorf("file should be unchanged, got %s", about)
	}
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

func TestEditPagePreservesCRLF(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }\r\n",
	})
	_ = serve(t, svc, call("edit_file", map[string]any{
		"route": "/", "find": "<h1>Hi</h1>", "replace": "<h1>Yo</h1>", "apply": true,
	}))
	data, _ := os.ReadFile(filepath.Join(svc.root, "src", "pages", "index.tsx"))
	if !strings.Contains(string(data), "\r\n") {
		t.Fatalf("expected CRLF preserved, got %q", data)
	}
	if strings.Contains(string(data), "<h1>Hi</h1>") {
		t.Errorf("expected replacement applied, got %q", data)
	}
}

// TestListContentFilesKeepsNestedPaths verifies nested content entries keep
// distinct dir-relative slugs instead of colliding on the bare filename.
func TestListContentFilesKeepsNestedPaths(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "index.md", "---\ntitle: A\n---\n")
	writeFile(t, dir, "guides/index.md", "---\ntitle: B\n---\n")
	files, err := listContentFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(files, ",")
	if !strings.Contains(joined, "index.md") || !strings.Contains(joined, "guides/index.md") {
		t.Fatalf("nested slugs collided or missing: %v", files)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %v", files)
	}
}

func TestReadContentListAndEntry(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":                 "export default function Page() { return <h1>Hi</h1>; }",
		"src/content/blog/hello-world.md":     "---\ntitle: Hello World\n---\n# Hello\n\nBody text.\n",
		"src/content/blog/guides/advanced.md": "---\ntitle: Advanced\n---\nDeep body.\n",
	})
	svc.cfg.Content = map[string]any{"blog": map[string]any{"dir": "src/content/blog"}}

	text := toolText(t, serve(t, svc, call("read_content", map[string]any{"collection": "nope"}))[0])
	if !strings.Contains(text, "no collection") {
		t.Fatalf("expected unknown-collection error, got %s", text)
	}

	listText := toolText(t, serve(t, svc, call("read_content", map[string]any{"collection": "blog"}))[0])
	var listing map[string]any
	if err := json.Unmarshal([]byte(listText), &listing); err != nil {
		t.Fatalf("listing is not JSON: %v\n%s", err, listText)
	}
	if listing["collection"] != "blog" {
		t.Errorf("collection = %v, want blog", listing["collection"])
	}
	entries := listing["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %v", entries)
	}
	first := entries[0].(map[string]any)
	if first["slug"] != "guides/advanced" {
		t.Errorf("first slug = %v, want guides/advanced", first["slug"])
	}
	if _, ok := first["data"].(map[string]any); !ok {
		t.Errorf("expected frontmatter in listing entry: %v", first)
	}

	entryText := toolText(t, serve(t, svc, call("read_content", map[string]any{"collection": "blog", "slug": "hello-world"}))[0])
	var entry map[string]any
	if err := json.Unmarshal([]byte(entryText), &entry); err != nil {
		t.Fatalf("entry is not JSON: %v\n%s", err, entryText)
	}
	if entry["slug"] != "hello-world" {
		t.Errorf("slug = %v, want hello-world", entry["slug"])
	}
	if !strings.Contains(entry["content"].(string), "Body text") {
		t.Errorf("expected raw content, got %v", entry["content"])
	}
	data := entry["data"].(map[string]any)
	if data["title"] != "Hello World" {
		t.Errorf("title = %v, want Hello World", data["title"])
	}
	if !strings.Contains(entry["body"].(string), "Body text") {
		t.Errorf("expected markdown body, got %v", entry["body"])
	}
	if _, ok := entry["html"].(string); !ok {
		t.Errorf("expected rendered html, got %v", entry["html"])
	}
}

func TestCreatePageTemplatesContentEntryAndLayout(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
	})
	svc.cfg.Content = map[string]any{"blog": map[string]any{"dir": "src/content/blog"}}

	resps := serve(t, svc, call("create_page", map[string]any{
		"route":      "/blog",
		"template":   "content-list",
		"collection": "blog",
		"title":      "Blog",
		"contentEntry": map[string]any{
			"slug":  "hello-world",
			"title": "Hello World",
		},
		"withLayout": true,
		"apply":      true,
	}))
	text := toolText(t, resps[0])
	if !strings.Contains(text, "Created") {
		t.Fatalf("expected success, got %s", text)
	}

	page, err := os.ReadFile(filepath.Join(svc.root, "src", "pages", "blog.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `getCollection("blog")`) {
		t.Errorf("expected collection glue in blog.tsx, got %s", page)
	}

	entry, err := os.ReadFile(filepath.Join(svc.root, "src", "content", "blog", "hello-world.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(entry), "slug") && !strings.Contains(string(entry), "title: Hello World") {
		t.Errorf("unexpected entry content: %s", entry)
	}

	if _, err := os.Stat(filepath.Join(svc.root, "src", "pages", "_layout.tsx")); err != nil {
		t.Errorf("withLayout should create _layout.tsx: %v", err)
	}
}

func TestConfigResourceShape(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	resp := serve(t, svc, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"krate://config"}}`)
	contents := mustResp(t, resp[0])["contents"].([]any)
	text := contents[0].(map[string]any)["text"].(string)
	if strings.Contains(text, svc.root) {
		t.Errorf("config resource must not leak absolute paths: %s", text)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("config resource is not JSON: %v", err)
	}
	if v["pagesDir"] != "src/pages" {
		t.Errorf("pagesDir = %v, want src/pages", v["pagesDir"])
	}
	if _, isBool := v["minify"].(bool); !isBool {
		t.Errorf("minify should be a bool, got %v", v["minify"])
	}
}

// serveServer drives a pre-configured server (extra tools allowed) and returns
// decoded responses in order.
func TestCancellationQueuedRequest(t *testing.T) {
	var out bytes.Buffer
	srv := NewServer(&out, "krate", "test")
	slow := func(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
		select {
		case <-ctx.Done():
			return ErrorResult("cancelled"), nil
		case <-time.After(300 * time.Millisecond):
			return TextResult("done"), nil
		}
	}
	srv.RegisterTool(Tool{Name: "slow", InputSchema: objSchema(nil), Handler: slow})
	srv.RegisterTool(Tool{Name: "fast", InputSchema: objSchema(nil), Handler: func(ctx context.Context, args map[string]any) (ToolResult, *rpcError) { return TextResult("fast"), nil }})

	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"slow"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":2,"method":"notifications/cancelled","params":{"requestId":2,"reason":"test"}}`,
	}
	if err := srv.Serve(strings.NewReader(strings.Join(requests, "\n") + "\n")); err != nil {
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
	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d: %v", len(responses), responses)
	}
	if responses[0]["result"] == nil {
		t.Errorf("slow request should complete: %v", responses[0])
	}
	errObj, ok := responses[1]["error"].(map[string]any)
	if !ok {
		t.Fatalf("cancelled request should be an error: %v", responses[1])
	}
	if code, _ := errObj["code"].(float64); code != codeRequestCancelled {
		t.Errorf("error code = %v, want %d", code, codeRequestCancelled)
	}
}

func TestReadContentContributedDocsCollection(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":       "export default function Page() { return <h1>Hi</h1>; }",
		"src/content/docs/start.md": "---\ntitle: Start\n---\nDocs body.\n",
	})
	svc.cfg.Plugins = []config.PluginConfig{{Name: "docs"}}

	// The docs plugin contributes the "docs" collection even though it is not
	// declared under content:.
	listText := toolText(t, serve(t, svc, call("read_content", map[string]any{"collection": "docs"}))[0])
	var listing map[string]any
	if err := json.Unmarshal([]byte(listText), &listing); err != nil {
		t.Fatalf("listing is not JSON: %v\n%s", err, listText)
	}
	if listing["collection"] != "docs" {
		t.Errorf("collection = %v, want docs", listing["collection"])
	}
	entries := listing["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected 1 docs entry, got %v", entries)
	}

	entryText := toolText(t, serve(t, svc, call("read_content", map[string]any{"collection": "docs", "slug": "start"}))[0])
	var entry map[string]any
	if err := json.Unmarshal([]byte(entryText), &entry); err != nil {
		t.Fatalf("entry is not JSON: %v\n%s", err, entryText)
	}
	if !strings.Contains(entry["content"].(string), "Docs body") {
		t.Errorf("expected docs content, got %v", entry["content"])
	}
}

func TestCreateContentValidatesSchemaAndWrites(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
	})
	svc.cfg.Content = map[string]any{"blog": map[string]any{
		"dir": "src/content/blog",
		"schema": map[string]any{
			"title": map[string]any{"type": "string", "required": true},
			"order": "number",
			"tags":  "string[]",
		},
	}}

	// Missing required field is refused.
	bad := toolText(t, serve(t, svc, call("create_content", map[string]any{
		"collection": "blog", "slug": "bad", "data": map[string]any{"order": 1},
	}))[0])
	if !strings.Contains(bad, "missing required field") {
		t.Fatalf("expected schema refusal, got %s", bad)
	}

	// Type mismatch is refused.
	badType := toolText(t, serve(t, svc, call("create_content", map[string]any{
		"collection": "blog", "slug": "bad", "data": map[string]any{"title": "T", "order": "nope"},
	}))[0])
	if !strings.Contains(badType, "must be number") {
		t.Fatalf("expected type refusal, got %s", badType)
	}

	// Dry run produces a diff and writes nothing.
	dry := toolText(t, serve(t, svc, call("create_content", map[string]any{
		"collection": "blog", "slug": "hello", "data": map[string]any{"title": "Hello", "order": 2},
	}))[0])
	if !strings.Contains(dry, "Dry run") {
		t.Fatalf("expected dry-run notice, got %s", dry)
	}
	if _, err := os.Stat(filepath.Join(svc.root, "src", "content", "blog", "hello.md")); err == nil {
		t.Fatal("dry run should not write the entry")
	}

	// apply writes frontmatter + body.
	ok := toolText(t, serve(t, svc, call("create_content", map[string]any{
		"collection": "blog", "slug": "hello", "apply": true,
		"data": map[string]any{"title": "Hello", "order": 2, "tags": []any{"a", "b"}},
		"body": "# Hi\n",
	}))[0])
	if !strings.Contains(ok, "Created") {
		t.Fatalf("expected Created, got %s", ok)
	}
	written, err := os.ReadFile(filepath.Join(svc.root, "src", "content", "blog", "hello.md"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(written)
	for _, want := range []string{"title: Hello", "order: 2", "tags: [a, b]", "# Hi"} {
		if !strings.Contains(out, want) {
			t.Errorf("entry missing %q:\n%s", want, out)
		}
	}

	// Refusing an existing entry.
	exists := toolText(t, serve(t, svc, call("create_content", map[string]any{
		"collection": "blog", "slug": "hello", "apply": true, "data": map[string]any{"title": "Again"},
	}))[0])
	if !strings.Contains(exists, "already exists") {
		t.Fatalf("expected already-exists refusal, got %s", exists)
	}

	// Unknown collection message mentions plugins.
	unknown := toolText(t, serve(t, svc, call("create_content", map[string]any{
		"collection": "nope", "slug": "x", "data": map[string]any{"title": "T"},
	}))[0])
	if !strings.Contains(unknown, "no collection") {
		t.Fatalf("expected unknown-collection error, got %s", unknown)
	}
}

func TestCreateContentContributedDocs(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":       "export default function Page() { return <h1>Hi</h1>; }",
		"src/content/docs/start.md": "---\ntitle: Start\n---\nbody",
	})
	svc.cfg.Plugins = []config.PluginConfig{{Name: "docs"}}

	// The docs schema is permissive, so a docs entry can be created.
	ok := toolText(t, serve(t, svc, call("create_content", map[string]any{
		"collection": "docs", "slug": "guides/advanced", "apply": true,
		"data": map[string]any{"title": "Advanced", "tags": []any{"guide"}},
	}))[0])
	if !strings.Contains(ok, "Created") {
		t.Fatalf("expected Created, got %s", ok)
	}
	written, err := os.ReadFile(filepath.Join(svc.root, "src", "content", "docs", "guides", "advanced.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "title: Advanced") {
		t.Errorf("unexpected docs entry: %s", written)
	}
}

func TestEditContentModes(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":             "export default function Page() { return <h1>Hi</h1>; }",
		"src/content/blog/hello-world.md": "---\ntitle: Hello\n---\nOriginal body.\n",
	})
	svc.cfg.Content = map[string]any{"blog": map[string]any{
		"dir":    "src/content/blog",
		"schema": map[string]any{"title": "string"},
	}}

	// Refusing a schema-violating full replace.
	bad := toolText(t, serve(t, svc, call("edit_content", map[string]any{
		"collection": "blog", "slug": "hello-world", "content": "---\ntitle: 123\n---\nnew",
	}))[0])
	if !strings.Contains(bad, "refusing to edit") {
		t.Fatalf("expected schema refusal, got %s", bad)
	}
	if !strings.Contains(bad, "src/content/blog/hello-world.md") {
		t.Errorf("expected project-relative path in refusal message, got %s", bad)
	}

	// Frontmatter rewrite via data keeps the body.
	dry := toolText(t, serve(t, svc, call("edit_content", map[string]any{
		"collection": "blog", "slug": "hello-world",
		"data": map[string]any{"title": "Renamed"},
	}))[0])
	if !strings.Contains(dry, "Dry run") {
		t.Fatalf("expected dry-run, got %s", dry)
	}
	if !strings.Contains(dry, "src/content/blog/hello-world.md") {
		t.Errorf("expected project-relative diff path, got %s", dry)
	}
	changed := toolText(t, serve(t, svc, call("edit_content", map[string]any{
		"collection": "blog", "slug": "hello-world", "apply": true,
		"data": map[string]any{"title": "Renamed"},
	}))[0])
	if !strings.Contains(changed, "Updated") {
		t.Fatalf("expected Updated, got %s", changed)
	}
	written, err := os.ReadFile(filepath.Join(svc.root, "src", "content", "blog", "hello-world.md"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(written)
	if !strings.Contains(out, "title: Renamed") {
		t.Errorf("expected renamed title, got %s", out)
	}
	if !strings.Contains(out, "Original body") {
		t.Errorf("data rewrite should keep the body, got %s", out)
	}

	// Find+replace targeted edit.
	findText := toolText(t, serve(t, svc, call("edit_content", map[string]any{
		"collection": "blog", "slug": "hello-world", "apply": true,
		"find": "Original body", "replace": "Rewritten body",
	}))[0])
	if !strings.Contains(findText, "Updated") {
		t.Fatalf("expected Updated, got %s", findText)
	}
	written2, err := os.ReadFile(filepath.Join(svc.root, "src", "content", "blog", "hello-world.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written2), "Rewritten body") {
		t.Errorf("expected rewritten body, got %s", written2)
	}

	// Refusing missing mode.
	incomplete := toolText(t, serve(t, svc, call("edit_content", map[string]any{
		"collection": "blog", "slug": "hello-world",
	}))[0])
	if !strings.Contains(incomplete, "provide content") {
		t.Fatalf("expected mode error, got %s", incomplete)
	}
}

func TestContentCompletionsUseContributedCollections(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":   "export default function Page() { return <h1>Hi</h1>; }",
		"src/content/docs/a.md": "---\ntitle: A\n---\nbody",
	})
	svc.cfg.Plugins = []config.PluginConfig{{Name: "docs"}}

	resp := mustResp(t, serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"completions/complete","params":{"ref":{"type":"ref/tool","name":"read_content"},"argument":{"name":"collection","value":""}}}`,
	)[0])
	v := resp["completion"].(map[string]any)
	if !strings.Contains(v["values"].([]any)[0].(string), "docs") {
		t.Errorf("expected docs collection completion, got %v", v)
	}

	// With a single effective collection (docs only), slug completion works.
	resp2 := mustResp(t, serve(t, svc,
		`{"jsonrpc":"2.0","id":2,"method":"completions/complete","params":{"ref":{"type":"ref/tool","name":"edit_content"},"argument":{"name":"slug","value":""}}}`,
	)[0])
	v2 := resp2["completion"].(map[string]any)
	vals, _ := v2["values"].([]any)
	found := false
	for _, s := range vals {
		if str, ok := s.(string); ok && str == "a" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected docs slug completion, got %v", v2)
	}
}

func TestSearchDocsSearchesFrameworkDocs(t *testing.T) {
	if kratedocs.Count() == 0 {
		t.Skip("framework docs not synced (run node scripts/sync-krate-docs.mjs)")
	}
	// The project has its own docs content that must NOT be searched.
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":   "export default function Page() { return <h1>Hi</h1>; }",
		"src/content/docs/x.md": "---\ntitle: User Secret Page\n---\nzzyzx-only-in-project\n",
	})

	resp := serve(t, svc, call("search_docs", map[string]any{"query": "content collections"}))[0]
	text := toolText(t, resp)
	var hits []map[string]any
	if err := json.Unmarshal([]byte(text), &hits); err != nil {
		t.Fatalf("search_docs is not JSON: %v\n%s", err, text)
	}
	if len(hits) == 0 {
		t.Fatal("expected framework docs hits")
	}
	foundTyped := false
	for _, h := range hits {
		if h["slug"] == "features/typed-routes" {
			foundTyped = true
			if h["resource"] != "krate://docs/features/typed-routes" {
				t.Errorf("resource = %v, want krate://docs/features/typed-routes", h["resource"])
			}
			if _, hasContent := h["content"]; hasContent {
				t.Error("content field must be absent unless full:true")
			}
		}
		if h["slug"] == "x" {
			t.Errorf("search_docs must not search project docs, got %v", h)
		}
	}
	if !foundTyped {
		t.Errorf("expected features/typed-routes among hits, got %v", hits)
	}

	// full:true adds an uncapped content field; maxChars only sizes excerpt.
	fullResp := serve(t, svc, call("search_docs", map[string]any{
		"query": "content collections", "full": true, "maxChars": 60, "limit": 1,
	}))[0]
	var fullHits []map[string]any
	if err := json.Unmarshal([]byte(toolText(t, fullResp)), &fullHits); err != nil {
		t.Fatalf("full search is not JSON: %v", err)
	}
	if len(fullHits) == 0 {
		t.Fatal("expected at least one full hit")
	}
	fullBody, _ := fullHits[0]["content"].(string)
	if fullBody == "" {
		t.Fatalf("full:true produced no content: %v", fullHits[0])
	}
	excerptText, _ := fullHits[0]["excerpt"].(string)
	if len(fullBody) <= len(excerptText) {
		t.Errorf("full content (%d) should exceed the %d-char-capped excerpt", len(fullBody), 60)
	}
	// The full body is uncapped, so maxChars must not have limited it.
	if len(fullBody) <= 240 {
		t.Errorf("full content suspiciously short (%d); maxChars may be truncating it", len(fullBody))
	}

	// A project-only term yields nothing - proof the search is Krate's docs.
	none := serve(t, svc, call("search_docs", map[string]any{"query": "zzyzx-only-in-project"}))[0]
	if got := toolText(t, none); strings.Contains(got, "zzyzx") {
		t.Errorf("framework search leaked project content: %s", got)
	}
}

func TestExcerptBoundaries(t *testing.T) {
	// Match at the very start: no leading ellipsis, first word preserved.
	got := excerpt("Plugin System is here and more text follows", "plugin", 20)
	if strings.HasPrefix(got, "…") {
		t.Errorf("unexpected leading ellipsis: %q", got)
	}
	if !strings.HasPrefix(got, "Plugin") {
		t.Errorf("first word dropped: %q", got)
	}
	// Entire text fits: no ellipses at all.
	full := excerpt("Short body", "short", 100)
	if strings.Contains(full, "…") {
		t.Errorf("full-window excerpt should have no ellipsis: %q", full)
	}
	// Match in the middle: ellipses on both sides.
	mid := excerpt(strings.Repeat("word ", 40)+"needle "+strings.Repeat("word ", 40), "needle", 40)
	if !strings.HasPrefix(mid, "…") || !strings.HasSuffix(mid, "…") {
		t.Errorf("expected both ellipses, got %q", mid)
	}
}

func TestReadDocsResource(t *testing.T) {
	if kratedocs.Count() == 0 {
		t.Skip("framework docs not synced (run node scripts/sync-krate-docs.mjs)")
	}
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }",
	})

	resp := mustResp(t, serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"krate://docs/features/mcp"}}`,
	)[0])
	contents := resp["contents"].([]any)
	first := contents[0].(map[string]any)
	if first["mimeType"] != "text/markdown" {
		t.Errorf("mimeType = %v, want text/markdown", first["mimeType"])
	}
	body := first["text"].(string)
	if !strings.Contains(body, "# MCP Server") {
		t.Errorf("expected MCP doc body, got %s", body)
	}

	// Unknown slug is an error, not a panic.
	bad := serve(t, svc,
		`{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"krate://docs/does/not/exist"}}`,
	)[0]
	if bad["error"] == nil {
		t.Fatalf("expected error for unknown doc slug, got %v", bad)
	}

	// The docs template also offers slug completions.
	comp := mustResp(t, serve(t, svc,
		`{"jsonrpc":"2.0","id":3,"method":"completions/complete","params":{"ref":{"type":"ref/resource","uri":"krate://docs/{slug}"},"argument":{"name":"slug","value":""}}}`,
	)[0])
	values, _ := comp["completion"].(map[string]any)["values"].([]any)
	hasMCP := false
	for _, v := range values {
		if s, _ := v.(string); s == "features/mcp" {
			hasMCP = true
		}
	}
	if !hasMCP {
		t.Errorf("expected features/mcp among doc slug completions, got %v", values)
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

func TestBatchEditAtomicAndValidation(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Old</h1>; }\n",
	})
	edits := []any{
		map[string]any{"path": "src/pages/index.tsx", "find": "Old", "replace": "New"},
		map[string]any{"path": "src/pages/about.tsx", "content": "export default function About() { return <h1>About</h1>; }\n"},
	}
	resps := serve(t, svc, call("batch_edit", map[string]any{"edits": edits, "apply": true}))
	if text := toolText(t, resps[0]); !strings.Contains(text, "Applied batch edit") {
		t.Fatalf("batch apply failed: %s", text)
	}
	if _, err := os.Stat(filepath.Join(svc.root, "src", "pages", "about.tsx")); err != nil {
		t.Fatal("batch create missing")
	}
	// A bad second operation must leave the first file unchanged.
	before, _ := os.ReadFile(filepath.Join(svc.root, "src", "pages", "index.tsx"))
	bad := []any{
		map[string]any{"path": "src/pages/index.tsx", "find": "New", "replace": "Broken"},
		map[string]any{"path": "src/pages/bad.tsx", "content": "<"},
	}
	resps = serve(t, svc, call("batch_edit", map[string]any{"edits": bad, "apply": true}))
	if text := toolText(t, resps[0]); !strings.Contains(text, "would not parse") {
		t.Fatalf("expected parse rejection, got: %s", text)
	}
	after, _ := os.ReadFile(filepath.Join(svc.root, "src", "pages", "index.tsx"))
	if string(before) != string(after) {
		t.Fatal("failed batch partially modified the first file")
	}
}

func TestWriteRoots(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }\n",
		"public/site.css":     "body { color: red; }\n",
	})
	svc.policy.WriteRoots = []string{"src"}
	resps := serve(t, svc, call("edit_file", map[string]any{
		"route": "public/site.css", "find": "red", "replace": "blue", "apply": true,
	}))
	if text := toolText(t, resps[0]); !strings.Contains(text, "refusing to edit") {
		t.Fatalf("writeRoots did not reject public/site.css: %s", text)
	}
	resps = serve(t, svc, call("edit_file", map[string]any{
		"route": "src/pages/index.tsx", "find": "Hi", "replace": "Hello", "apply": true,
	}))
	if text := toolText(t, resps[0]); strings.Contains(text, "refusing") {
		t.Fatalf("writeRoots rejected an allowed path: %s", text)
	}
}

func TestEditPageExpectedHash(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }\n",
	})
	text := toolText(t, serve(t, svc, call("edit_file", map[string]any{
		"route": "src/pages/index.tsx", "find": "Hi", "replace": "Hello",
		"expectedSha256": "not-the-current-hash", "apply": true,
	}))[0])
	if !strings.Contains(text, "changed since it was read") {
		t.Fatalf("expected stale hash refusal, got: %s", text)
	}
	text = toolText(t, serve(t, svc, call("edit_file", map[string]any{
		"route": "src/pages/index.tsx", "find": "Hi", "replace": "Hello",
		"expectedSha256": hashString("export default function Page() { return <h1>Hi</h1>; }\n"), "apply": true,
	}))[0])
	if strings.Contains(text, "changed since it was read") {
		t.Fatalf("matching hash was rejected: %s", text)
	}
}

func TestSearchProjectReadFilesAndReferences(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export function Greeting() { return <h1>Hello</h1>; }\nconst unused = 1;\n",
		"src/pages/about.tsx": "export default function About() { return <h1>About</h1>; }\n",
	})
	text := toolText(t, serve(t, svc, call("search_project", map[string]any{
		"query": "Greeting", "glob": "src/**/*.tsx",
	}))[0])
	if !strings.Contains(text, "src/pages/index.tsx") || !strings.Contains(text, "\"line\": 1") {
		t.Fatalf("search_project did not find the symbol: %s", text)
	}
	text = toolText(t, serve(t, svc, call("find_references", map[string]any{"symbol": "Greeting"}))[0])
	if !strings.Contains(text, "\"count\": 1") {
		t.Fatalf("find_references count mismatch: %s", text)
	}
	text = toolText(t, serve(t, svc, call("read_files", map[string]any{
		"paths": []any{"src/pages/index.tsx", "src/pages/about.tsx"},
	}))[0])
	if !strings.Contains(text, "Hello") || !strings.Contains(text, "About") {
		t.Fatalf("read_files missing content: %s", text)
	}
}

func TestGetDiagnosticsStructured(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>; }\n",
	})
	text := toolText(t, serve(t, svc, call("get_diagnostics", map[string]any{"check": false}))[0])
	if !strings.Contains(text, "\"diagnostics\"") || !strings.Contains(text, "compile") {
		t.Fatalf("expected structured compile diagnostics, got: %s", text)
	}
}

func TestListChangesAndApplyFix(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Old</h1>; }\n",
	})
	text := toolText(t, serve(t, svc, call("apply_fix", map[string]any{
		"path": "src/pages/index.tsx", "find": "Old", "replace": "New", "apply": true,
	}))[0])
	if !strings.Contains(text, "Updated") {
		t.Fatalf("apply_fix failed: %s", text)
	}
	text = toolText(t, serve(t, svc, call("list_changes", map[string]any{}))[0])
	if !strings.Contains(text, "src/pages/index.tsx") {
		t.Fatalf("list_changes missing transaction: %s", text)
	}
}

func TestProgressNotifications(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }\n",
	})
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_project","arguments":{"query":"Page"},"_meta":{"progressToken":"p1"}}}`
	resps := serve(t, svc, req)
	found := false
	for _, r := range resps {
		if r["method"] == "notifications/progress" {
			found = true
			params, _ := r["params"].(map[string]any)
			if params["progressToken"] != "p1" {
				t.Errorf("progress token = %v, want p1", params["progressToken"])
			}
		}
	}
	if !found {
		t.Fatalf("no progress notification in %v", resps)
	}
}

func TestResourceSubscriptions(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }\n",
	})
	resps := serve(t, svc,
		`{"jsonrpc":"2.0","id":1,"method":"resources/subscribe","params":{"uri":"krate://manifest"}}`,
		call("edit_file", map[string]any{"route": "/", "find": "Hi", "replace": "Hello", "apply": true}),
	)
	found := false
	for _, r := range resps {
		if r["method"] == "notifications/resources/updated" {
			params, _ := r["params"].(map[string]any)
			if params["uri"] == "krate://manifest" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("no resource update notification in %v", resps)
	}
}

func TestPaginationCursors(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":   "export default function Page() { return <h1>Hi</h1>; }\n",
		"src/pages/about.tsx":   "export default function About() { return <h1>About</h1>; }\n",
		"src/pages/contact.tsx": "export default function Contact() { return <h1>Contact</h1>; }\n",
	})
	var first map[string]any
	if err := json.Unmarshal([]byte(toolText(t, serve(t, svc, call("list_files", map[string]any{"glob": "src/pages/*.tsx", "limit": 1}))[0])), &first); err != nil {
		t.Fatal(err)
	}
	next, _ := first["nextCursor"].(string)
	if next == "" {
		t.Fatalf("expected nextCursor, got %v", first)
	}
	var second map[string]any
	if err := json.Unmarshal([]byte(toolText(t, serve(t, svc, call("list_files", map[string]any{"glob": "src/pages/*.tsx", "limit": 1, "cursor": next}))[0])), &second); err != nil {
		t.Fatal(err)
	}
	if second["nextCursor"] == next {
		t.Fatal("cursor did not advance")
	}
	var search map[string]any
	if err := json.Unmarshal([]byte(toolText(t, serve(t, svc, call("search_project", map[string]any{"query": "function", "limit": 1}))[0])), &search); err != nil {
		t.Fatal(err)
	}
	if search["nextCursor"] == "" {
		t.Fatalf("expected search nextCursor, got %v", search)
	}
}

func TestProtocolVersionMatrix(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }\n",
	})
	for _, version := range mcpVersions {
		req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + version + `"}}`
		resp := mustResp(t, serve(t, svc, req)[0])
		if resp["protocolVersion"] != version {
			t.Errorf("version %s negotiated as %v", version, resp["protocolVersion"])
		}
	}
}

func TestEditAnyProjectFile(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx":       "export default function Page() { return <h1>Hi</h1>; }\n",
		"src/api/hello.ts":          "export function GET() { return new Response('ok'); }\n",
		"src/styles/site.css":       "body { color: red; }\n",
		"src/content/docs/guide.md": "---\ntitle: Guide\n---\nOld body.\n",
		"docs/README.md":            "Old docs.\n",
	})
	cases := []struct {
		path, find, replace string
	}{
		{"src/api/hello.ts", "ok", "fine"},
		{"src/styles/site.css", "red", "blue"},
		{"src/content/docs/guide.md", "Old body", "New body"},
		{"docs/README.md", "Old docs", "New docs"},
	}
	for _, c := range cases {
		text := toolText(t, serve(t, svc, call("edit_file", map[string]any{
			"route": c.path, "find": c.find, "replace": c.replace, "apply": true,
		}))[0])
		if strings.Contains(text, "refusing") || strings.Contains(text, "outside writeRoots") {
			t.Fatalf("edit_file refused %s: %s", c.path, text)
		}
		data, err := os.ReadFile(filepath.Join(svc.root, filepath.FromSlash(c.path)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), c.replace) {
			t.Fatalf("%s was not updated: %s", c.path, data)
		}
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

func TestNewWorkflowPromptsListed(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }"})
	resp := mustResp(t, serve(t, svc, `{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`)[0])
	list, _ := resp["prompts"].([]any)
	names := map[string]bool{}
	for _, p := range list {
		names[p.(map[string]any)["name"].(string)] = true
	}
	for _, name := range []string{"repair-diagnostics", "refactor", "audit-dependencies", "scaffold-component"} {
		if !names[name] {
			t.Errorf("missing prompt %s", name)
		}
	}
}

func TestUndoConflictAndForce(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Old</h1>; }\n",
	})
	_ = serve(t, svc, call("edit_file", map[string]any{
		"route": "src/pages/index.tsx", "find": "Old", "replace": "New", "apply": true,
	}))
	path := filepath.Join(svc.root, "src", "pages", "index.tsx")
	_ = os.WriteFile(path, []byte("manual edit\n"), 0644)
	text := toolText(t, serve(t, svc, call("undo", map[string]any{}))[0])
	if !strings.Contains(text, "changed after the transaction") {
		t.Fatalf("expected undo conflict, got: %s", text)
	}
	text = toolText(t, serve(t, svc, call("undo", map[string]any{"force": true}))[0])
	if !strings.Contains(text, "Undid transaction") {
		t.Fatalf("forced undo failed: %s", text)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Old") {
		t.Fatalf("forced undo did not restore original: %s", data)
	}
}

func TestTransactionLimits(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": "export default function Page() { return <h1>Hi</h1>; }\n",
	})
	svc.policy.MaxBatchFiles = 1
	edits := []any{
		map[string]any{"path": "src/pages/index.tsx", "find": "Hi", "replace": "Hello"},
		map[string]any{"path": "src/pages/other.tsx", "content": "export default function Other() { return null; }\n"},
	}
	text := toolText(t, serve(t, svc, call("batch_edit", map[string]any{"edits": edits, "apply": true}))[0])
	if !strings.Contains(text, "limit is 1") {
		t.Fatalf("expected batch limit error, got: %s", text)
	}
	svc.policy.MaxFileBytes = 8
	text = toolText(t, serve(t, svc, call("edit_file", map[string]any{
		"route": "src/pages/index.tsx", "find": "Hi", "replace": "A much longer replacement", "apply": true,
	}))[0])
	if !strings.Contains(text, "byte limit") {
		t.Fatalf("expected file size error, got: %s", text)
	}
}
