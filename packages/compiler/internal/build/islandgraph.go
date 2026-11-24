package build

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

// Island chunks of every page are bundled together, with code splitting, so a
// module that two pages both import (a zustand store, a query client, the
// library itself) is one file with one instance. A page keeps its own entry
// file, whose name depends only on the entry's source, so the page script can
// name it before any other page is built.

// islandEntry is the entry module of one island chunk.
type islandEntry struct {
	Name       string // output file stem, "island_<hash>"
	Source     string
	ResolveDir string
	// Virtual holds the generated modules the source imports by name
	// (layout providers), keyed by their specifier.
	Virtual map[string]islandVirtualModule
	// Real marks a real-React entry, bundled by realIslandFlavour.
	Real bool
	// Boundary marks the hydration chunk of a client boundary.
	Boundary bool
}

// islandVirtualModule is a generated module an island entry imports.
type islandVirtualModule struct {
	Source     string
	ResolveDir string
}

const (
	islandEntryPrefix = "island_"
	islandSharedStem  = "isl-"
)

// islandEntryName names the entry of an island chunk. The name must not look
// like a content hash ("name.<hash>.js"): the entry's body changes when the
// shared chunks around it do, so it is revalidated, never cached as immutable.
func (b *Builder) islandEntryName(src, resolveDir string) string {
	sum := sha256.Sum256([]byte(src + "\x00" + b.relPath(resolveDir)))
	return islandEntryPrefix + hex.EncodeToString(sum[:])[:12]
}

func (b *Builder) islandEntryURL(name string) string {
	return assetHref(b.Cfg.BaseURLPath(), "chunks/"+name+".js")
}

// setIslandEntries records the island entries of a page, replacing what an
// earlier build of the page registered.
func (b *Builder) setIslandEntries(owner string, entries []islandEntry) {
	b.islandMu.Lock()
	defer b.islandMu.Unlock()
	if b.islandEntries == nil {
		b.islandEntries = map[string][]islandEntry{}
	}
	if len(entries) == 0 {
		if _, had := b.islandEntries[owner]; had {
			b.islandDirty = true
		}
		delete(b.islandEntries, owner)
		return
	}
	b.islandEntries[owner] = entries
	b.islandDirty = true
}

// takeIslandDirty reports whether a page registered island entries since the
// island chunks were last written, and clears the flag.
func (b *Builder) takeIslandDirty() bool {
	b.islandMu.Lock()
	defer b.islandMu.Unlock()
	d := b.islandDirty
	b.islandDirty = false
	return d
}

// resetIslandEntries forgets every registered entry (a full build starts over).
// The shared Preact chunks are forgotten too: the output directory they were
// written to is emptied, and a build that kept the memo left the import map
// pointing at files that no longer exist (dev rebuilds the whole site on a new
// page, a renamed file, a page turning request-time).
func (b *Builder) resetIslandEntries() {
	b.islandMu.Lock()
	b.islandEntries = nil
	b.preactChunks = nil
	b.islandDirty = true
	b.islandMu.Unlock()
}

// islandEntriesOf returns the entries a page registered.
func (b *Builder) islandEntriesOf(owner string) []islandEntry {
	b.islandMu.Lock()
	defer b.islandMu.Unlock()
	return append([]islandEntry(nil), b.islandEntries[owner]...)
}

