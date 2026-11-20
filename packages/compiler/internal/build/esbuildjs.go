package build

import (
	"context"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// esbuildBuild is api.Build for bundles that can reach project source, which may
// carry tier decorators.
func esbuildBuild(opts api.BuildOptions) api.BuildResult {
	return esbuildBuildFrom(opts, 3, "")
}

// esbuildBuildFrom is esbuildBuild for a wrapper: skip names the frame of the
// call site the profile counts the call under, prefixed with prefix.
func esbuildBuildFrom(opts api.BuildOptions, skip int, prefix string) api.BuildResult {
	site := prefix + callerSite(skip)
	tsstrip.HandleTierDecorators(&opts)
	withLoadCounter(&opts)
	t0 := time.Now()
	var res api.BuildResult
	pprof.Do(context.Background(), pprof.Labels("esbuild", site), func(context.Context) {
		res = api.Build(opts)
	})
	recordEsbuild(site, time.Since(t0))
	if esbuildDirStatsOn.Load() {
		recordEsbuildDir(esbuildDirOf(opts), site)
	}
	return res
}

// callerSite names the function skip frames up the stack, without its package
// path ("(*Builder).clientChunkFor").
func callerSite(skip int) string {
	pc, _, _, ok := runtime.Caller(skip)
	if !ok {
		return "unknown"
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "unknown"
	}
	name := fn.Name()
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.Index(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// esbuildStats counts esbuild invocations per call site for --profile.
var esbuildStats = struct {
	sync.Mutex
	m map[string]esbuildStat
}{m: map[string]esbuildStat{}}

type esbuildStat struct {
	n int
	d time.Duration
}

func recordEsbuild(site string, d time.Duration) {
	esbuildStats.Lock()
	s := esbuildStats.m[site]
	s.n++
	s.d += d
	esbuildStats.m[site] = s
	esbuildStats.Unlock()
}

// esbuildDirStatsOn makes every esbuild call also count under the directory
// it builds in (esbuildDirOf), so tests running side by side can count the
// calls of their own project. Off outside tests.
var esbuildDirStatsOn atomic.Bool

var esbuildDirStats = struct {
	sync.Mutex
	m map[[2]string]int // {dir, site} -> calls
}{m: map[[2]string]int{}}

func recordEsbuildDir(dir, site string) {
	esbuildDirStats.Lock()
	esbuildDirStats.m[[2]string{dir, site}]++
	esbuildDirStats.Unlock()
}

// esbuildDirOf is the directory a build works in: its working directory, the
// directory of its stdin entry, its output, or its first entry point.
func esbuildDirOf(opts api.BuildOptions) string {
	switch {
	case opts.AbsWorkingDir != "":
		return opts.AbsWorkingDir
	case opts.Stdin != nil && opts.Stdin.ResolveDir != "":
		return opts.Stdin.ResolveDir
	case opts.Outdir != "":
		return opts.Outdir
	case opts.Outfile != "":
		return filepath.Dir(opts.Outfile)
	case len(opts.EntryPoints) > 0:
		return filepath.Dir(opts.EntryPoints[0])
	case len(opts.EntryPointsAdvanced) > 0:
		return filepath.Dir(opts.EntryPointsAdvanced[0].InputPath)
	}
	return ""
}

// esbuildCallsUnder returns the calls per site counted under root so far
// (esbuildDirStatsOn must be set).
func esbuildCallsUnder(root string) map[string]int {
	root = filepath.Clean(root)
	esbuildDirStats.Lock()
	defer esbuildDirStats.Unlock()
	out := map[string]int{}
	for k, n := range esbuildDirStats.m {
		if d := filepath.Clean(k[0]); d == root || strings.HasPrefix(d, root+string(filepath.Separator)) {
			out[k[1]] += n
		}
	}
	return out
}

// esbuildCallsSince returns the calls per site counted under root since
// before (an esbuildCallsUnder result).
func esbuildCallsSince(root string, before map[string]int) map[string]int {
	out := map[string]int{}
	for site, n := range esbuildCallsUnder(root) {
		if n > before[site] {
			out[site] = n - before[site]
		}
	}
	return out
}

// esbuildStatsSnapshot copies the per-site counters.
func esbuildStatsSnapshot() map[string]esbuildStat {
	esbuildStats.Lock()
	defer esbuildStats.Unlock()
	out := make(map[string]esbuildStat, len(esbuildStats.m))
	for k, v := range esbuildStats.m {
		out[k] = v
	}
	return out
}

// esbuildStatsSince returns the calls made since the snapshot before.
func esbuildStatsSince(before map[string]esbuildStat) map[string]esbuildStat {
	out := map[string]esbuildStat{}
	for k, v := range esbuildStatsSnapshot() {
		b := before[k]
		if v.n > b.n {
			out[k] = esbuildStat{n: v.n - b.n, d: v.d - b.d}
		}
	}
	return out
}

// esbuildLoads, when set (by tests), counts how many times esbuild loads
// each file across every build of the process.
var esbuildLoads atomic.Pointer[fileLoadCounter]

type fileLoadCounter struct {
	mu sync.Mutex
	n  map[string]int
}

func newFileLoadCounter() *fileLoadCounter { return &fileLoadCounter{n: map[string]int{}} }

func (c *fileLoadCounter) counts() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int, len(c.n))
	for k, v := range c.n {
		out[k] = v
	}
	return out
}

// withLoadCounter puts the load counter first in opts, so it sees every file
// load before a plugin answers it.
func withLoadCounter(opts *api.BuildOptions) {
	c := esbuildLoads.Load()
	if c == nil {
		return
	}
	counter := api.Plugin{
		Name: "krate-test-load-counter",
		Setup: func(pb api.PluginBuild) {
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				c.mu.Lock()
				c.n[args.Path]++
				c.mu.Unlock()
				return api.OnLoadResult{}, nil
			})
		},
	}
	opts.Plugins = append([]api.Plugin{counter}, opts.Plugins...)
}
