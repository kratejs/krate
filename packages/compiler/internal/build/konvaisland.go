package build

import (
	"github.com/evanw/esbuild/pkg/api"
)

// konvaIslandEntry is the island build of @krate/konva. A "use client"
// component of a server-rendered page runs as a Preact island, where the
// Krate-source <Stage> cannot be compiled; @krate/konva/island has the same
// react-konva API rendered by Preact (see packages/konva/src/island).
const konvaIslandEntry = "@krate/konva/island"

// konvaIslandPlugin points @krate/konva at its island build in a Preact bundle
// (islands, client boundaries and their server render), so a canvas editor in
// a "use client" component shares the island runtime and its React libraries
// instead of shipping React.
func konvaIslandPlugin() api.Plugin {
	return api.Plugin{
		Name: "krate-konva-island",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^@krate/konva$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				r := pb.Resolve(konvaIslandEntry, api.ResolveOptions{ResolveDir: args.ResolveDir, Kind: args.Kind, Importer: args.Importer})
				if len(r.Errors) > 0 {
					// An older @krate/konva without an island build: fall through
					// to the normal resolution (and its error, if any).
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: r.Path, Namespace: r.Namespace, External: r.External, PluginData: r.PluginData}, nil
			})
		},
	}
}

// konvaRealReactPlugin resolves @krate/konva to react-konva in a real-React
// island. A module migrated to @krate/konva can still be rendered by a
// component that is a real-React island for another reason; React already
// ships there, and react-konva renders the same API. Without react-konva
// installed the build says what to do.
func konvaRealReactPlugin() api.Plugin {
	return api.Plugin{
		Name: "krate-konva-real-react",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^@krate/konva$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				r := pb.Resolve("react-konva", api.ResolveOptions{ResolveDir: args.ResolveDir, Kind: args.Kind, Importer: args.Importer})
				if len(r.Errors) > 0 {
					return api.OnResolveResult{Errors: []api.Message{{
						Text:  "@krate/konva is imported by a real-React island (a module that uses a React reconciler library renders it), where it runs as react-konva, which is not installed",
						Notes: []api.Note{{Text: "install react-konva (keep it after `krate migrate nextjs`), or render the canvas component outside the real-React island"}},
					}}}, nil
				}
				return api.OnResolveResult{Path: r.Path, Namespace: r.Namespace, External: r.External, PluginData: r.PluginData}, nil
			})
		},
	}
}
