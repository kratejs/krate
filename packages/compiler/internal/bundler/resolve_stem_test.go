package bundler

import (
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

func TestFileBeatsDirectoryWithSameStem(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"src/components/Button.tsx":       "export default function Button() { return <b />; }\n",
		"src/components/Button/style.css": "b { color: red; }\n",
		"src/components/Both.tsx":         "export const w = 'file';\n",
		"src/components/Both/index.tsx":   "export const w = 'dir';\n",
		"src/components/Dir/index.ts":     "export const w = 'dir';\n",
		"src/components/Empty/x.css":      "b {}\n",
		"src/pages/index.tsx":             "export default function P() { return <p />; }\n",
	})
	importer := filepath.Join(root, "src/pages/index.tsx")
	for spec, want := range map[string]string{
		"../components/Button": "src/components/Button.tsx",
		"../components/Both":   "src/components/Both.tsx",
		"../components/Dir":    "src/components/Dir/index.ts",
	} {
		if got := ResolveImport(importer, spec); got != filepath.Join(root, want) {
			t.Errorf("ResolveImport(%q) = %q, want %q", spec, got, want)
		}
	}
	if got := ResolveImport(importer, "../components/Empty"); got != "" {
		t.Errorf("a directory without an index must not resolve to itself, got %q", got)
	}

	aliases := []pathAlias{{prefix: "@/*", targets: []string{"src/*"}}}
	for spec, want := range map[string]string{
		"@/components/Button": "src/components/Button.tsx",
		"@/components/Both":   "src/components/Both.tsx",
	} {
		if got := resolvePathAlias(spec, aliases, root, resolver.Exists); got != filepath.Join(root, want) {
			t.Errorf("resolvePathAlias(%q) = %q, want %q", spec, got, want)
		}
	}
}

func TestBaseURLResolvesBareImports(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"src/components/Btn.tsx": "export default function Btn() { return <b>x</b>; }\n",
		"src/pages/index.tsx":    "import Btn from 'components/Btn';\nexport default function P() { return <Btn />; }\n",
	})
	b := New(root)
	b.SetBaseURL(filepath.Join(root, "src"))
	bundle, err := b.Bundle(filepath.Join(root, "src/pages/index.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	moduleNamed(t, bundle, "Btn.tsx")

	// Without baseUrl the same import is the usual missing-package error.
	if _, err := New(root).Bundle(filepath.Join(root, "src/pages/index.tsx")); err == nil {
		t.Fatal("expected an unresolved import without baseUrl")
	}
}
