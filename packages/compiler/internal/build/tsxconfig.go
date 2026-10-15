package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// tsxTsconfigRel is the project-relative path to the generated tsconfig used by
// `npx tsx` bootstraps (config load, generateStaticParams). It lets user source
// imported by a bootstrap resolve Krate-provided aliases (notably
// `krate/content`) and the project's own tsconfig path aliases.
const tsxTsconfigRel = ".krate/tsconfig.json"

// writeTsxTsconfig generates `.krate/tsconfig.json`: a tsconfig rooted at the
// project with the project's own path aliases plus the codegen'd `krate/content`
// module. `npx tsx --tsconfig .krate/tsconfig.json` then resolves
// `import { getCollection } from "krate/content"` in generateStaticParams.
//
// Returns the absolute path to the written file, or "" when there is nothing to
// write. Failures are non-fatal (returned as an error the caller may warn on).
func (b *Builder) writeTsxTsconfig() (string, error) {
	paths := map[string][]string{}

	// Project aliases, re-based to be relative to the project root (which is
	// this tsconfig's baseUrl).
	for _, a := range b.Cfg.PathAliases {
		if a.Prefix == "" {
			continue
		}
		var targets []string
		for _, t := range a.Targets {
			abs := t
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(b.Cfg.TSBaseDir, t)
			}
			rel, err := filepath.Rel(b.Root, abs)
			if err != nil {
				rel = abs
			}
			targets = append(targets, "./"+filepath.ToSlash(rel))
		}
		if len(targets) > 0 {
			paths[a.Prefix] = targets
		}
	}

	// Krate-provided virtual modules.
	contentMod := filepath.ToSlash(filepath.Join(routeGenDir, "content.ts"))
	if b.hasContentCollections() {
		paths["krate/content"] = []string{"./" + contentMod}
		paths["@krate/content"] = []string{"./" + contentMod}
	}

	pathsJSON, err := marshalPaths(paths)
	if err != nil {
		return "", err
	}

	doc := "{\n" +
		"  \"compilerOptions\": {\n" +
		"    \"target\": \"ES2022\",\n" +
		"    \"module\": \"ESNext\",\n" +
		"    \"moduleResolution\": \"bundler\",\n" +
		"    \"jsx\": \"preserve\",\n" +
		"    \"jsxImportSource\": \"@krate/runtime\",\n" +
		"    \"allowArbitraryExtensions\": true,\n" +
		"    \"strict\": false,\n" +
		"    \"skipLibCheck\": true,\n" +
		"    \"baseUrl\": \"..\",\n" +
		"    \"paths\": " + pathsJSON + "\n" +
		"  }\n" +
		"}\n"

	outPath := filepath.Join(b.Root, filepath.FromSlash(tsxTsconfigRel))
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return "", fmt.Errorf("creating .krate dir: %w", err)
	}
	if err := os.WriteFile(outPath, []byte(doc), 0644); err != nil {
		return "", fmt.Errorf("writing tsconfig: %w", err)
	}
	return outPath, nil
}

func (b *Builder) hasContentCollections() bool {
	cc := b.contentConfig()
	return cc != nil && len(cc.Collections) > 0
}

// marshalPaths renders the paths map deterministically (sorted keys).
func marshalPaths(paths map[string][]string) (string, error) {
	keys := make([]string, 0, len(paths))
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		quoted := make([]string, len(paths[k]))
		for i, t := range paths[k] {
			quoted[i] = strconvQuote(t)
		}
		parts = append(parts, "      "+strconvQuote(k)+": ["+strings.Join(quoted, ", ")+"]")
	}
	if len(parts) == 0 {
		return "{}", nil
	}
	return "{\n" + strings.Join(parts, ",\n") + "\n    }", nil
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
