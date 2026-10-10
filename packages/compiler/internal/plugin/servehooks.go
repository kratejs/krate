package plugin

import (
	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// ServeRequestArgs is the wire context for the ServeRequest hook. It mirrors
// the middleware request shape so plugins transform the incoming request.
type ServeRequestArgs struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
}

// ServeRequestResult is the wire result for the ServeRequest hook, matching
// the middleware result shape: 'continue' to fall through, 'rewrite' to
// rewrite the path, or 'respond' to short-circuit with a custom response.
type ServeRequestResult struct {
	Action  string            `json:"action"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	NewURL  string            `json:"newURL"`
	Error   string            `json:"error,omitempty"`
}

// ServeResponseArgs is the wire context for the ServeResponse hook. It wraps
// a buffered non-streaming response so plugins can inspect and rewrite it.
type ServeResponseArgs struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// ServeResponseResult returns the (possibly rewritten) buffered response.
type ServeResponseResult struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	Error   string            `json:"error,omitempty"`
}

// RunServeRequest runs the ServeRequest hook across every configured community
// plugin (in config order) and returns the first response, or nil to continue.
func RunServeRequest(plugins []config.PluginConfig, root, url, method, path string, headers map[string]string) (*ServeRequestResult, error) {
	return runServeRequestHooks(plugins, root, url, method, path, headers)
}

// RunServeResponse runs the ServeResponse hook across every configured
// community plugin and returns the final buffered response.
func RunServeResponse(plugins []config.PluginConfig, root, url, method, path string, status int, headers map[string]string, body string) (ServeResponseResult, error) {
	return runServeResponseHooks(plugins, root, url, method, path, status, headers, body)
}

// runServeRequestHooks iterates plugins, stopping at the first one that
// returns an explicit action (rewrite/respond).
func runServeRequestHooks(plugins []config.PluginConfig, root, url, method, path string, headers map[string]string) (*ServeRequestResult, error) {
	args := ServeRequestArgs{URL: url, Method: method, Path: path, Headers: headers}
	for _, pc := range plugins {
		if pc.Module == "" {
			continue
		}
		res, err := runServeRequestHook(pc, root, args)
		if err != nil {
			return nil, err
		}
		if res == nil {
			continue
		}
		if res.Action != "" && res.Action != "continue" {
			return res, nil
		}
	}
	return nil, nil
}

// mergeServeResponse applies a plugin's ServeResponse result onto the running
// response, overriding only the fields the plugin actually returned. A result
// that returns nothing (no status, no headers, no body) leaves the response
// untouched, so plugins without a ServeResponse hook - or those that return
// { action: 'continue' } - never wipe the body.
func mergeServeResponse(out *ServeResponseResult, res ServeResponseResult) {
	if res.Status > 0 {
		out.Status = res.Status
	}
	if res.Headers != nil {
		out.Headers = res.Headers
	}
	if res.Body != "" {
		out.Body = res.Body
	}
}

// runServeResponseHooks chains ServeResponse hooks; each sees the previous
// plugin's result.
func runServeResponseHooks(plugins []config.PluginConfig, root, url, method, path string, status int, headers map[string]string, body string) (ServeResponseResult, error) {
	out := ServeResponseResult{Status: status, Headers: headers, Body: body}
	for _, pc := range plugins {
		if pc.Module == "" {
			continue
		}
		res, err := runServeResponseHook(pc, root, ServeResponseArgs{
			URL: url, Method: method, Path: path,
			Status: out.Status, Headers: out.Headers, Body: out.Body,
		})
		if err != nil {
			return out, err
		}
		mergeServeResponse(&out, res)
	}
	return out, nil
}

// runServeRequestHook dispatches one ServeRequest hook to a single plugin,
// routing to Go or JS based on the plugin's runtime.
func runServeRequestHook(pc config.PluginConfig, root string, args ServeRequestArgs) (*ServeRequestResult, error) {
	if isGoPlugin(pc) {
		return runGoServeRequest(pc, root, args)
	}
	return runJSServeRequest(pc, root, args)
}

// runServeResponseHook dispatches one ServeResponse hook to a single plugin.
func runServeResponseHook(pc config.PluginConfig, root string, args ServeResponseArgs) (ServeResponseResult, error) {
	if isGoPlugin(pc) {
		return runGoServeResponse(pc, root, args)
	}
	return runJSServeResponse(pc, root, args)
}
