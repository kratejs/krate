// Package resolver provides the shared node_modules package resolution used by
// the bundler (bare import specifiers) and the docs plugin (npm docs themes).
// Resolution mirrors Node's `exports`/`module`/`main` fallback: a package's
// entry file is picked from package.json `exports` (the `import`/`default`
// condition) when present, then `module`, then `main`, then a top-level
// index.* file, by walking up the directory tree from a start dir looking for
// node_modules/<pkg>. Modern ESM packages (e.g. Radix UI) ship `.mjs` with an
// `exports` map, both of which are handled here.
package resolver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// PkgJSON is the subset of package.json the resolver reads to find a package
// entry file. Exports is kept raw because it may be a string, a conditions
// object, or a subpath map.
type PkgJSON struct {
	Main    string          `json:"main"`
	Module  string          `json:"module"`
	Exports json.RawMessage `json:"exports"`
}

// Exists reports whether a file or directory exists at path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// sourceExtensions are code extensions tried in order when a candidate lacks
// an extension, and when descending into a directory.
var sourceExtensions = []string{".tsx", ".ts", ".mts", ".jsx", ".js", ".mjs", ".cjs", ".md", ".mdx", ".css", ".json"}

// indexFiles are the directory index files tried when a path resolves to a dir.
var indexFiles = []string{
	"index.tsx", "index.ts", "index.mts", "index.jsx", "index.js", "index.mjs", "index.cjs",
}

// NodeModule resolves a bare npm package specifier ("formslib",
// "@scope/formslib", "@scope/formslib/subpath") by walking up from startDir
// looking for node_modules/pkg. It returns the absolute path to the package
// entry file, or "" if the package (or an entry file) cannot be found.
func NodeModule(startDir, spec string) string {
	pkg, sub := splitSpec(spec)
	if pkg == "" {
		return ""
	}
	dir := startDir
	for {
		base := filepath.Join(dir, "node_modules", pkg)
		if info, err := os.Stat(base); err == nil && info.IsDir() {
			return resolvePackage(base, sub)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// NodeModuleDir resolves a bare npm specifier that may include a subpath,
// e.g. "formslib", "@scope/formslib", or "formslib/styles/x.css". Kept as the
// public entry point used by the plugin API.
func NodeModuleDir(startDir, spec string) string {
	return NodeModule(startDir, spec)
}

// resolvePackage resolves a package directory (optionally with a subpath) to a
// concrete file, honoring `exports` first, then `module`/`main`, then index.
func resolvePackage(pkgDir, sub string) string {
	pkg := readPkgJSON(pkgDir)

	// A package's `exports` field is authoritative when present.
	if len(pkg.Exports) > 0 {
		if file := resolveExports(pkgDir, pkg.Exports, sub); file != "" {
			return file
		}
	}

	if sub != "" {
		candidate := filepath.Join(pkgDir, filepath.FromSlash(sub))
		if file := resolveFile(candidate); file != "" {
			return file
		}
		return ""
	}

	// Entry fields.
	for _, field := range []string{pkg.Module, pkg.Main} {
		if field == "" {
			continue
		}
		if file := resolveFile(filepath.Join(pkgDir, field)); file != "" {
			return file
		}
	}
	return resolveFile(pkgDir)
}

// resolveExports resolves a subpath against a package's `exports` field,
// handling the common shapes: a top-level string target, a conditions object,
// a subpath map (`"./x": ...`), and wildcard patterns (`"./*": "./dist/*.mjs"`).
func resolveExports(pkgDir string, exports json.RawMessage, sub string) string {
	// Top-level string or conditions object: only the root import is described.
	if !isJSONObject(exports) {
		if sub != "" {
			return ""
		}
		return pickExportTarget(pkgDir, exports)
	}

	var m map[string]json.RawMessage
	if json.Unmarshal(exports, &m) != nil {
		return ""
	}

	// A subpath map has keys starting with ".". A bare conditions map (no ".")
	// describes the root import only.
	_, hasRoot := m["."]
	if !hasRoot && !hasSubpathKeys(m) {
		if sub != "" {
			return ""
		}
		return pickExportTarget(pkgDir, exports)
	}

	key := "."
	if sub != "" {
		key = "./" + sub
	}

	// Exact subpath match.
	if raw, ok := m[key]; ok {
		if target := pickExportTarget(pkgDir, raw); target != "" {
			return target
		}
	}

	// Wildcard patterns: "./foo/*": ...
	for pattern, raw := range m {
		star := strings.IndexByte(pattern, '*')
		if star < 0 {
			continue
		}
		prefix := pattern[:star]
		suffix := pattern[star+1:]
		if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) {
			continue
		}
		mid := key[len(prefix) : len(key)-len(suffix)]
		target := pickExportTargetString(pkgDir, raw)
		if target == "" {
			continue
		}
		target = strings.ReplaceAll(target, "*", mid)
		if file := resolveFile(filepath.Join(pkgDir, target)); file != "" {
			return file
		}
	}
	return ""
}

// isJSONObject reports whether raw JSON denotes an object.
func isJSONObject(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return strings.HasPrefix(trimmed, "{")
}

// hasSubpathKeys reports whether an exports map has any "./x" subpath keys.
func hasSubpathKeys(m map[string]json.RawMessage) bool {
	for k := range m {
		if strings.HasPrefix(k, "./") {
			return true
		}
	}
	return false
}

// pickExportTarget returns the file a raw export value resolves to (a string
// target or a conditions object).
func pickExportTarget(pkgDir string, raw json.RawMessage) string {
	target := pickExportTargetString(pkgDir, raw)
	if target == "" {
		return ""
	}
	return resolveFile(filepath.Join(pkgDir, target))
}

// pickExportTargetString returns the raw target string a conditions object (or
// plain string) selects, preferring ESM-friendly conditions.
func pickExportTargetString(pkgDir string, raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s
	}
	var cond map[string]json.RawMessage
	if json.Unmarshal(raw, &cond) != nil {
		return ""
	}
	// Preference order: import (ESM) is what the compiler consumes; then
	// default; then require.
	for _, key := range []string{"import", "default", "module", "browser", "require"} {
		if nested, ok := cond[key]; ok {
			if target := pickExportTargetString(pkgDir, nested); target != "" {
				return target
			}
		}
	}
	return ""
}

// resolveFile resolves a candidate path to an existing file, trying the path as
// given, then with source extensions, then as a directory index.
func resolveFile(candidate string) string {
	candidate = filepath.Clean(candidate)
	if Exists(candidate) {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			for _, idx := range indexFiles {
				p := filepath.Join(candidate, idx)
				if Exists(p) {
					return p
				}
			}
			return ""
		}
		return candidate
	}
	for _, ext := range sourceExtensions {
		p := candidate + ext
		if Exists(p) {
			return p
		}
	}
	return ""
}

