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
