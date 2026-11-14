package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

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

func writeActionTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
