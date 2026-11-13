package jseval

import (
	"fmt"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// NodeRequireBanner lets CommonJS dependencies inside an ESM bundle that runs
// on Node, Bun or Deno call require() for built-ins ("fs", "node:path", ...).
// Without it esbuild's __require shim throws `Dynamic require of "fs" is not
// supported`. Only use it for bundles executed by a real Node-compatible
// runtime; the embedded QuickJS engine has no node:module.
const NodeRequireBanner = "import { createRequire as __krateCreateRequire } from 'node:module'; const require = __krateCreateRequire(import.meta.url);"

// ApplyNodeESM prepares opts for an ESM bundle that a Node-compatible runtime
// executes: the createRequire banner, so any dependency that runs on Node
// loads. Node built-ins are already external on the node platform. An existing
// js banner is kept after the require line.
func ApplyNodeESM(opts *api.BuildOptions) {
	banner := NodeRequireBanner
	if prev := opts.Banner["js"]; prev != "" {
		banner += "\n" + prev
	}
	next := map[string]string{}
	for k, v := range opts.Banner {
		next[k] = v
	}
	next["js"] = banner
	opts.Banner = next
}

// MarkerPackagesPlugin resolves the `server-only` and `client-only` marker
// packages to empty modules for bundles that only ever run on the server
// (build-time passes, server actions, route handlers, middleware). The real
// packages throw unless the bundler resolves the react-server condition, which
// Krate does not use. A client bundle must not use this plugin: it reports
// `server-only` as an error (see clientOnlyMarkersPlugin in internal/build).
func MarkerPackagesPlugin() api.Plugin {
	return api.Plugin{
		Name: "krate-server-markers",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^(server-only|client-only)$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: args.Path, Namespace: "krate-marker"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-marker"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
				empty := "export {};"
				return api.OnLoadResult{Contents: &empty, Loader: api.LoaderJS}, nil
			})
		},
	}
}

// explainDynamicRequire adds guidance to the QuickJS failure for a dependency
// that needs a real Node built-in.
func explainDynamicRequire(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	const marker = `Dynamic require of "`
	i := strings.Index(msg, marker)
	if i < 0 {
		return err
	}
	name := msg[i+len(marker):]
	if j := strings.Index(name, `"`); j >= 0 {
		name = name[:j]
	}
	return fmt.Errorf("%w (a dependency needs the Node built-in %q, which the embedded JavaScript engine does not provide; this module has to run on Node, Bun or Deno)", err, name)
}
