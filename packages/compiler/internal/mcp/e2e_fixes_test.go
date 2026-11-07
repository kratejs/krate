package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readRoot(t *testing.T, svc *Service, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(svc.root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeRoot(t *testing.T, svc *Service, rel, text string) {
	t.Helper()
	p := filepath.Join(svc.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o755); err != nil {
		t.Fatal(err)
	}
}

func callText(t *testing.T, svc *Service, name string, args map[string]any) (string, bool) {
	t.Helper()
	_, res := invoke(t, svc, name, args)
	var b strings.Builder
	for _, c := range res.Content {
		b.WriteString(c.Text)
	}
	return b.String(), res.IsError
}

func TestMoveFileUpdatesImportersAndItsOwnImports(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/lib/util.ts":     "import { z } from \"./z\";\nexport const x = z;\n",
		"src/lib/z.ts":        "export const z = 1;\n",
		"src/pages/index.tsx": "import { x } from \"../lib/util\";\nexport default function P() { return <p>{x}</p>; }\n",
		"src/pages/lazy.tsx":  "const a = () => import(\"../lib/util.ts\");\nexport default function L() { return <p>a</p>; }\n",
		"src/pages/other.tsx": "import \"../lib/z\";\nexport default function O() { return <p>o</p>; }\n",
	})
	preview, _ := callText(t, svc, "move_file", map[string]any{"from": "src/lib/util.ts", "to": "src/shared/deep/h.ts"})
	if !strings.Contains(preview, "src/pages/index.tsx") || !strings.Contains(readRoot(t, svc, "src/pages/index.tsx"), "../lib/util") {
		t.Errorf("a dry run must list importers and change nothing: %s", preview)
	}
	if text, isErr := callText(t, svc, "move_file", map[string]any{"from": "src/lib/util.ts", "to": "src/shared/deep/h.ts", "apply": true}); isErr {
		t.Fatal(text)
	}
	for rel, want := range map[string]string{
		"src/pages/index.tsx":  `from "../shared/deep/h"`,
		"src/pages/lazy.tsx":   `import("../shared/deep/h.ts")`,
		"src/pages/other.tsx":  `import "../lib/z"`,
		"src/shared/deep/h.ts": `from "../../lib/z"`,
	} {
		if got := readRoot(t, svc, rel); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want %s", rel, got, want)
		}
	}
	if text, isErr := callText(t, svc, "undo", nil); isErr {
		t.Fatal(text)
	}
	if !strings.Contains(readRoot(t, svc, "src/pages/index.tsx"), `"../lib/util"`) {
		t.Error("undo must restore the importers too")
	}
}

func TestDeleteFileWarnsAboutImporters(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"src/lib/util.ts":     "export const x = 1;\n",
		"src/pages/index.tsx": "import { x } from \"../lib/util\";\nexport default function P() { return <p>{x}</p>; }\n",
	})
	text, _ := callText(t, svc, "delete_file", map[string]any{"path": "src/lib/util.ts"})
	if !strings.Contains(text, "still imported by src/pages/index.tsx") {
		t.Errorf("delete_file = %s", text)
	}
}

func TestFormatFileUsesTheProjectFormatterOnly(t *testing.T) {
	const page = "export default function P() {\n  return (\n    <main>\n      <h1>x</h1>\n    </main>\n  );\n}\n"
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": page})
	args := map[string]any{"path": "src/pages/index.tsx", "apply": true}
	if text, isErr := callText(t, svc, "format_file", args); !isErr || !strings.Contains(text, "no formatter found") {
		t.Errorf("no formatter installed: %s", text)
	}
	if got := readRoot(t, svc, "src/pages/index.tsx"); got != page {
		t.Errorf("format_file must not rewrite the file without a formatter:\n%s", got)
	}
	if runtime.GOOS == "windows" {
		return
	}
	writeRoot(t, svc, "node_modules/.bin/prettier", "#!/bin/sh\ncat\necho '// formatted'\n")
	if text, isErr := callText(t, svc, "format_file", args); !isErr || !strings.Contains(text, "allowTasks") {
		t.Errorf("it runs project code, so it must be opted in: %s", text)
	}
	writePolicy(t, svc, `{"allowTasks":["format"]}`)
	if text, isErr := callText(t, svc, "format_file", args); isErr {
		t.Fatal(text)
	}
	if got := readRoot(t, svc, "src/pages/index.tsx"); got != page+"// formatted\n" {
		t.Errorf("formatted = %q", got)
	}
}

