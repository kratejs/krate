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
	"sync"
	"time"
)

// PkgJSON is the subset of package.json the resolver reads to find a package
// entry file. Exports is kept raw because it may be a string, a conditions
// object, or a subpath map.
type PkgJSON struct {
	Main    string          `json:"main"`
	Module  string          `json:"module"`
	Exports json.RawMessage `json:"exports"`
	Imports json.RawMessage `json:"imports"`
	Krate   json.RawMessage `json:"krate"`
}

// Stat is a cached filesystem probe. Within a build a path's existence, kind,
// size, and mtime do not change, so probes are memoized (ResetCaches clears
// them at the start of a build so dev edits are seen).
type Stat struct {
	Exists bool
	IsDir  bool
	Mtime  time.Time
	Size   int64
}

var (
	cacheMu      sync.Mutex
	statMemo     = map[string]Stat{}
	pkgMemo      = map[string]PkgJSON{}
	cacheEnabled = os.Getenv("KRATE_NO_RESOLVER_CACHE") == ""
)

// StatPath returns the cached stat for path (a negative result is cached too).
// Set KRATE_NO_RESOLVER_CACHE to bypass the memo (used to A/B resolver caching).
func StatPath(path string) Stat {
	if !cacheEnabled {
		return statNow(path)
	}
	cacheMu.Lock()
	if s, ok := statMemo[path]; ok {
		cacheMu.Unlock()
		return s
	}
	cacheMu.Unlock()

	s := statNow(path)

	cacheMu.Lock()
	statMemo[path] = s
	cacheMu.Unlock()
	return s
}

func statNow(path string) Stat {
	s := Stat{}
	if fi, err := os.Stat(path); err == nil {
		s = Stat{Exists: true, IsDir: fi.IsDir(), Mtime: fi.ModTime(), Size: fi.Size()}
	}
	return s
}

// ResetCaches clears the stat and package.json memos. Call once per build.
func ResetCaches() {
	cacheMu.Lock()
	statMemo = map[string]Stat{}
	pkgMemo = map[string]PkgJSON{}
	cacheMu.Unlock()
}

// Exists reports whether a file or directory exists at path.
func Exists(path string) bool {
	return StatPath(path).Exists
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
	return NodeModuleWith(startDir, spec, defaultConditions)
}

