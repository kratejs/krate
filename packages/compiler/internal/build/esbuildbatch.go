package build

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
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
// (wrappedByOthers flags the entries that affects). esbuildBatch therefore
// serves only builds that are read, never shipped: the check builds of client
// components and the resource-hint renders. The server render modules of a
// full build use the same entries in a code-split build whose output may
// differ from separate builds (see clientSSRImpls.buildCombined).
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
	// Own is, in a split build, how many of Files (from the first) are the
	// entry's own outputs, and OwnMetafile holds their "outputs".
	Own         int
	OwnMetafile string
	// Inputs lists every file the entry loaded (its own import graph, not the
	// batch's), including the ones tree shaking left out of its output, as
	// absolute paths. The entry's Metafile has no "inputs" to read them from.
	Inputs []string
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

// esbuildCwd is the directory esbuild printed the paths of a build relative
// to. esbuild resolves the symlinks of its working directory (on macOS the
// temporary directory /var is /private/var) but, on Windows, keeps a short
// name as given, so the directory is the candidate whose relative path to
// probe is one the metafile names.
func esbuildCwd(opts api.BuildOptions, metafile, probe string) string {
	cwd := opts.AbsWorkingDir
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	real, err := filepath.EvalSymlinks(cwd)
	if err != nil || real == cwd {
		return cwd
	}
	if strings.Contains(metafile, strconv.Quote(relSlash(real, probe))) {
		return real
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
	return runEsbuildBatch(opts, entries, false)
}

// esbuildBatchSplit is esbuildBatch with code splitting: code that several
// entries reach is printed once, in a chunk they import. Each entry's Files
// are its entry output first, then the chunks it reaches by import, then its
// CSS; its Metafile holds the "outputs" of all of them.
//
// Esbuild tree shakes the batch as a whole and puts all the live code of a
// file in the chunk of the set of entries that reach the file. An entry's
// own output holds only files no other entry reaches, whose live code is the
// code a separate build of the entry keeps. The chunks it shares may also
// hold code only other entries use: its files are a superset of a separate
// build's code. An entry that reaches an import() is flagged Unsafe: on its
// own that module is inlined and wrapped for lazy evaluation, here it is a
// chunk of its own.
func esbuildBatchSplit(opts api.BuildOptions, entries []batchEntry) ([]batchOutput, api.BuildResult) {
	return runEsbuildBatch(opts, entries, true)
}

func runEsbuildBatch(opts api.BuildOptions, entries []batchEntry, split bool) ([]batchOutput, api.BuildResult) {
	if len(entries) == 0 {
		return nil, api.BuildResult{}
	}
	entries = withBatchEntries(&opts, entries)
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
	opts.Splitting = split
	// The call is counted under the caller of esbuildBatch(Split).
	res := esbuildBuildFrom(opts, 4, "batch:")
	if len(res.Errors) > 0 {
		return nil, res
	}
	cwd := esbuildCwd(opts, res.Metafile, batchModulePath(0, entries[0]))
	keys := make(map[string]int, len(entries))
	for i := range entries {
		keys[relSlash(cwd, batchModulePath(i, entries[i]))] = i
	}
	outs := splitBatchOutputs(cwd, len(entries), func(ep string) (int, bool) {
		i, ok := keys[ep]
		return i, ok
	}, res, wantMeta, split)
	for i := range wrappedByOthers(res.Metafile, keys, split) {
		outs[i].Unsafe = true
	}
	if wantMeta {
		for i, in := range batchEntryInputs(cwd, res.Metafile, keys) {
			outs[i].Inputs = in
		}
	}
	// A separate build lists its Stdin module as an input at the Stdin path.
	for i := range outs {
		from, _ := json.Marshal(relSlash(cwd, batchModulePath(i, entries[i])))
		to, _ := json.Marshal(stdinPrettyPath(cwd, entries[i]))
		outs[i].Metafile = strings.ReplaceAll(outs[i].Metafile, string(from), string(to))
		outs[i].OwnMetafile = strings.ReplaceAll(outs[i].OwnMetafile, string(from), string(to))
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

// withBatchEntries makes entries the entry points of opts, served from memory
// by batchEntryPlugin, and returns the entries as served (with the tier
// decorators blanked, as esbuildBuild blanks them in Stdin).
func withBatchEntries(opts *api.BuildOptions, entries []batchEntry) []batchEntry {
	entries = append([]batchEntry(nil), entries...)
	points := make([]api.EntryPoint, len(entries))
	for i := range entries {
		name := entries[i].Sourcefile
		if name == "" {
			name = "stdin.tsx"
		}
		entries[i].Source = tsstrip.BlankTierDecorators(name, entries[i].Source)
		out := entries[i].OutputPath
		if out == "" {
			out = "krate-batch-" + strconv.Itoa(i)
		}
		points[i] = api.EntryPoint{InputPath: "krate-batch-entry:" + strconv.Itoa(i), OutputPath: out}
	}
	opts.EntryPointsAdvanced = points
	opts.EntryPoints = nil
	opts.Stdin = nil
	opts.Outfile = ""
	opts.Plugins = append([]api.Plugin{batchEntryPlugin(entries)}, opts.Plugins...)
	return entries
}

// splitBatchOutputs assigns each output file of a batched build to its entry
// by the metafile, which names the entry point of every output relative to
// cwd (from esbuildCwd). entryOf maps a metafile entryPoint to the entry's
// index. Each entry's metafile holds only its "outputs".
//
// With split, an entry also owns the chunks its outputs import, statically or
// with import(), transitively: they come after its entry output, in the order
// found, and a chunk several entries import belongs to each of them.
func splitBatchOutputs(cwd string, n int, entryOf func(string) (int, bool), res api.BuildResult, wantMeta, split bool) []batchOutput {
	var meta struct {
		Outputs map[string]json.RawMessage `json:"outputs"`
	}
	_ = json.Unmarshal([]byte(res.Metafile), &meta)
	outs := make([]batchOutput, n)
	metaOf := make([]map[string]json.RawMessage, n)
	entryInput := make([]string, n)
	type outInfo struct {
		EntryPoint string `json:"entryPoint"`
		CSSBundle  string `json:"cssBundle"`
		Imports    []struct {
			Path     string `json:"path"`
			Kind     string `json:"kind"`
			External bool   `json:"external"`
		} `json:"imports"`
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
	// owned[i] lists the output paths of entry i in file order: the entry
	// output, the chunks it reaches, its CSS.
	owned := make([][]string, n)
	var css [][]string
	if split {
		css = make([][]string, n)
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
		isCSS := false
		if !ok {
			i, ok = cssOwner[path]
			isCSS = ok
		}
		if !ok {
			continue
		}
		if isCSS && split {
			css[i] = append(css[i], path)
		} else {
			owned[i] = append(owned[i], path)
		}
	}
	fileAt := make(map[string]int, len(res.OutputFiles))
	for k, f := range res.OutputFiles {
		fileAt[filepath.Clean(f.Path)] = k
	}
	absOut := func(path string) string {
		abs := filepath.FromSlash(path)
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(cwd, abs)
		}
		return filepath.Clean(abs)
	}
	// An entry's own outputs keep esbuild's file order (its JS first).
	for i := range owned {
		sort.SliceStable(owned[i], func(a, b int) bool {
			return fileAt[absOut(owned[i][a])] < fileAt[absOut(owned[i][b])]
		})
	}
	own := make([]int, n)
	for i := range owned {
		own[i] = len(owned[i])
	}
	if split {
		for i := range owned {
			seen := map[string]bool{}
			for _, p := range owned[i] {
				seen[p] = true
			}
			for k := 0; k < len(owned[i]); k++ {
				var o outInfo
				if json.Unmarshal(meta.Outputs[owned[i][k]], &o) != nil {
					continue
				}
				for _, imp := range o.Imports {
					if imp.External || seen[imp.Path] || (imp.Kind != "import-statement" && imp.Kind != "dynamic-import") {
						continue
					}
					if _, ok := meta.Outputs[imp.Path]; !ok {
						continue
					}
					seen[imp.Path] = true
					owned[i] = append(owned[i], imp.Path)
				}
			}
			owned[i] = append(owned[i], css[i]...)
		}
	}
	ownMeta := make([]map[string]json.RawMessage, n)
	for i, paths := range owned {
		added := map[int]bool{}
		ownMeta[i] = map[string]json.RawMessage{}
		for k, path := range paths {
			if metaOf[i] == nil {
				metaOf[i] = map[string]json.RawMessage{}
			}
			metaOf[i][path] = meta.Outputs[path]
			if k < own[i] {
				ownMeta[i][path] = meta.Outputs[path]
			}
			abs := absOut(path)
			for _, p := range []string{abs, abs + ".map"} {
				if f, ok := fileAt[p]; ok && !added[f] {
					added[f] = true
					outs[i].Files = append(outs[i].Files, res.OutputFiles[f])
					if k < own[i] {
						outs[i].Own++
					}
				}
			}
		}
	}
	if wantMeta {
		for i := range outs {
			data, _ := json.Marshal(map[string]any{"outputs": metaOf[i]})
			outs[i].Metafile = string(data)
			if split {
				data, _ = json.Marshal(map[string]any{"outputs": ownMeta[i]})
				outs[i].OwnMetafile = string(data)
			}
		}
	}
	return outs
}

// batchEntryInputs lists, for each entry, the files in its import graph: what
// its own build would have loaded. A batch's metafile holds one graph for all
// its entries, so a stamp taken from it would follow every entry's files, and a
// per-entry metafile has no inputs at all. Entries are keyed as in
// runEsbuildBatch; paths come back absolute and sorted, without the entries'
// stand-in modules (which are not files).
func batchEntryInputs(cwd, metafile string, entries map[string]int) map[int][]string {
	var meta struct {
		Inputs map[string]struct {
			Imports []struct {
				Path     string `json:"path"`
				External bool   `json:"external"`
			} `json:"imports"`
		} `json:"inputs"`
	}
	if json.Unmarshal([]byte(metafile), &meta) != nil {
		return nil
	}
	out := make(map[int][]string, len(entries))
	for ep, i := range entries {
		seen := map[string]bool{ep: true}
		queue := []string{ep}
		var files []string
		for len(queue) > 0 {
			in := queue[0]
			queue = queue[1:]
			if in != ep {
				if p, ok := metafileInputPath(cwd, in); ok {
					files = append(files, p)
				}
			}
			for _, imp := range meta.Inputs[in].Imports {
				if imp.External || seen[imp.Path] {
					continue
				}
				seen[imp.Path] = true
				queue = append(queue, imp.Path)
			}
		}
		sort.Strings(files)
		out[i] = files
	}
	return out
}

// wrappedByOthers returns the entries a batch may print differently from
// separate builds. esbuild decides per file, for the whole build, whether a
// module is wrapped for lazy evaluation: a module that one entry loads with
// require() or import() is wrapped for every entry, and a wrapped module keeps
// code tree shaking would drop. An entry that reaches such a module without
// loading it lazily itself would be printed differently on its own.
//
// With split, an entry that reaches an import() is returned too: a separate
// build inlines and wraps the imported module, a split build makes it a chunk.
func wrappedByOthers(metafile string, entries map[string]int, split bool) map[int]bool {
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
	dynamic := map[string]bool{} // importers with an import() of a bundled module
	for in, v := range meta.Inputs {
		for _, imp := range v.Imports {
			if imp.External {
				continue
			}
			edges[in] = append(edges[in], imp.Path)
			if split && imp.Kind == "dynamic-import" {
				dynamic[in] = true
			}
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
	if len(lazy) == 0 && len(dynamic) == 0 {
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
		for n := range dynamic {
			if reach[n] {
				out[i] = true
				break
			}
		}
		if out[i] {
			continue
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
