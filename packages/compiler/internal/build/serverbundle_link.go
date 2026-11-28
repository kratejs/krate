package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/evanw/esbuild/pkg/api"
)

// Page server bundles are built one page at a time (serverBundleRun.compile),
// so each carries its own copy of the app modules it imports. The renderer
// keeps every page module it loads, so a process that serves many pages holds
// a copy of a shared module per page, each with its own module state. A
// production build therefore builds the pages again, together, with code
// splitting: the pages keep their bundle paths and import the modules they
// share from chunks under server-bundles/_chunks/, one instance per process.
//
// The per-page builds stay the source of truth. A page joins the linked
// build only when the build loads every module it reaches exactly as its own
// build did, and any failure keeps the per-page bundles.
//
// What a page's plugins decide for a module other than its entry depends on
// the page: its client boundaries become stubs, its async layouts get the
// page globals, its island stubs replace its own imports, and it appends the
// server exports its regions render. The linked build serves the union of
// those decisions, so it checks, from the import graph in its metafile, that
// no page reaches a module another page decides about and it does not (see
// pageLink.violations). Such pages are dropped and the rest built again.

// pageLinkChunks is the directory, under the page server bundles, of the
// chunks linkPageBundles splits out.
const pageLinkChunks = "_chunks"

// ssrBundlesPlaceholder stands for the path from an output file of the
// linked build to the server bundle directory, in the imports of the server
// render modules, until the outputs are known. A per-page build knows its
// output path up front; a module in a shared chunk does not.
const ssrBundlesPlaceholder = "__KRATE_SSR_BUNDLES__"

// pageLinkMaxRounds bounds how often the linked build runs again without the
// pages the previous round found unsafe, or with the shared package stubs
// made from the pages that reach them.
const pageLinkMaxRounds = 6

// pageLinkSpec is what the per-page build of a page bundle derived from the
// page, kept so the linked build can give the page the same entry.
type pageLinkSpec struct {
	page           *PageResult
	absSource      string
	outPath        string
	injectedSource string
	entryFile      string
	clientPage     bool
}

// pageLinkClaims are the canonical paths of the modules, other than its
// entry, whose loading a page's own plugins decide.
type pageLinkClaims struct {
	boundaries map[string]bool
	layouts    map[string]bool
	exports    map[string]bool
	island     string
	// packages are the page's package boundaries by specifier.
	packages map[string]*pkgBoundaries
}

// linkPageBundles replaces the bundles of the run's linkable pages with the
// outputs of one code-split build of them.
func (r *serverBundleRun) linkPageBundles() {
	_ = os.RemoveAll(filepath.Join(r.bundleDir, pageLinkChunks))
	r.linkMu.Lock()
	specs := append([]pageLinkSpec(nil), r.linkSpecs...)
	r.linkMu.Unlock()
	sort.Slice(specs, func(i, j int) bool { return specs[i].outPath < specs[j].outPath })
	set := r.linkCandidates(specs)
	// sharedFrom names, by specifier, the pages (by output path) whose
	// package boundaries make the shared stub; nil for every page that has
	// boundaries for the specifier.
	var sharedFrom map[string]map[string]bool
	for round := 0; round < pageLinkMaxRounds && len(set) >= 2; round++ {
		l := r.newPageLink(set, sharedFrom)
		res := esbuildBuild(l.opts)
		if len(res.Errors) > 0 && !l.mixed.Load() {
			// A module that imports a name the shared stub of a package
			// lacks belongs to a page without boundaries for the package
			// (its own build imports the package); every other page's own
			// stub had the names its modules import.
			if kept := l.withoutUnclaimedPackages(res.Errors); len(kept) < len(set) {
				linkDebugf("round %d: %d errors from package stubs, %d pages without them left out", round, len(res.Errors), len(set)-len(kept))
				set = kept
				continue
			}
		}
		if len(res.Errors) > 0 || l.mixed.Load() {
			linkDebugf("round %d: build failed (%d errors, mixed packages %v)", round, len(res.Errors), l.mixed.Load())
			for i, e := range res.Errors {
				if i < 5 {
					linkDebugf("  %s", e.Text)
				}
			}
			return
		}
		bad, next := l.violations(res.Metafile)
		linkDebugf("round %d: %d pages, %d unsafe, shared package stubs redone %v", round, len(set), len(bad), next != nil)
		if len(bad) == 0 && next == nil {
			l.write(res.OutputFiles)
			return
		}
		if next != nil {
			sharedFrom = next
		}
		kept := set[:0:0]
		for i, s := range set {
			if !bad[i] {
				kept = append(kept, s)
			}
		}
		set = kept
	}
}

