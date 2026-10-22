package bundler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/astprint"
)

func TestRewriteActionImports(t *testing.T) {
	root := t.TempDir()
	actionPath := filepath.Join(root, "src", "actions.ts")
	pagePath := filepath.Join(root, "src", "pages", "index.tsx")
	writeActionFile(t, actionPath, `"use server"; export async function saveTodo() {}`)
	writeActionFile(t, pagePath, `import { saveTodo as save } from "../actions";
export default function Page() {
  return <form action={save}><button>Save</button></form>;
}`)

	b := New(root)
	b.SetActionModules(map[string]map[string]string{
		filepath.Clean(actionPath): {"saveTodo": "a123"},
	})
	b.SetActionBasePath("/base")
	bundle, err := b.Bundle(pagePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := bundle.ActionImports["save"]; got != "/base/__krate/action/a123" {
		t.Fatalf("action URL = %q", got)
	}
	var out strings.Builder
	for _, mod := range bundle.Modules {
		if mod.Path == actionPath {
			t.Fatal("action module must not enter the page bundle")
		}
		if mod.Program != nil {
			out.WriteString(astprint.Print(mod.Program))
		}
	}
	src := out.String()
	if strings.Contains(src, `from "../actions"`) {
		t.Fatalf("action import was not removed:\n%s", src)
	}
	if !strings.Contains(src, `__krateAction("a123")`) {
		t.Fatalf("missing client action proxy:\n%s", src)
	}
}

func TestRewriteActionImportMissingExport(t *testing.T) {
	root := t.TempDir()
	actionPath := filepath.Join(root, "src", "actions.ts")
	pagePath := filepath.Join(root, "src", "pages", "index.tsx")
	writeActionFile(t, actionPath, `"use server"; export async function save() {}`)
	writeActionFile(t, pagePath, `import { missing } from "../actions";
export default function Page() { return <div>{missing}</div>; }`)

	b := New(root)
	b.SetActionModules(map[string]map[string]string{
		filepath.Clean(actionPath): {"save": "a123"},
	})
	_, err := b.Bundle(pagePath)
	if err == nil || !strings.Contains(err.Error(), `imports "missing"`) {
		t.Fatalf("expected missing-export error, got %v", err)
	}
}

func writeActionFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
