package jsruntime

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// APIRouteRuntime executes compiled API routes via embedded quickjs
type APIRouteRuntime struct {
	apiDir string
	mu     sync.Mutex
}

// APIRequest is the input to an API route handler
type APIRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// APIResult is the output of an API route handler
type APIResult struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	Error   string            `json:"error"`
}

// stripExports removes ESM export statements and converts named exports to globals.
// esbuild ESM output: function GET() {...}; export { GET }; export { _default as default };
// Handles both single-line and multi-line export blocks.
func stripExports(code string) string {
	// Phase 1: Whole-text regex replacements for multi-line patterns

	// export { X as default }; â†’ var __default = X;
	reMultiLineAsDefault := regexp.MustCompile(`export\s*\{\s*(\w+)\s+as\s+(\w+)\s*\}\s*;?`)
	code = reMultiLineAsDefault.ReplaceAllString(code, "var __default = $1;")

	// export { a as X, b as Y }; â†’ var X = a; var Y = b;
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
			// Check for "local as exported"
			asParts := strings.SplitN(part, " as ", 2)
			if len(asParts) == 2 {
				local := strings.TrimSpace(asParts[0])
				exported := strings.TrimSpace(asParts[1])
				if exported == "default" {
					bindings = append(bindings, fmt.Sprintf("var __default = %s;", local))
				} else {
					bindings = append(bindings, fmt.Sprintf("var %s = %s;", exported, local))
				}
			} else {
				// Plain name â€” already a global from function/const declaration
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
	reExportDecl := regexp.MustCompile(`^export\s+(const|let|var|function|class)\s+`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// export default ... â†’ need special handling
		if reExportDefaultDecl.MatchString(trimmed) {
			stripped := reExportDefaultDecl.ReplaceAllString(trimmed, "")
			// "export default function(...)" â†’ "function _default(...)"
			reAnonFunc := regexp.MustCompile(`^function\s*\(`)
			if reAnonFunc.MatchString(stripped) {
				stripped = "function _default(" + strings.TrimPrefix(stripped, "function(")
			}
			result = append(result, stripped)
			result = append(result, "var __default = (typeof _default !== 'undefined') ? _default : undefined;")
			continue
		}

		// export const/let/var/function/class â†’ const/let/var/function/class
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
	return &APIRouteRuntime{apiDir: apiDir}
}

// Execute runs the API route handler for the given path
func (a *APIRouteRuntime) Execute(req APIRequest) APIResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Resolve the API file
	relPath := strings.TrimPrefix(req.Path, "/api")
	if relPath == "" {
		relPath = "/index"
	}

	targetFile := a.resolveAPIFile(relPath)
	if targetFile == "" {
		return APIResult{Status: 404, Body: `{"error":"API Route Not Found"}`}
	}

	// Create a new VM for this request (API routes should not share state)
	rt, err := New()
	if err != nil {
		return APIResult{Status: 500, Body: `{"error":"Failed to create JS runtime"}`}
	}
	defer rt.Close()

	code, err := os.ReadFile(targetFile)
	if err != nil {
		return APIResult{Status: 500, Body: `{"error":"Failed to read API route"}`}
	}

	// Strip ESM export statements for global scope execution
	cleanCode := stripExports(string(code))

	_, err = rt.Execute(cleanCode + "\n")
	if err != nil {
		return APIResult{Status: 500, Body: jsonError(err.Error())}
	}

	// Find the handler: try GET, POST, PUT, DELETE, PATCH, OPTIONS, HEAD, then default
	method := strings.ToUpper(req.Method)
	handlerName := a.findHandler(rt, method)

	if handlerName == "" {
		return APIResult{Status: 500, Body: `{"error":"API route must export a handler function"}`}
	}

	// Check if this is a legacy handler(req, res) vs modern handler(request)
	// Function.length gives the number of formal parameters
	isLegacy, _ := rt.Execute(fmt.Sprintf(`typeof %s === 'function' && %s.length >= 2`, handlerName, handlerName))

	reqJSON, _ := json.Marshal(req)

	var result any
	if isTrue(isLegacy) {
		// Legacy pattern: handler(req, res) with Node.js-style shims
		jsCode := fmt.Sprintf(`
			(function() {
				try {
					var raw = %s;
					var req = {
						url: raw.url,
						method: raw.method,
						headers: raw.headers || {},
						body: raw.body || '',
						socket: { remoteAddress: '127.0.0.1' }
					};
					var _status = 200;
					var _headers = {};
					var _body = '';
					var _finished = false;
					var res = {
						statusCode: 200,
						setHeader: function(k, v) { _headers[k.toLowerCase()] = String(v); },
						getHeader: function(k) { return _headers[k.toLowerCase()]; },
						removeHeader: function(k) { delete _headers[k.toLowerCase()]; },
						write: function(chunk) { _body += chunk; },
						end: function(chunk) {
							if (chunk) _body += chunk;
							_finished = true;
							_status = this.statusCode || 200;
						},
						on: function() {}
					};
					var result = %s(req, res);
					if (_finished) {
						return JSON.stringify({ status: _status, headers: _headers, body: _body });
					}
					if (result && typeof result.status === 'number') {
						return JSON.stringify({ status: result.status, headers: {}, body: '' });
					}
					return JSON.stringify({ status: 200, body: _body || '' });
				} catch (e) {
					return JSON.stringify({ status: 500, body: '', error: e.message });
				}
			})()
		`, string(reqJSON), handlerName)

		var err error
		result, err = rt.Execute(jsCode)
		if err != nil {
			return APIResult{Status: 500, Body: jsonError(err.Error())}
		}
	} else {
		// Modern pattern: handler(request) with web standard APIs
		// Phase 1: Call handler, set up promise resolution via globals
		jsSetup := fmt.Sprintf(`
			var __apiResult = undefined;
			var __apiError = '';
			var __apiIsPromise = false;
			try {
				var raw = %s;
				var request = {
					url: raw.url,
					method: raw.method,
					path: raw.path,
					headers: raw.headers || {},
					body: raw.body || '',
					json: function() {
						try { return JSON.parse(raw.body || '{}'); } catch(e) { return {}; }
					},
					text: function() { return raw.body || ''; }
				};

				var result = %s(request);

				if (result && typeof result.then === 'function') {
					__apiIsPromise = true;
					result.then(function(v) { __apiResult = v; }, function(e) { __apiError = e && e.message ? e.message : 'Promise rejected'; });
				} else {
					__apiResult = result;
				}
			} catch (e) {
				__apiError = e.message;
			}
		`, string(reqJSON), handlerName)

		if _, err := rt.Execute(jsSetup); err != nil {
			return APIResult{Status: 500, Body: jsonError(err.Error())}
		}

		// Phase 2: Drain microtask queue so promise callbacks fire
		if _, err := rt.Execute("(function(){})();"); err == nil {
			rt.DrainJobs()
		}

		// Phase 3: Read resolved value and format response
		jsFormat := `
			(function() {
				if (__apiError) {
					return JSON.stringify({ status: 500, body: '', error: __apiError });
				}
				var result = __apiResult;
				if (__apiIsPromise && result === undefined) {
					return JSON.stringify({ status: 500, body: '', error: 'Async handler returned unresolved promise' });
				}
				if (result && typeof result.status === 'number') {
					var headers = {};
					if (result.headers) {
						if (typeof result.headers.entries === 'function') {
							var entries = result.headers.entries();
							for (var i = 0; i < entries.length; i++) {
								headers[entries[i][0]] = entries[i][1];
							}
						} else {
							for (var k in result.headers) {
								if (k.charAt(0) !== '_' && typeof result.headers[k] === 'string') {
									headers[k] = result.headers[k];
								}
							}
						}
					}
					var body = '';
					if (result.status !== 204 && typeof result.text === 'function') {
						body = result.text();
					}
					return JSON.stringify({ status: result.status, headers: headers, body: body });
				}
				if (result !== undefined && result !== null) {
					return JSON.stringify({ status: 200, headers: { 'content-type': 'application/json' }, body: JSON.stringify(result) });
				}
				return JSON.stringify({ status: 204, body: '' });
			})()
		`
		var err error
		result, err = rt.Execute(jsFormat)
		if err != nil {
			return APIResult{Status: 500, Body: jsonError(err.Error())}
		}
	}

	var apiResult APIResult
	if err := json.Unmarshal([]byte(result.(string)), &apiResult); err != nil {
		return APIResult{Status: 500, Body: `{"error":"Invalid handler response"}`}
	}

	return apiResult
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

	return ""
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

// findHandler checks which named export matches the HTTP method
func (a *APIRouteRuntime) findHandler(rt *Runtime, method string) string {
	methods := []string{method, "__default", "default"}
	for _, m := range methods {
		result, err := rt.Execute(fmt.Sprintf("typeof %s === 'function'", m))
		if err == nil {
			if isTrue(result) {
				return m
			}
		}
	}
	return ""
}

// isTrue checks if a JS result is truthy
func isTrue(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case int:
		return val != 0
	case int64:
		return val != 0
	case float64:
		return val != 0
	case string:
		return val != ""
	case map[string]any:
		return len(val) > 0
	default:
		return v != nil
	}
}
