package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// fileBatchMax caps the entry points of one esbuildBatchFiles call, which
// keeps its peak memory bounded.
const fileBatchMax = 24

// splitBatches splits items into the fewest slices of at most limit items,
// of near-equal size.
func splitBatches[T any](items []T, limit int) [][]T {
	if len(items) == 0 {
		return nil
	}
	n := (len(items) + limit - 1) / limit
	size := (len(items) + n - 1) / n
	var out [][]T
	for len(items) > 0 {
		k := min(size, len(items))
		out = append(out, items[:k:k])
		items = items[k:]
	}
	return out
}

// fileBatchEntry is one source file of esbuildBatchFiles and the output file
// a separate build of it (Outfile) writes.
type fileBatchEntry struct {
	In, Out string
}

// esbuildBatchFiles bundles source files that share opts in one esbuild call
// that writes nothing; the caller writes each entry's Files. Without code
// splitting esbuild links every entry point on its own, from only the files
// it reaches, so wrapping for lazy evaluation and tree shaking are decided per
// entry and an entry gets the output of a separate build. Each entry's
// Metafile holds the inputs its entry point reaches and its own outputs, as a
// separate build's would.
//
// It returns nil when the batch has errors or cannot be split per entry.
// Callers then build the entries on their own, so an error is reported
// against the entry that has it. The call is counted under the caller of
// esbuildBatchFiles.
func esbuildBatchFiles(opts api.BuildOptions, outdir string, entries []fileBatchEntry) ([]*batchOutput, api.BuildResult) {
	o := opts
	o.EntryPoints = nil
	o.Stdin = nil
	o.Outfile = ""
	o.Outdir = outdir
	o.Outbase = ""
	o.EntryNames = ""
	o.Write = false
	o.Metafile = true
	o.Splitting = false
	ext := ""
	for i, e := range entries {
		rel, err := filepath.Rel(outdir, e.Out)
		if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, api.BuildResult{}
		}
		x := filepath.Ext(e.Out)
		if i == 0 {
			ext = x
		} else if x != ext {
			return nil, api.BuildResult{}
		}
		o.EntryPointsAdvanced = append(o.EntryPointsAdvanced, api.EntryPoint{
			InputPath:  e.In,
			OutputPath: filepath.ToSlash(strings.TrimSuffix(rel, x)),
		})
	}
	if ext != ".js" {
		o.OutExtension = map[string]string{".js": ext}
	}
	res := esbuildBuildFrom(o, 3, "batch:")
	if len(res.Errors) > 0 || len(entries) == 0 {
		return nil, res
	}
	cwd := esbuildCwd(o, res.Metafile, entries[0].In)
	var meta struct {
		Inputs map[string]json.RawMessage `json:"inputs"`
	}
	if json.Unmarshal([]byte(res.Metafile), &meta) != nil {
		return nil, res
	}
	keys := make(map[string]int, len(entries))
	for i, e := range entries {
		keys[batchInputKey(cwd, e.In, meta.Inputs)] = i
	}
	if len(keys) != len(entries) {
		return nil, res
	}
	outs := splitBatchOutputs(cwd, len(entries), func(ep string) (int, bool) {
		i, ok := keys[ep]
		return i, ok
	}, res, true, false)
	if len(outs) != len(entries) {
		return nil, res
	}
	files := 0
	for i, e := range entries {
		js := false
		want := canonicalOutPath(e.Out)
		for _, f := range outs[i].Files {
			p := canonicalOutPath(f.Path)
			if p != want && p != want+".map" {
				return nil, res
			}
			js = js || p == want
		}
		if !js {
			return nil, res
		}
		files += len(outs[i].Files)
	}
	if files != len(res.OutputFiles) {
		return nil, res
	}
	inputs, ok := entryInputs(res.Metafile, keys)
	if !ok {
		return nil, res
	}
	got := make([]*batchOutput, len(entries))
	for i := range entries {
		var m struct {
			Outputs map[string]json.RawMessage `json:"outputs"`
		}
		if json.Unmarshal([]byte(outs[i].Metafile), &m) != nil {
			return nil, res
		}
		data, err := json.Marshal(map[string]map[string]json.RawMessage{"inputs": inputs[i], "outputs": m.Outputs})
		if err != nil {
			return nil, res
		}
		out := outs[i]
		out.Metafile = string(data)
		got[i] = &out
	}
	return got, res
}

// batchInputKey is how the metafile names the input file in: relative to
// cwd, and with its symlinks resolved when esbuild resolved them (on macOS the
// temporary directory /var is /private/var).
func batchInputKey(cwd, in string, inputs map[string]json.RawMessage) string {
	key := relSlash(cwd, in)
	if _, ok := inputs[key]; ok {
		return key
	}
	if real := relSlash(cwd, canonicalPath(in)); real != key {
		if _, ok := inputs[real]; ok {
			return real
		}
	}
	return key
}

// canonicalOutPath is p with the symlinks of its directory resolved, so an
// output file esbuild names under a resolved outdir matches the path asked
// for. The file itself need not exist.
func canonicalOutPath(p string) string {
	p = filepath.Clean(p)
	return filepath.Join(canonicalPath(filepath.Dir(p)), filepath.Base(p))
}

// entryInputs returns, per entry, the metafile inputs its entry point reaches,
// which are the inputs a separate build of it lists. ok is false when an
// input is reachable from no entry: esbuild redirected an import to a file
// another entry loaded (a package's "main" in place of its "module"), which a
// separate build would not have done.
func entryInputs(metafile string, entries map[string]int) ([]map[string]json.RawMessage, bool) {
	var meta struct {
		Inputs map[string]json.RawMessage `json:"inputs"`
	}
	if json.Unmarshal([]byte(metafile), &meta) != nil {
		return nil, false
	}
	edges := make(map[string][]string, len(meta.Inputs))
	for k, raw := range meta.Inputs {
		var in struct {
			Imports []struct {
				Path     string `json:"path"`
				External bool   `json:"external"`
			} `json:"imports"`
		}
		if json.Unmarshal(raw, &in) != nil {
			return nil, false
		}
		for _, imp := range in.Imports {
			if !imp.External {
				edges[k] = append(edges[k], imp.Path)
			}
		}
	}
	out := make([]map[string]json.RawMessage, len(entries))
	reached := make(map[string]bool, len(meta.Inputs))
	for ep, i := range entries {
		seen := map[string]json.RawMessage{}
		stack := []string{ep}
		for len(stack) > 0 {
			k := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			raw, ok := meta.Inputs[k]
			if _, dup := seen[k]; dup || !ok {
				continue
			}
			seen[k] = raw
			reached[k] = true
			stack = append(stack, edges[k]...)
		}
		out[i] = seen
	}
	return out, len(reached) == len(meta.Inputs)
}

// writeOutputFiles writes the files of a build run with Write false as
// esbuild would have written them.
func writeOutputFiles(files []api.OutputFile) error {
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(f.Path), 0755); err != nil {
			return err
		}
		mode := os.FileMode(0666)
		if strings.HasPrefix(string(f.Contents), "#!") {
			mode = 0777
		}
		if err := os.WriteFile(f.Path, f.Contents, mode); err != nil {
			return err
		}
	}
	return nil
}
