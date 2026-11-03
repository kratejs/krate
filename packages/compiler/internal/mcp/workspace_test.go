package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestWorkspaceSymbolsAndSemanticReferences(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/pages/index.tsx": `// Greeting is mentioned in a comment
const label = "Greeting";
export function Greeting() { return <h1>Hello</h1>; }
export default function Page() { return <Greeting />; }
`,
	})
	text := toolText(t, serve(t, svc, call("workspace_symbols", map[string]any{"query": "Greet"}))[0])
	if !strings.Contains(text, `"name": "Greeting"`) || !strings.Contains(text, `"kind": "function"`) {
		t.Fatalf("workspace_symbols missed the function: %s", text)
	}
	if !strings.Contains(text, `"parsed": 1`) {
		t.Fatalf("first index pass did not parse the file: %s", text)
	}
	text = toolText(t, serve(t, svc, call("find_references", map[string]any{"symbol": "Greeting"}))[0])
	var resp struct {
		References []projectMatch `json:"references"`
		Count      int            `json:"count"`
		Index      struct {
			Files  int `json:"files"`
			Parsed int `json:"parsed"`
		} `json:"index"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatal(err)
	}
	// The comment and string literal are not references; the declaration and
	// JSX usage are.
	if resp.Count != 2 {
		t.Fatalf("semantic references = %+v", resp)
	}
	if resp.Index.Files != 1 {
		t.Fatalf("index stats = %+v", resp.Index)
	}
	// A second query reuses the unchanged file instead of reparsing it.
	text = toolText(t, serve(t, svc, call("workspace_symbols", map[string]any{"query": "Page"}))[0])
	if !strings.Contains(text, `"reused": 1`) {
		t.Fatalf("incremental index did not reuse the file: %s", text)
	}
	writeFile(t, svc.root, "src/pages/extra.tsx", "export const Extra = 1;\n")
	text = toolText(t, serve(t, svc, call("workspace_symbols", map[string]any{"query": "Extra"}))[0])
	if !strings.Contains(text, `"parsed": 1`) || !strings.Contains(text, `"reused": 1`) || !strings.Contains(text, `"name": "Extra"`) {
		t.Fatalf("incremental index did not parse only the new file: %s", text)
	}
}

func TestMultiRootSearchReadAndSymbols(t *testing.T) {
	root := t.TempDir()
	lib := t.TempDir()
	writeFile(t, root, "src/pages/index.tsx", "export function App() { return <h1>App</h1>; }\n")
	writeFile(t, lib, "lib/util.ts", "export function SharedHelper() { return 1; }\nexport const other = SharedHelper;\n")
	cfg := config.Default()
	cfg.Resolve(root)
	svc := NewService(Options{Root: root, Cfg: cfg, ExtraRoots: []WorkspaceRoot{{Name: "lib", Path: lib}}})

	text := toolText(t, serve(t, svc, call("workspace_symbols", map[string]any{"query": "Shared"}))[0])
	if !strings.Contains(text, `"path": "lib:lib/util.ts"`) || !strings.Contains(text, `"root": "lib"`) {
		t.Fatalf("multi-root symbol path mismatch: %s", text)
	}
	text = toolText(t, serve(t, svc, call("search_project", map[string]any{"query": "SharedHelper", "root": "lib"}))[0])
	if !strings.Contains(text, "lib:lib/util.ts") {
		t.Fatalf("multi-root search missed lib: %s", text)
	}
	text = toolText(t, serve(t, svc, call("read_file", map[string]any{"path": "lib:lib/util.ts"}))[0])
	if !strings.Contains(text, "SharedHelper") || !strings.Contains(text, `"root": "lib"`) {
		t.Fatalf("multi-root read failed: %s", text)
	}
	text = toolText(t, serve(t, svc, call("list_files", map[string]any{"root": "lib"}))[0])
	if !strings.Contains(text, "lib:lib/util.ts") {
		t.Fatalf("multi-root list failed: %s", text)
	}
}
