package build

import (
	"bytes"
	"encoding/json"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// With code splitting, esbuild makes the target of an import() its own chunk.
// When that module is CommonJS the chunk's only export is
// `export default module.exports`, so `const { readPsd } = await
// import("ag-psd")` finds no readPsd. An unsplit build (and webpack, which
// Next uses) resolves the import() to the module's exports as named exports,
// through __toESM. cjsNamespaceJS is that conversion, applied to the chunk's
// namespace: a getter per own property of module.exports, and default set to
// module.exports, or to its default when it was compiled from ESM
// (__esModule).
const cjsNamespaceJS = `function(m){var e=m.default,n=e!=null?Object.create(Object.getPrototypeOf(e)):{},d;` +
	`if(!e||!e.__esModule)Object.defineProperty(n,"default",{value:e,enumerable:!0});` +
	`if(e&&typeof e=="object"||typeof e=="function")Object.getOwnPropertyNames(e).forEach(function(k){` +
	`Object.prototype.hasOwnProperty.call(n,k)||Object.defineProperty(n,k,{get:function(){return e[k]},enumerable:!(d=Object.getOwnPropertyDescriptor(e,k))||d.enumerable})});` +
	`return n}`

// interopCJSDynamicImports rewrites, in the outputs of a code-split build,
// each import() of a chunk that is a CommonJS module's entry to resolve to
// the module's named exports (see cjsNamespaceJS). cwd is the build's working
// directory, which the metafile's paths are relative to.
func interopCJSDynamicImports(files []api.OutputFile, metafile, cwd string) {
	if metafile == "" || !strings.Contains(metafile, `"dynamic-import"`) {
		return
	}
	var meta struct {
		Inputs map[string]struct {
			Format string `json:"format"`
		} `json:"inputs"`
		Outputs map[string]struct {
			EntryPoint string   `json:"entryPoint"`
			Exports    []string `json:"exports"`
			Imports    []struct {
				Path     string `json:"path"`
				Kind     string `json:"kind"`
				External bool   `json:"external"`
			} `json:"imports"`
		} `json:"outputs"`
	}
	if json.Unmarshal([]byte(metafile), &meta) != nil {
		return
	}
	cjs := map[string]bool{}
	for out, o := range meta.Outputs {
		if o.EntryPoint != "" && meta.Inputs[o.EntryPoint].Format == "cjs" && len(o.Exports) == 1 && o.Exports[0] == "default" {
			cjs[out] = true
		}
	}
	if len(cjs) == 0 {
		return
	}
	index := outputFileIndex(files, cwd)
	for out, o := range meta.Outputs {
		i, ok := index[out]
		if !ok {
			continue
		}
		contents := files[i].Contents
		for _, imp := range o.Imports {
			if imp.Kind != "dynamic-import" || imp.External || !cjs[imp.Path] {
				continue
			}
			call := []byte("import(" + strconv.Quote(chunkSpecifier(out, imp.Path)) + ")")
			wrapped := append(append(append([]byte(nil), call...), ".then("...), cjsNamespaceJS+")"...)
			contents = replaceCall(contents, call, wrapped)
		}
		files[i].Contents = contents
	}
}

// replaceCall replaces each call in src with wrapped, leaving one already
// wrapped alone.
func replaceCall(src, call, wrapped []byte) []byte {
	var out []byte
	rest := src
	for {
		i := bytes.Index(rest, call)
		if i < 0 {
			break
		}
		if out == nil {
			out = make([]byte, 0, len(src)+len(wrapped))
		}
		out = append(out, rest[:i]...)
		if bytes.HasPrefix(rest[i:], wrapped) {
			out = append(out, wrapped...)
			rest = rest[i+len(wrapped):]
			continue
		}
		out = append(out, wrapped...)
		rest = rest[i+len(call):]
	}
	if out == nil {
		return src
	}
	return append(out, rest...)
}

// chunkSpecifier is the import() specifier esbuild prints in output from for
// output to (both metafile paths).
func chunkSpecifier(from, to string) string {
	rel, err := filepath.Rel(filepath.FromSlash(path.Dir(from)), filepath.FromSlash(to))
	if err != nil {
		return "./" + path.Base(to)
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, "../") {
		rel = "./" + rel
	}
	return rel
}

// outputFileIndex maps the metafile path of each output file (relative to
// cwd, or to its real path, which esbuild may print instead) to its index.
func outputFileIndex(files []api.OutputFile, cwd string) map[string]int {
	dirs := []string{cwd}
	if real, err := filepath.EvalSymlinks(cwd); err == nil && real != cwd {
		dirs = append(dirs, real)
	}
	index := make(map[string]int, len(files)*len(dirs))
	for i, f := range files {
		for _, dir := range dirs {
			if rel, err := filepath.Rel(dir, f.Path); err == nil {
				index[filepath.ToSlash(rel)] = i
			}
		}
	}
	return index
}
