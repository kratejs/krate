package build

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/jseval"
)

// nativeAddons collects the packages that own a .node file the bundler tried
// to load.
type nativeAddons struct {
	mu   sync.Mutex
	pkgs map[string]bool
}

func (n *nativeAddons) add(pkg string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.pkgs == nil {
		n.pkgs = map[string]bool{}
	}
	n.pkgs[pkg] = true
}

func (n *nativeAddons) list() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]string, 0, len(n.pkgs))
	for p := range n.pkgs {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// ownerPackage is the npm package a file inside node_modules belongs to, or ""
// when the file is not inside one.
func ownerPackage(path string) string {
	p := filepath.ToSlash(path)
	i := strings.LastIndex(p, "/node_modules/")
	if i < 0 {
		return ""
	}
	parts := strings.Split(p[i+len("/node_modules/"):], "/")
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	if strings.HasPrefix(parts[0], "@") {
		if len(parts) < 2 {
			return ""
		}
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

// packageName is the package part of an import specifier ("a/b" -> "a",
// "@s/a/b" -> "@s/a"). Relative, absolute and node: specifiers give "".
func packageName(spec string) string {
	if spec == "" || strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") || strings.HasPrefix(spec, "node:") || strings.Contains(spec, ":") {
		return ""
	}
	parts := strings.Split(spec, "/")
	if strings.HasPrefix(parts[0], "@") {
		if len(parts) < 2 {
			return ""
		}
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

// nativeAddonPlugin handles .node files, which esbuild has no loader for. It
// records the owning package and fails the load with a message that names it;
// the caller either retries with that package external or reports the error.
// explain turns the owning package ("" when none) into the error text.
func nativeAddonPlugin(found *nativeAddons, explain func(pkg, file string) string) api.Plugin {
	return api.Plugin{
		Name: "krate-native-addon",
		Setup: func(b api.PluginBuild) {
			b.OnLoad(api.OnLoadOptions{Filter: `\.node$`}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				pkg := ownerPackage(args.Path)
				if pkg != "" {
					found.add(pkg)
				}
				return api.OnLoadResult{}, fmt.Errorf("%s", explain(pkg, args.Path))
			})
		},
	}
}

// pathAliasPlugin resolves tsconfig path aliases ("@/components") for bundles
// that set TsconfigRaw, which makes esbuild ignore the project's tsconfig.json.
// It must come before externalNodeModulesPlugin, which would otherwise take an
// alias for an npm package. Importers inside node_modules are left alone.
func pathAliasPlugin(res *bundler.ImportResolver) api.Plugin {
	return api.Plugin{
		Name: "krate-path-alias",
		Setup: func(b api.PluginBuild) {
			b.OnResolve(api.OnResolveOptions{Filter: res.AliasFilter()}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if strings.Contains(filepath.ToSlash(args.Importer), "/node_modules/") {
					return api.OnResolveResult{}, nil
				}
				if p := res.ResolveAlias(args.Path); p != "" {
					return api.OnResolveResult{Path: p}, nil
				}
				return api.OnResolveResult{}, nil
			})
		},
	}
}

// packageRootOf is the directory of the package a file inside node_modules
// belongs to, or "" when the file is not inside one.
func packageRootOf(path string) string {
	p := filepath.ToSlash(path)
	i := strings.LastIndex(p, "/node_modules/")
	if i < 0 {
		return ""
	}
	name := ownerPackage(path)
	if name == "" {
		return ""
	}
	return filepath.FromSlash(p[:i+len("/node_modules/")] + name)
}

// visibleFrom reports whether Node, importing the bare package name from a file
// in dir, would load the package installed at pkgRoot. A workspace package's
// own dependencies live in its node_modules, which is not on the path of an
// output directory that belongs to another package.
func visibleFrom(dir, pkgRoot string) bool {
	name := ownerPackage(filepath.Join(pkgRoot, "x"))
	want, err := filepath.EvalSymlinks(pkgRoot)
	if err != nil || name == "" {
		return true
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return true
	}
	for {
		if got, err := filepath.EvalSymlinks(filepath.Join(dir, "node_modules", filepath.FromSlash(name))); err == nil && got == want {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// externalNodeModulesPlugin is api.PackagesExternal that still bundles source:
// a bare import that resolves to a file outside node_modules (a workspace
// package symlinked into node_modules, or a tsconfig path alias) is bundled,
// because Node could not load its TypeScript source at run time. Imports that
// resolve into node_modules and built-ins stay external. A package that Node
// could not find from outFile (a dependency of a workspace package, which pnpm
// installs only under that package) is imported by the file URL of the file
// esbuild resolved, so the bundle loads it from where it is installed. A bare
// import that does not resolve is left to esbuild, which reports it.
func externalNodeModulesPlugin(outFile string) api.Plugin {
	return externalNodeModulesPluginMemo(outFile, nil)
}

// resolveMemo remembers the resolutions the plugins of a family of builds
// (the same options, such as every page's server bundle) ask esbuild for, so
// each import is resolved once for the family instead of once per build.
type resolveMemo struct {
	m sync.Map // resolveMemoKey -> resolveMemoValue, or *resolveMemoEntry when native
	// native marks a memo whose resolutions are esbuild's own: the resolve it
	// wraps reaches no plugin that answers it, and none that enters the memo
	// again. esbuild resolves a path from its directory, its kind and its
	// import attributes, never from the importer, so such a memo keys by those
	// alone, and a build that asks while another build of the family resolves
	// the same key waits for that answer.
	native bool
}

// newNativeResolveMemo is a native resolveMemo (see resolveMemo.native).
func newNativeResolveMemo() *resolveMemo { return &resolveMemo{native: true} }

type resolveMemoKey struct {
	importer, namespace, dir, path, with string
	kind                                 api.ResolveKind
}

type resolveMemoValue struct {
	path, namespace, suffix string
	external, failed        bool
}

// resolveMemoEntry is a resolution of a native memo, ready once done is
// closed.
type resolveMemoEntry struct {
	done chan struct{}
	v    resolveMemoValue
}

func memoKey(args api.OnResolveArgs) resolveMemoKey {
	var with string
	if len(args.With) > 0 {
		keys := make([]string, 0, len(args.With))
		for k, v := range args.With {
			keys = append(keys, k+"="+v)
		}
		sort.Strings(keys)
		with = strings.Join(keys, "\x00")
	}
	return resolveMemoKey{importer: args.Importer, namespace: args.Namespace, dir: args.ResolveDir, path: args.Path, with: with, kind: args.Kind}
}

// resolve returns resolve(args), from the memo when the family resolved the
// same import before. A nil memo always resolves.
func (m *resolveMemo) resolve(args api.OnResolveArgs, resolve func() api.ResolveResult) resolveMemoValue {
	if m == nil {
		return memoValue(resolve())
	}
	key := memoKey(args)
	if m.native {
		return m.resolveNative(key, resolve)
	}
	if v, ok := m.m.Load(key); ok {
		return v.(resolveMemoValue)
	}
	v := memoValue(resolve())
	m.m.Store(key, v)
	return v
}

// resolveNative is resolve for a native memo.
func (m *resolveMemo) resolveNative(key resolveMemoKey, resolve func() api.ResolveResult) resolveMemoValue {
	key.importer = ""
	if e, ok := m.m.Load(key); ok {
		e := e.(*resolveMemoEntry)
		<-e.done
		return e.v
	}
	e := &resolveMemoEntry{done: make(chan struct{})}
	if prev, loaded := m.m.LoadOrStore(key, e); loaded {
		prev := prev.(*resolveMemoEntry)
		<-prev.done
		return prev.v
	}
	// A resolve that panics leaves the waiters a failed resolution, which
	// makes the plugin leave the import to esbuild.
	e.v.failed = true
	defer close(e.done)
	e.v = memoValue(resolve())
	return e.v
}

func memoValue(r api.ResolveResult) resolveMemoValue {
	return resolveMemoValue{path: r.Path, namespace: r.Namespace, suffix: r.Suffix, external: r.External, failed: len(r.Errors) > 0}
}

// externalNodeModulesPluginMemo is externalNodeModulesPlugin for a family of
// builds with the same options, which share their resolutions through memo.
func externalNodeModulesPluginMemo(outFile string, memo *resolveMemo) api.Plugin {
	var dirs []string
	if outFile != "" {
		dirs = []string{filepath.Dir(outFile)}
	}
	return externalNodeModulesPluginDirs(dirs, memo, nil)
}

// externalNodeModulesPluginDirs is externalNodeModulesPluginMemo for a build
// with an output file in each of outDirs (none for no output file). A package
// that some of them see in node_modules and others do not sets mixed: the
// build cannot import it the way each separate build would, and its output
// must not be used.
func externalNodeModulesPluginDirs(outDirs []string, memo *resolveMemo, mixed *atomic.Bool) api.Plugin {
	const marker = "krate-external-check"
	outDirs = slices.Compact(slices.Sorted(slices.Values(outDirs)))
	var visible sync.Map // package root -> bool
	return api.Plugin{
		Name: "krate-external-node-modules",
		Setup: func(b api.PluginBuild) {
			jseval.RegisterCloudflareImports(b)
			// Krate renders without react-dom: the resource hints and flushSync
			// of its server module stand in for it.
			b.OnResolve(api.OnResolveOptions{Filter: `^react-dom$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.Kind == api.ResolveJSRequireCall {
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: "@krate/runtime/hints", External: true}, nil
			})
			// A specifier with a colon (node:, https:, data:) is never handled.
			b.OnResolve(api.OnResolveOptions{Filter: externalPackageFilter}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.PluginData == marker || strings.Contains(args.Path, ":") {
					return api.OnResolveResult{}, nil
				}
				r := memo.resolve(args, func() api.ResolveResult {
					return b.Resolve(args.Path, api.ResolveOptions{
						Importer:   args.Importer,
						Namespace:  args.Namespace,
						ResolveDir: args.ResolveDir,
						Kind:       args.Kind,
						PluginData: marker,
					})
				})
				if r.external {
					return api.OnResolveResult{Path: args.Path, External: true}, nil
				}
				if r.failed {
					return api.OnResolveResult{}, nil
				}
				if strings.Contains(filepath.ToSlash(r.path), "/node_modules/") {
					if root := packageRootOf(r.path); root != "" && len(outDirs) > 0 {
						v, ok := visible.Load(root)
						if !ok {
							vis := visibleFrom(outDirs[0], root)
							for _, d := range outDirs[1:] {
								if mixed != nil && visibleFrom(d, root) != vis {
									mixed.Store(true)
								}
							}
							v, _ = visible.LoadOrStore(root, vis)
						}
						if !v.(bool) {
							return api.OnResolveResult{Path: fileURL(r.path), External: true}, nil
						}
					}
					return api.OnResolveResult{Path: args.Path, External: true}, nil
				}
				return api.OnResolveResult{Path: r.path, Namespace: r.namespace}, nil
			})
		},
	}
}

// fileURL is the file: URL of an absolute path.
func fileURL(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

func externalPatterns(pkgs []string) []string {
	out := make([]string, 0, len(pkgs)*2)
	for _, p := range pkgs {
		out = append(out, p, p+"/*")
	}
	return out
}

// buildExternalizingNative runs a server-side bundle in which the packages in
// extra, and every package that turns out to contain a native addon, stay
// external: Node loads them from node_modules at run time. The packages added
// because of a native addon are returned. A bundle that still fails (an addon
// in the project's own files) reports the error from nativeAddonPlugin.
func buildExternalizingNative(opts api.BuildOptions, extra []string) (api.BuildResult, []string) {
	externals := append([]string(nil), extra...)
	var detected []string
	var res api.BuildResult
	for attempt := 0; attempt < 5; attempt++ {
		found := &nativeAddons{}
		res = esbuildBuild(externalizingNativeOptions(opts, externals, found))
		pkgs := found.list()
		if len(res.Errors) == 0 || len(pkgs) == 0 {
			return res, detected
		}
		for _, p := range pkgs {
			externals = append(externals, p)
			detected = append(detected, p)
		}
	}
	return res, detected
}

// externalizingNativeOptions are the options of a buildExternalizingNative
// attempt with externals left external; found collects the native addons the
// attempt meets.
func externalizingNativeOptions(opts api.BuildOptions, externals []string, found *nativeAddons) api.BuildOptions {
	o := opts
	o.External = append(append([]string(nil), opts.External...), externalPatterns(externals)...)
	o.Plugins = append(append([]api.Plugin(nil), opts.Plugins...), nativeAddonPlugin(found, func(pkg, file string) string {
		if pkg == "" {
			return fmt.Sprintf("%s is a native addon in your own sources and cannot be bundled; load it with createRequire(import.meta.url) from a package listed in serverExternalPackages", file)
		}
		return fmt.Sprintf("package %q contains a native addon (%s) and cannot be bundled; add it to serverExternalPackages in krate.config", pkg, filepath.Base(file))
	}))
	return o
}

// externalPackagesFromMetafile lists the packages a bundle still imports at run
// time (external imports that are not Node built-ins).
func externalPackagesFromMetafile(meta string) []string {
	if meta == "" {
		return nil
	}
	var m struct {
		Outputs map[string]struct {
			Imports []struct {
				Path     string `json:"path"`
				External bool   `json:"external"`
			} `json:"imports"`
		} `json:"outputs"`
	}
	if json.Unmarshal([]byte(meta), &m) != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, out := range m.Outputs {
		for _, imp := range out.Imports {
			if !imp.External {
				continue
			}
			name := packageName(imp.Path)
			if name == "" || nodeBuiltinNames[name] || strings.HasPrefix(name, "@krate/") {
				continue
			}
			seen[name] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// nativeAddonHint adds the way out to an esbuild "no loader for .node" message
// from a bundle that cannot keep packages external (middleware, the embedded
// runtime).
func nativeAddonHint(text string) string {
	if strings.Contains(text, `".node"`) && strings.Contains(text, "No loader") {
		return text + " (middleware is bundled into one file and cannot load a native addon, and the embedded QuickJS runtime cannot run one at all; call the addon from an API route instead: routes that import a native package run on the Node sidecar automatically)"
	}
	return text
}
