package plugin

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/astjson"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

// jsPluginBundleCache memoizes the esbuild-bundled IIFE for each plugin module
// path. Bundling happens once per file; hook invocations run on a pooled VM.
var jsPluginBundleCache sync.Map // absPath -> bundle string or error

// pluginGlobalPrefix prefixes the per-module esbuild IIFE global. Each plugin
// module is assigned its own global (keyed by its absolute path) so several
// plugins can be loaded on one pooled VM without clobbering each other.
const pluginGlobalPrefix = "__kratePlugin_"

// pluginGlobalName returns the esbuild GlobalName for a plugin module.
func pluginGlobalName(absPath string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(absPath))
	return pluginGlobalPrefix + strconv.FormatUint(uint64(h.Sum32()), 16)
}

// pluginVM is a pooled QuickJS VM used for JS plugin hook invocations. Creating a
// VM and injecting the polyfill bundle dominates a hook call, so VMs are reused.
// The krate host functions are registered once (they read the current
// invocation's capabilities from cap) and the log prefix/env are re-applied per
// call. A VM is handed to one goroutine at a time (QuickJS VMs are not
// concurrency-safe).
type pluginVM struct {
	rt  *jsruntime.Runtime
	cap *krateCapabilities
}

// pluginVMPool holds idle plugin VMs, bounded to GOMAXPROCS.
var pluginVMPool = make(chan *pluginVM, max(1, runtime.GOMAXPROCS(0)))

// pluginVMPoolEnabled can be disabled with KRATE_NO_PLUGIN_VM_POOL (used for
// before/after measurement, and as an escape hatch).
func pluginVMPoolEnabled() bool { return os.Getenv("KRATE_NO_PLUGIN_VM_POOL") == "" }

func acquirePluginVM() (*pluginVM, error) {
	if pluginVMPoolEnabled() {
		select {
		case vm := <-pluginVMPool:
			return vm, nil
		default:
		}
	}
	rt, err := jsruntime.New()
	if err != nil {
		return nil, err
	}
	vm := &pluginVM{rt: rt}
	if err := registerKrateHostFuncs(vm); err != nil {
		_ = rt.Close()
		return nil, err
	}
	return vm, nil
}

func releasePluginVM(vm *pluginVM, healthy bool) {
	if vm == nil {
		return
	}
	if !healthy || !pluginVMPoolEnabled() {
		_ = vm.rt.Close()
		return
	}
	vm.cap = nil
	select {
	case pluginVMPool <- vm:
	default:
		_ = vm.rt.Close()
	}
}

