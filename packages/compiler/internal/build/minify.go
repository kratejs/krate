package build

import (
	"sync"

	"github.com/evanw/esbuild/pkg/api"
)

// minifyMemo caches minify output by input hash. esbuild's Go API has no
// shareable service across differing inputs - `api.Context` binds both options
// and input, and `api.Transform` is single-shot - so the practical reuse is to
// skip re-minifying identical inputs. The runtime chunk is written on multiple
// build paths from the same source, and pages can share an identical hydration
// bundle. Content-keyed, so it is always correct across dev rebuilds; bounded so
// a long-lived dev process cannot grow without limit.
const minifyMemoMax = 256

var minifyMemo = struct {
	mu sync.Mutex
	m  map[string]string
}{m: make(map[string]string)}

func minifyMemoKey(mode, sourcefile, js string) string {
	return mode + "\x00" + sourcefile + "\x00" + hashContent([]byte(js))
}

func minifyMemoGet(key string) (string, bool) {
	minifyMemo.mu.Lock()
	defer minifyMemo.mu.Unlock()
	v, ok := minifyMemo.m[key]
	return v, ok
}

func minifyMemoPut(key, out string) {
	minifyMemo.mu.Lock()
	defer minifyMemo.mu.Unlock()
	if len(minifyMemo.m) >= minifyMemoMax {
		return
	}
	minifyMemo.m[key] = out
}

// minifyJS minifies a JS bundle (hydration script or runtime chunk) with
// esbuild's full minifier (whitespace, syntax, and scope-aware identifier
// mangling). Free identifiers (createSignal, findSlot, window globals, etc.)
// are left untouched, so cross-chunk references stay valid. On any esbuild
// error the input is returned unchanged rather than shipping broken JS.
func minifyJS(js string) string {
	return minifyJSBase(js)
}

// minifyJSBase is the shared entry point for JS minification (also used for the
// runtime chunk), memoized so the same source is only transformed once.
func minifyJSBase(js string) string {
	key := minifyMemoKey("js", "", js)
	if v, ok := minifyMemoGet(key); ok {
		return v
	}
	out := minifyJSRaw(js)
	minifyMemoPut(key, out)
	return out
}

// minifyJSRaw runs the esbuild transform without the memo. Kept separate so the
// memo can be validated against a fresh transform.
func minifyJSRaw(js string) string {
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
	return minifyJSWithMapRaw(js, sourcefile)
}

// minifyJSWithMapRaw runs the esbuild transform (with source map) without the
// memo.
func minifyJSWithMapRaw(js, sourcefile string) (string, string) {
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
