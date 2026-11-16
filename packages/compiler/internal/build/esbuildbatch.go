package build

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// A batched esbuild build bundles many generated entry modules in one call
// instead of one call each: the modules the entries share are read, resolved
// and parsed once. esbuild links every entry point on its own when code
// splitting is off, but not byte for byte as a separate build would: it
// numbers the symbols of non-minified output across the build, and wraps a
// module for lazy evaluation for every entry when one entry loads it lazily
// (wrappedByOthers flags the entries that affects). Batches therefore serve
// only builds that are read, never shipped: the check builds of client
// components and the resource-hint renders.
//
// Like Stdin, an entry is a module of the file namespace in its ResolveDir,
// so plugins see its imports as they see a Stdin module's (some act only on
// imports from files); each entry gets a directory of its own there, as two
// entries may share a ResolveDir.

// batchEntry is one generated entry module of a batched build: what a separate
// build would pass as Stdin.
type batchEntry struct {
	Source     string
	ResolveDir string
	// Sourcefile names the module like Stdin.Sourcefile does.
	Sourcefile string
	Loader     api.Loader
	// OutputPath is the entry's output path relative to Outdir, without the
	// extension. Empty picks a unique name.
	OutputPath string
}

// batchOutput is the part of a batched build that belongs to one entry.
type batchOutput struct {
	Files []api.OutputFile
	// Metafile holds only this entry's outputs, in esbuild's metafile shape.
	Metafile string
	// Unsafe is set when the batch may have printed the entry differently
	// from a separate build of it (see wrappedByOthers); the caller builds
	// it on its own.
	Unsafe bool
}