// allIslandEntries lists the distinct registered entries by name.
func (b *Builder) allIslandEntries() []islandEntry {
	b.islandMu.Lock()
	defer b.islandMu.Unlock()
	seen := map[string]bool{}
	var out []islandEntry
	for _, list := range b.islandEntries {
		for _, e := range list {
			if seen[e.Name] {
				continue
			}
			seen[e.Name] = true
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// islandEntryPlugin serves the registered entries and their generated modules
// to a build as virtual modules.
func islandEntryPlugin(entries []islandEntry, virtual map[string]islandVirtualModule) api.Plugin {
	byName := make(map[string]islandEntry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}
	const entryPrefix = "krate-island-entry:"
	return api.Plugin{
		Name: "krate-island-entry",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^krate-island-entry:`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: args.Path, Namespace: "krate-island-entry"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-island-entry"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				e, ok := byName[strings.TrimPrefix(args.Path, entryPrefix)]
				if !ok {
					return api.OnLoadResult{}, fmt.Errorf("unknown island entry %s", args.Path)
				}
				src := e.Source
				return api.OnLoadResult{Contents: &src, Loader: api.LoaderTSX, ResolveDir: e.ResolveDir}, nil
			})
			pb.OnResolve(api.OnResolveOptions{Filter: `^` + islandVirtualPrefix}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if _, ok := virtual[args.Path]; !ok {
					return api.OnResolveResult{}, fmt.Errorf("unknown generated island module %s", args.Path)
				}
				return api.OnResolveResult{Path: args.Path, Namespace: "krate-island-virtual"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-island-virtual"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				m := virtual[args.Path]
				src := m.Source
				return api.OnLoadResult{Contents: &src, Loader: api.LoaderTSX, ResolveDir: m.ResolveDir}, nil
			})
		},
	}
}

// islandVirtualPrefix starts the specifier of a generated island module.
const islandVirtualPrefix = "krate-island-module:"

// writeIslandBundles bundles every registered island entry into /chunks with
// code splitting. Modules used by several entries land in shared chunks, so a
// client-side navigation that loads another page's entry finds the store
// modules it already evaluated.
func (b *Builder) writeIslandBundles() error {
	all := b.allIslandEntries()
	// A dev rebuild whose entries and files are the ones of the last build
	// finds its chunks already written (see devmemo.go).
	sig := ""
	if b.DevMode {
		sig = b.islandBuildSig(all)
		if b.islandMemo.valid(sig) {
			b.Prof.Add("dev:island-bundles-reused", 0)
			return nil
		}
		b.islandMemo = nil
	}
	chunksDir := filepath.Join(b.Cfg.OutDir, "chunks")
	keep := map[string]bool{}
	var entries, realEntries []islandEntry
	for _, e := range all {
		if e.Real {
			realEntries = append(realEntries, e)
		} else {
			entries = append(entries, e)
		}
	}
	var flavours []*islandFlavour
	if len(realEntries) > 0 {
		f, err := b.realIslandFlavour(realEntries, chunksDir)
		if err != nil {
			return err
		}
		flavours = append(flavours, f)
	}
	if len(entries) > 0 {
		if _, err := b.ensurePreactShared(); err != nil {
			return fmt.Errorf("island runtime could not be built: %w", err)
		}
		f, err := b.preactIslandFlavour(entries, chunksDir)
		if err != nil {
			return err
		}
		flavours = append(flavours, f)
	}
	if len(flavours) == 0 {
		pruneIslandFiles(chunksDir, keep, b.retiredChunksDir())
		b.setIslandGraph(nil)
		return nil
	}
	shared := b.buildIslandFlavours(flavours, chunksDir)
	for _, f := range flavours {
		if err := b.checkIslandFlavour(f); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(chunksDir, 0755); err != nil {
		return fmt.Errorf("creating chunks dir: %w", err)
	}
	for _, f := range flavours {
		if err := writeIslandChunkFiles(f.result.OutputFiles, chunksDir, f.stem, keep); err != nil {
			return err
		}
	}
	if err := writeIslandChunkFiles(shared.OutputFiles, chunksDir, islandNeutralStem, keep); err != nil {
		return err
	}
	pruneIslandFiles(chunksDir, keep, b.retiredChunksDir())
	graph := islandModuleGraph{}
	for _, m := range append([]string{shared.Metafile}, flavourMetafiles(flavours)...) {
		if err := graph.addMetafile(m); err != nil {
			// Without the graph a page preloads only its island entries.
			graph = nil
			break
		}
	}
	b.setIslandGraph(graph)
	if b.DevMode {
		var inputs []string
		for _, m := range append([]string{shared.Metafile}, flavourMetafiles(flavours)...) {
			inputs = append(inputs, metafileAllInputs(b.Root, m)...)
		}
		b.islandMemo = &buildMemo{sig: sig, stamps: b.stampInputs(inputs), config: b.projectConfigStamp()}
	}
	return nil
}

// islandBuildSig is what the island chunk build depends on besides files:
// the entries (with their generated modules), the output mode and the values
// defined into the client code.
func (b *Builder) islandBuildSig(entries []islandEntry) string {
	h := sha256.New()
	fmt.Fprintf(h, "%v|%v|%v|", b.shouldMinifyJS(), b.clientEnvDefines(), b.actionModules())
	for _, e := range entries {
		fmt.Fprintf(h, "\x00%s\x00%s\x00%s\x00%v\x00%v", e.Name, e.Source, e.ResolveDir, e.Real, e.Boundary)
		keys := make([]string, 0, len(e.Virtual))
		for k := range e.Virtual {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(h, "\x00%s\x00%s\x00%s", k, e.Virtual[k].Source, e.Virtual[k].ResolveDir)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func flavourMetafiles(flavours []*islandFlavour) []string {
	out := make([]string, 0, len(flavours))
	for _, f := range flavours {
		out = append(out, f.result.Metafile)
	}
	return out
}

// preactIslandFlavour returns the build of the Preact island entries.
func (b *Builder) preactIslandFlavour(entries []islandEntry, chunksDir string) (*islandFlavour, error) {
	virtual := map[string]islandVirtualModule{}
	points := make([]api.EntryPoint, 0, len(entries))
	for _, e := range entries {
		for k, v := range e.Virtual {
			virtual[k] = v
		}
		points = append(points, api.EntryPoint{InputPath: "krate-island-entry:" + e.Name, OutputPath: e.Name})
	}
	sink := &islandCSSSink{root: b.Root}
	minify := b.shouldMinifyJS()
	guard := b.newUnavailableGuard()
	// One resolve memo for every build made from these options (see
	// buildIslandFlavours).
	plugins := []api.Plugin{purePrunePluginMemo(b.Root, &resolveMemo{}), guard.plugin(), islandEntryPlugin(entries, virtual), islandCSSPlugin(sink), islandJSXPlugin(), islandTagsPlugin(b.Root, false), islandRequirePlugin(), reconcilerGuardPlugin(), reactDOMHintsPlugin(b.Root), reactCompatPlugin(b.Root), routerHooksPlugin(b.Root), threeIslandPlugin(), konvaIslandPlugin()}
	var injects []string
	for _, e := range entries {
		if !e.Boundary {
			continue
		}
		shim, err := boundaryShims(filepath.Join(b.Cfg.OutDir, ".krate"))
		if err != nil {
			return nil, err
		}
		injects = []string{shim}
		plugins = append(plugins, clientOnlyMarkersPlugin(false), actionServerBundlePlugin(b.importResolver(), b.actionModules(), b.Cfg.BaseURLPath()))
		break
	}
	opts := api.BuildOptions{
		AbsWorkingDir:       b.Root,
		EntryPointsAdvanced: points,
		Outdir:              chunksDir,
		EntryNames:          "[name]",
		ChunkNames:          islandSharedStem + "[hash]",
		Splitting:           true,
		Bundle:              true,
		Write:               false,
		MinifyWhitespace:    minify,
		MinifyIdentifiers:   minify,
		MinifySyntax:        minify,
		Format:              api.FormatESModule,
		Platform:            api.PlatformBrowser,
		Target:              api.ES2020,
		JSX:                 api.JSXAutomatic,
		JSXImportSource:     "preact",
		LogLevel:            api.LogLevelSilent,
		Alias:               islandAlias(b.Root, false),
		External:            []string{"preact", "preact/compat", "preact/hooks", "preact/jsx-runtime"},
		Inject:              injects,
		Plugins:             plugins,
		Define:              b.clientEnvDefines(),
	}
	if err := withContextKeys(&opts, b.Root, filepath.Join(b.Cfg.OutDir, ".krate")); err != nil {
		return nil, err
	}
	withNamespaceImports(&opts)
	names := make([]string, len(points))
	for i, p := range points {
		names[i] = p.OutputPath
	}
	return &islandFlavour{opts: opts, names: names, guard: guard, stem: islandSharedStem}, nil
}

// checkIslandFlavour drops the chunks nothing loads from a flavour's result
// and returns its errors.
func (b *Builder) checkIslandFlavour(f *islandFlavour) error {
	result := f.result
	if len(result.Errors) == 0 {
		result.OutputFiles = liveOutputs(result.OutputFiles, f.names)
		interopCJSDynamicImports(result.OutputFiles, result.Metafile, f.opts.AbsWorkingDir)
	}
	result.Errors = append(result.Errors, f.guard.resultErrors(result)...)
	f.result = result
	if len(result.Errors) == 0 {
		return nil
	}
	var msgs []string
	for _, e := range result.Errors {
		if e.Location != nil {
			msgs = append(msgs, fmt.Sprintf("%s:%d:%d: %s", b.esbuildFile(e.Location.File), e.Location.Line, e.Location.Column+1, e.Text))
		} else {
			msgs = append(msgs, e.Text)
		}
	}
	joined := strings.Join(msgs, "\n")
	if !f.real {
		return fmt.Errorf("bundling island chunks:\n%s", joined)
	}
	if be := b.realReactMissing(joined); be != nil {
		return be
	}
	return fmt.Errorf("bundling real-React island chunks:\n%s", joined)
}

// writeIslandChunkFiles writes a build's chunks to /chunks and records their
// names in keep. Shared chunk names (stem + hash) are lower-cased; the cache
// policy recognizes content-hashed names by their lower-case hash.
func writeIslandChunkFiles(files []api.OutputFile, chunksDir, stem string, keep map[string]bool) error {
	rename := map[string]string{}
	var pairs []string
	for _, f := range files {
		base := filepath.Base(f.Path)
		if strings.HasPrefix(base, stem) {
			to := strings.ToLower(base)
			rename[base] = to
			if to != base {
				pairs = append(pairs, base, to)
			}
		}
	}
	// Each name differs from its new one only in case, so one pass over a
	// file renames what replacing the names one at a time would.
	lower := strings.NewReplacer(pairs...)
	for _, f := range files {
		base := filepath.Base(f.Path)
		if strings.HasSuffix(base, ".map") {
			continue
		}
		data := string(f.Contents)
		if len(pairs) > 0 {
			data = lower.Replace(data)
		}
		if to, ok := rename[base]; ok {
			base = to
		}
		keep[base] = true
		if err := fsutil.WriteFileIfChanged(filepath.Join(chunksDir, base), []byte(data), 0644); err != nil {
			return fmt.Errorf("writing island chunk: %w", err)
		}
	}
	return nil
}

// retiredChunksDir is where dev moves the chunks a rebuild replaced, empty
// outside dev (see retireChunk).
func (b *Builder) retiredChunksDir() string {
	if !b.DevMode {
		return ""
	}
	return devRetiredChunksDir(b.Root)
}

// pruneIslandFiles removes island entries and shared chunks an earlier build
// left in /chunks that the current set no longer contains, or moves them to
// retired when that is set.
func pruneIslandFiles(chunksDir string, keep map[string]bool, retired string) {
	files, err := os.ReadDir(chunksDir)
	if err != nil {
		return
	}
	for _, f := range files {
		name := f.Name()
		if f.IsDir() || keep[name] {
			continue
		}
		if strings.HasPrefix(name, islandEntryPrefix) || strings.HasPrefix(name, islandSharedStem) || strings.HasPrefix(name, islandRealSharedStem) || strings.HasPrefix(name, islandNeutralStem) || strings.HasPrefix(name, islandNeutralEntryPrefix) {
			retireChunk(retired, filepath.Join(chunksDir, name))
		}
	}
}
