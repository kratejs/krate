package plugin

import (
	"encoding/json"
	"fmt"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

// runJSServeRequest invokes a plugin's ServeRequest hook inside a fresh
// QuickJS VM (the same pattern as the other JS hook paths).
func runJSServeRequest(pc config.PluginConfig, root string, args ServeRequestArgs) (*ServeRequestResult, error) {
	payload := map[string]interface{}{
		"url": args.URL, "method": args.Method, "path": args.Path, "headers": args.Headers,
	}
	out, err := runJSServeHook(pc.Module, "ServeRequest", root, payload)
	if err != nil {
		return nil, err
	}
	res := &ServeRequestResult{}
	if err := json.Unmarshal(out, res); err != nil {
		return nil, fmt.Errorf("decoding ServeRequest result: %w", err)
	}
	if res.Error != "" {
		return nil, fmt.Errorf("%s", res.Error)
	}
	return res, nil
}

// runJSServeResponse invokes a plugin's ServeResponse hook inside a fresh VM.
func runJSServeResponse(pc config.PluginConfig, root string, args ServeResponseArgs) (ServeResponseResult, error) {
	payload := map[string]interface{}{
		"url": args.URL, "method": args.Method, "path": args.Path,
		"status": args.Status, "headers": args.Headers, "body": args.Body,
	}
	out, err := runJSServeHook(pc.Module, "ServeResponse", root, payload)
	if err != nil {
		return ServeResponseResult{}, err
	}
	res := ServeResponseResult{}
	if err := json.Unmarshal(out, &res); err != nil {
		return ServeResponseResult{}, fmt.Errorf("decoding ServeResponse result: %w", err)
	}
	if res.Error != "" {
		return ServeResponseResult{}, fmt.Errorf("%s", res.Error)
	}
	return res, nil
}

// runJSServeHook loads a plugin module and invokes the given serve hook with the
// supplied context payload, returning the raw result JSON.
func runJSServeHook(module, hookName, root string, payload map[string]interface{}) ([]byte, error) {
	bundleCode, err := bundleJSPlugin(module, root)
	if err != nil {
		return nil, err
	}
	rt, err := jsruntime.New()
	if err != nil {
		return nil, fmt.Errorf("creating JS runtime: %w", err)
	}
	defer rt.Close()
	_ = rt.SetEnv(environ.Current)
	if _, err := rt.Execute(bundleCode); err != nil {
		return nil, fmt.Errorf("loading plugin bundle: %w", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	call := fmt.Sprintf(`
(function() {
  try {
    var mod = __kratePlugin || {};
    var plugin = mod.default || mod;
    if (typeof plugin === 'function') plugin = plugin({});
    var hooks = (plugin && plugin.hooks) || mod.hooks || plugin || {};
    var fn = hooks[%s];
    if (typeof fn !== 'function') return JSON.stringify({ action: 'continue' });
    var out = fn(%s, {}, {});
    if (out && typeof out.then === 'function') {
      return JSON.stringify({ action: 'continue', error: 'Async serve hooks not supported' });
    }
    return JSON.stringify(out || { action: 'continue' });
  } catch (e) {
    return JSON.stringify({ action: 'continue', error: (e && e.message) || String(e) });
  }
})()
`, jsString(hookName), string(payloadJSON))
	res, err := rt.Execute(call)
	if err != nil {
		return nil, fmt.Errorf("invoking %s hook: %w", hookName, err)
	}
	s, ok := res.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected serve hook result type %T", res)
	}
	return []byte(s), nil
}
