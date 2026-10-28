package jsruntime

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
)

// krateAsyncPending is the sentinel an invoke wrapper returns when the handler
// produced a promise that still needs its microtasks drained.
const krateAsyncPending = "__KRATE_PENDING__"

// preparedRoute is the export-stripped source of one route plus the content
// version it was prepared from (so edits invalidate the cache).
type preparedRoute struct {
	code    string
	version int64
}

// apiInvokeBytecode is a process-wide cache of compiled invoke-wrapper bytecode,
// keyed by route id + method. Each worker previously compiled its own copy on the
// first request for a route; the wrapper depends only on (route id, method) and
// QuickJS serialized bytecode is portable across VMs of the same library version,
// so one compile is shared by every worker.
var apiInvokeBytecode sync.Map

// sharedInvokeBytecode returns cached bytecode for key, compiling it once via
// compile on a miss.
func sharedInvokeBytecode(key string, compile func() ([]byte, error)) ([]byte, error) {
	if v, ok := apiInvokeBytecode.Load(key); ok {
		return v.([]byte), nil
	}
	bc, err := compile()
	if err != nil {
		return nil, err
	}
	actual, _ := apiInvokeBytecode.LoadOrStore(key, bc)
	return actual.([]byte), nil
}

// apiPendingBytecode is compiled once and shared by all workers (it depends only
// on the static apiFormatPendingJS source).
var (
	apiPendingMu  sync.Mutex
	apiPendingBC  []byte
	apiPendingErr error
)

func apiPendingBytecode(rt *Runtime) ([]byte, error) {
	apiPendingMu.Lock()
	defer apiPendingMu.Unlock()
	if apiPendingBC == nil && apiPendingErr == nil {
		apiPendingBC, apiPendingErr = rt.Compile(apiFormatPendingJS, EvalGlobal)
	}
	return apiPendingBC, apiPendingErr
}

// apiWorker is one long-lived VM in the pool. It caches which routes it has
// already loaded (keyed by route id -> version) so it evaluates each route once.
type apiWorker struct {
	rt      *Runtime
	loaded  map[string]int64
	pending []byte
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
	if _, err := rt.Execute(apiWorkerHelpers); err != nil {
		_ = rt.Close()
		return nil, err
	}
	pending, err := apiPendingBytecode(rt)
	if err != nil {
		_ = rt.Close()
		return nil, err
	}
	return &apiWorker{
		rt:      rt,
		loaded:  make(map[string]int64),
		pending: pending,
	}, nil
}

// invoke runs the handler for a route + method, using process-shared bytecode for
// the wrapper so repeated requests (and other workers) avoid re-parsing.
func (w *apiWorker) invoke(routeID, method string) (string, error) {
	bc, err := sharedInvokeBytecode(routeID+"\x00"+method, func() ([]byte, error) {
		return w.rt.Compile(apiInvokeJS(routeID, method), EvalGlobal)
	})
	if err != nil {
		return "", err
	}
	v, err := w.rt.EvalBytecode(bc)
	if err != nil {
		return "", err
	}
	s, _ := v.(string)
	return s, nil
}