// batchEntryPlugin serves the batch's entry modules from memory.
func batchEntryPlugin(entries []batchEntry) api.Plugin {
	return api.Plugin{
		Name: "krate-batch-entries",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^krate-batch-entry:`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.Kind != api.ResolveEntryPoint {
					return api.OnResolveResult{}, nil
				}
				i, err := strconv.Atoi(strings.TrimPrefix(args.Path, "krate-batch-entry:"))
				if err != nil || i < 0 || i >= len(entries) {
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: batchModulePath(i, entries[i]), Namespace: "file"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: batchDirRe, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				for i, e := range entries {
					if args.Path == batchModulePath(i, e) {
						src := e.Source
						return api.OnLoadResult{Contents: &src, ResolveDir: e.ResolveDir, Loader: e.Loader}, nil
					}
				}
				return api.OnLoadResult{}, nil
			})
		},
	}
}

// batchDirRe matches the paths of batch entry modules (batchModulePath).
const batchDirRe = `[/\\]\.krate-batch-[0-9]+[/\\][^/\\]+$`

// batchModulePath is the path of entry i: its Sourcefile, in a directory of
// its own in its ResolveDir. It ends in the Sourcefile, so esbuild derives the
// identifier names a Stdin module gets.
func batchModulePath(i int, e batchEntry) string {
	name := e.Sourcefile
	if name == "" {
		name = "stdin.tsx"
	}
	return filepath.Join(e.ResolveDir, ".krate-batch-"+strconv.Itoa(i), name)
}

// esbuildCwd is the directory esbuild prints paths relative to: the working
// directory with its symlinks resolved, as esbuild resolves it (on macOS the
// temporary directory /var is /private/var; on Windows a short name expands).
func esbuildCwd(opts api.BuildOptions) string {
	cwd := opts.AbsWorkingDir
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	return cwd
}

// relSlash is p relative to cwd with forward slashes, as esbuild prints it.
func relSlash(cwd, p string) string {
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, p); err == nil {
			p = rel
		}
	}
	return filepath.ToSlash(p)
}

// stdinPrettyPath is how esbuild names the Stdin module of a separate build in
// its output comments: the Sourcefile in ResolveDir, relative to the working
// directory.
func stdinPrettyPath(cwd string, e batchEntry) string {
	name := e.Sourcefile
	if name == "" {
		name = "<stdin>"
	}
	if e.ResolveDir == "" {
		return name
	}
	p := filepath.Join(e.ResolveDir, name)
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, p); err == nil {
			p = rel
		}
	}
	return filepath.ToSlash(p)
}

// esbuildBatch bundles entries in one esbuild call with the shared options
// opts (which must not set Stdin, EntryPoints or Splitting). It returns each
// entry's outputs, in entry order, and the build result for its errors. Any
// error fails the whole batch; callers then build the entries separately so
// each error is reported against the entry that has it. With opts.Metafile
// each entry gets the "outputs" of its metafile (not the "inputs").
func esbuildBatch(opts api.BuildOptions, entries []batchEntry) ([]batchOutput, api.BuildResult) {
	if len(entries) == 0 {
		return nil, api.BuildResult{}
	}
	entries = append([]batchEntry(nil), entries...)
	points := make([]api.EntryPoint, len(entries))
	for i := range entries {
		name := entries[i].Sourcefile
		if name == "" {
			name = "stdin.tsx"
		}
		// Stdin gets the tier decorators blanked by esbuildBuild; an entry
		// module does not go through Stdin.
		entries[i].Source = tsstrip.BlankTierDecorators(name, entries[i].Source)
		out := entries[i].OutputPath
		if out == "" {
			out = "krate-batch-" + strconv.Itoa(i)
		}
		points[i] = api.EntryPoint{InputPath: "krate-batch-entry:" + strconv.Itoa(i), OutputPath: out}
	}
	opts.EntryPointsAdvanced = points
	opts.Stdin = nil
	opts.Outfile = ""
	if opts.Outdir == "" {
		opts.Outdir = filepath.Join(opts.AbsWorkingDir, ".krate", "batch")
		if opts.AbsWorkingDir == "" {
			opts.Outdir = filepath.Join(".krate", "batch")
		}
	}
	// The default entry names "[dir]/[name]" keep the directories of
	// OutputPath; "[name]" would drop them.
	opts.EntryNames = ""
	wantMeta := opts.Metafile
	opts.Metafile = true
	opts.Plugins = append([]api.Plugin{batchEntryPlugin(entries)}, opts.Plugins...)
	res := esbuildBuildFrom(opts, 3, "batch:")
	if len(res.Errors) > 0 {
		return nil, res
	}
	cwd := esbuildCwd(opts)
	keys := make(map[string]int, len(entries))
	for i := range entries {
		keys[relSlash(cwd, batchModulePath(i, entries[i]))] = i
	}
	outs := splitBatchOutputs(opts, len(entries), func(ep string) (int, bool) {
		i, ok := keys[ep]
		return i, ok
	}, res, wantMeta)
	for i := range wrappedByOthers(res.Metafile, keys) {
		outs[i].Unsafe = true
	}
	// A separate build lists its Stdin module as an input at the Stdin path.
	for i := range outs {
		if outs[i].Metafile != "" {
			from, _ := json.Marshal(relSlash(cwd, batchModulePath(i, entries[i])))
			to, _ := json.Marshal(stdinPrettyPath(cwd, entries[i]))
			outs[i].Metafile = strings.ReplaceAll(outs[i].Metafile, string(from), string(to))
		}
	}
	if !opts.MinifyWhitespace {
		// Name each entry module in the output comments as its Stdin build would.
		for i := range outs {
			from := []byte("// " + relSlash(cwd, batchModulePath(i, entries[i])) + "\n")
			to := []byte("// " + stdinPrettyPath(cwd, entries[i]) + "\n")
			for k, f := range outs[i].Files {
				if bytes.Contains(f.Contents, from) {
					outs[i].Files[k].Contents = bytes.ReplaceAll(f.Contents, from, to)
				}
			}
		}
	}
	return outs, res
}

// splitBatchOutputs assigns each output file of a batched build to its entry
// by the metafile, which names the entry point of every output. entryOf maps
// a metafile entryPoint to the entry's index. Each entry's metafile holds
// only its "outputs".
func splitBatchOutputs(opts api.BuildOptions, n int, entryOf func(string) (int, bool), res api.BuildResult, wantMeta bool) []batchOutput {
	var meta struct {
		Outputs map[string]json.RawMessage `json:"outputs"`
	}
	_ = json.Unmarshal([]byte(res.Metafile), &meta)
	outs := make([]batchOutput, n)
	metaOf := make([]map[string]json.RawMessage, n)
	entryInput := make([]string, n)
	ownerOf := map[string]int{}
	cwd := esbuildCwd(opts)
	type outInfo struct {
		EntryPoint string `json:"entryPoint"`
		CSSBundle  string `json:"cssBundle"`
	}
	cssOwner := map[string]int{}
	for _, raw := range meta.Outputs {
		var o outInfo
		if json.Unmarshal(raw, &o) != nil || o.EntryPoint == "" {
			continue
		}
		if i, ok := entryOf(o.EntryPoint); ok && o.CSSBundle != "" {
			cssOwner[o.CSSBundle] = i
		}
	}
	for path, raw := range meta.Outputs {
		var o outInfo
		if json.Unmarshal(raw, &o) != nil {
			continue
		}
		i, ok := -1, false
		if o.EntryPoint != "" {
			i, ok = entryOf(o.EntryPoint)
			if ok {
				entryInput[i] = o.EntryPoint
			}
		}
		if !ok {
			i, ok = cssOwner[path]
		}
		if !ok {
			continue
		}
		if metaOf[i] == nil {
			metaOf[i] = map[string]json.RawMessage{}
		}
		metaOf[i][path] = raw
		abs := filepath.FromSlash(path)
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(cwd, abs)
		}
		ownerOf[filepath.Clean(abs)] = i
		ownerOf[filepath.Clean(abs)+".map"] = i
	}
	for _, f := range res.OutputFiles {
		if i, ok := ownerOf[filepath.Clean(f.Path)]; ok {
			outs[i].Files = append(outs[i].Files, f)
		}
	}
	if wantMeta {
		for i := range outs {
			data, _ := json.Marshal(map[string]any{"outputs": metaOf[i]})
			outs[i].Metafile = string(data)
		}
	}
	return outs
}

// wrappedByOthers returns the entries a batch may print differently from
// separate builds. esbuild decides per file, for the whole build, whether a
// module is wrapped for lazy evaluation: a module that one entry loads with
// require() or import() is wrapped for every entry, and a wrapped module keeps
// code tree shaking would drop. An entry that reaches such a module without
// loading it lazily itself would be printed differently on its own.
func wrappedByOthers(metafile string, entries map[string]int) map[int]bool {
	var meta struct {
		Inputs map[string]struct {
			Format  string `json:"format"`
			Imports []struct {
				Path     string `json:"path"`
				Kind     string `json:"kind"`
				External bool   `json:"external"`
			} `json:"imports"`
		} `json:"inputs"`
	}
	if json.Unmarshal([]byte(metafile), &meta) != nil {
		// Without the graph nothing can be shown safe.
		all := map[int]bool{}
		for _, i := range entries {
			all[i] = true
		}
		return all
	}
	lazy := map[string]map[string]bool{} // target -> importers loading it lazily
	edges := map[string][]string{}
	for in, v := range meta.Inputs {
		for _, imp := range v.Imports {
			if imp.External {
				continue
			}
			edges[in] = append(edges[in], imp.Path)
			switch imp.Kind {
			case "require-call", "require-resolve", "dynamic-import":
				if meta.Inputs[imp.Path].Format == "cjs" {
					continue // CommonJS is wrapped in every build
				}
				if lazy[imp.Path] == nil {
					lazy[imp.Path] = map[string]bool{}
				}
				lazy[imp.Path][in] = true
			}
		}
	}
	out := map[int]bool{}
	if len(lazy) == 0 {
		return out
	}
	for ep, i := range entries {
		reach := map[string]bool{}
		stack := []string{ep}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if reach[n] {
				continue
			}
			reach[n] = true
			stack = append(stack, edges[n]...)
		}
		for target, importers := range lazy {
			if !reach[target] {
				continue
			}
			own := false
			for imp := range importers {
				own = own || reach[imp]
			}
			if !own {
				out[i] = true
				break
			}
		}
	}
	return out
}