// linkablePage reports whether the linked build can reproduce a page's
// bundle at all. Baked Go HTML is one virtual module the Go component shims
// of every page import, so it cannot be told apart per page. A "use client"
// page with island stubs would stub the imports of its own source file,
// which other pages may import.
func (r *serverBundleRun) linkablePage(s pageLinkSpec) bool {
	p := s.page
	if len(p.GoBaked) > 0 || (s.clientPage && p.IslandStubs != nil) {
		return false
	}
	rel, err := filepath.Rel(r.bundleDir, s.outPath)
	if err != nil || !filepath.IsLocal(rel) || !strings.HasSuffix(rel, ".mjs") {
		return false
	}
	first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	return first != pageLinkChunks && first != "client"
}

// linkCandidates returns the pages of specs the linked build may take, in
// order: a page is left out when an export of its stub for a client boundary
// module, or a server export key it adds to a module, disagrees with an
// earlier page's. Package boundaries are checked against the pages that
// reach each shared stub once the build shows which do (see violations).
func (r *serverBundleRun) linkCandidates(specs []pageLinkSpec) []pageLinkSpec {
	stubs := map[string]*boundaryExports{}
	keys := map[string]string{}
	var out []pageLinkSpec
	for _, s := range specs {
		if !r.linkablePage(s) {
			linkDebugf("unlinkable page %s (go baked %d, client page %v, boundaries %d)", s.page.SourcePath, len(s.page.GoBaked), s.clientPage, len(s.page.ClientBoundaries))
			continue
		}
		own := pageFileBoundaries(s.page)
		ownKeys := map[string]string{}
		ok := true
		for _, e := range s.page.ServerExports {
			if samePath(e.SourcePath, s.absSource) {
				continue
			}
			v := canonicalPath(e.SourcePath) + "\x00" + e.Name
			if prev, seen := keys[e.Key]; seen && prev != v {
				ok = false
			}
			if prev, seen := ownKeys[e.Key]; seen && prev != v {
				ok = false
			}
			ownKeys[e.Key] = v
		}
		for p, exp := range own {
			if prev, seen := stubs[p]; seen && !prev.agrees(exp) {
				linkDebugf("page %s: boundary stub of %s differs from an earlier page's", s.page.SourcePath, p)
				ok = false
			}
		}
		if !ok {
			linkDebugf("page %s left out", s.page.SourcePath)
			continue
		}
		for p, exp := range own {
			if prev, seen := stubs[p]; seen {
				prev.merge(exp)
			} else {
				stubs[p] = exp
			}
		}
		for k, v := range ownKeys {
			keys[k] = v
		}
		out = append(out, s)
	}
	return out
}

// linkedBoundaryStub is the stub clientBoundaryPlugin serves for cb, with the
// server render module imported through ssrBundlesPlaceholder.
func linkedBoundaryStub(cb ClientBoundary) string {
	if cb.Real {
		return realBoundaryStub(cb)
	}
	return clientBoundaryStub(cb, ssrBundlesPlaceholder+"/client/"+cb.ModuleID+".ssr.mjs")
}

// pageLinkEntry is the entry module of one page in the linked build.
type pageLinkEntry struct {
	path, source, resolveDir string
	out                      string // output path relative to the bundle dir, without .mjs
}

