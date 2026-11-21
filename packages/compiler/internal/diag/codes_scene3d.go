package diag

// KR87xx: @krate/3d scenes.
const (
	// CodeScene3DFallback: a <Canvas> from @krate/3d could not be compiled
	// ahead of time and runs on the runtime reconciler instead.
	CodeScene3DFallback Code = "KR8701"
	// CodeScene3DAssets: the engine files a scene loads at run time could not
	// be provided (the filament package is missing, or is the wrong version).
	CodeScene3DAssets Code = "KR8702"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeScene3DFallback,
			Title: "3D scene runs on the runtime path",
			Explanation: "The compiler turns the JSX scene of an @krate/3d `<Canvas>` into direct Filament engine calls: " +
				"static transforms are flattened into world matrices, identical meshes become instances, and reactive " +
				"props become typed-array writes. A scene it cannot fully analyse (spread props, unknown components, " +
				"dynamic tag names, a reactive `<group>` transform, a `<For>` over a signal, a reactive geometry argument, " +
				"or a non-static shader string) is left as written and mounted by the runtime component path, which " +
				"creates the same Filament objects in the browser without those optimisations. The message names the " +
				"construct. This is a warning, not an error: the scene still renders. Remove the construct or hoist " +
				"static values into module-level constants to get the compiled path.",
		},
		CodeInfo{
			Code:  CodeScene3DAssets,
			Title: "3D engine files are not available",
			Explanation: "An @krate/3d scene loads the Filament engine in the browser. The build does not bundle it: it copies " +
				"filament.js and filament.wasm from the app's `filament` package into dist/assets/3d/ and tells the scene " +
				"where they are, so the engine never goes through a bundler. When the `filament` package cannot be found " +
				"from the project root, or its version does not match the Filament release the packaged materials were " +
				"built for, the build warns and the scene cannot start in the browser. Install it with `npm install filament` " +
				"(version 1.53.4 for this release of @krate/3d).",
		},
	)
}
