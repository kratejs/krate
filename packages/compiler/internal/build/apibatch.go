package build

import (
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/evanw/esbuild/pkg/api"
)

// apiRoutePlan is what the batched builds of CompileAPIRoutes settled for a
// route before compileSingleRoute runs.
type apiRoutePlan struct {
	// detected is set when the batched detection decided nodeOnly, which is
	// then what provablyNodeOnly returns for the route.
	detected, nodeOnly bool
	// node is the route's Node bundle, and quickjs its QuickJS bundle as the
	// first attempt of buildExternalizingNative builds it; nil when the route
	// builds its own.
	node, quickjs *batchOutput
	// decision is routeRuntimeDecision's verdict on the route's QuickJS
	// bundle, when the batch already made it.
	decision *routeDecision
}

// planAPIRoutes runs the esbuild work of routes in batched calls of at most
// fileBatchMax entries, in parallel on pool. With the embedded runtime it runs
// the Node-only detection builds, then the Node bundles of the proven
// Node-only routes and the QuickJS bundles of the others; with a Node API
// runtime, the Node bundles. A route a batch cannot serve, or outside
// apiSrcDir, gets a partial plan or none and builds the rest on its own.
func (b *Builder) planAPIRoutes(routes []string, apiSrcDir string, fam *apiRouteFamily, pool *workerPool) map[string]*apiRoutePlan {
	quickjsOpts, nodeOpts := b.apiRouteOptions()
	qdir, ndir := apiBundleDir(b.Cfg.OutDir), apiNodeBundleDir(b.Cfg.OutDir)
	extra := b.Cfg.ServerExternalPackages

	type item struct {
		file          string
		plan          *apiRoutePlan
		quickjs, node fileBatchEntry
	}
	plans := make(map[string]*apiRoutePlan, len(routes))
	var items []*item
	for _, r := range routes {
		rel, err := filepath.Rel(apiSrcDir, r)
		if err != nil || !filepath.IsLocal(rel) {
			continue
		}
		outName := strings.TrimSuffix(rel, filepath.Ext(rel)) + ".js"
		it := &item{
			file:    r,
			plan:    &apiRoutePlan{},
			quickjs: fileBatchEntry{In: r, Out: filepath.Join(qdir, outName)},
			node:    fileBatchEntry{In: r, Out: filepath.Join(ndir, outName)},
		}
		plans[r] = it.plan
		items = append(items, it)
	}

	// batches runs run on each batch of items, in parallel.
	batches := func(items []*item, run func(part []*item, entries []fileBatchEntry)) {
		var wg sync.WaitGroup
		for _, part := range splitBatches(items, fileBatchMax) {
			if len(part) < 2 {
				continue
			}
			wg.Go(func() {
				pool.acquire()
				defer pool.release()
				entries := make([]fileBatchEntry, len(part))
				for i, it := range part {
					entries[i] = it.quickjs
				}
				run(part, entries)
			})
		}
		wg.Wait()
	}
	nodeBundles := func(items []*item) {
		batches(items, func(part []*item, _ []fileBatchEntry) {
			entries := make([]fileBatchEntry, len(part))
			for i, it := range part {
				entries[i] = it.node
			}
			outs := batchNodeBundles(nodeOpts, ndir, entries, &fam.node)
			for i, it := range part {
				if outs == nil {
					b.Prof.Add("batch:api-node-unbatched", 0)
				} else {
					it.plan.node = outs[i]
				}
			}
		})
	}

	if !embeddedRuntime(b.Cfg.SSR.APIRuntime) {
		nodeBundles(items)
		return plans
	}
	if legacyNodeOnlyDetection {
		return plans
	}
	batches(items, func(part []*item, entries []fileBatchEntry) {
		proven := provablyNodeOnlyBatch(quickjsOpts, extra, fam, qdir, entries)
		for i, it := range part {
			if proven == nil {
				b.Prof.Add("batch:api-detect-unbatched", 0)
			} else {
				it.plan.detected, it.plan.nodeOnly = true, proven[i]
			}
		}
	})
	var nodeOnly, embedded []*item
	for _, it := range items {
		if it.plan.detected && readAPIRouteRuntime(it.file) == apiRuntimeNodeJS {
			it.plan.nodeOnly = true
		}
		switch {
		case !it.plan.detected:
		case it.plan.nodeOnly:
			nodeOnly = append(nodeOnly, it)
		default:
			embedded = append(embedded, it)
		}
	}
	nodeBundles(nodeOnly)
	var (
		movedMu sync.Mutex
		moved   []*item
	)
	batches(embedded, func(part []*item, entries []fileBatchEntry) {
		outs := batchQuickJSBundles(quickjsOpts, extra, qdir, entries)
		for i, it := range part {
			if outs == nil {
				b.Prof.Add("batch:api-quickjs-unbatched", 0)
				continue
			}
			it.plan.quickjs = outs[i]
			// The same decision compileSingleRoute makes; routes it moves
			// to Node get their Node bundles in a batch too.
			d := b.routeRuntimeDecision(outs[i].Metafile, readAPIRouteRuntime(it.file))
			it.plan.decision = &d
			if d.nodeOnly {
				movedMu.Lock()
				moved = append(moved, it)
				movedMu.Unlock()
			}
		}
	})
	nodeBundles(moved)
	return plans
}

// batchQuickJSBundles builds the QuickJS bundles of entries in one call, as
// the first attempt of buildExternalizingNative builds each. It returns nil
// when the batch fails, a native addon included, which leaves the routes to
// buildExternalizingNative and its retries.
func batchQuickJSBundles(quickjsOpts api.BuildOptions, extra []string, outdir string, entries []fileBatchEntry) []*batchOutput {
	outs, _ := esbuildBatchFiles(externalizingNativeOptions(quickjsOpts, extra, &nativeAddons{}), outdir, entries)
	return outs
}

// batchNodeBundles builds the Node bundles of entries in one call, nil when
// the batch fails or the entries' output directories would import a package
// differently (externalNodeModulesPluginDirs).
func batchNodeBundles(nodeOpts api.BuildOptions, outdir string, entries []fileBatchEntry, memo *resolveMemo) []*batchOutput {
	var mixed atomic.Bool
	dirs := make([]string, len(entries))
	for i, e := range entries {
		dirs[i] = filepath.Dir(e.Out)
	}
	o := nodeOpts
	o.Plugins = append(append([]api.Plugin(nil), nodeOpts.Plugins...), externalNodeModulesPluginDirs(dirs, memo, &mixed))
	outs, _ := esbuildBatchFiles(o, outdir, entries)
	if mixed.Load() {
		return nil
	}
	return outs
}
