package build

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/evanw/esbuild/pkg/api"
)

// nodeRouteChunks is the directory, under the Node route bundles, of the
// chunks linkNodeRoutes splits out.
const nodeRouteChunks = "_chunks"

// linkNodeRoutes rebuilds the Node bundles of a production build's sidecar
// routes as one code-split build. Built one by one, every route carries its
// own copy of the app modules it imports, so a process that serves many
// routes loads (and keeps) each module once per route, along with its
// module state: a connection pool created at module scope is created once
// per route. Linked, the routes import the modules they share from chunks
// and get one instance of each, as in Next.js. Packages stay external as
// before. Any failure keeps the bundles the per-route builds wrote.
func (b *Builder) linkNodeRoutes(apiSrcDir string, routes map[string]apiRouteMeta) {
	if b.DevMode || b.noLinkedNodeRoutes {
		return
	}
	embedded := embeddedRuntime(b.Cfg.SSR.APIRuntime)
	ndir := apiNodeBundleDir(b.Cfg.OutDir)
	// The routes were just written unlinked: chunks of an earlier build are
	// imported by nothing.
	_ = os.RemoveAll(filepath.Join(ndir, nodeRouteChunks))
	var srcs []string
	for src, m := range routes {
		if !embedded || m.NodeOnly {
			srcs = append(srcs, src)
		}
	}
	if len(srcs) < 2 {
		return
	}
	sort.Strings(srcs)
	entries := make([]api.EntryPoint, 0, len(srcs))
	dirs := []string{filepath.Join(ndir, nodeRouteChunks)}
	for _, src := range srcs {
		rel, err := filepath.Rel(apiSrcDir, src)
		if err != nil || !filepath.IsLocal(rel) {
			return
		}
		out := strings.TrimSuffix(rel, filepath.Ext(rel))
		entries = append(entries, api.EntryPoint{InputPath: src, OutputPath: filepath.ToSlash(out)})
		dirs = append(dirs, filepath.Dir(filepath.Join(ndir, out)))
	}
	_, o := b.apiRouteOptions()
	o.EntryPoints, o.Stdin, o.Outfile = nil, nil, ""
	o.EntryPointsAdvanced = entries
	o.Outdir = ndir
	o.Outbase = ""
	o.Splitting = true
	o.ChunkNames = nodeRouteChunks + "/[name]-[hash]"
	o.Write = false
	o.Metafile = false
	var mixed atomic.Bool
	o.Plugins = append(append([]api.Plugin(nil), o.Plugins...), externalNodeModulesPluginDirs(dirs, nil, &mixed))
	res := esbuildBuild(o)
	if len(res.Errors) > 0 || mixed.Load() {
		b.Prof.Add("api-node-link-skipped", 0)
		return
	}
	if err := writeOutputFiles(res.OutputFiles); err != nil {
		b.Prof.Add("api-node-link-skipped", 0)
	}
}