// pageLink is one linked build of a set of pages.
type pageLink struct {
	r       *serverBundleRun
	set     []pageLinkSpec
	entries []pageLinkEntry
	claims  []pageLinkClaims
	// strict lists the modules that a page which does not claim them would
	// load differently, by category.
	strictBoundaries, strictLayouts, strictExports, strictIslands map[string]bool
	// pkgShared holds the exports of the shared stub of each package
	// specifier.
	pkgShared  map[string]*boundaryExports
	sharedFrom map[string]map[string]bool
	opts       api.BuildOptions
	mixed      atomic.Bool
}

func (r *serverBundleRun) newPageLink(set []pageLinkSpec, sharedFrom map[string]map[string]bool) *pageLink {
	l := &pageLink{
		r: r, set: set,
		strictBoundaries: map[string]bool{}, strictLayouts: map[string]bool{}, strictExports: map[string]bool{}, strictIslands: map[string]bool{},
	}
	var islands, composed []api.Plugin
	var exports []ServerExport
	files := map[string]*boundaryExports{}
	var fileOrder []string
	pkgs := &linkedPackages{entryOf: map[string]int{}, shared: map[string]string{}, resolveDir: r.bundleDir}
	pkgShared := map[string]*boundaryExports{}
	seenExport := map[string]bool{}
	dirs := []string{filepath.Join(r.bundleDir, pageLinkChunks)}
	points := make([]api.EntryPoint, len(set))
	for i, s := range set {
		p := s.page
		dir := filepath.Dir(s.absSource)
		rel, _ := filepath.Rel(r.bundleDir, s.outPath)
		e := pageLinkEntry{
			path:       filepath.Join(dir, ".krate-page-"+strconv.Itoa(i), s.entryFile),
			source:     s.injectedSource,
			resolveDir: dir,
			out:        filepath.ToSlash(strings.TrimSuffix(rel, ".mjs")),
		}
		c := pageLinkClaims{boundaries: map[string]bool{}, layouts: map[string]bool{}, exports: map[string]bool{}, packages: pagePackageBoundaries(p)}
		pkgs.entryOf[e.path] = i
		if p.IslandStubs != nil {
			islands = append(islands, islandServerStubPluginFor(p.IslandStubs, s.absSource, []string{s.absSource, e.path}, "krate-island-stub-"+strconv.Itoa(i)))
			c.island = canonicalPath(s.absSource)
			l.strictIslands[c.island] = true
		}
		if len(p.ComposedLayouts) > 0 {
			// Each page has its own virtual page module.
			pageModule := "krate-composed-page-" + strconv.Itoa(i)
			pkgs.entryOf[pageModule] = i
			e.source = composedEntrySource(pageModule, p.ComposedLayouts)
			composed = append(composed, composedLayoutPlugin(r.root, pageModule, s.injectedSource, dir, p.ComposedLayouts, r.files))
			for _, lay := range p.ComposedLayouts {
				c.layouts[canonicalPath(lay)] = true
				l.strictLayouts[canonicalPath(lay)] = true
			}
		}
		for path, exp := range pageFileBoundaries(p) {
			c.boundaries[path] = true
			l.strictBoundaries[path] = true
			if u, ok := files[path]; ok {
				u.merge(exp)
			} else {
				files[path] = exp
				fileOrder = append(fileOrder, path)
			}
		}
		own := map[string]string{}
		for spec, pb := range c.packages {
			own[spec] = linkedPackageStub(pb.cbs)
		}
		pkgs.own = append(pkgs.own, own)
		for _, se := range p.ServerExports {
			if samePath(se.SourcePath, s.absSource) {
				continue
			}
			file := canonicalPath(se.SourcePath)
			c.exports[file] = true
			if k := file + "\x00" + se.Key; !seenExport[k] {
				seenExport[k] = true
				exports = append(exports, se)
			}
		}
		l.entries = append(l.entries, e)
		l.claims = append(l.claims, c)
		points[i] = api.EntryPoint{InputPath: "krate-page-entry:" + strconv.Itoa(i), OutputPath: e.out}
		dirs = append(dirs, filepath.Dir(s.outPath))
	}
	sort.Strings(fileOrder)
	var boundaries []ClientBoundary
	for _, path := range fileOrder {
		boundaries = append(boundaries, files[path].boundaries()...)
	}
	// The shared stub of a specifier is made from the pages of sharedFrom
	// that have it, in page order; when none is left, from every page that
	// has it, so imports outside the entries still resolve to a stub and
	// violations sees which pages reach it.
	for i, s := range set {
		for _, spec := range slices.Sorted(maps.Keys(l.claims[i].packages)) {
			if from := sharedFrom[spec]; from != nil && !from[s.outPath] {
				continue
			}
			if pkgShared[spec] == nil {
				pkgShared[spec] = newBoundaryExports()
			}
			pkgShared[spec].merge(l.claims[i].packages[spec].exports)
		}
	}
	fallback := map[string]bool{}
	for i := range set {
		for _, spec := range slices.Sorted(maps.Keys(l.claims[i].packages)) {
			if pkgShared[spec] == nil {
				pkgShared[spec] = newBoundaryExports()
				fallback[spec] = true
			}
			if fallback[spec] {
				pkgShared[spec].merge(l.claims[i].packages[spec].exports)
			}
		}
	}
	l.pkgShared, l.sharedFrom = pkgShared, sharedFrom
	for spec, u := range pkgShared {
		pkgs.shared[spec] = linkedPackageStub(u.boundaries())
	}
	// A module that gets server exports appended loads as it would without
	// them unless the export plugin also gives it page globals the Go
	// component plugin would not.
	for _, se := range exports {
		file := canonicalPath(se.SourcePath)
		if _, done := l.strictExports[file]; done {
			continue
		}
		l.strictExports[file] = !l.exportLoadMatches(se.SourcePath)
	}

	plugins := append(islands, composed...)
	plugins = append(plugins,
		reactServerShimPlugin(),
		serverCSSPlugin(r.root, r.impls.res),
		linkedBoundaryPlugin(boundaries, pkgs, r.files),
		unusedClientModulePlugin(r.files),
		serverExportPlugin(exports, "", r.files),
		goServerBundlePlugin(nil, r.root, r.files),
		actionServerBundlePlugin(r.impls.res, r.actionModules, r.actionBasePath),
		inlineActionPlugin(r.root, r.actionBasePath),
		serverRuntimeTagsPlugin(),
		serverGlobalsPlugin(r.root),
		pathAliasPlugin(r.impls.res),
		externalNodeModulesPluginDirs(dirs, r.serverMemo, &l.mixed),
	)
	l.opts = api.BuildOptions{
		AbsWorkingDir:       r.root,
		Bundle:              true,
		Format:              api.FormatESModule,
		Platform:            api.PlatformNode,
		EntryPointsAdvanced: points,
		Outdir:              r.bundleDir,
		Splitting:           true,
		ChunkNames:          pageLinkChunks + "/[name]-[hash]",
		OutExtension:        map[string]string{".js": ".mjs"},
		Write:               false,
		Metafile:            true,
		Sourcemap:           sourceMapMode(r.sourcemap),
		JSX:                 api.JSXAutomatic,
		JSXSideEffects:      false,
		JSXImportSource:     "@krate/runtime/server",
		TsconfigRaw:         `{ "compilerOptions": { "jsx": "react-jsx", "jsxImportSource": "@krate/runtime/server" } }`,
		Banner:              serverPageBanner(r.actionBasePath, r.authBanner),
		External:            []string{"@krate/runtime", "@krate/runtime/*"},
		Plugins:             plugins,
	}
	newSourceMeta(sourceRootFor(r.root), "").apply(&l.opts)
	// The entries are not files: the source locations were rewritten when
	// the per-page builds made them, so the entry plugin is not wrapped.
	l.opts.Plugins = append([]api.Plugin{pageLinkEntryPlugin(l.entries)}, l.opts.Plugins...)
	return l
}

