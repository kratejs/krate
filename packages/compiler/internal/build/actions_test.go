package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestParseActionExports(t *testing.T) {
	src := `"use server";

async function helper() { return 1; }
export async function saveTodo(formData: FormData) { return helper(); }
export const removeTodo = async (id: string) => id;
export { helper as ping };
`
	exports, err := parseActionExports(src, "src/actions/todo.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"saveTodo", "removeTodo", "ping"} {
		if exports[name] == "" {
			t.Fatalf("missing action export %q: %v", name, exports)
		}
	}
	if exports["saveTodo"] == exports["removeTodo"] {
		t.Fatal("action ids must be unique per export")
	}
}

func TestParseActionExportsRejectsNonFunction(t *testing.T) {
	_, err := parseActionExports("\"use server\"; export const nope = 1;", "src/actions/bad.ts")
	if err == nil || !strings.Contains(err.Error(), "non-function") {
		t.Fatalf("expected non-function error, got %v", err)
	}
}

func TestPrepareActionsBuildsManifestAndBundle(t *testing.T) {
	root := t.TempDir()
	writeActionTestFile(t, filepath.Join(root, "src", "actions", "todo.ts"), `"use server";

export async function saveTodo(input: FormData | { text: string }) {
  return { text: input instanceof FormData ? input.get("text") : input.text };
}
`)
	cfg := config.Default()
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	if err := b.prepareActions(); err != nil {
		t.Fatal(err)
	}
	if b.actions == nil || len(b.actions.Entries) != 1 {
		t.Fatalf("expected one action, got %+v", b.actions)
	}
	entry := b.actions.Entries[0]
	if entry.Export != "saveTodo" || entry.ID == "" {
		t.Fatalf("unexpected action entry: %+v", entry)
	}
	if _, err := os.Stat(filepath.Join(root, ".krate", "actions", entry.File)); err != nil {
		t.Fatalf("action bundle missing: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".krate", "actions", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m actionManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Actions) != 1 || m.Actions[0].ID != entry.ID {
		t.Fatalf("manifest mismatch: %+v", m)
	}
}

func TestActionFilesAreNotPages(t *testing.T) {
	root := t.TempDir()
	action := filepath.Join(root, "src", "pages", "actions.ts")
	writeActionTestFile(t, action, "\"use server\";\nexport async function save() {}")
	if !isUseServerFile(action) {
		t.Fatal("expected action file detection")
	}
}

func writeActionTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestActionPropForwardedToChildComponent(t *testing.T) {
	root := t.TempDir()
	writeActionTestFile(t, filepath.Join(root, "src", "actions", "feedback.ts"), `"use server";
export async function saveFeedback(input: FormData | { text: string }) {
  return { text: input instanceof FormData ? input.get("text") : input.text };
}
`)
	writeActionTestFile(t, filepath.Join(root, "src", "pages", "index.tsx"), `import { saveFeedback } from "../actions/feedback";
function ActionForm(props: { action: unknown }) {
  return <form action={props.action}><button type="submit">Save</button></form>;
}
export default function Page() {
  return <ActionForm action={saveFeedback} />;
}
`)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	b := New(root, cfg)
	if err := b.prepareActions(); err != nil {
		t.Fatal(err)
	}

	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	if b.actions == nil || len(b.actions.Entries) != 1 {
		t.Fatalf("actions were not discovered: %+v", b.actions)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	if !strings.Contains(html, `action="/__krate/action/`) {
		t.Fatalf("child component action was not rendered as URL:\n%s", html)
	}
	if !strings.Contains(html, `data-krate-action`) || !strings.Contains(html, `method="post"`) {
		t.Fatalf("child component action missing POST marker:\n%s", html)
	}
}
