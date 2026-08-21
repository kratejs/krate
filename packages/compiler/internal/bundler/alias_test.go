package bundler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAliasImportResolution verifies that the `@/*` TypeScript path alias
// resolves through the full Bundle() pipeline — the same path a real page
// build takes — so users can import from the project source root (e.g.
// `@/lib/helpers`, `@/components/badge`) without the compiler failing to find
// the module. Components live under `@/components/*`
func TestAliasImportResolution(t *testing.T) {
	root := t.TempDir()

	// tsconfig paths: `@/*` → `./*` with baseUrl `./src`.
	tsconfig := `{
		"compilerOptions": {
			"baseUrl": "./src",
			"paths": {
				"@/*": ["./*"]
			}
		}
	}`
	os.MkdirAll(filepath.Join(root, "src"), 0755)
	os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(tsconfig), 0644)

	// helpers module target
	os.MkdirAll(filepath.Join(root, "src", "lib"), 0755)
	os.WriteFile(filepath.Join(root, "src", "lib", "helpers.ts"),
		[]byte(`export function greet(name: string): string { return "hi " + name; }`), 0644)

	// badge component target (via @/components alias)
	os.MkdirAll(filepath.Join(root, "src", "components"), 0755)
	os.WriteFile(filepath.Join(root, "src", "components", "badge.tsx"),
		[]byte(`export default function Badge() { return <span>badge</span>; }`), 0644)

	// page that imports via the @/ alias
	pageDir := filepath.Join(root, "src", "pages")
	os.MkdirAll(pageDir, 0755)
	page := filepath.Join(pageDir, "index.tsx")
	pageSrc := `import { greet } from '@/lib/helpers';
import Badge from '@/components/badge';
export default function Page() {
  return <div>{greet("world")}<Badge /></div>;
}`
	os.WriteFile(page, []byte(pageSrc), 0644)

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

// TestAliasImportResolutionConfig verifies alias resolution using the same
// config-driven wiring the Builder uses: aliases extracted from tsconfig.json
// via LoadTSConfigPaths, then applied to the bundler.
func TestAliasImportResolutionConfig(t *testing.T) {
	root := t.TempDir()

	os.MkdirAll(filepath.Join(root, "src", "utils"), 0755)
	os.WriteFile(filepath.Join(root, "src", "utils", "helper.ts"), []byte(`export const x = 1;`), 0644)
	os.MkdirAll(filepath.Join(root, "src", "pages"), 0755)
	page := filepath.Join(root, "src", "pages", "index.tsx")
	os.WriteFile(page, []byte(`import { x } from '@/utils/helper'; export default function P() { return <div>{x}</div>; }`), 0644)

	b := New(root)
	b.SetPathAliases(
		[]string{"@/*"},
		[][]string{{"./*"}},
		filepath.Join(root, "src"),
	)

	if _, err := b.Bundle(page); err != nil {
		t.Fatalf("Bundle with @/ alias: %v", err)
	}
}

// TestNonAliasScopeDoesNotResolve verifies that a scope-style import like
// `@components/*` is NOT treated as a path alias when only `@/*` is
// configured. The compiler should not resolve it as if it were an alias.
func TestNonAliasScopeDoesNotResolve(t *testing.T) {
	root := t.TempDir()

	os.MkdirAll(filepath.Join(root, "src", "components"), 0755)
	os.WriteFile(filepath.Join(root, "src", "components", "badge.tsx"),
		[]byte(`export default function Badge() { return <span>badge</span>; }`), 0644)
	os.MkdirAll(filepath.Join(root, "src", "pages"), 0755)
	page := filepath.Join(root, "src", "pages", "index.tsx")
	os.WriteFile(page, []byte(`import Badge from '@components/badge'; export default function P() { return <Badge />; }`), 0644)

	b := New(root)
	b.SetPathAliases(
		[]string{"@/*"},
		[][]string{{"./*"}},
		filepath.Join(root, "src"),
	)

	// `@components/badge` is not an alias and is not a resolvable module — the
	// bundled modules must NOT include the badge component under that name.
	bundle, err := b.Bundle(page)
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	for _, mod := range bundle.Modules {
		norm := filepath.ToSlash(mod.Path)
		if strings.HasSuffix(norm, "components/badge.tsx") {
			t.Errorf("@components/* should not resolve as an alias; found %s in bundle", norm)
		}
	}
}
