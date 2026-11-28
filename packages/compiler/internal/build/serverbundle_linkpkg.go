package build

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// A client boundary stub exports, for each name, a wrapper of one boundary's
// render module (see writeClientBoundaryExports and realBoundaryExports). A
// page's own build stubs a module, or a package specifier, with the exports
// of its own boundaries; the linked build serves one stub for every page that
// shares it. That stub exports the union of the names, and is safe for a page
// when each name the page's own stub exports stands for the same boundary
// export. Extra names are harmless: the page's modules do not import them,
// and their render modules exist because another page needs them.

// boundaryExportSig is the code a boundary stub writes for the export name
// of cb: two stubs agree on a name when their signatures are equal.
func boundaryExportSig(cb ClientBoundary, name string) string {
	one := cb
	one.Exports = []string{name}
	var sb strings.Builder
	if cb.Real {
		realBoundaryExports(&sb, one, nil)
	} else {
		writeClientBoundaryExports(&sb, one, "", "", nil)
	}
	return sb.String()
}

// boundaryExports maps the export names of one stub to the boundaries that
// provide them, in the order the stub writes them.
type boundaryExports struct {
	names []string
	sig   map[string]string
	def   map[string]ClientBoundary
}

func newBoundaryExports() *boundaryExports {
	return &boundaryExports{sig: map[string]string{}, def: map[string]ClientBoundary{}}
}

// add adds the names of cb that are not taken yet (the first boundary wins a
// name, as in packageBoundaryStub).
func (u *boundaryExports) add(cb ClientBoundary) {
	for _, name := range cb.Exports {
		if _, ok := u.sig[name]; ok {
			continue
		}
		u.sig[name] = boundaryExportSig(cb, name)
		u.def[name] = cb
		u.names = append(u.names, name)
	}
}

// agrees reports whether every name of own stands for the same boundary
// export in u.
func (u *boundaryExports) agrees(own *boundaryExports) bool {
	for _, name := range own.names {
		if sig, ok := u.sig[name]; ok && sig != own.sig[name] {
			return false
		}
	}
	return true
}

// merge adds the names of own that u does not have.
func (u *boundaryExports) merge(own *boundaryExports) {
	for _, name := range own.names {
		if _, ok := u.sig[name]; ok {
			continue
		}
		u.sig[name] = own.sig[name]
		u.def[name] = own.def[name]
		u.names = append(u.names, name)
	}
}

// boundaries returns one boundary per render module of u, each with the
// names u takes from it, in the order of their first name.
func (u *boundaryExports) boundaries() []ClientBoundary {
	var out []ClientBoundary
	at := map[string]int{}
	for _, name := range u.names {
		cb := u.def[name]
		key := cb.ModuleID + "\x00" + strconv.FormatBool(cb.Real) + "\x00" + cb.ChunkURL + "\x00" + strconv.FormatBool(cb.hydrates())
		i, ok := at[key]
		if !ok {
			i = len(out)
			at[key] = i
			g := cb
			g.Exports = nil
			g.Hints = nil
			g.pending = nil
			out = append(out, g)
		}
		out[i].Exports = append(out[i].Exports, name)
		if tags, ok := cb.Hints[name]; ok {
			if out[i].Hints == nil {
				out[i].Hints = map[string][]string{}
			}
			out[i].Hints[name] = tags
		}
	}
	return out
}

// pagePackageBoundaries returns the package boundaries of page by
// specifier, as its own build stubs each specifier.
func pagePackageBoundaries(page *PageResult) map[string]*pkgBoundaries {
	out := map[string]*pkgBoundaries{}
	for _, cb := range page.ClientBoundaries {
		if cb.Spec == "" {
			continue
		}
		pb := out[cb.Spec]
		if pb == nil {
			pb = &pkgBoundaries{exports: newBoundaryExports()}
			out[cb.Spec] = pb
		}
		pb.cbs = append(pb.cbs, cb)
		pb.exports.add(cb)
	}
	return out
}

