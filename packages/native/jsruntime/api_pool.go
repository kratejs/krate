package jsruntime

import (
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
)

// preparedRoute is the export-stripped source of one route plus the content
// version it was prepared from (so edits invalidate the cache).
type preparedRoute struct {
	code    string
	version int64
}

// apiRunnerBytecode is the route runner (apiruntime.js), compiled once and
// shared by all workers.
var apiRunnerBC sharedBytecode

func apiRunnerBytecode() ([]byte, error) {
	return apiRunnerBC.get(apiRunnerSource)
}

// apiWorker is one long-lived VM in the pool. It caches which routes it has
// already loaded (keyed by route id -> version) so it evaluates each route once.
type apiWorker struct {
	rt     *Runtime
	loaded map[string]int64
	hot    *hotCode
}

// DefaultEmbeddedPoolSize is the default size of the embedded QuickJS API and
// middleware pools. It is 1 on purpose: the VM's libc allocator takes one
// process-wide lock on every malloc, so extra VMs only add contention and
// measured throughput falls as the pool grows. Set ssr.apiWorkers to override.
const DefaultEmbeddedPoolSize = 1

// DefaultAPIWorkers returns the default worker count for the Node/Bun/Deno
// sidecars, min(NumCPU, 8). The embedded runtime uses DefaultEmbeddedPoolSize.
func DefaultAPIWorkers() int {
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	if n > 8 {
		n = 8
	}
	return n
}

// runnerFeatures are the prelude features apiruntime.js itself uses.
var runnerFeatures = []string{"fetch"}

// runnerOptions returns the VM options for an API or middleware runner VM: the
// runner's own features plus the ones the code it serves references, as
// scanned at build time. Unknown (an older build, or a scan that could not
// see everything) installs every feature.
func runnerOptions(features []string, known bool) Options {
	if !known {
		return Options{PreloadAll: true}
	}
	return Options{Preload: append(slices.Clone(runnerFeatures), features...)}
}

// newAPIWorker creates a VM, injects env + the shared handler helpers, and
// returns a ready worker.
func newAPIWorker(opts Options, env map[string]string, hooks *CacheHooks) (*apiWorker, error) {
	rt, err := NewWithOptions(opts)
	if err != nil {
		return nil, err
	}
	if err := rt.registerBodyHost(); err != nil {
		_ = rt.Close()
		return nil, err
	}
	if err := rt.registerCacheHooks(hooks); err != nil {
		_ = rt.Close()
		return nil, err
	}
	if err := rt.SetEnv(env); err != nil {
		_ = rt.Close()
		return nil, err
	}
	bc, err := apiRunnerBytecode()
	if err != nil {
		_ = rt.Close()
		return nil, err
	}
	if _, err := rt.EvalBytecode(bc); err != nil {
		_ = rt.Close()
		return nil, err
	}
	hot, err := compileHot(rt, false)
	if err != nil {
		_ = rt.Close()
		return nil, err
	}
	return &apiWorker{
		rt:     rt,
		loaded: make(map[string]int64),
		hot:    hot,
	}, nil
}

// ensure loads a route module into the worker if it is missing or stale. The
// module's namespace is registered under its route id so the invoke wrapper can
// reach it without re-parsing global names.
func (w *apiWorker) ensure(routeID, code string, version int64) error {
	if v, ok := w.loaded[routeID]; ok && v == version {
		return nil
	}
	// Clear the per-eval global so a stripped-ESM route (which defines its
	// exports as globals) is not shadowed by a previously loaded IIFE route.
	if _, err := w.rt.Execute("globalThis.__krate_route = undefined;"); err != nil {
		return err
	}
	if _, err := w.rt.Execute(code + "\n;void 0;"); err != nil {
		return err
	}
	if _, err := w.rt.Execute(apiRegisterJS(routeID)); err != nil {
		return err
	}
	w.loaded[routeID] = version
	return nil
}

// acquireWorker takes an idle worker, growing the pool up to its limit, or (in
// isolation mode) creates a fresh one for the single request.
func (a *APIRouteRuntime) acquireWorker() (*apiWorker, error) {
	if a.isolation {
		// A fresh VM serves one request, so features load as it uses them.
		return newAPIWorker(Options{Preload: runnerFeatures}, a.env, a.hooks)
	}

	select {
	case w := <-a.idle:
		return w, nil
	default:
	}

	a.mu.Lock()
	if a.created < a.workers {
		a.created++
		a.mu.Unlock()
		w, err := newAPIWorker(runnerOptions(a.features, a.featuresKnown), a.env, a.hooks)
		if err != nil {
			a.mu.Lock()
			a.created--
			a.mu.Unlock()
			return nil, err
		}
		return w, nil
	}
	a.mu.Unlock()

	return <-a.idle, nil
}

// releaseWorker returns a worker to the pool, or closes it in isolation mode.
func (a *APIRouteRuntime) releaseWorker(w *apiWorker) {
	if w == nil {
		return
	}
	if a.isolation {
		if w.rt != nil {
			_ = w.rt.Close()
		}
		return
	}
	a.idle <- w
}

// apiRegisterJS stores a route's module namespace under its route id. IIFE
// builds expose `__krate_route`; stripped ESM/global source leaves the exports
// as globals, so `globalThis` is used as the namespace in that case.
func apiRegisterJS(routeID string) string {
	id, _ := json.Marshal(routeID)
	return fmt.Sprintf(`
		globalThis.__krate_modules = globalThis.__krate_modules || {};
		(function(){
			var M = (typeof globalThis.__krate_route === 'object' && globalThis.__krate_route !== null) ? globalThis.__krate_route : globalThis;
			globalThis.__krate_modules[%s] = M;
			return 'ok';
		})()
	`, string(id))
}
