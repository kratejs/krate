package build

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
)

// Node's ES module loader sees the named exports of a CommonJS module only
// through cjs-module-lexer, which misses most bundled CommonJS (ncc builds such
// as @next/env, webpack bundles): `import { loadEnvConfig } from '@next/env'`
// in a server bundle that keeps the package external fails at load time with
// "Named export 'loadEnvConfig' not found". Server bundles therefore import an
// external package that Node loads as CommonJS through a shim: its default
// import (module.exports, which always works) wrapped in a CommonJS module, so
// esbuild reads named imports off module.exports at run time. The default
// import keeps Node's meaning: module.exports.
const (
	cjsInteropNS    = "krate-cjs-interop"
	cjsInteropESMNS = "krate-cjs-interop-esm"
)

// cjsInteropApplies reports whether a build loads in Node as an ES module, the
// only output that needs the shim.
func cjsInteropApplies(b api.PluginBuild) bool {
	o := b.InitialOptions
	return o != nil && o.Format == api.FormatESModule && o.Platform == api.PlatformNode
}

// registerCJSInterop adds the shim modules to a build. The outer module is
// CommonJS and requires the inner ES module, whose default import of spec is
// the external import left in the output.
func registerCJSInterop(b api.PluginBuild) {
	b.OnResolve(api.OnResolveOptions{Filter: `.*`, Namespace: cjsInteropNS}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
		return api.OnResolveResult{Path: args.Path, Namespace: cjsInteropESMNS}, nil
	})
	b.OnResolve(api.OnResolveOptions{Filter: `.*`, Namespace: cjsInteropESMNS}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
		return api.OnResolveResult{Path: args.Path, External: true}, nil
	})
	b.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: cjsInteropNS}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
		// Node's default import is module.exports even when it carries
		// __esModule, which esbuild's interop would replace by its .default:
		// such an object is wrapped so the default stays module.exports.
		src := "var m = require(" + jsString(args.Path) + ").default;\n" +
			"module.exports = m != null && (typeof m === 'object' || typeof m === 'function') && m.__esModule ? Object.create(m, { default: { value: m, enumerable: true } }) : m;\n"
		return api.OnLoadResult{Contents: &src, Loader: api.LoaderJS}, nil
	})
	b.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: cjsInteropESMNS}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
		src := "import m from " + jsString(args.Path) + ";\nexport default m;\n"
		return api.OnLoadResult{Contents: &src, Loader: api.LoaderJS}, nil
	})
}

// cjsInteropResult routes an import statement of spec, which stays external,
// through the shim.
func cjsInteropResult(spec string) api.OnResolveResult {
	return api.OnResolveResult{Path: spec, Namespace: cjsInteropNS}
}

// cjsExternalInteropPlugin applies the shim to the packages a build keeps
// external by name (serverExternalPackages and native addons, see
// buildExternalizingNative), which esbuild's external option would otherwise
// leave as plain imports. It must come before the plugins that resolve them.
func cjsExternalInteropPlugin(externals []string) api.Plugin {
	return api.Plugin{
		Name: "krate-cjs-external-interop",
		Setup: func(b api.PluginBuild) {
			if len(externals) == 0 || !cjsInteropApplies(b) {
				return
			}
			registerCJSInterop(b)
			det := &cjsDetector{}
			b.OnResolve(api.OnResolveOptions{Filter: externalPackageFilter}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.Kind != api.ResolveJSImportStatement || (args.Namespace != "file" && args.Namespace != "") {
					return api.OnResolveResult{}, nil
				}
				pkg := packageName(args.Path)
				if pkg == "" || !slices.Contains(externals, pkg) {
					return api.OnResolveResult{}, nil
				}
				root := nodePackageRoot(args.ResolveDir, pkg)
				if root == "" || !det.packageImportIsCommonJS(root, strings.TrimPrefix(args.Path, pkg)) {
					return api.OnResolveResult{}, nil
				}
				return cjsInteropResult(args.Path), nil
			})
		},
	}
}

