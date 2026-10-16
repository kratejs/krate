package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	pluginsdk "github.com/kratejs/krate/packages/compiler/pluginsdk"
)

// ---------------------------------------------------------------------------
// Host-side lifecycle for Go plugins
// ---------------------------------------------------------------------------

var goPluginPool = &goPluginPoolT{clients: map[string]*goPluginHost{}}

type goPluginPoolT struct {
	mu      sync.Mutex
	clients map[string]*goPluginHost
}

// goPluginHost holds a single long-lived host client for a Go plugin binary.
// The subprocess is spawned lazily on first hook use, kept alive across dev
// hot-reloads, and killed at builder/client shutdown.
type goPluginHost struct {
	name      string
	binary    string
	client    *goplugin.Client
	clientMu  sync.Mutex
	rpcClient *pluginsdk.RPCClient
}

// ensure starts the plugin subprocess if it is not already running and returns
// the RPC client for it.
func (h *goPluginHost) ensure() (*pluginsdk.RPCClient, error) {
	h.clientMu.Lock()
	defer h.clientMu.Unlock()
	if h.rpcClient != nil {
		return h.rpcClient, nil
	}

	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  pluginsdk.HandshakeConfig,
		Plugins:          map[string]goplugin.Plugin{pluginsdk.PluginName: &pluginsdk.Bridge{}},
		Cmd:              exec.Command(h.binary),
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolNetRPC},
		Logger:           hclog.New(&hclog.LoggerOptions{Level: hclog.Error, Output: os.Stderr}),
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("starting Go plugin %q: %w", h.name, err)
	}
	raw, err := rpcClient.Dispense(pluginsdk.PluginName)
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("connecting to Go plugin %q: %w", h.name, err)
	}
	impl, ok := raw.(*pluginsdk.RPCClient)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("Go plugin %q returned unexpected client type %T", h.name, raw)
	}

	h.client = client
	h.rpcClient = impl
	return impl, nil
}

// dispatch runs one hook against the plugin and returns the raw output envelope.
func (h *goPluginHost) dispatch(kind, hook string, args interface{}) (pluginsdk.Result, error) {
	impl, err := h.ensure()
	if err != nil {
		return pluginsdk.Result{}, err
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return pluginsdk.Result{}, fmt.Errorf("serializing %s args: %w", hook, err)
	}
	out, err := impl.Dispatch(kind, hook, raw)
	if err != nil {
		return pluginsdk.Result{}, fmt.Errorf("hook %s: %w", hook, err)
	}
	var res pluginsdk.Result
	if len(out) > 0 {
		if err := json.Unmarshal(out, &res); err != nil {
			return pluginsdk.Result{}, fmt.Errorf("decoding %s result: %w", hook, err)
		}
	}
	return res, nil
}

// rawDispatch sends a raw already-marshalled request and returns the raw JSON
// output envelope (useful for serve hooks whose result is not a Result).
func (h *goPluginHost) rawDispatch(kind, hook string, args json.RawMessage) (json.RawMessage, error) {
	impl, err := h.ensure()
	if err != nil {
		return nil, err
	}
	out, err := impl.Dispatch(kind, hook, args)
	if err != nil {
		return nil, fmt.Errorf("hook %s: %w", hook, err)
	}
	return out, nil
}

// kill terminates the plugin subprocess if it is running.
func (h *goPluginHost) kill() {
	h.clientMu.Lock()
	defer h.clientMu.Unlock()
	if h.client != nil {
		h.client.Kill()
		h.client = nil
		h.rpcClient = nil
	}
}

// ---------------------------------------------------------------------------
// Binary resolution
// ---------------------------------------------------------------------------

// goPlugins tracks the manifest data resolved for each configured module path.
var goPluginManifestCache sync.Map // module -> goPluginDescriptor

// goPluginDescriptor is the static manifest a JS plugin descriptor exposes for
// a Go plugin: the runtime marker plus the per-platform binary paths.
type goPluginDescriptor struct {
	Runtime  string            `json:"runtime"`
	Binaries map[string]string `json:"binaries"`
}

