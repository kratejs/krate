package build

import (
	"github.com/evanw/esbuild/pkg/api"
)

// threeIslandEntries maps @krate/three entries to their Preact island builds.
// A "use client" component of a server-rendered page runs as a Preact island
// (React semantics on preact/compat), where @krate/three's Krate-source
// <Canvas> cannot be compiled; @krate/three/island has the same API built on
// Preact (see packages/three/src/island).
var threeIslandEntries = map[string]string{
	"@krate/three":      "@krate/three/island",
	"@krate/three/drei": "@krate/three/island/drei",
}

// threeIslandPlugin points @krate/three and @krate/three/drei at their island
// builds in a Preact bundle (islands, client boundaries and their server
// render), so a scene in a "use client" component shares the island runtime
// and its React libraries instead of shipping React.
func threeIslandPlugin() api.Plugin {
	return api.Plugin{
		Name: "krate-three-island",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^@krate/three(/drei)?$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				target, ok := threeIslandEntries[args.Path]
				if !ok {
					return api.OnResolveResult{}, nil
				}
				r := pb.Resolve(target, api.ResolveOptions{ResolveDir: args.ResolveDir, Kind: args.Kind, Importer: args.Importer})
				if len(r.Errors) > 0 {
					// An older @krate/three without island builds: fall through
					// to the normal resolution (and its error, if any).
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: r.Path, Namespace: r.Namespace, External: r.External, PluginData: r.PluginData}, nil
			})
		},
	}
}
