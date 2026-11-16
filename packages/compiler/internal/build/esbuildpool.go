package build

import (
	"context"
	"path/filepath"
	"regexp"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// esbuildPool builds generated entry modules that share one set of options,
// each as a build of its own, on a few reused esbuild contexts. A context
// keeps the files it parsed, so a module the entries share is parsed once per
// context instead of once per entry.
//
// An entry stands in for a Stdin build: it is the module ResolveDir/Sourcefile
// in the file namespace, which is how esbuild presents Stdin to plugins (as
// the importer of its imports) and names it in output comments, served from
// memory. A batched build of many entry points cannot stand in for separate
// builds: esbuild numbers the symbols of non-minified output across the whole
// build.
type poolEntry struct {
	Source, ResolveDir, Sourcefile string
	Loader                         api.Loader
}

func (e poolEntry) path() string { return filepath.Join(e.ResolveDir, e.Sourcefile) }

// esbuildPoolStream runs workers contexts with opts (which must not set Stdin
// or EntryPoints; Write must be off). Each builds the entries next hands out
// (ok false ends it) and reports each result with its id to emit, which must
// be safe for concurrent use. The returned channel closes once every entry is
// built. All entries must share one Sourcefile.
func esbuildPoolStream(site string, opts api.BuildOptions, sourcefile string, workers int, next func() (poolEntry, int, bool), emit func(id int, res api.BuildResult)) <-chan struct{} {
	var mu sync.Mutex
	take := func() (poolEntry, int, bool) {
		mu.Lock()
		defer mu.Unlock()
		return next()
	}
	var wg sync.WaitGroup
	for w := 0; w < max(1, workers); w++ {
		wg.Add(1)
		go pprof.Do(context.Background(), pprof.Labels("esbuild", site), func(context.Context) {
			defer wg.Done()
			cur := &poolCurrent{}
			o := opts
			o.Stdin = nil
			o.EntryPoints = []string{poolEntryName}
			o.EntryPointsAdvanced = nil
			o.Plugins = append([]api.Plugin{poolEntryPlugin(cur, sourcefile)}, opts.Plugins...)
			tsstrip.HandleTierDecorators(&o)
			withLoadCounter(&o)
			ctx, cerr := api.Context(o)
			for {
				e, id, ok := take()
				if !ok {
					break
				}
				if cerr != nil {
					emit(id, api.BuildResult{Errors: cerr.Errors})
					continue
				}
				// Stdin gets the tier decorators blanked by esbuildBuild.
				e.Source = tsstrip.BlankTierDecorators(e.Sourcefile, e.Source)
				cur.set(e)
				t0 := time.Now()
				res := ctx.Rebuild()
				recordEsbuild(site, time.Since(t0))
				emit(id, res)
			}
			if ctx != nil {
				ctx.Dispose()
			}
		})
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	return done
}

const poolEntryName = "krate-pool-entry"

// poolCurrent is the entry a context builds now.
type poolCurrent struct {
	mu sync.Mutex
	e  poolEntry
}

func (c *poolCurrent) set(e poolEntry) {
	c.mu.Lock()
	c.e = e
	c.mu.Unlock()
}

func (c *poolCurrent) get() poolEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.e
}

// poolEntryPlugin resolves the pool's entry to the entry its context builds
// now and serves its source.
func poolEntryPlugin(cur *poolCurrent, sourcefile string) api.Plugin {
	return api.Plugin{
		Name: "krate-pool-entry",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^` + poolEntryName + `$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.Kind != api.ResolveEntryPoint {
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: cur.get().path(), Namespace: "file"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: regexp.QuoteMeta(sourcefile) + `$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				e := cur.get()
				if args.Path != e.path() {
					return api.OnLoadResult{}, nil
				}
				src := e.Source
				return api.OnLoadResult{Contents: &src, ResolveDir: e.ResolveDir, Loader: e.Loader}, nil
			})
		},
	}
}