// formatPending formats the value a promise resolved to after jobs are drained.
func (w *apiWorker) formatPending() (string, error) {
	v, err := w.rt.EvalBytecode(w.pending)
	if err != nil {
		return "", err
	}
	s, _ := v.(string)
	return s, nil
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

// apiInvokeJS builds the single-eval request path: resolve the handler for the
// method, call it (async-aware), and return the final JSON response string.
func apiInvokeJS(routeID, method string) string {
	id, _ := json.Marshal(routeID)
	m, _ := json.Marshal(strings.ToUpper(method))
	return fmt.Sprintf(`
		(function(){
			try {
				var ns = globalThis.__krate_modules ? globalThis.__krate_modules[%s] : null;
				if (!ns) return JSON.stringify({ status: 500, body: '', error: 'API route module not loaded' });
				var fn = ns[%s] || ns['__default'] || ns['default'];
				if (typeof fn !== 'function') return JSON.stringify({ status: 500, body: '', error: 'API route must export a handler function' });
				globalThis.__krate_err = '';
				globalThis.__krate_result = undefined;
				var raw = JSON.parse(__krate_req);
				return (fn.length >= 2) ? __krate_legacy(fn, raw) : __krate_modern(fn, raw);
			} catch (e) {
				return JSON.stringify({ status: 500, body: '', error: e.message });
			}
		})()
	`, string(id), string(m))
}

// apiFormatPendingJS formats the value a promise resolved to, after the
// microtask queue has been drained.
const apiFormatPendingJS = `
	(function(){
		if (globalThis.__krate_err) return JSON.stringify({ status: 500, body: '', error: globalThis.__krate_err });
		var r = globalThis.__krate_result;
		if (r === undefined) return JSON.stringify({ status: 500, body: '', error: 'Async handler returned unresolved promise' });
		return __krate_format(r);
	})()
`

// apiWorkerHelpers defines the shared helpers each worker evaluates once:
// __krate_format (result -> JSON), __krate_modern (Request-style handler), and
// __krate_legacy (Node-style handler(req, res)).
const apiWorkerHelpers = `
function __krate_format(result){
	if (result && typeof result.status === 'number') {
		var headers = {};
		if (result.headers) {
			if (typeof result.headers.entries === 'function') {
				var entries = result.headers.entries();
				for (var i = 0; i < entries.length; i++) headers[entries[i][0]] = entries[i][1];
			} else {
				for (var k in result.headers) {
					if (k.charAt(0) !== '_' && typeof result.headers[k] === 'string') headers[k] = result.headers[k];
				}
			}
		}
		var body = '';
		if (result.status !== 204 && typeof result.text === 'function') body = result.text();
		return JSON.stringify({ status: result.status, headers: headers, body: body });
	}
	if (result !== undefined && result !== null) {
		return JSON.stringify({ status: 200, headers: { 'content-type': 'application/json' }, body: JSON.stringify(result) });
	}
	return JSON.stringify({ status: 204, body: '' });
}
function __krate_modern(fn, raw){
	var request = {
		url: raw.url,
		method: raw.method,
		path: raw.path,
		headers: raw.headers || {},
		body: raw.body || '',
		json: function(){ try { return JSON.parse(raw.body || '{}'); } catch (e) { return {}; } },
		text: function(){ return raw.body || ''; }
	};
	var result = fn(request);
	if (result && typeof result.then === 'function') {
		result.then(function(v){ globalThis.__krate_result = v; globalThis.__krate_err = ''; },
			function(e){ globalThis.__krate_err = (e && e.message) ? e.message : 'Promise rejected'; });
		return '__KRATE_PENDING__';
	}
	return __krate_format(result);
}
function __krate_legacy(fn, raw){
	var req = {
		url: raw.url,
		method: raw.method,
		headers: raw.headers || {},
		body: raw.body || '',
		socket: { remoteAddress: '127.0.0.1' }
	};
	var _status = 200, _headers = {}, _body = '', _finished = false;
	var res = {
		statusCode: 200,
		setHeader: function(k, v){ _headers[k.toLowerCase()] = String(v); },
		getHeader: function(k){ return _headers[k.toLowerCase()]; },
		removeHeader: function(k){ delete _headers[k.toLowerCase()]; },
		write: function(chunk){ _body += chunk; },
		end: function(chunk){ if (chunk) _body += chunk; _finished = true; _status = this.statusCode || 200; },
		on: function(){}
	};
	var result = fn(req, res);
	if (result && typeof result.then === 'function') {
		result.then(function(v){
			if (_finished) { globalThis.__krate_result = { status: _status, headers: _headers, body: _body }; }
			else if (v && typeof v.status === 'number') { globalThis.__krate_result = { status: v.status, headers: {}, body: '' }; }
			else { globalThis.__krate_result = { status: 200, headers: _headers, body: _body }; }
			globalThis.__krate_err = '';
		}, function(e){ globalThis.__krate_err = (e && e.message) ? e.message : 'Promise rejected'; });
		return '__KRATE_PENDING__';
	}
	if (_finished) return JSON.stringify({ status: _status, headers: _headers, body: _body });
	if (result && typeof result.status === 'number') return JSON.stringify({ status: result.status, headers: {}, body: '' });
	return JSON.stringify({ status: 200, headers: _headers, body: _body });
}
`
