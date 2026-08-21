package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
	"krate-compiler/internal/config"
	"krate-compiler/internal/jsruntime"
)

// jsPluginBundleCache memoizes the esbuild-bundled IIFE for each plugin module
// path. Bundling happens once per file; a fresh QuickJS VM is still created for
// every hook invocation because page builds run concurrently and a single VM is
// not safe for concurrent use.
var jsPluginBundleCache sync.Map // absPath -> bundle string or error

// pluginGlobal is the esbuild IIFE global that receives the bundled module
// namespace. The plugin descriptor lives at __kratePlugin.default.
const pluginGlobal = "__kratePlugin"

// runJSPluginHook executes a JS community plugin hook inside an embedded
// QuickJS VM — no subprocess, no stdin/stdout. The plugin module is bundled
// with esbuild into a self-contained IIFE and evaluated in a fresh VM. Hooks
// are invoked as fn(ctx, options, krate) where ctx is the JSON-serialized hook
// context and the return value is a { files, routes, generatedPages, html,
// headHTML, rawCSS } result object (optionally a Promise).
func runJSPluginHook(hookName string, pc config.PluginConfig, root, outDir string, hookCtx interface{}) error {
	bundleCode, err := bundleJSPlugin(pc.Module, root)
	if err != nil {
		return err
	}

	rt, err := jsruntime.New()
	if err != nil {
		return fmt.Errorf("creating JS runtime: %w", err)
	}
	defer rt.Close()

	if _, err := rt.Execute(bundleCode); err != nil {
		return fmt.Errorf("loading plugin bundle: %w", err)
	}

	ctxJSON, err := json.Marshal(hookCtx)
	if err != nil {
		return fmt.Errorf("serializing hook context: %w", err)
	}
	optionsJSON, err := json.Marshal(pc.Options)
	if err != nil {
		return fmt.Errorf("serializing plugin options: %w", err)
	}
	krateJSON, err := json.Marshal(map[string]string{
		"root":    root,
		"outDir":  outDir,
		"version": "1.0.0",
	})
	if err != nil {
		return fmt.Errorf("serializing krate metadata: %w", err)
	}

	callScript := fmt.Sprintf(`
(function() {
  try {
    var mod = %[1]s || {};
    var plugin = mod.default || mod;
    if (typeof plugin === 'function') plugin = plugin(%[2]s);
    // Hooks live on the descriptor's .hooks, the module's named `+"`hooks`"+` export
    // (factory style), or the descriptor itself (single-hook shorthand).
    var hooks = (plugin && plugin.hooks) || mod.hooks || plugin || {};
    var fn = hooks[%[4]s];
    if (typeof fn !== 'function') return JSON.stringify({ skip: true });
    var out = fn(%[3]s, %[2]s, %[5]s);
    if (out && typeof out.then === 'function') {
      __krateResult = undefined;
      __krateError = '';
      out.then(function(v) { __krateResult = v; }, function(e) { __krateError = (e && e.message) || String(e); });
      return JSON.stringify({ pending: true });
    }
    return JSON.stringify({ result: out });
  } catch (e) {
    return JSON.stringify({ error: (e && e.message) || String(e) });
  }
})()
`, pluginGlobal, string(optionsJSON), string(ctxJSON), jsString(hookName), string(krateJSON))

	res, err := rt.Execute(callScript)
	if err != nil {
		return fmt.Errorf("invoking %s hook: %w", hookName, err)
	}

	msg, err := decodePluginMessage(res)
	if err != nil {
		return err
	}

	if msg.Pending {
		rt.DrainJobs()
		readScript := fmt.Sprintf(`
(function() {
  if (typeof __krateError === 'string' && __krateError) {
    return JSON.stringify({ error: __krateError });
  }
  return JSON.stringify({ result: __krateResult });
})()
`)
		res2, err := rt.Execute(readScript)
		if err != nil {
			return fmt.Errorf("resolving async %s hook: %w", hookName, err)
		}
		if msg, err = decodePluginMessage(res2); err != nil {
			return err
		}
	}

	if msg.Skip {
		return nil
	}
	if msg.Error != "" {
		return fmt.Errorf("%s", msg.Error)
	}

	var output communityOutput
	if msg.Result != nil && len(msg.Result) > 0 {
		if err := json.Unmarshal(msg.Result, &output); err != nil {
			return fmt.Errorf("invalid plugin result JSON: %w", err)
		}
	}

	return applyPluginOutput(hookName, &output, outDir, hookCtx)
}

// pluginCallMessage is the JSON envelope returned by the JS hook invocation.
type pluginCallMessage struct {
	Pending bool            `json:"pending,omitempty"`
	Skip    bool            `json:"skip,omitempty"`
	Error   string          `json:"error,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

// decodePluginMessage converts a JS execution result (a JSON string) into a
// pluginCallMessage.
func decodePluginMessage(res any) (pluginCallMessage, error) {
	s, ok := res.(string)
	if !ok {
		return pluginCallMessage{}, fmt.Errorf("unexpected hook invocation result type %T", res)
	}
	var msg pluginCallMessage
	if err := json.Unmarshal([]byte(s), &msg); err != nil {
		return pluginCallMessage{}, fmt.Errorf("decoding hook invocation result: %w", err)
	}
	return msg, nil
}

// bundleJSPlugin bundles a JS plugin module into a self-contained IIFE that
// assigns the module namespace to the pluginGlobal global. Results are cached
// per absolute module path.
func bundleJSPlugin(module, root string) (string, error) {
	absPath, err := resolveJSPluginPath(module, root)
	if err != nil {
		return "", err
	}
	if v, ok := jsPluginBundleCache.Load(absPath); ok {
		if err, isErr := v.(error); isErr {
			return "", err
		}
		return v.(string), nil
	}

	result := api.Build(api.BuildOptions{
		EntryPoints: []string{absPath},
		Bundle:      true,
		Platform:    api.PlatformNode,
		Format:      api.FormatIIFE,
		GlobalName:  pluginGlobal,
		LogLevel:    api.LogLevelError,
		Write:       false,
	})

	if len(result.Errors) > 0 {
		var sb strings.Builder
		for _, e := range result.Errors {
			sb.WriteString(e.Text)
			sb.WriteString("\n")
		}
		err := fmt.Errorf("plugin bundle error:\n%s", sb.String())
		jsPluginBundleCache.Store(absPath, err)
		return "", err
	}
	if len(result.OutputFiles) == 0 {
		err := fmt.Errorf("plugin bundle produced no output")
		jsPluginBundleCache.Store(absPath, err)
		return "", err
	}

	bundle := string(result.OutputFiles[0].Contents)
	jsPluginBundleCache.Store(absPath, bundle)
	return bundle, nil
}

// resolveJSPluginPath resolves a plugin module path relative to the project
// root. Directories are resolved to their index entry point.
func resolveJSPluginPath(module, root string) (string, error) {
	p := module
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}

	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		for _, name := range []string{"index.js", "index.mjs", "index.cjs", "plugin.js", "plugin.mjs", "plugin.cjs"} {
			cand := filepath.Join(p, name)
			if fi2, err := os.Stat(cand); err == nil && !fi2.IsDir() {
				return cand, nil
			}
		}
		return "", fmt.Errorf("no plugin entry point found in directory %s (expected index.js, index.mjs, or index.cjs)", module)
	}

	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		return p, nil
	}
	return "", fmt.Errorf("plugin file not found at %s", module)
}

// jsString quotes a string for safe embedding inside a JavaScript string literal.
func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
