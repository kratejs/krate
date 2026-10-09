package bundler

import (
	"os"
	"path/filepath"
	"testing"
)

// TestModuleCacheSharesParse verifies that a shared ModuleCache lets separate
// bundles reuse the same parsed *ast.Program for a shared (rewrite-free) module,
// instead of re-parsing it per entry.
func TestModuleCacheSharesParse(t *testing.T) {
	dir := t.TempDir()
	shared := "export function Card(props) {\n  return <div>{props.title}</div>;\n}\n"
	writeFile(t, filepath.Join(dir, "shared.tsx"), shared)
	writeFile(t, filepath.Join(dir, "a.tsx"), "import { Card } from './shared';\nexport default function A() { return <Card title=\"a\" />; }\n")
	writeFile(t, filepath.Join(dir, "b.tsx"), "import { Card } from './shared';\nexport default function B() { return <Card title=\"b\" />; }\n")

	cache := NewModuleCache()
	sharedProg := func(entry string) any {
		b := New(dir)
		b.SetModuleCache(cache)
		bundle, err := b.Bundle(filepath.Join(dir, entry))
		if err != nil {
			t.Fatalf("bundle %s: %v", entry, err)
		}
		for _, m := range bundle.Modules {
			if filepath.Base(m.Path) == "shared.tsx" {
				return m.Program
			}
		}
		t.Fatalf("shared.tsx missing from %s bundle", entry)
		return nil
	}

	if p1, p2 := sharedProg("a.tsx"), sharedProg("b.tsx"); p1 != p2 {
		t.Fatalf("expected the shared module's parsed program to be reused across bundles")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