func TestCreatePageTemplates(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"content/blog/one.md": "---\ntitle: One\n---\n\nBody\n",
		"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }\n",
	})
	svc.cfg.Content = map[string]any{"blog": map[string]any{"dir": "content/blog", "schema": map[string]any{"title": "string"}}}
	create := func(args map[string]any) string {
		t.Helper()
		args["apply"] = true
		text, isErr := callText(t, svc, "create_page", args)
		if isErr {
			t.Fatalf("create_page %v: %s", args, text)
		}
		return text
	}
	if text := create(map[string]any{"route": "/docs/[...parts]"}); !strings.Contains(text, "src/pages/docs/[...parts].tsx") {
		t.Errorf("paths must be project-relative: %s", text)
	}
	create(map[string]any{"route": "/posts", "template": "content-list", "collection": "blog"})
	if got := readRoot(t, svc, "src/pages/posts.tsx"); !strings.Contains(got, "`/posts/${post.slug}`") {
		t.Errorf("a list page must link under its own route:\n%s", got)
	}
	create(map[string]any{"route": "/items/[id]", "template": "detail", "collection": "blog"})
	for _, bad := range []string{"/../evil", "/a/../../evil", "/x..y"} {
		if text, isErr := callText(t, svc, "create_page", map[string]any{"route": bad, "apply": true}); !isErr {
			t.Errorf("%s must be refused: %s", bad, text)
		}
	}
	if text, isErr := callText(t, svc, "build", nil); isErr {
		t.Fatalf("scaffolded pages must build: %s", text)
	}
	if _, err := os.Stat(filepath.Join(svc.root, "dist", "items", "one", "index.html")); err != nil {
		t.Errorf("the detail template must generate its pages: %v", err)
	}
}

func TestCreateComponentRejectsLowercaseNames(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }\n"})
	if text, isErr := callText(t, svc, "create_component", map[string]any{"name": "card2", "apply": true}); !isErr {
		t.Errorf("a lowercase name renders as an HTML tag: %s", text)
	}
}

func TestEditContentDoesNotGrowBlankLines(t *testing.T) {
	svc := newTestService(t, map[string]string{
		"content/blog/one.md": "---\ntitle: One\n---\n\nBody\n",
		"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }\n",
	})
	svc.cfg.Content = map[string]any{"blog": map[string]any{"dir": "content/blog", "schema": map[string]any{"title": "string"}}}
	for i := 0; i < 3; i++ {
		if text, isErr := callText(t, svc, "edit_content", map[string]any{"collection": "blog", "slug": "one", "data": map[string]any{"title": fmt.Sprint("T", i)}, "apply": true}); isErr {
			t.Fatal(text)
		}
	}
	if got := readRoot(t, svc, "content/blog/one.md"); got != "---\ntitle: T2\n---\n\nBody\n" {
		t.Errorf("entry = %q", got)
	}
}

func TestDiagnosticsLocateFindingsAndHaveUniqueIDs(t *testing.T) {
	svc := newBuiltService(t, map[string]string{
		"src/pages/index.tsx": "export default function P() {\n  return (\n    <main>\n      <h1>x</h1>\n      <img src=\"/a.png\" />\n      <input name=\"q\" />\n    </main>\n  );\n}\n",
	})
	m := mustOK(t, svc, "get_diagnostics", map[string]any{"route": "/"})
	seen := map[string]bool{}
	lines := map[string]float64{}
	for _, raw := range list(t, m, "diagnostics") {
		d := raw.(map[string]any)
		id := d["id"].(string)
		if seen[id] {
			t.Errorf("duplicate id %s", id)
		}
		seen[id] = true
		if line, ok := d["line"].(float64); ok {
			lines[d["code"].(string)] = line
		}
	}
	if lines["a11y/img-alt"] != 5 || lines["a11y/form-label"] != 6 {
		t.Errorf("finding lines = %v", lines)
	}
	if strings.Contains(fmt.Sprint(m), svc.root) {
		t.Errorf("output leaks the machine path: %v", m)
	}
}

func TestRouteFiltersRefuseUnknownRoutes(t *testing.T) {
	svc := newBuiltService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }\n"})
	for _, tool := range []string{"get_diagnostics", "audit_page"} {
		if msg := mustFail(t, svc, tool, map[string]any{"route": "/nope"}); !strings.Contains(msg, "no route matches") {
			t.Errorf("%s: %s", tool, msg)
		}
	}
}

func TestInstalledVersionFollowsNodeResolution(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "apps", "web")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	dep := filepath.Join(parent, "node_modules", "@krate", "core")
	if err := os.MkdirAll(dep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dep, "package.json"), []byte(`{"name":"@krate/core","version":"1.2.3"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Service{root: root}
	if got := s.installedVersion("@krate/core"); got != "1.2.3" {
		t.Errorf("a workspace keeps packages in a parent node_modules; got %q", got)
	}
}

func TestLogsAcceptDurationsAndEmptyResults(t *testing.T) {
	svc := newTestService(t, map[string]string{"src/pages/index.tsx": "export default function P() { return <h1>x</h1>; }\n"})
	writeRoot(t, svc, ".krate/dev.log", "2020-01-01T00:00:00Z old line\n")
	m := mustOK(t, svc, "get_logs", map[string]any{"since": "1h"})
	if got := list(t, m, "lines"); len(got) != 0 {
		t.Errorf("lines = %v", got)
	}
}
