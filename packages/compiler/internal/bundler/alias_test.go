package bundler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAliasImportResolution verifies that the `@/*` TypeScript path alias
// resolves through the full Bundle() pipeline - the same path a real page
// build takes - so users can import from the project source root (e.g.
// `@/lib/helpers`, `@/components/badge`) without the compiler failing to find
// the module. Components live under `@/components/*`
func TestAliasImportResolution(t *testing.T) {
	root := t.TempDir()

	// tsconfig paths: `@/*` -> `./*` with baseUrl `./src`.
	tsconfig := `{
		"compilerOptions": {
			"baseUrl": "./src",
			"paths": {
				"@/*": ["./*"]
			}
		}
	}`
	_ = os.MkdirAll(filepath.Join(root, "src"), 0755)
	_ = os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(tsconfig), 0644)

	// helpers module target
	_ = os.MkdirAll(filepath.Join(root, "src", "lib"), 0755)
	_ = os.WriteFile(filepath.Join(root, "src", "lib", "helpers.ts"),
		[]byte(`export function greet(name: string): string { return "hi " + name; }`), 0644)

	// badge component target (via @/components alias)
	_ = os.MkdirAll(filepath.Join(root, "src", "components"), 0755)
	_ = os.WriteFile(filepath.Join(root, "src", "components", "badge.tsx"),
		[]byte(`export default function Badge() { return <span>badge</span>; }`), 0644)

	// page that imports via the @/ alias
	pageDir := filepath.Join(root, "src", "pages")
	_ = os.MkdirAll(pageDir, 0755)
	page := filepath.Join(pageDir, "index.tsx")
	pageSrc := `import { greet } from '@/lib/helpers';
import Badge from '@/components/badge';
export default function Page() {
  return <div>{greet("world")}<Badge /></div>;
}`
	_ = os.WriteFile(page, []byte(pageSrc), 0644)

	b := New(root)
	b.SetPathAliases(
		[]string{"@/*"},
		[][]string{{"./*"}},
		filepath.Join(root, "src"),
	)

	bundle, err := b.Bundle(page)
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}

	// Verify both aliased modules made it into the bundle.
	foundHelpers := false
	foundBadge := false
	for _, mod := range bundle.Modules {
		norm := filepath.ToSlash(mod.Path)
		if strings.HasSuffix(norm, "lib/helpers.ts") {
			foundHelpers = true
		}
		if strings.HasSuffix(norm, "components/badge.tsx") {
			foundBadge = true
		}
	}
	if !foundHelpers {
		t.Error("did not resolve '@/lib/helpers' through the bundler")
	}
	if !foundBadge {
		t.Error("did not resolve '@/components/badge' through the bundler")
	}

	// The page module itself must be present as the entry.
	foundEntry := false
	for _, mod := range bundle.Modules {
		if mod.IsEntry && mod.Path == page {
			foundEntry = true
		}
	}
	if !foundEntry {
		t.Error("entry page module not present in bundle")
	}
}
