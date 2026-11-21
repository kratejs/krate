package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
)

// apiRouteFamily is what the routes of one CompileAPIRoutes call share: the
// resolutions of their Node bundles and detection builds, and the package
// probes of provablyNodeOnly.
type apiRouteFamily struct {
	node, shallow resolveMemo
	probes        sync.Map // package entry file -> *packageProbe
}

type packageProbe struct {
	once     sync.Once
	external bool
	native   []string // packages buildExternalizingNative made external
}

// probePkgPrefix marks the package imports a detection build leaves external,
// as probePkgPrefix + package name + ":" + resolved file.
const probePkgPrefix = "krate-probe-pkg:"

// provablyNodeOnly reports whether the QuickJS bundle that
// buildExternalizingNative(opts, extra) would produce is certain to keep an
// external import, which makes the route Node-only, without bundling the
// route's npm packages. false means not proven; the caller then builds the
// full bundle.
//
// It builds the route with every import that resolves into node_modules left
// external, so only the project's own files are bundled. Their tree shaking
// matches the full bundle: packages never import project files, and the
// imports of other externals are untouched. An external import that survives
// in this output therefore survives in the full bundle. For a package import
// that survives, the full bundle includes the package entry when it has side
// effects, so a probe that imports just that entry (apiRouteFamily.probe)
// keeps a subset of the full bundle's external imports. And when the probe
// finds a native addon in the imported package itself, the full bundle finds
// it too and leaves the import external.
func provablyNodeOnly(opts api.BuildOptions, extra []string, fam *apiRouteFamily) bool {
	res := esbuildBuild(detectionOptions(opts, extra, fam))
	if len(res.Errors) > 0 {
		return false
	}
	return provenByDetection(res.Metafile, opts, extra, fam)
}

// provablyNodeOnlyBatch runs the detection builds of provablyNodeOnly for
// entries, routes that share opts, in one call. A route's kept imports come
// from its own output, which matches its separate detection build. proven is
// nil when the batch fails; the caller then runs provablyNodeOnly per route.
func provablyNodeOnlyBatch(opts api.BuildOptions, extra []string, fam *apiRouteFamily, outdir string, entries []fileBatchEntry) (proven []bool) {
	outs, _ := esbuildBatchFiles(detectionOptions(opts, extra, fam), outdir, entries)
	if outs == nil {
		return nil
	}
	proven = make([]bool, len(entries))
	for i, out := range outs {
		proven[i] = provenByDetection(out.Metafile, opts, extra, fam)
	}
	return proven
}

// detectionOptions are the options of provablyNodeOnly's build for a route
// whose QuickJS options are opts.
func detectionOptions(opts api.BuildOptions, extra []string, fam *apiRouteFamily) api.BuildOptions {
	o := opts
	o.Write = false
	o.Sourcemap = api.SourceMapNone
	o.Metafile = true
	o.External = append(append([]string(nil), opts.External...), externalPatterns(extra)...)
	o.Plugins = append(append([]api.Plugin(nil), opts.Plugins...), packageImportsExternalPlugin(&fam.shallow))
	return o
}

// provenByDetection decides provablyNodeOnly from the metafile of a route's
// detection build.
func provenByDetection(meta string, opts api.BuildOptions, extra []string, fam *apiRouteFamily) bool {
	var pkgImports []string
	for _, imp := range outputImports(meta) {
		if !imp.External {
			continue
		}
		rest, ok := strings.CutPrefix(imp.Path, probePkgPrefix)
		if !ok {
			return true
		}
		switch imp.Kind {
		case "import-statement", "require-call", "dynamic-import":
			pkgImports = append(pkgImports, rest)
		}
	}
	for _, rest := range pkgImports {
		pkg, entry, ok := strings.Cut(rest, ":")
		if !ok {
			continue
		}
		if p := fam.probe(entry, opts, extra); p.external || slices.Contains(p.native, pkg) {
			return true
		}
	}
	return false
}

// probe bundles `import "entry"` for QuickJS, once per family: external
// reports whether the bundle keeps an external import (false when it fails).
func (f *apiRouteFamily) probe(entry string, opts api.BuildOptions, extra []string) *packageProbe {
	v, _ := f.probes.LoadOrStore(entry, &packageProbe{})
	p := v.(*packageProbe)
	p.once.Do(func() {
		o := opts
		o.EntryPoints = nil
		o.EntryPointsAdvanced = nil
		o.Outfile = ""
		o.Outdir = ""
		o.Write = false
		o.Sourcemap = api.SourceMapNone
		o.Metafile = true
		o.Stdin = &api.StdinOptions{
			Contents:   "import " + jsString(entry) + ";\n",
			ResolveDir: filepath.Dir(entry),
			Sourcefile: "krate-package-probe.js",
			Loader:     api.LoaderJS,
		}
		res, native := buildExternalizingNative(o, extra)
		p.native = native
		p.external = len(res.Errors) == 0 && hasExternalImport(res.Metafile)
	})
	return p
}

// packageImportsExternalPlugin leaves every bare import that resolves into
// node_modules external, under a probePkgPrefix path. Other imports are left
// to esbuild.
func packageImportsExternalPlugin(memo *resolveMemo) api.Plugin {
	const marker = "krate-probe-check"
	return api.Plugin{
		Name: "krate-package-imports-external",
		Setup: func(b api.PluginBuild) {
			b.OnResolve(api.OnResolveOptions{Filter: externalPackageFilter}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.PluginData == marker {
					return api.OnResolveResult{}, nil
				}
				r := memo.resolve(args, func() api.ResolveResult {
					return b.Resolve(args.Path, api.ResolveOptions{
						Importer:   args.Importer,
						Namespace:  args.Namespace,
						ResolveDir: args.ResolveDir,
						Kind:       args.Kind,
						PluginData: marker,
						With:       args.With,
					})
				})
				if r.external || r.failed || r.namespace != "file" || r.suffix != "" || !strings.Contains(filepath.ToSlash(r.path), "/node_modules/") {
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: probePkgPrefix + packageName(args.Path) + ":" + r.path, Namespace: "krate-probe-pkg", External: true}, nil
			})
		},
	}
}

type metafileImport struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	External bool   `json:"external"`
}

// outputImports lists the imports a metafile's outputs keep.
func outputImports(meta string) []metafileImport {
	var m struct {
		Outputs map[string]struct {
			Imports []metafileImport `json:"imports"`
		} `json:"outputs"`
	}
	if meta == "" || json.Unmarshal([]byte(meta), &m) != nil {
		return nil
	}
	var out []metafileImport
	for _, o := range m.Outputs {
		out = append(out, o.Imports...)
	}
	return out
}

// hasExternalImport reports whether a bundle's outputs keep an external import.
func hasExternalImport(meta string) bool {
	for _, imp := range outputImports(meta) {
		if imp.External {
			return true
		}
	}
	return false
}

// nodeOnlyStub is what a Node-only route's QuickJS artifact holds: the file
// lets the embedded runtime resolve the URL (krate serve then forwards it to
// the Node sidecar), and running it fails with a clear message.
func nodeOnlyStub(route string) []byte {
	return []byte("// Node-only API route: krate serve runs it on the Node sidecar.\nthrow new Error(" +
		jsString(route+" is a Node-only API route and cannot run in the embedded runtime; it is served by the Node sidecar") + ");\n")
}

// writeNodeOnlyStub replaces a route's QuickJS artifact with nodeOnlyStub.
func writeNodeOnlyStub(path, route string) error {
	removeStaleArtifact(path)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, nodeOnlyStub(route), 0644)
}