// nodePackageRoot is the directory Node loads package pkg from for a file in
// dir, or "" when no node_modules on the way up has it.
func nodePackageRoot(dir, pkg string) string {
	for dir != "" {
		p := filepath.Join(dir, "node_modules", filepath.FromSlash(pkg))
		if regularFileExists(filepath.Join(p, "package.json")) {
			if real, err := filepath.EvalSymlinks(p); err == nil {
				return real
			}
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// cjsDetector answers, once per package and subpath, whether Node's ES module
// loader would load an import as CommonJS.
type cjsDetector struct {
	m sync.Map // key -> bool
}

// packageImportIsCommonJS reports whether `import ... from '<pkg><sub>'`
// loads a CommonJS file in Node, where pkgRoot is the package's directory and
// sub is "" or "/subpath". It resolves the way Node does for an import:
// package.json "exports" with the node, import, module-sync and default
// conditions, else "main" (or index.js) with the CommonJS extension search.
// Anything it cannot resolve counts as not CommonJS, which leaves the import
// as it is.
func (d *cjsDetector) packageImportIsCommonJS(pkgRoot, sub string) bool {
	key := pkgRoot + "\x00" + sub
	if v, ok := d.m.Load(key); ok {
		return v.(bool)
	}
	file := nodeImportTarget(pkgRoot, sub)
	v := file != "" && fileIsCommonJS(file)
	d.m.Store(key, v)
	return v
}

// fileIsCommonJS reports whether a file is CommonJS by the file name file
// (a file: URL import), memoized.
func (d *cjsDetector) fileIsCommonJS(file string) bool {
	key := "\x00" + file
	if v, ok := d.m.Load(key); ok {
		return v.(bool)
	}
	v := fileIsCommonJS(file)
	d.m.Store(key, v)
	return v
}

// nodeImportTarget is the file an import of sub ("" or "/x") from the package
// at pkgRoot loads, or "" when it does not resolve.
func nodeImportTarget(pkgRoot, sub string) string {
	data, err := os.ReadFile(filepath.Join(pkgRoot, "package.json"))
	if err != nil {
		return ""
	}
	var pj struct {
		Main    string          `json:"main"`
		Exports json.RawMessage `json:"exports"`
	}
	if json.Unmarshal(data, &pj) != nil {
		return ""
	}
	if len(pj.Exports) > 0 && string(pj.Exports) != "null" {
		exports, err := decodeOrderedJSON(pj.Exports)
		if err != nil {
			return ""
		}
		target, ok := resolvePackageExports(exports, "."+sub)
		if !ok {
			return ""
		}
		return filepath.Join(pkgRoot, filepath.FromSlash(target))
	}
	base := strings.TrimPrefix(sub, "/")
	if base == "" {
		base = pj.Main
		if base == "" {
			base = "index.js"
		}
	}
	p := filepath.Join(pkgRoot, filepath.FromSlash(base))
	for _, c := range []string{p, p + ".js", p + ".json", filepath.Join(p, "index.js")} {
		if regularFileExists(c) {
			return c
		}
	}
	return ""
}

// nodeImportConditions are the conditions Node matches for an import.
var nodeImportConditions = map[string]bool{"node": true, "import": true, "module-sync": true, "node-addons": true, "default": true}

// resolvePackageExports resolves subpath ("." or "./x") against an "exports"
// value, following Node's PACKAGE_EXPORTS_RESOLVE.
func resolvePackageExports(exports orderedJSON, subpath string) (string, bool) {
	isMap := exports.obj != nil && len(exports.keys) > 0 && strings.HasPrefix(exports.keys[0], ".")
	if !isMap {
		if subpath != "." {
			return "", false
		}
		return resolvePackageTarget(exports, "")
	}
	for i, k := range exports.keys {
		if k == subpath && !strings.Contains(k, "*") {
			return resolvePackageTarget(exports.obj[i], "")
		}
	}
	best, bestStar := -1, ""
	for i, k := range exports.keys {
		star := strings.Index(k, "*")
		if star < 0 || strings.Count(k, "*") != 1 {
			continue
		}
		prefix, suffix := k[:star], k[star+1:]
		if !strings.HasPrefix(subpath, prefix) || subpath == prefix || !strings.HasSuffix(subpath, suffix) || len(subpath) < len(k) {
			continue
		}
		if best < 0 || patternKeyLess(exports.keys[best], k) {
			best, bestStar = i, subpath[len(prefix):len(subpath)-len(suffix)]
		}
	}
	if best < 0 {
		return "", false
	}
	return resolvePackageTarget(exports.obj[best], bestStar)
}

// patternKeyLess reports whether pattern key b is more specific than a (Node's
// PATTERN_KEY_COMPARE): the longer prefix wins, then the longer key.
func patternKeyLess(a, b string) bool {
	pa, pb := strings.Index(a, "*"), strings.Index(b, "*")
	if pa != pb {
		return pb > pa
	}
	return len(b) > len(a)
}

// resolvePackageTarget resolves one target of "exports" with Node's import
// conditions; star replaces "*" in a pattern target.
func resolvePackageTarget(t orderedJSON, star string) (string, bool) {
	switch {
	case t.isStr:
		if !strings.HasPrefix(t.str, "./") {
			return "", false
		}
		return strings.ReplaceAll(t.str, "*", star), true
	case t.arr != nil:
		for _, e := range t.arr {
			if r, ok := resolvePackageTarget(e, star); ok {
				return r, true
			}
		}
		return "", false
	case t.obj != nil:
		for i, k := range t.keys {
			if !nodeImportConditions[k] {
				continue
			}
			v := t.obj[i]
			if v.null {
				return "", false
			}
			if r, ok := resolvePackageTarget(v, star); ok {
				return r, true
			}
		}
	}
	return "", false
}

// esmSyntax matches a line that starts with an import or export statement:
// Node loads a .js file outside a "type": "module" package as an ES module
// when it has ES module syntax.
var esmSyntax = regexp.MustCompile(`(?m)^[ \t]*(?:import[ \t]*[\w{*'"]|export[ \t]+[\w{*]|export[ \t]*\{)`)

// esmSyntaxCache remembers, across builds, which files esmSyntax matched,
// keyed by path and validated by size and modification time.
var esmSyntaxCache sync.Map // path -> esmSyntaxEntry

type esmSyntaxEntry struct {
	size    int64
	modTime int64
	esm     bool
}

// fileIsCommonJS reports whether Node loads file as CommonJS.
func fileIsCommonJS(file string) bool {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".cjs":
		return true
	case ".js", "":
	default:
		return false
	}
	if nearestPackageType(filepath.Dir(file)) == "module" {
		return false
	}
	st, err := os.Stat(file)
	if err != nil {
		return false
	}
	if v, ok := esmSyntaxCache.Load(file); ok {
		if e := v.(esmSyntaxEntry); e.size == st.Size() && e.modTime == st.ModTime().UnixNano() {
			return !e.esm
		}
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	esm := esmSyntax.Match(data)
	esmSyntaxCache.Store(file, esmSyntaxEntry{size: st.Size(), modTime: st.ModTime().UnixNano(), esm: esm})
	return !esm
}

// nearestPackageType is the "type" of the package.json nearest to dir.
func nearestPackageType(dir string) string {
	for {
		if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
			var pj struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(data, &pj)
			return pj.Type
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// orderedJSON is a JSON value whose objects keep their key order, which
// "exports" conditions depend on.
type orderedJSON struct {
	isStr bool
	str   string
	null  bool
	arr   []orderedJSON
	keys  []string
	obj   []orderedJSON // values of keys; non-nil for an object
}

func decodeOrderedJSON(data []byte) (orderedJSON, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	v, err := decodeOrderedValue(dec)
	if err != nil {
		return orderedJSON{}, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return orderedJSON{}, errors.New("trailing data")
	}
	return v, nil
}

func decodeOrderedValue(dec *json.Decoder) (orderedJSON, error) {
	tok, err := dec.Token()
	if err != nil {
		return orderedJSON{}, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			v := orderedJSON{obj: []orderedJSON{}}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return orderedJSON{}, err
				}
				k, _ := kt.(string)
				val, err := decodeOrderedValue(dec)
				if err != nil {
					return orderedJSON{}, err
				}
				v.keys = append(v.keys, k)
				v.obj = append(v.obj, val)
			}
			_, err := dec.Token()
			return v, err
		case '[':
			v := orderedJSON{arr: []orderedJSON{}}
			for dec.More() {
				e, err := decodeOrderedValue(dec)
				if err != nil {
					return orderedJSON{}, err
				}
				v.arr = append(v.arr, e)
			}
			_, err := dec.Token()
			return v, err
		}
	case string:
		return orderedJSON{isStr: true, str: t}, nil
	case nil:
		return orderedJSON{null: true}, nil
	}
	return orderedJSON{}, nil
}
