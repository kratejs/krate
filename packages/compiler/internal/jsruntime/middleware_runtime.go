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
				var request = {
					url: req.url,
					method: req.method,
					path: req.path,
					headers: req.headers || {},
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
					return JSON.stringify({ action: 'continue', error: 'Async middleware not supported in embedded runtime' });
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
				return JSON.stringify({ action: 'continue', error: e.message });
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
		fmt.Fprintf(os.Stderr, "  ⚠Middleware load error: %s\n", err)
		return nil
	}

	return rt
}