// exportLoadMatches reports whether the module at path, which
// serverExportPlugin claims to append server exports, loads as the
// Go-component plugin would load it in a page that appends none.
func (l *pageLink) exportLoadMatches(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".tsx", ".js", ".jsx":
	default:
		return false
	}
	data, err := l.r.files.serverSource(l.r.root, path)
	return err == nil && injectServerGlobals(string(data)) == string(data)
}

const pageLinkEntryFilter = `[/\\]\.krate-page-[0-9]+[/\\][^/\\]+$`

// pageLinkEntryPlugin serves each page's entry module at its own path.
func pageLinkEntryPlugin(entries []pageLinkEntry) api.Plugin {
	byPath := make(map[string]pageLinkEntry, len(entries))
	for _, e := range entries {
		byPath[e.path] = e
	}
	return api.Plugin{
		Name: "krate-page-link-entries",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^krate-page-entry:[0-9]+$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.Kind != api.ResolveEntryPoint {
					return api.OnResolveResult{}, nil
				}
				i, err := strconv.Atoi(strings.TrimPrefix(args.Path, "krate-page-entry:"))
				if err != nil || i < 0 || i >= len(entries) {
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: entries[i].path, Namespace: "file"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: pageLinkEntryFilter, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				e, ok := byPath[args.Path]
				if !ok {
					return api.OnLoadResult{}, nil
				}
				src := e.source
				return api.OnLoadResult{Contents: &src, ResolveDir: e.resolveDir, Loader: api.LoaderTSX}, nil
			})
		},
	}
}