// platformKey returns the GOOS-GOARCH key used in a descriptor's binaries map.
func platformKey() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// resolveGoBinary resolves the binary path for the host's platform from a
// plugin module. For a local path (not a resolvable npm module) the module
// itself may be the binary; otherwise the descriptor is read from the running
// JS factory.
func resolveGoBinary(pc config.PluginConfig) (string, error) {
	// Runtime is returned by the running JS descriptor. To avoid bundling the
	// plugin just to learn its runtime, try an inexpensive probe: if the module
	// resolves to an executable file directly, use it.
	if bin, err := resolveGoBinaryFromModulePath(pc.Module); err == nil {
		return bin, nil
	}

	// Fall back to the JS descriptor manifest (bundles the plugin module and
	// runs its factory). Look up the current platform's binary and resolve it
	// relative to the module.
	desc, err := discoverGoManifest(pc.Module)
	if err != nil {
		return "", err
	}
	rel, ok := desc.Binaries[platformKey()]
	if !ok {
		return "", fmt.Errorf("Go plugin %q has no binary for %s (available: %v)", pc.Name, platformKey(), desc.Binaries)
	}
	abs, err := resolveGoBinaryPath(pc.Module, rel)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// discoverGoManifest runs the plugin module's JS factory once (via the existing
// QuickJS machinery) and reads the descriptor it returns. Descriptors are
// cached regardless of runtime so pure-JS plugins do not pay the cost repeatedly.
func discoverGoManifest(module string) (goPluginDescriptor, error) {
	if v, ok := goPluginManifestCache.Load(module); ok {
		return v.(goPluginDescriptor), nil
	}
	desc, err := runJSManifest(module)
	if err != nil {
		return goPluginDescriptor{}, err
	}
	goPluginManifestCache.Store(module, desc)
	return desc, nil
}

// resolveGoBinaryFromModulePath returns the module path itself as the binary if
// it resolves to an existing executable, but only when the plugin is configured
// as Go (checked by the caller before treating non-existent modules as errors).
func resolveGoBinaryFromModulePath(module string) (string, error) {
	p := module
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("module path is relative")
	}
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0111 != 0 {
		return p, nil
	}
	return "", fmt.Errorf("not an executable file")
}

// resolveGoBinaryPath resolves a descriptor binary path relative to the module.
// The module is taken relative to the project root; the path is resolved to its
// absolute location first, then the binary is joined and cleaned.
func resolveGoBinaryPath(module, rel string) (string, error) {
	absModule, err := filepath.Abs(module)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(absModule); err == nil && fi.IsDir() {
		absModule = filepath.Join(absModule, "index.js")
	}
	p := filepath.Join(filepath.Dir(absModule), filepath.FromSlash(rel))
	p = filepath.Clean(p)
	if fi, err := os.Stat(p); err != nil || fi.IsDir() {
		if fi != nil && fi.IsDir() {
			return "", fmt.Errorf("Go plugin binary path %s is a directory", p)
		}
		return "", fmt.Errorf("Go plugin binary not found at %s", p)
	}
	return p, nil
}

// CloseGoPlugins terminates every running Go plugin subprocess.
func CloseGoPlugins() {
	goPluginPool.mu.Lock()
	defer goPluginPool.mu.Unlock()
	for _, h := range goPluginPool.clients {
		h.kill()
	}
	goPluginPool.clients = map[string]*goPluginHost{}
}

// goPluginFor returns the host for a Go plugin configured at the module path,
// creating and caching it on first use.
func goPluginFor(pc config.PluginConfig) (*goPluginHost, error) {
	goPluginPool.mu.Lock()
	defer goPluginPool.mu.Unlock()
	if h, ok := goPluginPool.clients[pc.Module]; ok {
		return h, nil
	}
	binary, err := resolveGoBinary(pc)
	if err != nil {
		return nil, err
	}
	h := &goPluginHost{name: pc.Name, binary: binary}
	goPluginPool.clients[pc.Module] = h
	return h, nil
}

// goHookRuns reports whether a plugin is a Go plugin, and if so whether it
// implements the given hook (used by serve-time to know whether to spin the
// subprocess). Non-Go plugins always return false.
func isGoPlugin(pc config.PluginConfig) bool {
	if pc.Module == "" {
		return false
	}
	desc, err := discoverGoManifest(pc.Module)
	if err != nil {
		return false
	}
	return desc.Runtime == "go"
}
