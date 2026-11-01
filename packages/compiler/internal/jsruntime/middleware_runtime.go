package jsruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// MiddlewareRuntime executes middleware.ts via embedded quickjs
type MiddlewareRuntime struct {
	rt *Runtime
	mu sync.Mutex
}

// MiddlewareRequest is the input to the middleware function
type MiddlewareRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
}

// MiddlewareResult is the output of the middleware function
type MiddlewareResult struct {
	Action  string            `json:"action"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	Error   string            `json:"error"`
}

// NewMiddlewareRuntime creates a new middleware runtime and loads the middleware.js
func NewMiddlewareRuntime(middlewarePath string) (*MiddlewareRuntime, error) {
	m := &MiddlewareRuntime{}

	code, err := os.ReadFile(middlewarePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read middleware.js: %w", err)
	}

	rt, err := New()
	if err != nil {
		return nil, fmt.Errorf("failed to create JS runtime: %w", err)
	}
	m.rt = rt

	// Strip ESM export statements for global scope execution
	cleanCode := stripExports(string(code))

	_, err = rt.Execute(cleanCode + "\n")
	if err != nil {
		rt.Close()
		return nil, fmt.Errorf("failed to execute middleware.js: %w", err)
	}

	_, err = rt.Execute("typeof middleware === 'function' || typeof middleware === 'object'")
	if err != nil {
		rt.Close()
		return nil, fmt.Errorf("middleware.js does not export a middleware function")
	}

	return m, nil
}

// Execute runs the middleware with the given request and returns the result
func (m *MiddlewareRuntime) Execute(req MiddlewareRequest) MiddlewareResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return MiddlewareResult{Error: err.Error()}
	}

	jsCode := fmt.Sprintf(`
		(function() {
			try {
				var req = %s;
				var rawHeaders = req.headers || {};
				var headerView = {
					get: function(name) {
						var key = String(name).toLowerCase();
						var value = rawHeaders[key];
						if (value === undefined) {
							for (var k in rawHeaders) {
								if (String(k).toLowerCase() === key) { value = rawHeaders[k]; break; }
							}
						}
						return value === undefined ? null : String(value);
					},
					has: function(name) { return this.get(name) !== null; },
					forEach: function(fn) {
						for (var k in rawHeaders) fn(String(rawHeaders[k]), String(k), this);
					},
				};
				var cookieJar = {};
				var cookieHeader = headerView.get('cookie') || '';
				if (cookieHeader) {
					var parts = cookieHeader.split(';');
					for (var ci = 0; ci < parts.length; ci++) {
						var part = parts[ci];
						var eq = part.indexOf('=');
						if (eq <= 0) continue;
						var cname = part.slice(0, eq).trim();
						if (cname && cookieJar[cname] === undefined) cookieJar[cname] = part.slice(eq + 1).trim();
					}
				}
				var request = {
					url: req.url,
					method: req.method,
					path: req.path,
					headers: headerView,
					cookies: {
						get: function(name) { return cookieJar[String(name)]; },
						getAll: function() { var out = []; for (var k in cookieJar) out.push({ name: k, value: cookieJar[k] }); return out; },
						has: function(name) { return cookieJar[String(name)] !== undefined; },
					},
				};

				var result;
				if (typeof middleware === 'function') {
					result = middleware(request);
				} else if (middleware && typeof middleware.middleware === 'function') {
					result = middleware.middleware(request);
				} else {
					return JSON.stringify({ action: 'continue' });
				}

				if (result && typeof result.then === 'function') {
					return JSON.stringify({ action: 'error', error: 'Async middleware is not supported by the embedded runtime; use ssr.middlewareRuntime: "node"' });
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
								headers[k] = result.headers[k];
							}
						}
					}
					var body = '';
					if (result.status !== 204 && typeof result.text === 'function') {
						body = result.text();
					}
					return JSON.stringify({ status: result.status, headers: headers, body: body });
				}

				return JSON.stringify({ action: 'continue' });
			} catch (e) {
				return JSON.stringify({ action: 'error', error: e.message });
			}
		})()
	`, string(reqJSON))

	result, err := m.rt.Execute(jsCode)
	if err != nil {
		return MiddlewareResult{Error: err.Error()}
	}

	var middlewareResult MiddlewareResult
	if err := json.Unmarshal([]byte(result.(string)), &middlewareResult); err != nil {
		return MiddlewareResult{Error: err.Error()}
	}

	return middlewareResult
}

// Close releases the runtime resources
func (m *MiddlewareRuntime) Close() {
	if m.rt != nil {
		m.rt.Close()
	}
}

// LoadMiddlewareRuntime loads the middleware.js and returns a runtime, or nil if no middleware
func LoadMiddlewareRuntime(root string) *MiddlewareRuntime {
	middlewarePath := ""
	for _, p := range []string{
		".krate/middleware.js",
	} {
		fullPath := filepath.Join(root, p)
		if _, err := os.Stat(fullPath); err == nil {
			middlewarePath = fullPath
			break
		}
	}

	if middlewarePath == "" {
		return nil
	}

	rt, err := NewMiddlewareRuntime(middlewarePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  !Middleware load error: %s\n", err)
		return nil
	}

	return rt
}
