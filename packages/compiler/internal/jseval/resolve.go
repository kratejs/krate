package jseval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// krateResolver resolves bare `@krate/*` imports through Go instead of esbuild's
// own resolver. esbuild can fail to read Windows junctions (a monorepo
// `npm install` links node_modules/@krate/core as a junction), which surfaces as
// "Cannot read file ...: Incorrect function". Resolving here - and canonicalizing
// the real path - makes config/tailwind evaluation independent of the
// node_modules link type. Non-@krate specifiers are left to esbuild.
func krateResolver() api.Plugin {
	return api.Plugin{
		Name: "krate-resolve",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^@krate/`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.Namespace != "" && args.Namespace != "file" {
					return api.OnResolveResult{}, nil
				}
				base := args.ResolveDir
				if filepath.IsAbs(args.Importer) {
					base = filepath.Dir(args.Importer)
				}
				if base == "" {
					return api.OnResolveResult{}, fmt.Errorf("cannot resolve %q: unknown base directory", args.Path)
				}
				abs, err := resolveKrateEntry(base, args.Path)
				if err != nil {
					return api.OnResolveResult{}, err
				}
				return api.OnResolveResult{Path: abs, Namespace: "file"}, nil
			})
		},
	}
}

// resolveKrateEntry resolves a `@krate/<name>[/<sub>]` specifier to the package's
// real entry file, walking node_modules upward from base.
func resolveKrateEntry(base, spec string) (string, error) {
	parts := strings.SplitN(spec, "/", 3)
	if len(parts) < 2 || parts[0] != "@krate" || parts[1] == "" {
		return "", fmt.Errorf("cannot resolve %q: not a @krate package specifier", spec)
	}
	pkg := parts[0] + "/" + parts[1]
	sub := ""
	if len(parts) == 3 {
		sub = parts[2]
	}

	dir := base
	for {
		candidate := filepath.Join(dir, "node_modules", pkg)
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			return resolvePackageEntry(candidate, pkg, sub)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("cannot resolve %q from %s: run your package manager install (e.g. `npm install`)", spec, base)
}

// packageJSON is the subset of package.json needed to locate the entry file.
type packageJSON struct {
	Main    string          `json:"main"`
	Module  string          `json:"module"`
	Exports json.RawMessage `json:"exports"`
}

func resolvePackageEntry(pkgDir, pkg, sub string) (string, error) {
	data, err := os.ReadFile(filepath.Join(pkgDir, "package.json"))
	if err != nil {
		return "", fmt.Errorf("cannot resolve %q: reading package.json: %w", pkg, err)
	}
	var pkgJSON packageJSON
	if err := json.Unmarshal(data, &pkgJSON); err != nil {
		return "", fmt.Errorf("cannot resolve %q: parsing package.json: %w", pkg, err)
	}

	entry := exportsEntry(pkgJSON.Exports, sub)
	if entry == "" {
		entry = pkgJSON.Module
	}
	if entry == "" {
		entry = pkgJSON.Main
	}
	if entry == "" {
		entry = "index.js"
	}

	resolved, err := resolveToFile(filepath.Join(pkgDir, entry))
	if err != nil {
		return "", fmt.Errorf("cannot resolve %q: %w", pkg, err)
	}
	if real, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = real
	}
	return resolved, nil
}

// exportsEntry selects the entry for a subpath from the package.json `exports`
// field, following Node's simplified algorithm for the runtime (non-`types`)
// condition.
func exportsEntry(raw json.RawMessage, sub string) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		// `exports` may be a bare string entry.
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
		return ""
	}

	key := "."
	if sub != "" {
		key = "./" + sub
	}
	if v, ok := m[key]; ok {
		return pickEntry(v)
	}
	// No subpath key: the object may be a condition map for "." (keys that do
	// not start with "."), e.g. `{"import": "./dist/index.js"}`.
	if sub == "" {
		for k := range m {
			if strings.HasPrefix(k, ".") {
				return ""
			}
		}
		return pickEntry(raw)
	}
	return ""
}

// pickEntry returns the runtime entry from an exports value, which may be a
// string or a condition object. Preference matches an ESM import.
func pickEntry(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	for _, cond := range []string{"import", "default", "require", "module"} {
		if v, ok := m[cond]; ok {
			if e := pickEntry(v); e != "" {
				return e
			}
		}
	}
	return ""
}

// resolveToFile resolves a candidate path to an existing file, trying common
// extensions and an index file under a directory path.
func resolveToFile(base string) (string, error) {
	base = filepath.Clean(base)
	for _, c := range []string{base, base + ".js", base + ".mjs", base + ".cjs", base + ".ts", base + ".tsx"} {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, nil
		}
	}
	for _, ext := range []string{".js", ".mjs", ".cjs", ".ts", ".tsx"} {
		c := filepath.Join(base, "index"+ext)
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("%s not found", base)
}