// pkgBoundaries are a page's boundaries for one package specifier.
type pkgBoundaries struct {
	cbs     []ClientBoundary
	exports *boundaryExports
}

// pageFileBoundaries returns the file boundaries of page by canonical path,
// as its own build stubs each module (the last boundary of a path wins, as
// in clientBoundaryPlugin).
func pageFileBoundaries(page *PageResult) map[string]*boundaryExports {
	out := map[string]*boundaryExports{}
	for _, cb := range page.ClientBoundaries {
		if cb.Spec != "" {
			continue
		}
		u := newBoundaryExports()
		u.add(cb)
		out[canonicalPath(cb.SourcePath)] = u
	}
	return out
}

// linkedPackageStub is packageBoundaryStub with the render modules imported
// through ssrBundlesPlaceholder.
func linkedPackageStub(cbs []ClientBoundary) string {
	var sb strings.Builder
	sb.WriteString(boundaryStubHeader)
	seen := map[string]bool{}
	for i, cb := range cbs {
		if cb.Real {
			realBoundaryExports(&sb, cb, seen)
			continue
		}
		writeClientBoundaryExports(&sb, cb, ssrBundlesPlaceholder+"/client/"+cb.ModuleID+".ssr.mjs", strconv.Itoa(i), seen)
	}
	return sb.String()
}

// Namespaces of the package boundary stubs in a linked build: an import from
// a page's own entry module gets that page's stub, exactly as in its own
// build, and an import from any other module gets the shared stub of the
// specifier.
const (
	linkedPkgNamespace      = "krate-pkg-boundary"
	linkedPkgEntryNamespace = "krate-pkg-boundary-entry-"
)

// linkedPackages are the package boundary stubs of a linked build.
type linkedPackages struct {
	// entryOf maps the importer path of a page's entry module (its entry, or
	// the virtual page module of a composed page) to the page.
	entryOf map[string]int
	// own holds each page's stubs by specifier.
	own []map[string]string
	// shared holds the shared stub of each specifier.
	shared map[string]string
	// resolveDir is where the stubs' imports resolve from (the per-page
	// builds use the page bundle's directory; every import is external).
	resolveDir string
}

func (lp *linkedPackages) empty() bool { return len(lp.shared) == 0 }

func (lp *linkedPackages) setup(pb api.PluginBuild) {
	var specs []string
	for spec := range lp.shared {
		specs = append(specs, spec)
	}
	pb.OnResolve(api.OnResolveOptions{Filter: exactSpecFilter(specs)}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
		if strings.Contains(filepath.ToSlash(args.Importer), "/node_modules/") {
			return api.OnResolveResult{}, nil
		}
		if i, ok := lp.entryOf[args.Importer]; ok {
			if _, claimed := lp.own[i][args.Path]; !claimed {
				// The page's own build resolves the package.
				return api.OnResolveResult{}, nil
			}
			return api.OnResolveResult{Path: args.Path, Namespace: linkedPkgEntryNamespace + strconv.Itoa(i)}, nil
		}
		if _, ok := lp.shared[args.Path]; !ok {
			return api.OnResolveResult{}, nil
		}
		return api.OnResolveResult{Path: args.Path, Namespace: linkedPkgNamespace}, nil
	})
	pb.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: linkedPkgNamespace}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
		stub, ok := lp.shared[args.Path]
		if !ok {
			return api.OnLoadResult{}, nil
		}
		return api.OnLoadResult{Contents: &stub, Loader: api.LoaderJS, ResolveDir: lp.resolveDir}, nil
	})
	for i, own := range lp.own {
		if len(own) == 0 {
			continue
		}
		own := own
		pb.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: linkedPkgEntryNamespace + strconv.Itoa(i)}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
			stub, ok := own[args.Path]
			if !ok {
				return api.OnLoadResult{}, nil
			}
			return api.OnLoadResult{Contents: &stub, Loader: api.LoaderJS, ResolveDir: lp.resolveDir}, nil
		})
	}
}
