package build

import (
	"encoding/json"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// islandModuleGraph maps each island chunk, by its URL relative to the base
// path ("/chunks/x.js"), to what it imports statically: other chunk URLs, or
// bare specifiers that the page's import map resolves (the preact* chunks).
// It comes from the esbuild metafiles of the island builds, so a page can
// modulepreload every module its islands load instead of letting the browser
// find them one import level at a time.
type islandModuleGraph map[string][]string

type chunkMetafile struct {
	Outputs map[string]struct {
		Imports []struct {
			Path     string `json:"path"`
			Kind     string `json:"kind"`
			External bool   `json:"external"`
		} `json:"imports"`
	} `json:"outputs"`
}

// islandChunkURL is the URL, relative to the base path, of an island output
// file as writeIslandChunkFiles writes it: shared chunk names are lower-cased.
func islandChunkURL(p string) string {
	base := filepath.Base(filepath.FromSlash(p))
	for _, stem := range []string{islandSharedStem, islandRealSharedStem, islandNeutralStem} {
		if strings.HasPrefix(base, stem) {
			base = strings.ToLower(base)
			break
		}
	}
	return "/chunks/" + base
}

// addMetafile records the static imports of every .js output of one build.
func (g islandModuleGraph) addMetafile(metafile string) error {
	outs, err := metafileImports(metafile)
	for out, deps := range outs {
		if strings.HasSuffix(out, ".js") {
			g[islandChunkURL(out)] = deps
		}
	}
	return err
}

// metafileImports lists the static imports of each output of a metafile. A
// dynamic import() is left out: its chunk loads only when that code runs.
func metafileImports(metafile string) (map[string][]string, error) {
	if metafile == "" {
		return nil, nil
	}
	var m chunkMetafile
	if err := json.Unmarshal([]byte(metafile), &m); err != nil {
		return nil, err
	}
	outs := make(map[string][]string, len(m.Outputs))
	for out, o := range m.Outputs {
		deps := []string{}
		for _, imp := range o.Imports {
			if imp.Kind != "import-statement" {
				continue
			}
			switch {
			case !imp.External:
				deps = append(deps, islandChunkURL(imp.Path))
			case strings.HasPrefix(imp.Path, "./"):
				// A shared module of the other React flavour (isn_*), next
				// to the importer in /chunks.
				deps = append(deps, islandChunkURL(path.Base(imp.Path)))
			case !strings.HasPrefix(imp.Path, "/") && !strings.Contains(imp.Path, "://"):
				deps = append(deps, imp.Path)
			}
		}
		outs[out] = deps
	}
	return outs, nil
}

// preloadClosure expands the island entries a page imports on load into
// every module they load statically, each once, dependencies first. Bare
// specifiers resolve through importMap (specifier -> URL relative to the
// base path); a URL the graph does not know is kept as it is.
//
// The import-mapped modules come first, visited by specifier: they import
// only each other, so the order stays dependencies first, and it does not
// depend on the content-hashed names of the chunks that reach them.
func (g islandModuleGraph) preloadClosure(direct []string, importMap map[string]string) []string {
	seen := map[string]bool{}
	reached := map[string]bool{}
	var out []string
	var visit func(u string)
	visit = func(u string) {
		if seen[u] {
			return
		}
		seen[u] = true
		for _, d := range g[u] {
			if strings.HasPrefix(d, "/") {
				visit(d)
			} else if _, ok := importMap[d]; ok {
				reached[d] = true
			}
		}
		out = append(out, u)
	}
	for _, u := range direct {
		visit(u)
	}
	islands := out
	out = nil
	specs := make([]string, 0, len(reached))
	for spec := range reached {
		specs = append(specs, spec)
	}
	sort.Strings(specs)
	// The mapped modules' own imports are bare too; resolve them as edges.
	mapped := islandModuleGraph{}
	for _, spec := range specs {
		mapped.addMapped(g, importMap, importMap[spec])
	}
	seenMapped := map[string]bool{}
	var visitMapped func(u string)
	visitMapped = func(u string) {
		if seenMapped[u] || seen[u] {
			return
		}
		seenMapped[u] = true
		for _, d := range mapped[u] {
			visitMapped(d)
		}
		out = append(out, u)
	}
	for _, spec := range specs {
		visitMapped(importMap[spec])
	}
	return append(out, islands...)
}

// addMapped records, from u on, the import-mapped modules each one imports,
// as URLs.
func (m islandModuleGraph) addMapped(g islandModuleGraph, importMap map[string]string, u string) {
	if _, done := m[u]; done {
		return
	}
	m[u] = []string{}
	for _, d := range g[u] {
		if !strings.HasPrefix(d, "/") {
			var ok bool
			if d, ok = importMap[d]; !ok {
				continue
			}
		}
		m[u] = append(m[u], d)
		m.addMapped(g, importMap, d)
	}
}

// setIslandGraph replaces the recorded island chunk graph.
func (b *Builder) setIslandGraph(g islandModuleGraph) {
	b.islandMu.Lock()
	b.islandGraph = g
	b.islandMu.Unlock()
}

// islandPreloads expands a page's direct chunk preloads (URLs relative to
// the base path) with the island modules they load statically, including
// the import-mapped Preact chunks.
func (b *Builder) islandPreloads(direct []string) []string {
	b.islandMu.Lock()
	defer b.islandMu.Unlock()
	if len(b.islandGraph) == 0 && len(b.preactChunkDeps) == 0 {
		return direct
	}
	g := islandModuleGraph{}
	for k, v := range b.islandGraph {
		g[k] = v
	}
	basePath := b.Cfg.BaseURLPath()
	importMap := make(map[string]string, len(b.preactChunks))
	for spec, u := range b.preactChunks {
		importMap[spec] = strings.TrimPrefix(u, basePath)
	}
	for u, deps := range b.preactChunkDeps {
		g[u] = deps
	}
	return g.preloadClosure(direct, importMap)
}