// linkedBoundaryPlugin is clientBoundaryPlugin for the file boundaries of
// several pages, which linkCandidates checked agree on each module's stub.
// The render modules are imported through ssrBundlesPlaceholder.
func linkedBoundaryPlugin(boundaries []ClientBoundary, pkgs *linkedPackages, files *serverBundleFiles) api.Plugin {
	byPath := map[string]ClientBoundary{}
	for _, cb := range boundaries {
		byPath[filepath.Clean(cb.SourcePath)] = cb
		if resolved := canonicalPath(cb.SourcePath); resolved != filepath.Clean(cb.SourcePath) {
			byPath[resolved] = cb
		}
	}
	markers := clientOnlyMarkersPlugin(false)
	return api.Plugin{
		Name: "krate-client-boundaries",
		Setup: func(pb api.PluginBuild) {
			markers.Setup(pb)
			if !pkgs.empty() {
				pkgs.setup(pb)
			}
			if len(byPath) == 0 && pkgs.empty() {
				return
			}
			pb.OnResolve(api.OnResolveOptions{Filter: `\.ssr\.mjs$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if strings.Contains(args.Path, "/client/c") || strings.Contains(args.Path, "\\client\\c") {
					return api.OnResolveResult{Path: args.Path, External: true}, nil
				}
				return api.OnResolveResult{}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `\.[cm]?[jt]sx?$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				cb, ok := byPath[filepath.Clean(args.Path)]
				if !ok {
					for _, candidate := range boundaries {
						if files.sameFile(args.Path, candidate.SourcePath) {
							cb, ok = candidate, true
							break
						}
					}
				}
				if !ok {
					return api.OnLoadResult{}, nil
				}
				stub := linkedBoundaryStub(cb)
				return api.OnLoadResult{Contents: &stub, Loader: api.LoaderJS, ResolveDir: filepath.Dir(args.Path)}, nil
			})
		},
	}
}

// pkgStubRef finds the package stubs that esbuild errors name.
var pkgStubRef = regexp.MustCompile(regexp.QuoteMeta(linkedPkgNamespace) + `:([^"]+)"`)