// runJSPluginHook executes a JS community plugin hook inside an embedded
// QuickJS VM - no subprocess, no stdin/stdout. The plugin module is bundled
// with esbuild into a self-contained IIFE and evaluated in a pooled VM. Hooks
// are invoked as fn(ctx, options, krate) where ctx is the JSON-serialized hook
// context and the return value is a { files, routes, generatedPages, html,
// headHTML, rawCSS } result object (optionally a Promise).
func runJSPluginHook(hookName string, pc config.PluginConfig, root, outDir string, env CommunityEnv, hookCtx interface{}) error {
	bundleCode, globalName, err := bundleJSPlugin(pc.Module, root)
	if err != nil {
		return err
	}

	vm, err := acquirePluginVM()
	if err != nil {
		return fmt.Errorf("creating JS runtime: %w", err)
	}
	healthy := false
	defer func() { releasePluginVM(vm, healthy) }()
	rt := vm.rt

	_ = rt.SetEnv(environ.Current)

	// Attribute all console/log output to this plugin.
	rt.SetLogPrefix("[plugin:" + pc.Name + "]")

	// Assemble the richer `krate` object's capabilities (resolve/read/emit/
	// write/inject). The Go-backed host functions read these from vm.cap, which
	// is set before the hook runs and cleared when the VM is released.
	cap := newKrateCapabilities(root, outDir, env, hookName, hookCtx)
	vm.cap = cap

	if _, err := rt.Execute(bundleCode); err != nil {
		return fmt.Errorf("loading plugin bundle: %w", err)
	}

	ctxJSON, err := marshalHookContext(hookName, hookCtx)
	if err != nil {
		return fmt.Errorf("serializing hook context: %w", err)
	}
	optionsJSON, err := json.Marshal(pc.Options)
	if err != nil {
		return fmt.Errorf("serializing plugin options: %w", err)
	}
	krateJSON, err := json.Marshal(cap.metadata())
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
    var krate = %[5]s;
    krate.resolveFile = function(spec) { return _krate_resolveFile(String(spec)); };
    krate.readFile = function(spec) { return _krate_readFile(String(spec)); };
    krate.emitFile = function(path, content) { _krate_emitFile(String(path), String(content)); };
    krate.writeFileToRoot = function(rel, content) { _krate_writeFileToRoot(String(rel), String(content)); };
    krate.injectHead = function(html) { return _krate_injectHead(String(html)); };
    krate.injectCSS = function(css) { return _krate_injectCSS(String(css)); };
    krate.log = function() { _krate_log(JSON.stringify(Array.prototype.slice.call(arguments))); };
    krate.warn = function() { _krate_warn(JSON.stringify(Array.prototype.slice.call(arguments))); };
    var out = fn(%[3]s, %[2]s, krate);
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
`, globalName, string(optionsJSON), string(ctxJSON), jsString(hookName), string(krateJSON))

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
		readScript := `
(function() {
  if (typeof __krateError === 'string' && __krateError) {
    return JSON.stringify({ error: __krateError });
  }
  return JSON.stringify({ result: __krateResult });
})()
`
		res2, err := rt.Execute(readScript)
		if err != nil {
			return fmt.Errorf("resolving async %s hook: %w", hookName, err)
		}
		if msg, err = decodePluginMessage(res2); err != nil {
			return err
		}
	}

	// All JS interaction is done; the VM is safe to return to the pool.
	healthy = true

	if msg.Skip {
		return nil
	}
	if msg.Error != "" {
		return fmt.Errorf("%s", msg.Error)
	}

	var output communityOutput
	if len(msg.Result) > 0 {
		if err := json.Unmarshal(msg.Result, &output); err != nil {
			return fmt.Errorf("invalid plugin result JSON: %w", err)
		}
	}

	// Fold in side-effects produced through the krate capability methods
	// (emitFile / injectHead / injectCSS) before applying the output.
	cap.applyTo(&output)

	return applyPluginOutput(hookName, &output, outDir, hookCtx)
}

// marshalHookContext serializes a hook context for the JS runtime. The
// AfterParse context carries the AST as a kind-tagged document (astjson) so
// JavaScript plugins inspect and edit a stable AST shape.
func marshalHookContext(hookName string, hookCtx interface{}) ([]byte, error) {
	if hookName == "AfterParse" {
		if pctx, ok := hookCtx.(*ParseHookCtx); ok && pctx != nil {
			doc := map[string]interface{}{"page": pctx.Page}
			if pctx.Program != nil {
				progDoc, err := astjson.EncodeProgram(pctx.Program)
				if err != nil {
					return nil, err
				}
				doc["program"] = json.RawMessage(progDoc)
			}
			return json.Marshal(doc)
		}
	}
	return json.Marshal(hookCtx)
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
// assigns the module namespace to its per-module global, returning the bundled
// code and that global's name. Results are cached per absolute module path.
func bundleJSPlugin(module, root string) (string, string, error) {
	absPath, err := resolveJSPluginPath(module, root)
	if err != nil {
		return "", "", err
	}
	globalName := pluginGlobalName(absPath)
	if v, ok := jsPluginBundleCache.Load(absPath); ok {
		if err, isErr := v.(error); isErr {
			return "", "", err
		}
		return v.(string), globalName, nil
	}

	result := api.Build(api.BuildOptions{
		EntryPoints: []string{absPath},
		Bundle:      true,
		Platform:    api.PlatformNode,
		Format:      api.FormatIIFE,
		GlobalName:  globalName,
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
		return "", "", err
	}
	if len(result.OutputFiles) == 0 {
		err := fmt.Errorf("plugin bundle produced no output")
		jsPluginBundleCache.Store(absPath, err)
		return "", "", err
	}

	bundle := string(result.OutputFiles[0].Contents)
	jsPluginBundleCache.Store(absPath, bundle)
	return bundle, globalName, nil
}

// resolveJSPluginPath resolves a plugin module path relative to the project
// root. Directories are resolved to their index entry point.
func resolveJSPluginPath(module, root string) (string, error) {
	p := module
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}

	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		for _, name := range []string{"index.js", "index.mjs", "index.cjs", "index.ts", "index.tsx", "plugin.js", "plugin.mjs", "plugin.cjs", "plugin.ts", "plugin.tsx"} {
			cand := filepath.Join(p, name)
			if fi2, err := os.Stat(cand); err == nil && !fi2.IsDir() {
				return cand, nil
			}
		}
		return "", fmt.Errorf("no plugin entry point found in directory %s (expected index.js, index.mjs, index.cjs, index.ts, or index.tsx)", module)
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
