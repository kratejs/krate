package jsruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"krate-compiler/internal/escape"
)

// RuntimeComponentRuntime executes compiled runtime component bundles via embedded quickjs.
// Each component is a standalone IIFE that defines globalThis.__krate_render(propsJSON).
type RuntimeComponentRuntime struct {
	componentDir string // absolute path to dist/server-components/
	mu           sync.Mutex
	cache        map[string][]byte // componentName → cached bundle code
}

// RuntimeComponentResult is the output of a runtime component render.
type RuntimeComponentResult struct {
	HTML  string `json:"html"`
	Error string `json:"error,omitempty"`
}

// NewRuntimeComponentRuntime creates a runtime for executing compiled runtime components.
func NewRuntimeComponentRuntime(componentDir string) *RuntimeComponentRuntime {
	return &RuntimeComponentRuntime{
		componentDir: componentDir,
		cache:        make(map[string][]byte),
	}
}

// RenderComponent executes a compiled runtime component bundle and returns the HTML.
// componentName is used to locate the <name>.runtime.js file in the component directory.
// propsJSON is a JSON string of props to pass to the component function.
func (r *RuntimeComponentRuntime) RenderComponent(componentName, propsJSON string) RuntimeComponentResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	code, err := r.readBundle(componentName)
	if err != nil {
		return RuntimeComponentResult{Error: err.Error()}
	}

	// Create a fresh VM for each render (no shared state between requests)
	rt, err := New()
	if err != nil {
		return RuntimeComponentResult{Error: fmt.Sprintf("failed to create JS runtime: %v", err)}
	}
	defer rt.Close()

	// Execute the IIFE bundle — this defines globalThis.__krate_render
	_, err = rt.Execute(string(code))
	if err != nil {
		return RuntimeComponentResult{Error: fmt.Sprintf("bundle execution error: %v", err)}
	}

	// Call __krate_render(propsJSON) and get the HTML string
	escapedProps := escape.JSStringDQ(propsJSON)
	jsCall := fmt.Sprintf(`(function() {
		try {
			if (typeof globalThis.__krate_render !== 'function') {
				return JSON.stringify({error: '__krate_render not defined after loading bundle'});
			}
			var html = globalThis.__krate_render(%s);
			return JSON.stringify({html: html || ''});
		} catch(e) {
			return JSON.stringify({error: e.message || String(e)});
		}
	})()`, escapedProps)

	result, err := rt.Execute(jsCall)
	if err != nil {
		return RuntimeComponentResult{Error: fmt.Sprintf("render call error: %v", err)}
	}

	resultStr, ok := result.(string)
	if !ok {
		return RuntimeComponentResult{Error: "render returned non-string result"}
	}

	var parsed struct {
		HTML  string `json:"html"`
		Error string `json:"error"`
	}
	if err := jsonUnmarshal([]byte(resultStr), &parsed); err != nil {
		return RuntimeComponentResult{Error: fmt.Sprintf("invalid render result: %v", err)}
	}

	if parsed.Error != "" {
		return RuntimeComponentResult{Error: parsed.Error}
	}

	return RuntimeComponentResult{HTML: parsed.HTML}
}

// RenderComponentAsync is like RenderComponent but supports async components.
// It drains the microtask queue after execution to resolve any promises.
func (r *RuntimeComponentRuntime) RenderComponentAsync(componentName, propsJSON string) RuntimeComponentResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	code, err := r.readBundle(componentName)
	if err != nil {
		return RuntimeComponentResult{Error: err.Error()}
	}

	rt, err := New()
	if err != nil {
		return RuntimeComponentResult{Error: fmt.Sprintf("failed to create JS runtime: %v", err)}
	}
	defer rt.Close()

	_, err = rt.Execute(string(code))
	if err != nil {
		return RuntimeComponentResult{Error: fmt.Sprintf("bundle execution error: %v", err)}
	}

	// Call render and capture the result (may be a promise)
	escapedProps := escape.JSStringDQ(propsJSON)
	jsSetup := fmt.Sprintf(`(function() {
		try {
			if (typeof globalThis.__krate_render !== 'function') {
				globalThis.__render_error = '__krate_render not defined';
				return;
			}
			var result = globalThis.__krate_render(%s);
			if (result && typeof result.then === 'function') {
				globalThis.__render_is_promise = true;
				result.then(function(v) { globalThis.__render_html = v || ''; }, function(e) { globalThis.__render_error = e.message || String(e); });
			} else {
				globalThis.__render_html = result || '';
			}
		} catch(e) {
			globalThis.__render_error = e.message || String(e);
		}
	})()`, escapedProps)

	if _, err := rt.Execute(jsSetup); err != nil {
		return RuntimeComponentResult{Error: fmt.Sprintf("render setup error: %v", err)}
	}

	// Drain microtask queue for async resolution
	rt.DrainJobs()

	jsRead := `(function() {
		if (globalThis.__render_error) return JSON.stringify({error: globalThis.__render_error});
		return JSON.stringify({html: globalThis.__render_html || ''});
	})()`

	result, err := rt.Execute(jsRead)
	if err != nil {
		return RuntimeComponentResult{Error: fmt.Sprintf("render read error: %v", err)}
	}

	resultStr, ok := result.(string)
	if !ok {
		return RuntimeComponentResult{Error: "render returned non-string result"}
	}

	var parsed struct {
		HTML  string `json:"html"`
		Error string `json:"error"`
	}
	if err := jsonUnmarshal([]byte(resultStr), &parsed); err != nil {
		return RuntimeComponentResult{Error: fmt.Sprintf("invalid render result: %v", err)}
	}

	if parsed.Error != "" {
		return RuntimeComponentResult{Error: parsed.Error}
	}

	return RuntimeComponentResult{HTML: parsed.HTML}
}

// readBundle reads the bundle code for a component, using cache if available.
func (r *RuntimeComponentRuntime) readBundle(componentName string) ([]byte, error) {
	if cached, ok := r.cache[componentName]; ok {
		return cached, nil
	}

	bundlePath := r.resolveBundle(componentName)
	if bundlePath == "" {
		return nil, fmt.Errorf("runtime component %q not found", componentName)
	}

	code, err := os.ReadFile(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read bundle: %v", err)
	}

	r.cache[componentName] = code
	return code, nil
}

// ClearCache removes all cached bundle code. Call during dev mode hot reload
// when a *.runtime.tsx file changes to force re-read from disk.
func (r *RuntimeComponentRuntime) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[string][]byte)
}

// resolveBundle finds the compiled JS file for a runtime component.
func (r *RuntimeComponentRuntime) resolveBundle(componentName string) string {
	candidates := []string{
		componentName + ".runtime.js",
		componentName + ".js",
	}
	for _, name := range candidates {
		p := strings.ReplaceAll(r.componentDir, "\\", "/") + "/" + name
		if _, err := os.Stat(p); err == nil {
			return p
		}
		p = r.componentDir + "/" + name
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// jsonUnmarshal parses JSON data into v.
func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