// withoutUnclaimedPackages returns the pages that have boundaries for every
// package whose shared stub the errors name; all of them when the errors
// name none.
func (l *pageLink) withoutUnclaimedPackages(errs []api.Message) []pageLinkSpec {
	specs := map[string]bool{}
	for _, e := range errs {
		for _, m := range pkgStubRef.FindAllStringSubmatch(e.Text, -1) {
			specs[m[1]] = true
		}
	}
	if len(specs) == 0 {
		return l.set
	}
	var kept []pageLinkSpec
	for i, s := range l.set {
		ok := true
		for spec := range specs {
			if l.claims[i].packages[spec] == nil {
				ok = false
			}
		}
		if ok {
			kept = append(kept, s)
		}
	}
	return kept
}

// namespacedInput matches a metafile input of a plugin namespace
// ("ns:path"); a Windows drive letter is a single character.
var namespacedInput = regexp.MustCompile(`^[A-Za-z0-9_-]{2,}:`)

// violations returns the indexes of the pages that reach, in the linked
// build's import graph, a module that some page's plugins decide about and
// their own do not: their own build loaded it differently. A page that
// reaches the shared stub of a package specifier must have boundaries for
// it, and each name its own stub exports must mean the same in the shared
// one. When a shared stub disagrees with a page that reaches it, next says
// which pages to make the stubs from in the next round (the ones that reach
// each, less those that disagree with the earlier ones); nil when nothing
// changes.
func (l *pageLink) violations(metafile string) (map[int]bool, map[string]map[string]bool) {
	reachers := map[string][]int{}
	var meta struct {
		Inputs map[string]struct {
			Imports []struct {
				Path     string `json:"path"`
				External bool   `json:"external"`
			} `json:"imports"`
		} `json:"inputs"`
	}
	bad := map[int]bool{}
	if err := json.Unmarshal([]byte(metafile), &meta); err != nil || len(l.entries) == 0 {
		for i := range l.set {
			bad[i] = true
		}
		return bad, nil
	}
	cwd := esbuildCwd(l.opts, metafile, l.entries[0].path)
	canon := map[string]string{}
	fileOf := func(key string) string {
		if v, ok := canon[key]; ok {
			return v
		}
		v := ""
		if !namespacedInput.MatchString(key) {
			p := filepath.FromSlash(key)
			if !filepath.IsAbs(p) {
				p = filepath.Join(cwd, p)
			}
			v = canonicalPath(p)
		}
		canon[key] = v
		return v
	}
	for i, e := range l.entries {
		start := relSlash(cwd, e.path)
		if _, ok := meta.Inputs[start]; !ok {
			bad[i] = true
			continue
		}
		c := l.claims[i]
		reachedPkg := map[string]bool{}
		seen := map[string]bool{start: true}
		queue := []string{start}
		for len(queue) > 0 && !bad[i] {
			key := queue[0]
			queue = queue[1:]
			if spec, ok := strings.CutPrefix(key, linkedPkgNamespace+":"); ok {
				if c.packages[spec] == nil {
					// Its own build imports the package itself.
					linkDebugf("page %s reaches the package stub of %s without boundaries for it", l.set[i].page.SourcePath, spec)
					bad[i] = true
					break
				}
				reachedPkg[spec] = true
			}
			if key != start {
				if f := fileOf(key); f != "" {
					if (l.strictBoundaries[f] && !c.boundaries[f]) ||
						(l.strictLayouts[f] && !c.layouts[f]) ||
						(l.strictIslands[f] && c.island != f) ||
						(l.strictExports[f] && !c.exports[f]) {
						linkDebugf("page %s reaches %s (boundary %v/%v layout %v/%v island %v export %v/%v)", l.set[i].page.SourcePath, f, l.strictBoundaries[f], c.boundaries[f], l.strictLayouts[f], c.layouts[f], l.strictIslands[f], l.strictExports[f], c.exports[f])
						bad[i] = true
						break
					}
				}
			}
			for _, imp := range meta.Inputs[key].Imports {
				if imp.External || seen[imp.Path] {
					continue
				}
				seen[imp.Path] = true
				queue = append(queue, imp.Path)
			}
		}
		if !bad[i] {
			for spec := range reachedPkg {
				reachers[spec] = append(reachers[spec], i)
			}
		}
	}
	var next map[string]map[string]bool
	for _, spec := range slices.Sorted(maps.Keys(reachers)) {
		built := l.pkgShared[spec]
		agree := true
		for _, i := range reachers[spec] {
			if !bad[i] && !built.agrees(l.claims[i].packages[spec].exports) {
				agree = false
			}
		}
		if agree {
			continue
		}
		// Make the stub from the pages that reach it, the first ones
		// winning a name.
		u := newBoundaryExports()
		from := map[string]bool{}
		for _, i := range reachers[spec] {
			if bad[i] {
				continue
			}
			own := l.claims[i].packages[spec].exports
			if !u.agrees(own) {
				linkDebugf("page %s: package boundary exports of %s differ from an earlier page's", l.set[i].page.SourcePath, spec)
				bad[i] = true
				continue
			}
			u.merge(own)
			from[l.set[i].outPath] = true
		}
		if next == nil {
			next = maps.Clone(l.sharedFrom)
			if next == nil {
				next = map[string]map[string]bool{}
			}
		}
		next[spec] = from
	}
	return bad, next
}