// NodeModuleWith is NodeModule with an explicit condition set for the
// package's `exports` map.
func NodeModuleWith(startDir, spec string, conds Conditions) string {
	pkg, sub := splitSpec(spec)
	if pkg == "" {
		return ""
	}
	// Follow symlinks to the real package directory before walking up. pnpm
	// links workspace/node_modules entries to a virtual store; a package's own
	// dependencies (e.g. `@radix-ui/react-popover` from `radix-ui`) only exist
	// next to the real directory, so walking the symlink path would miss them.
	dir := startDir
	if resolved, err := filepath.EvalSymlinks(startDir); err == nil {
		dir = resolved
	}
	for {
		base := filepath.Join(dir, "node_modules", pkg)
		if s := StatPath(base); s.Exists && s.IsDir {
			// A workspace package linked into node_modules (pnpm/npm workspaces)
			// is project source: resolve through the link so its files are not
			// mistaken for a compiled dependency.
			if !strings.HasPrefix(pkg, "@krate/") {
				if real, err := filepath.EvalSymlinks(base); err == nil && real != base && !strings.Contains(filepath.ToSlash(real), "/node_modules/") {
					base = real
				}
			}
			return resolvePackage(base, sub, conds)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// PackageImport resolves a package.json `imports` specifier ("#util") for a
// file in importerDir against the nearest enclosing package.json. Returns ""
// when there is no such mapping or its target is missing.
func PackageImport(importerDir, spec string, conds Conditions) string {
	for dir := importerDir; ; dir = filepath.Dir(dir) {
		if Exists(filepath.Join(dir, "package.json")) {
			pkg := readPkgJSON(dir)
			if len(pkg.Imports) == 0 {
				return ""
			}
			keys, m := orderedObject(pkg.Imports)
			best, mid := "", ""
			if _, ok := m[spec]; ok && !strings.Contains(spec, "*") {
				best = spec
			} else {
				for _, pattern := range keys {
					star := strings.IndexByte(pattern, '*')
					if star < 0 || !strings.HasPrefix(spec, pattern[:star]) || !strings.HasSuffix(spec, pattern[star+1:]) || len(spec) < len(pattern)-1 {
						continue
					}
					if best == "" || patternKeyLess(pattern, best) {
						best, mid = pattern, spec[star:len(spec)-(len(pattern)-star-1)]
					}
				}
			}
			if best == "" {
				return ""
			}
			for _, cs := range []Conditions{conds, conds.withRequire()} {
				target, ok := pickTarget(m[best], cs)
				if !ok || target == "" {
					continue
				}
				target = strings.ReplaceAll(target, "*", mid)
				if strings.HasPrefix(target, ".") {
					if f := resolveFile(filepath.Join(dir, target)); f != "" {
						return f
					}
					continue
				}
				if f := NodeModuleWith(dir, target, conds); f != "" {
					return f
				}
			}
			return ""
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

// NodeModuleDir resolves a bare npm specifier that may include a subpath,
// e.g. "formslib", "@scope/formslib", or "formslib/styles/x.css". Kept as the
// public entry point used by the plugin API.
func NodeModuleDir(startDir, spec string) string {
	return NodeModule(startDir, spec)
}

// resolvePackage resolves a package directory (optionally with a subpath) to a
// concrete file, honoring `exports` first, then `module`/`main`, then index.
func resolvePackage(pkgDir, sub string, conds Conditions) string {
	pkg := readPkgJSON(pkgDir)

	// A package's `exports` field is authoritative when present.
	if len(pkg.Exports) > 0 {
		if file := resolveExports(pkgDir, pkg.Exports, sub, conds); file != "" {
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

// Conditions is the active set of package.json condition names, in addition to
// the always-active "default". Order in the set is irrelevant: Node picks by
// the key order of the package's own conditions object.
type Conditions []string

// ClientConditions are the conditions for code that runs in the browser.
func ClientConditions(dev bool) Conditions {
	return Conditions{"browser", "import", "module", modeCondition(dev)}
}

// ServerConditions are the conditions for code that runs under Node-like
// runtimes (build-time evaluation, SSR).
func ServerConditions(dev bool) Conditions {
	return Conditions{"node", "import", "module", modeCondition(dev)}
}

func modeCondition(dev bool) string {
	if dev {
		return "development"
	}
	return "production"
}

// defaultConditions backs NodeModule for callers that do not pick a side.
var defaultConditions = ClientConditions(false)

func (c Conditions) has(name string) bool {
	if name == "default" {
		return true
	}
	for _, n := range c {
		if n == name {
			return true
		}
	}
	return false
}

// withRequire widens the set with "require" for the CJS-only fallback pass.
func (c Conditions) withRequire() Conditions {
	return append(append(Conditions(nil), c...), "require")
}

// resolveExports resolves a subpath against a package's `exports` field,
// following Node's package exports algorithm: exact subpath keys first, then
// the best wildcard pattern, with conditions matched in the object's key order.
func resolveExports(pkgDir string, exports json.RawMessage, sub string, conds Conditions) string {
	if file := resolveExportsWith(pkgDir, exports, sub, conds); file != "" {
		return file
	}
	// CJS-only packages describe themselves with "require" alone; fall back to
	// them last, as earlier releases did.
	return resolveExportsWith(pkgDir, exports, sub, conds.withRequire())
}

func resolveExportsWith(pkgDir string, exports json.RawMessage, sub string, conds Conditions) string {
	keys, m := orderedObject(exports)
	subpathMap := false
	for _, k := range keys {
		if strings.HasPrefix(k, ".") {
			subpathMap = true
			break
		}
	}
	if !subpathMap {
		// A string, array or bare conditions object describes the root only.
		if sub != "" {
			return ""
		}
		return targetFile(pkgDir, exports, "", conds)
	}

	key := "."
	if sub != "" {
		key = "./" + sub
	}
	if raw, ok := m[key]; ok && !strings.Contains(key, "*") {
		return targetFile(pkgDir, raw, "", conds)
	}

	bestKey, bestMid := "", ""
	for _, pattern := range keys {
		star := strings.IndexByte(pattern, '*')
		if star < 0 {
			continue
		}
		prefix, suffix := pattern[:star], pattern[star+1:]
		if len(key) < len(pattern)-1 || !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) {
			continue
		}
		if bestKey == "" || patternKeyLess(pattern, bestKey) {
			bestKey = pattern
			bestMid = key[len(prefix) : len(key)-len(suffix)]
		}
	}
	if bestKey == "" {
		return ""
	}
	return targetFile(pkgDir, m[bestKey], bestMid, conds)
}

// patternKeyLess reports whether pattern a is a better match than b under
// Node's PATTERN_KEY_COMPARE: the longer literal prefix wins, then the longer key.
func patternKeyLess(a, b string) bool {
	ai, bi := strings.IndexByte(a, '*'), strings.IndexByte(b, '*')
	if ai != bi {
		return ai > bi
	}
	if len(a) != len(b) {
		return len(a) > len(b)
	}
	return a < b
}

// orderedObject returns the keys of a JSON object in source order plus the raw
// values. A non-object yields no keys.
func orderedObject(raw json.RawMessage) ([]string, map[string]json.RawMessage) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, nil
	}
	var keys []string
	vals := map[string]json.RawMessage{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, nil
		}
		k, _ := kt.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil
		}
		if _, dup := vals[k]; !dup {
			keys = append(keys, k)
		}
		vals[k] = v
	}
	return keys, vals
}

// targetFile resolves an exports/imports target to a file under pkgDir. mid
// replaces "*" in the target for pattern keys.
func targetFile(pkgDir string, raw json.RawMessage, mid string, conds Conditions) string {
	target, ok := pickTarget(raw, conds)
	if !ok || target == "" {
		return ""
	}
	if mid != "" || strings.Contains(target, "*") {
		target = strings.ReplaceAll(target, "*", mid)
	}
	return resolveFile(filepath.Join(pkgDir, target))
}

// pickTarget walks a target value: a string is the answer, an array is tried in
// order, and an object is scanned in key order for the first active condition.
// An explicit null blocks the path (ok is false, and callers stop).
func pickTarget(raw json.RawMessage, conds Conditions) (string, bool) {
	t := strings.TrimSpace(string(raw))
	switch {
	case t == "" || t == "null":
		return "", false
	case strings.HasPrefix(t, "\""):
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return "", false
		}
		return s, true
	case strings.HasPrefix(t, "["):
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) != nil {
			return "", false
		}
		for _, el := range arr {
			if s, ok := pickTarget(el, conds); ok {
				return s, true
			}
		}
		return "", false
	case strings.HasPrefix(t, "{"):
		keys, m := orderedObject(raw)
		for _, k := range keys {
			if !conds.has(k) {
				continue
			}
			v := strings.TrimSpace(string(m[k]))
			if v == "null" {
				return "", false
			}
			if s, ok := pickTarget(m[k], conds); ok {
				return s, true
			}
		}
	}
	return "", false
}

// resolveFile resolves a candidate path to an existing file, trying the path as
// given, then with source extensions, then as a directory index.
func resolveFile(candidate string) string {
	candidate = filepath.Clean(candidate)
	// An exact file wins, then the path plus an extension, then a directory
	// index (Node and tsc order), so Button.tsx beats a Button/ directory.
	s := StatPath(candidate)
	if s.Exists && !s.IsDir {
		return candidate
	}
	for _, ext := range sourceExtensions {
		p := candidate + ext
		if Exists(p) {
			return p
		}
	}
	if s.Exists && s.IsDir {
		for _, idx := range indexFiles {
			p := filepath.Join(candidate, idx)
			if Exists(p) {
				return p
			}
		}
	}
	return ""
}

// readPkgJSON reads a package's package.json, returning an empty struct when it
// is missing or unparseable. Results are memoized per build.
func readPkgJSON(pkgDir string) PkgJSON {
	p := filepath.Join(pkgDir, "package.json")
	if cacheEnabled {
		cacheMu.Lock()
		if cached, ok := pkgMemo[p]; ok {
			cacheMu.Unlock()
			return cached
		}
		cacheMu.Unlock()
	}

	var pkg PkgJSON
	if data, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(data, &pkg)
	}

	if cacheEnabled {
		cacheMu.Lock()
		pkgMemo[p] = pkg
		cacheMu.Unlock()
	}
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
	return resolvePackage(pkgDir, "", defaultConditions)
}

// IsCompiledDependency reports whether path is a file of a compiled third-party
// package under node_modules (as opposed to project or Krate source). Krate
// scope packages, packages declaring a "krate" field, and packages that ship
// TypeScript source are Krate source and return false.
func IsCompiledDependency(path string) bool {
	norm := filepath.ToSlash(path)
	i := strings.LastIndex(norm, "/node_modules/")
	if i < 0 {
		return false
	}
	rest := norm[i+len("/node_modules/"):]
	if strings.HasPrefix(rest, "@krate/") {
		return false
	}
	parts := strings.Split(rest, "/")
	n := 1
	if strings.HasPrefix(rest, "@") {
		n = 2
	}
	if len(parts) <= n {
		return true
	}
	pkgDir := filepath.FromSlash(norm[:i+len("/node_modules/")] + strings.Join(parts[:n], "/"))
	if len(readPkgJSON(pkgDir).Krate) > 0 {
		return false
	}
	switch strings.ToLower(filepath.Ext(norm)) {
	case ".tsx", ".ts", ".mts", ".cts":
		return strings.HasSuffix(norm, ".d.ts")
	}
	return true
}
