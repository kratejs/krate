package build

import (
	"github.com/evanw/esbuild/pkg/api"
)

// minifyJS minifies a JS bundle (hydration script or runtime chunk) with
// esbuild's full minifier (whitespace, syntax, and scope-aware identifier
// mangling). Free identifiers (createSignal, findSlot, window globals, etc.)
// are left untouched, so cross-chunk references stay valid. On any esbuild
// error the input is returned unchanged rather than shipping broken JS.
func minifyJS(js string) string {
	return minifyJSBase(js)
}

// minifyJSBase is kept as the shared entry point for JS minification.
func minifyJSBase(js string) string {
	result := api.Transform(js, api.TransformOptions{
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Charset:           api.CharsetUTF8,
	})
	if len(result.Errors) > 0 {
		return js
	}
	return string(result.Code)
}

// minifyJSWithMap minifies like minifyJS and also returns a real (column-aware)
// source map as JSON, so the minified output can be decoded back to the
// pre-minification code. `sourcefile` labels the input in the map's `sources`.
// On any esbuild error it returns the input unchanged and an empty map.
func minifyJSWithMap(js, sourcefile string) (string, string) {
	result := api.Transform(js, api.TransformOptions{
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Charset:           api.CharsetUTF8,
		Sourcefile:        sourcefile,
		SourcesContent:    api.SourcesContentInclude,
		Sourcemap:         api.SourceMapExternal,
	})
	if len(result.Errors) > 0 {
		return js, ""
	}
	return string(result.Code), string(result.Map)
}