// write fills in the render module imports of the outputs and writes them
// over the per-page bundles, the chunks first: a page bundle is replaced only
// once every chunk is in place, and each file atomically, so a failure leaves
// every page with a bundle that works. Nothing is written when an output is
// not what the per-page build checked for.
func (l *pageLink) write(files []api.OutputFile) {
	r := l.r
	bundleDir, err := filepath.Abs(r.bundleDir)
	if err != nil {
		return
	}
	chunkDir := filepath.Join(bundleDir, pageLinkChunks)
	entryOut := map[string]int{}
	for i, e := range l.entries {
		entryOut[filepath.Join(bundleDir, filepath.FromSlash(e.out)+".mjs")] = i
	}
	var chunks, rest []api.OutputFile
	found := 0
	for _, f := range files {
		rel, err := filepath.Rel(bundleDir, f.Path)
		if err != nil || !filepath.IsLocal(rel) {
			return
		}
		contents := f.Contents
		if strings.HasSuffix(f.Path, ".mjs") {
			up, err := filepath.Rel(filepath.Dir(f.Path), bundleDir)
			if err != nil {
				return
			}
			contents = bytes.ReplaceAll(contents, []byte(`"`+ssrBundlesPlaceholder+`/`), []byte(`"`+filepath.ToSlash(up)+`/`))
			if bytes.Contains(contents, []byte(ssrBundlesPlaceholder)) {
				return
			}
		}
		for i := range l.entries {
			contents = bytes.ReplaceAll(contents, []byte("/.krate-page-"+strconv.Itoa(i)+"/"), []byte("/"))
		}
		f.Contents = contents
		if i, ok := entryOut[f.Path]; ok {
			found++
			for _, se := range l.set[i].page.ServerExports {
				if !bytes.Contains(contents, []byte(se.Key)) {
					return
				}
			}
		}
		if strings.HasPrefix(f.Path, chunkDir+string(filepath.Separator)) {
			chunks = append(chunks, f)
		} else {
			rest = append(rest, f)
		}
	}
	if found != len(l.entries) {
		return
	}
	for _, f := range chunks {
		if err := writeFileAtomic(f.Path, f.Contents); err != nil {
			_ = os.RemoveAll(chunkDir)
			return
		}
	}
	for _, f := range rest {
		if err := writeFileAtomic(f.Path, f.Contents); err != nil {
			return
		}
	}
}

// writeFileAtomic writes data to path through a temporary file in the same
// directory, so a reader sees the old file or the new one.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".krate-tmp"
	if err := os.WriteFile(tmp, data, 0666); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// linkDebugf logs a page-link decision when KRATE_DEBUG_PAGE_LINK is set.
func linkDebugf(format string, args ...any) {
	if os.Getenv("KRATE_DEBUG_PAGE_LINK") != "" {
		fmt.Fprintf(os.Stderr, "[page-link] "+format+"\n", args...)
	}
}
