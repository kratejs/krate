package jsruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// APIRouteRuntime executes compiled API routes via embedded quickjs.
//
// In the default (shared) mode it keeps a pool of long-lived VMs, each of which
// loads a route module once and reuses it, so a request costs one handler call
// rather than a fresh VM + full module re-evaluation. Module state persists per
// worker across requests, matching Node/Next semantics.
//
// In isolation mode it creates a fresh VM per request (slower, but each request
// gets clean module state); this is also used in dev so file edits are picked up.
type APIRouteRuntime struct {
	apiDir    string
	env       map[string]string
	hooks     *CacheHooks
	workers   int
	isolation bool

	mu      sync.Mutex
	created int
	idle    chan *apiWorker

	prepMu sync.Mutex
	prep   map[string]*preparedRoute
}

// APIRequest is the input to an API route handler.
type APIRequest struct {
	URL    string
	Method string
	Path   string
	// Headers are the request headers as received (multi-valued).
	Headers map[string][]string
	// Body is the raw request body; it crosses into the VM as base64.
	Body []byte
	// Params are the dynamic route segments ([id], [...slug]), matching the
	// second argument Next.js route handlers receive.
	Params map[string]string
	// RemoteAddr is the client address (host:port) for legacy handlers.
	RemoteAddr string
}

// APIResult is a fully buffered API response (used by tests and callers that
// do not stream).
type APIResult struct {
	Status  int
	Headers map[string][]string
	Body    []byte
	Error   string
	Stack   string
	Allow   []string
}

// APISink receives a response as the handler produces it.
type APISink interface {
	// Start is called once, before any Write, with the response head.
	Start(status int, headers [][2]string) error
	// Write delivers one body chunk; an error means the client is gone.
	Write(p []byte) error
}

// APIFailure describes a request that did not produce a response head (or a
// stream that broke afterwards, with Started set).
type APIFailure struct {
	Status  int
	Message string
	Stack   string
	Allow   []string
	Started bool
}

