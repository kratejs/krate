package jsruntime

import (
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
)

// preparedRoute is the export-stripped source of one route plus the content
// version it was prepared from (so edits invalidate the cache).
type preparedRoute struct {
	code    string
	version int64
}

// apiRunnerBytecode is the route runner (apiruntime.js), compiled once and
// shared by all workers.
var (
	apiRunnerMu  sync.Mutex
	apiRunnerBC  []byte
	apiRunnerErr error
)

func apiRunnerBytecode(rt *Runtime) ([]byte, error) {
	apiRunnerMu.Lock()
	defer apiRunnerMu.Unlock()
	if apiRunnerBC == nil && apiRunnerErr == nil {
		apiRunnerBC, apiRunnerErr = rt.Compile(apiRunnerSource, EvalGlobal)
	}
	return apiRunnerBC, apiRunnerErr
}

// apiWorker is one long-lived VM in the pool. It caches which routes it has
// already loaded (keyed by route id -> version) so it evaluates each route once.
type apiWorker struct {
	rt     *Runtime
	loaded map[string]int64
	begin  []byte
}

// DefaultAPIWorkers returns the default embedded API worker-pool size.
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

// newAPIWorker creates a VM, injects env + the shared handler helpers, and
// returns a ready worker.
func newAPIWorker(env map[string]string, hooks *CacheHooks) (*apiWorker, error) {
	rt, err := New()
	if err != nil {
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
	bc, err := apiRunnerBytecode(rt)
	if err != nil {
		_ = rt.Close()
		return nil, err
	}
	if _, err := rt.EvalBytecode(bc); err != nil {
		_ = rt.Close()
		return nil, err
	}
	begin, err := rt.Compile("__krate_begin(__krate_route_id, __krate_method, __krate_req)", EvalGlobal)
	if err != nil {
		_ = rt.Close()
		return nil, err
	}
	return &apiWorker{
		rt:     rt,
		loaded: make(map[string]int64),
		begin:  begin,
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
	if _, err := w.rt.Execute(code); err != nil {
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
		return newAPIWorker(a.env, a.hooks)
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
		w, err := newAPIWorker(a.env, a.hooks)
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
