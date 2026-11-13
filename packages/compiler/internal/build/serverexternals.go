package build

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
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

// externalNodeModulesPlugin is api.PackagesExternal that still bundles source:
// a bare import that resolves to a file outside node_modules (a workspace
// package symlinked into node_modules, or a tsconfig path alias) is bundled,
// because Node could not load its TypeScript source at run time. Imports that
// resolve into node_modules, built-ins and unresolvable names stay external.
func externalNodeModulesPlugin() api.Plugin {
	const marker = "krate-external-check"
	return api.Plugin{
		Name: "krate-external-node-modules",
		Setup: func(b api.PluginBuild) {
			jseval.RegisterCloudflareImports(b)
			b.OnResolve(api.OnResolveOptions{Filter: `^[^./]`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.PluginData == marker || strings.Contains(args.Path, ":") {
					return api.OnResolveResult{}, nil
				}
				r := b.Resolve(args.Path, api.ResolveOptions{
					Importer:   args.Importer,
					Namespace:  args.Namespace,
					ResolveDir: args.ResolveDir,
					Kind:       args.Kind,
					PluginData: marker,
				})
				if len(r.Errors) > 0 || r.External {
					return api.OnResolveResult{Path: args.Path, External: true}, nil
				}
				if strings.Contains(filepath.ToSlash(r.Path), "/node_modules/") {
					return api.OnResolveResult{Path: args.Path, External: true}, nil
				}
				return api.OnResolveResult{Path: r.Path, Namespace: r.Namespace}, nil
			})
		},
	}
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
		o := opts
		o.External = append(append([]string(nil), opts.External...), externalPatterns(externals)...)
		o.Plugins = append(append([]api.Plugin(nil), opts.Plugins...), nativeAddonPlugin(found, func(pkg, file string) string {
			if pkg == "" {
				return fmt.Sprintf("%s is a native addon in your own sources and cannot be bundled; load it with createRequire(import.meta.url) from a package listed in serverExternalPackages", file)
			}
			return fmt.Sprintf("package %q contains a native addon (%s) and cannot be bundled; add it to serverExternalPackages in krate.config", pkg, filepath.Base(file))
		}))
		res = esbuildBuild(o)
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