// readPkgJSON reads a package's package.json, returning an empty struct when it
// is missing or unparseable.
func readPkgJSON(pkgDir string) PkgJSON {
	data, err := os.ReadFile(filepath.Join(pkgDir, "package.json"))
	if err != nil {
		return PkgJSON{}
	}
	var pkg PkgJSON
	_ = json.Unmarshal(data, &pkg)
	return pkg
}

// splitSpec separates a package specifier into its package name and optional
// subpath ("@scope/pkg/sub/file" -> "@scope/pkg", "sub/file").
func splitSpec(spec string) (pkg, sub string) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", ""
	}
	if strings.HasPrefix(spec, "@") {
		parts := strings.SplitN(spec, "/", 3)
		if len(parts) < 2 || parts[1] == "" {
			return "", ""
		}
		pkg = parts[0] + "/" + parts[1]
		if len(parts) == 3 {
			sub = parts[2]
		}
		return pkg, sub
	}
	parts := strings.SplitN(spec, "/", 2)
	if parts[0] == "" {
		return "", ""
	}
	if len(parts) == 2 {
		sub = parts[1]
	}
	return parts[0], sub
}

// PackageDir resolves a package directory's entry file: package.json `exports`
// (import/default), then `module`, then `main`, then a top-level index.* - or ""
// if none exist. Kept for callers that already have a package directory.
func PackageDir(pkgDir string) string {
	return resolvePackage(pkgDir, "")
}
