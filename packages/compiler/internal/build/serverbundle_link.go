package build

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
// pages the previous round found unsafe.
const pageLinkMaxRounds = 4

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
	for round := 0; round < pageLinkMaxRounds && len(set) >= 2; round++ {
		l := r.newPageLink(set)
		res := esbuildBuild(l.opts)
		if len(res.Errors) > 0 || l.mixed.Load() {
			return
		}
		bad := l.violations(res.Metafile)
		if len(bad) == 0 {
			l.write(res.OutputFiles)
			return
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
// bundle at all. A package boundary resolves a bare specifier to a stub of
// the page's own exports for every importer, and baked Go HTML is one
// virtual module the Go component shims of every page import; neither can
// be told apart per page. A "use client" page with island stubs would stub
// the imports of its own source file, which other pages may import.
func (r *serverBundleRun) linkablePage(s pageLinkSpec) bool {
	p := s.page
	if len(p.GoBaked) > 0 || (s.clientPage && p.IslandStubs != nil) {
		return false
	}
	for _, cb := range p.ClientBoundaries {
		if cb.Spec != "" {
			return false
		}
	}
	rel, err := filepath.Rel(r.bundleDir, s.outPath)
	if err != nil || !filepath.IsLocal(rel) || !strings.HasSuffix(rel, ".mjs") {
		return false
	}
	first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	return first != pageLinkChunks && first != "client"
}

// linkCandidates returns the pages of specs the linked build may take, in
// order: a page is left out when its stub for a client boundary, or a server
// export key it adds to a module, disagrees with an earlier page's.
func (r *serverBundleRun) linkCandidates(specs []pageLinkSpec) []pageLinkSpec {
	stubs := map[string]string{}
	keys := map[string]string{}
	var out []pageLinkSpec
	for _, s := range specs {
		if !r.linkablePage(s) {
			continue
		}
		own := map[string]string{}
		for _, cb := range s.page.ClientBoundaries {
			own[canonicalPath(cb.SourcePath)] = linkedBoundaryStub(cb)
		}
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
		for p, stub := range own {
			if prev, seen := stubs[p]; seen && prev != stub {
				ok = false
			}
		}
		if !ok {
			continue
		}
		for p, stub := range own {
			stubs[p] = stub
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
	opts                                                          api.BuildOptions
	mixed                                                         atomic.Bool
}

func (r *serverBundleRun) newPageLink(set []pageLinkSpec) *pageLink {
	l := &pageLink{
		r: r, set: set,
		strictBoundaries: map[string]bool{}, strictLayouts: map[string]bool{}, strictExports: map[string]bool{}, strictIslands: map[string]bool{},
	}
	var islands, composed []api.Plugin
	var boundaries []ClientBoundary
	var exports []ServerExport
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
		c := pageLinkClaims{boundaries: map[string]bool{}, layouts: map[string]bool{}, exports: map[string]bool{}}
		if p.IslandStubs != nil {
			islands = append(islands, islandServerStubPluginFor(p.IslandStubs, s.absSource, []string{s.absSource, e.path}, "krate-island-stub-"+strconv.Itoa(i)))
			c.island = canonicalPath(s.absSource)
			l.strictIslands[c.island] = true
		}
		if len(p.ComposedLayouts) > 0 {
			// Each page has its own virtual page module.
			pageModule := "krate-composed-page-" + strconv.Itoa(i)
			e.source = composedEntrySource(pageModule, p.ComposedLayouts)
			composed = append(composed, composedLayoutPlugin(r.root, pageModule, s.injectedSource, dir, p.ComposedLayouts, r.files))
			for _, lay := range p.ComposedLayouts {
				c.layouts[canonicalPath(lay)] = true
				l.strictLayouts[canonicalPath(lay)] = true
			}
		}
		for _, cb := range p.ClientBoundaries {
			boundaries = append(boundaries, cb)
			c.boundaries[canonicalPath(cb.SourcePath)] = true
			l.strictBoundaries[canonicalPath(cb.SourcePath)] = true
		}
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
		linkedBoundaryPlugin(boundaries, r.files),
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
func linkedBoundaryPlugin(boundaries []ClientBoundary, files *serverBundleFiles) api.Plugin {
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
			if len(byPath) == 0 {
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

// namespacedInput matches a metafile input of a plugin namespace
// ("ns:path"); a Windows drive letter is a single character.
var namespacedInput = regexp.MustCompile(`^[A-Za-z0-9_-]{2,}:`)

// violations returns the indexes of the pages that reach, in the linked
// build's import graph, a module that some page's plugins decide about and
// their own do not: their own build loaded it differently.
func (l *pageLink) violations(metafile string) map[int]bool {
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
		return bad
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
		seen := map[string]bool{start: true}
		queue := []string{start}
		for len(queue) > 0 && !bad[i] {
			key := queue[0]
			queue = queue[1:]
			if key != start {
				if f := fileOf(key); f != "" {
					if (l.strictBoundaries[f] && !c.boundaries[f]) ||
						(l.strictLayouts[f] && !c.layouts[f]) ||
						(l.strictIslands[f] && c.island != f) ||
						(l.strictExports[f] && !c.exports[f]) {
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
	}
	return bad
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