// stripExports removes ESM export statements and converts named exports to globals.
// esbuild ESM output: function GET() {...}; export { GET }; export { _default as default };
// Handles both single-line and multi-line export blocks.
func stripExports(code string) string {
	// Phase 1: Whole-text regex replacements for multi-line patterns

	// export { X as default }; -> var __default = X;
	reMultiLineAsDefault := regexp.MustCompile(`export\s*\{\s*(\w+)\s+as\s+(\w+)\s*\}\s*;?`)
	code = reMultiLineAsDefault.ReplaceAllString(code, "var __default = $1;")

	// export { a as X, b as Y }; -> var X = a; var Y = b;
	// Handle multi-name export blocks with as-clauses
	reExportAsBlock := regexp.MustCompile(`export\s*\{([^}]*)\}`)
	if m := reExportAsBlock.FindStringSubmatch(code); m != nil {
		names := m[1]
		var bindings []string
		// Split by comma and process each
		for _, part := range strings.Split(names, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			// Check for "local as exported". A plain name (no "as") is already
			// a global from its function/const declaration, so skip it.
			asParts := strings.SplitN(part, " as ", 2)
			if len(asParts) != 2 {
				continue
			}
			local := strings.TrimSpace(asParts[0])
			exported := strings.TrimSpace(asParts[1])
			if exported == "default" {
				bindings = append(bindings, fmt.Sprintf("var __default = %s;", local))
			} else {
				bindings = append(bindings, fmt.Sprintf("var %s = %s;", exported, local))
			}
		}
		// Replace the entire export block with the bindings
		reFullBlock := regexp.MustCompile(`export\s*\{[^}]*\}\s*;?`)
		code = reFullBlock.ReplaceAllString(code, strings.Join(bindings, "\n"))
	}

	// Phase 2: Line-by-line replacements for single-line patterns
	lines := strings.Split(code, "\n")
	var result []string

	reExportDefaultDecl := regexp.MustCompile(`^export\s+default\s+`)
	reExportDecl := regexp.MustCompile(`^export\s+(async\s+function|const|let|var|function|class)\s+`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// export default ... -> need special handling
		if reExportDefaultDecl.MatchString(trimmed) {
			stripped := reExportDefaultDecl.ReplaceAllString(trimmed, "")
			// "export default function(...)" -> "function _default(...)"
			reAnonFunc := regexp.MustCompile(`^function\s*\(`)
			if reAnonFunc.MatchString(stripped) {
				stripped = "function _default(" + strings.TrimPrefix(stripped, "function(")
			}
			result = append(result, stripped)
			result = append(result, "var __default = (typeof _default !== 'undefined') ? _default : undefined;")
			continue
		}

		// export const/let/var/function/class -> const/let/var/function/class
		if reExportDecl.MatchString(trimmed) {
			result = append(result, reExportDecl.ReplaceAllString(trimmed, "$1 "))
			continue
		}

		// Skip any remaining bare export lines that slipped through
		if strings.HasPrefix(trimmed, "export ") {
			continue
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// NewAPIRouteRuntime creates a new API route runtime
func NewAPIRouteRuntime(apiDir string) *APIRouteRuntime {
	return &APIRouteRuntime{
		apiDir:  apiDir,
		workers: DefaultAPIWorkers(),
		idle:    make(chan *apiWorker, DefaultAPIWorkers()),
		prep:    make(map[string]*preparedRoute),
	}
}

// SetEnv injects environment variables into every worker created by the pool.
// Call before serving.
func (a *APIRouteRuntime) SetEnv(env map[string]string) {
	a.env = env
	// Propagate to already-created idle workers (best effort).
	for {
		select {
		case w := <-a.idle:
			_ = w.rt.SetEnv(env)
			a.idle <- w
		default:
			return
		}
	}
}

// SetCacheHooks connects routes to the host's revalidation endpoint and data
// cache (revalidatePath/revalidateTag, unstable_cache, fetch caching). Call
// before serving.
func (a *APIRouteRuntime) SetCacheHooks(h *CacheHooks) {
	a.hooks = h
}

// SetPool configures the pool size and whether each request runs in a fresh VM
// (isolation) instead of a shared, stateful worker. Call before serving.
func (a *APIRouteRuntime) SetPool(workers int, isolation bool) {
	if workers > 0 {
		a.workers = workers
		a.idle = make(chan *apiWorker, workers)
	}
	a.isolation = isolation
}

// Close releases all idle workers.
func (a *APIRouteRuntime) Close() {
	for {
		select {
		case w := <-a.idle:
			if w != nil && w.rt != nil {
				_ = w.rt.Close()
			}
		default:
			return
		}
	}
}

// bufferSink collects a response into memory.
type bufferSink struct {
	status  int
	headers [][2]string
	body    []byte
}

func (b *bufferSink) Start(status int, headers [][2]string) error {
	b.status, b.headers = status, headers
	return nil
}

func (b *bufferSink) Write(p []byte) error {
	b.body = append(b.body, p...)
	return nil
}

// Execute runs the API route handler for the given path and buffers the
// whole response.
func (a *APIRouteRuntime) Execute(req APIRequest) APIResult {
	sink := &bufferSink{}
	fail := a.Serve(context.Background(), req, sink)
	if fail != nil && !fail.Started {
		res := APIResult{Status: fail.Status, Error: fail.Message, Stack: fail.Stack, Allow: fail.Allow, Body: []byte(jsonError(fail.Message))}
		res.Headers = map[string][]string{"content-type": {"application/json"}}
		return res
	}
	res := APIResult{Status: sink.status, Body: sink.body, Headers: map[string][]string{}}
	for _, h := range sink.headers {
		res.Headers[h[0]] = append(res.Headers[h[0]], h[1])
	}
	if fail != nil {
		res.Error = fail.Message
	}
	return res
}

// apiTimeout bounds how long a handler may stay pending (per phase: waiting
// for the response head, and waiting for each streamed chunk).
func apiTimeout() time.Duration { return envDuration("KRATE_API_TIMEOUT", 30*time.Second) }

// Serve runs the route and streams the response into sink. It returns nil on
// success, or a failure describing what went wrong.
func (a *APIRouteRuntime) Serve(ctx context.Context, req APIRequest, sink APISink) *APIFailure {
	relPath := strings.TrimPrefix(req.Path, "/api")
	if relPath == "" {
		relPath = "/index"
	}

	targetFile := a.resolveAPIFile(relPath)
	if targetFile == "" {
		return &APIFailure{Status: 404, Message: "API Route Not Found"}
	}

	version := fileVersion(targetFile)
	prep, err := a.prepared(targetFile, version)
	if err != nil {
		return &APIFailure{Status: 500, Message: "Failed to read API route: " + err.Error()}
	}

	worker, err := a.acquireWorker()
	if err != nil {
		return &APIFailure{Status: 500, Message: "Failed to create JS runtime: " + err.Error()}
	}
	defer a.releaseWorker(worker)

	if err := worker.ensure(targetFile, prep.code, version); err != nil {
		return &APIFailure{Status: 500, Message: err.Error()}
	}

	req.Params = apiRouteParams(a.apiDir, targetFile, req.Path)
	return worker.run(ctx, targetFile, req, sink)
}

// prepared returns the (export-stripped) source for a route, cached by content
// version so a stable build reads/strips each route exactly once.
func (a *APIRouteRuntime) prepared(path string, version int64) (*preparedRoute, error) {
	a.prepMu.Lock()
	defer a.prepMu.Unlock()

	if p, ok := a.prep[path]; ok && p.version == version {
		return p, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p := &preparedRoute{code: stripExports(string(data)), version: version}
	a.prep[path] = p
	return p, nil
}

// fileVersion is a cheap change token: modification time (ns) plus size. It
// lets a long-lived worker detect an edited route and reload it (dev).
func fileVersion(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.ModTime().UnixNano() ^ (fi.Size() << 1)
}

// ResolveFile returns the out-dir-relative file name (slash-separated, e.g.
// "users/index.js") that would serve the given request path, or "" when none
// exists. The server uses it to decide whether a Node-only route must be
// forwarded to the sidecar.
func (a *APIRouteRuntime) ResolveFile(reqPath string) string {
	relPath := strings.TrimPrefix(reqPath, "/api")
	if relPath == "" {
		relPath = "/index"
	}
	p := a.resolveAPIFile(relPath)
	if p == "" {
		return ""
	}
	rel, err := filepath.Rel(a.apiDir, p)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

// apiRouteParams extracts dynamic segments from a request path using the
// matched route file ([id], [...slug], [[...slug]]).
func apiRouteParams(apiDir, file, reqPath string) map[string]string {
	rel, err := filepath.Rel(apiDir, file)
	if err != nil || rel == "" {
		return nil
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimSuffix(rel, filepath.Ext(rel))
	if rel == "index" {
		rel = ""
	} else if strings.HasSuffix(rel, "/index") {
		rel = strings.TrimSuffix(rel, "/index")
	}
	fileSegs := strings.Split(strings.Trim(rel, "/"), "/")
	if rel == "" {
		fileSegs = nil
	}
	rawPath := strings.TrimPrefix(reqPath, "/api")
	pathSegs := strings.Split(strings.Trim(rawPath, "/"), "/")
	if rawPath == "" || rawPath == "/" {
		pathSegs = nil
	}
	params := map[string]string{}
	for i, seg := range fileSegs {
		switch {
		case strings.HasPrefix(seg, "[[...") && strings.HasSuffix(seg, "]]"):
			name := strings.TrimSuffix(strings.TrimPrefix(seg, "[[..."), "]]")
			if i < len(pathSegs) && len(pathSegs) > i {
				params[name] = strings.Join(pathSegs[i:], "/")
			}
			return params
		case strings.HasPrefix(seg, "[...") && strings.HasSuffix(seg, "]"):
			name := strings.TrimSuffix(strings.TrimPrefix(seg, "[..."), "]")
			params[name] = strings.Join(pathSegs[minInt(i, len(pathSegs)):], "/")
			return params
		case strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]"):
			name := strings.TrimSuffix(strings.TrimPrefix(seg, "["), "]")
			if i < len(pathSegs) {
				params[name] = pathSegs[i]
			}
		}
	}
	if len(params) == 0 {
		return nil
	}
	return params
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// resolveAPIFile finds the compiled JS file for a given API path. The path is
// attacker-controlled (it derives from the request URL), so it is validated
// before touching the filesystem: any residual percent-encoding is decoded,
// path separators and traversal segments are rejected, and the resolved file
// must remain inside the API directory.
func (a *APIRouteRuntime) resolveAPIFile(relPath string) string {
	if decoded, err := url.PathUnescape(relPath); err == nil {
		relPath = decoded
	}
	relPath = strings.TrimLeft(relPath, "/")
	if relPath == "" {
		relPath = "index"
	}

	// Reject null bytes and backslashes (a path separator on Windows). Forward
	// slashes between segments are legitimate (nested route dirs), but a ".."
	// segment must never be allowed to walk out of the API directory.
	if strings.ContainsAny(relPath, "\x00\\") {
		return ""
	}
	for _, seg := range strings.Split(relPath, "/") {
		if seg == ".." {
			return ""
		}
	}

	extensions := []string{".js", ".ts"}
	for _, ext := range extensions {
		if p := a.containedPath(relPath, ext); p != "" {
			return p
		}
	}

	for _, ext := range extensions {
		if p := a.containedPath(filepath.Join(relPath, "index"), ext); p != "" {
			return p
		}
	}

	return a.resolveDynamicAPIFile(strings.Split(relPath, "/"), extensions)
}

// resolveDynamicAPIFile matches [id], [...slug] and [[...slug]] route files
// when the literal path does not exist.
func (a *APIRouteRuntime) resolveDynamicAPIFile(segs, exts []string) string {
	return a.resolveDynamicAPIDir(a.apiDir, segs, exts)
}

func (a *APIRouteRuntime) resolveDynamicAPIDir(dir string, segs, exts []string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	if len(segs) == 0 {
		for _, ext := range exts {
			if fi, err := os.Stat(filepath.Join(dir, "index"+ext)); err == nil && !fi.IsDir() {
				return filepath.Join(dir, "index"+ext)
			}
		}
		return ""
	}
	if len(segs) > 0 {
		next := filepath.Join(dir, segs[0])
		if fi, err := os.Stat(next); err == nil && fi.IsDir() {
			if p := a.resolveDynamicAPIDir(next, segs[1:], exts); p != "" {
				return p
			}
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		full := filepath.Join(dir, name)
		if entry.IsDir() {
			if isCatchAllSegment(name) && len(segs) > 0 {
				rel, _ := filepath.Rel(a.apiDir, full)
				for _, ext := range exts {
					if p := a.containedPath(filepath.Join(rel, "index"), ext); p != "" {
						return p
					}
				}
			}
			if isDynamicSegment(name) && len(segs) > 0 {
				if p := a.resolveDynamicAPIDir(full, segs[1:], exts); p != "" {
					return p
				}
			}
			continue
		}
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if isCatchAllSegment(base) && (len(segs) > 0 || strings.HasPrefix(base, "[[...")) {
			return full
		}
		if len(segs) == 1 && isDynamicSegment(base) {
			return full
		}
	}
	return ""
}

func isDynamicSegment(name string) bool {
	return strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]") && !strings.HasPrefix(name, "[...") && !strings.HasPrefix(name, "[[...")
}

func isCatchAllSegment(name string) bool {
	return strings.HasPrefix(name, "[...") || strings.HasPrefix(name, "[[...")
}

// containedPath joins apiDir with the given relative path and extension, then
// verifies the result is still inside apiDir. Returns "" when the candidate
// path would escape the API directory or does not exist.
func (a *APIRouteRuntime) containedPath(rel string, ext string) string {
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return ""
	}
	p := filepath.Join(a.apiDir, clean+ext)
	relToDir, err := filepath.Rel(a.apiDir, p)
	if err != nil || relToDir == ".." || strings.HasPrefix(relToDir, ".."+string(filepath.Separator)) {
		return ""
	}
	if fi, err := os.Stat(p); err != nil || fi.IsDir() {
		return ""
	}
	return p
}

// jsonError marshals a message into a JSON error body, so attacker or runtime
// error strings cannot break out of the JSON literal.
func jsonError(msg string) string {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return string(b)
}

// Header returns the first value of a (lowercase) response header.
func (r APIResult) Header(name string) string {
	if v := r.Headers[strings.ToLower(name)]; len(v) > 0 {
		return v[0]
	}
	return ""
}
