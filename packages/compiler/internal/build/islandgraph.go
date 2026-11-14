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
	// Real marks a real-React entry, bundled by writeRealIslandBundles.
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
func (b *Builder) resetIslandEntries() {
	b.islandMu.Lock()
	b.islandEntries = nil
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
	if err := b.writeRealIslandBundles(realEntries, chunksDir, keep); err != nil {
		return err
	}
	if len(entries) == 0 {
		pruneIslandFiles(chunksDir, keep)
		return nil
	}
	if _, err := b.ensurePreactShared(); err != nil {
		return fmt.Errorf("island runtime could not be built: %w", err)
	}
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
	plugins := []api.Plugin{islandEntryPlugin(entries, virtual), islandCSSPlugin(sink), islandJSXPlugin(), islandTagsPlugin(b.Root, false), islandRequirePlugin(), reconcilerGuardPlugin()}
	var injects []string
	for _, e := range entries {
		if !e.Boundary {
			continue
		}
		shim, err := boundaryShims(filepath.Join(b.Cfg.OutDir, ".krate"))
		if err != nil {
			return err
		}
		injects = []string{shim}
		plugins = append(plugins, clientOnlyMarkersPlugin(false), actionServerBundlePlugin(b.importResolver(), b.actionModules(), b.Cfg.BaseURLPath()))
		break
	}
	result := esbuildBuild(api.BuildOptions{
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
	})
	if len(result.Errors) > 0 {
		var msgs []string
		for _, e := range result.Errors {
			if e.Location != nil {
				msgs = append(msgs, fmt.Sprintf("%s:%d:%d: %s", b.esbuildFile(e.Location.File), e.Location.Line, e.Location.Column+1, e.Text))
			} else {
				msgs = append(msgs, e.Text)
			}
		}
		return fmt.Errorf("bundling island chunks:\n%s", strings.Join(msgs, "\n"))
	}
	// Shared chunk names are upper-case; the cache policy recognizes
	// content-hashed names by their lower-case hash.
	rename := map[string]string{}
	for _, f := range result.OutputFiles {
		base := filepath.Base(f.Path)
		if strings.HasPrefix(base, islandSharedStem) {
			rename[base] = strings.ToLower(base)
		}
	}
	if err := os.MkdirAll(chunksDir, 0755); err != nil {
		return fmt.Errorf("creating chunks dir: %w", err)
	}
	for _, f := range result.OutputFiles {
		base := filepath.Base(f.Path)
		if strings.HasSuffix(base, ".map") {
			continue
		}
		data := string(f.Contents)
		for from, to := range rename {
			if from != to {
				data = strings.ReplaceAll(data, from, to)
			}
		}
		if to, ok := rename[base]; ok {
			base = to
		}
		keep[base] = true
		if err := fsutil.WriteFileIfChanged(filepath.Join(chunksDir, base), []byte(data), 0644); err != nil {
			return fmt.Errorf("writing island chunk: %w", err)
		}
	}
	pruneIslandFiles(chunksDir, keep)
	return nil
}

// pruneIslandFiles removes island entries and shared chunks an earlier build
// left in /chunks that the current set no longer contains.
func pruneIslandFiles(chunksDir string, keep map[string]bool) {
	files, err := os.ReadDir(chunksDir)
	if err != nil {
		return
	}
	for _, f := range files {
		name := f.Name()
		if f.IsDir() || keep[name] {
			continue
		}
		if strings.HasPrefix(name, islandEntryPrefix) || strings.HasPrefix(name, islandSharedStem) || strings.HasPrefix(name, islandRealSharedStem) {
			_ = os.Remove(filepath.Join(chunksDir, name))
		}
	}
}
