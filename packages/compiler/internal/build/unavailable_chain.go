package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// builtinHit is the import of a Node built-in behind an unavailableGuard
// error: the file that imports it and the specifier.
type builtinHit struct{ importer, spec string }

// builtinFixHint is the hint of an error that summarize wrote.
const builtinFixHint = "import a browser-safe subpath of the package instead of its root, or move the code that needs Node " +
	"built-ins to a server component, an API route or a \"use server\" function"

// metaImport is one import record of a metafile input.
type metaImport struct {
	Path     string `json:"path"`
	Original string `json:"original"`
	External bool   `json:"external"`
}

// metaGraph is the import graph of a build, from its metafile.
type metaGraph struct {
	imports map[string][]metaImport // input -> its imports
	abs     map[string]string       // absolute path of a file input -> input
}

func parseMetaGraph(root, metafile string) *metaGraph {
	if metafile == "" {
		return nil
	}
	var meta struct {
		Inputs map[string]struct {
			Imports []metaImport `json:"imports"`
		} `json:"inputs"`
	}
	if json.Unmarshal([]byte(metafile), &meta) != nil || len(meta.Inputs) == 0 {
		return nil
	}
	g := &metaGraph{imports: make(map[string][]metaImport, len(meta.Inputs)), abs: map[string]string{}}
	for k, v := range meta.Inputs {
		g.imports[k] = v.Imports
		if strings.Contains(k, ":") || k == "<stdin>" {
			continue
		}
		p := filepath.Join(root, filepath.FromSlash(k))
		g.abs[filepath.Clean(p)] = k
		g.abs[canonicalPath(p)] = k
	}
	return g
}

// roots are the inputs nothing imports: the entry points of the build.
func (g *metaGraph) roots() []string {
	imported := map[string]bool{}
	for _, imps := range g.imports {
		for _, im := range imps {
			imported[im.Path] = true
		}
	}
	var out []string
	for k := range g.imports {
		if !imported[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// bfs returns the shortest import path from root to every reachable input
// as the input before it and the import record that led to it.
func (g *metaGraph) bfs(root string) (parent map[string]string, via map[string]metaImport) {
	parent = map[string]string{root: ""}
	via = map[string]metaImport{}
	queue := []string{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, im := range g.imports[cur] {
			if im.External {
				continue
			}
			if _, seen := parent[im.Path]; seen {
				continue
			}
			if _, known := g.imports[im.Path]; !known {
				continue
			}
			parent[im.Path] = cur
			via[im.Path] = im
			queue = append(queue, im.Path)
		}
	}
	return parent, via
}

// chainNode is one step of an import chain: a project file, or a package
// (every file of it in a row).
type chainNode struct {
	label string
	local string // project file input, empty for a package
	pkg   string // package name, empty for a project file
	via   metaImport
}

// packageOf names the npm package a node_modules path belongs to, or "".
func packageOf(path string) string {
	slash := filepath.ToSlash(path)
	i := strings.LastIndex(slash, "node_modules/")
	if i < 0 {
		return ""
	}
	parts := strings.Split(slash[i+len("node_modules/"):], "/")
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	if strings.HasPrefix(parts[0], "@") && len(parts) > 1 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

// chain lists the steps from the entry to key, files of one package merged.
// The entry itself (a stdin or virtual input) is not a step.
func (g *metaGraph) chain(rel func(string) string, parent map[string]string, via map[string]metaImport, key string) []chainNode {
	var keys []string
	for k := key; k != ""; k = parent[k] {
		keys = append(keys, k)
	}
	var out []chainNode
	// keys[len-1] is the entry of the build, which is no step of the chain.
	for i := len(keys) - 2; i >= 0; i-- {
		k := keys[i]
		if pkg := packageOf(k); pkg != "" {
			if n := len(out); n > 0 && out[n-1].pkg == pkg {
				continue
			}
			node := chainNode{label: pkg, pkg: pkg, via: via[k]}
			// The import the project's own code wrote is the one to change.
			if n := len(out); n > 0 && out[n-1].pkg == "" && node.via.Original == pkg {
				node.label = pkg + " (package root)"
			}
			out = append(out, node)
			continue
		}
		out = append(out, chainNode{label: rel(k), local: k, via: via[k]})
	}
	return out
}

func joinChain(nodes []chainNode) string {
	labels := make([]string, len(nodes))
	for i, n := range nodes {
		labels[i] = n.label
	}
	return strings.Join(labels, " -> ")
}

// builtinGroup is what one island entry reaches: the imports of Node
// built-ins in the modules its chunk bundles.
type builtinGroup struct {
	hits  []api.Message
	chain []chainNode // to the first of them
	depth int
}

// summarize replaces the one error per import of a Node built-in with one
// error for the island: the built-ins named, how many imports there are, and
// the import chain from the island's entry module to the first module that
// pulled in the Node-only code. A package such as a database driver imports
// built-ins in thousands of files, and listing each hides the one import that
// matters. With verbose set the full list follows the summary. Other errors
// pass through.
func (g *unavailableGuard) summarize(msgs []api.Message, metafile string) []api.Message {
	var hits, rest []api.Message
	for _, m := range msgs {
		if _, ok := m.Detail.(builtinHit); ok {
			hits = append(hits, m)
		} else {
			rest = append(rest, m)
		}
	}
	if len(hits) == 0 {
		return msgs
	}
	graph := parseMetaGraph(g.root, metafile)
	groups := map[string]*builtinGroup{}
	var order []string
	if graph != nil {
		for _, root := range graph.roots() {
			parent, via := graph.bfs(root)
			for _, h := range hits {
				key, ok := graph.abs[filepath.Clean(h.Detail.(builtinHit).importer)]
				if !ok {
					key, ok = graph.abs[canonicalPath(h.Detail.(builtinHit).importer)]
				}
				if _, reached := parent[key]; !ok || !reached {
					continue
				}
				grp := groups[root]
				if grp == nil {
					grp = &builtinGroup{}
					groups[root] = grp
					order = append(order, root)
				}
				grp.hits = append(grp.hits, h)
				chain := graph.chain(g.rel, parent, via, key)
				if d := len(chain); grp.chain == nil || d < grp.depth {
					grp.chain, grp.depth = chain, d
				}
			}
		}
	}
	if len(groups) == 0 {
		groups[""] = &builtinGroup{hits: hits}
		order = []string{""}
	}
	var out []api.Message
	for _, root := range order {
		out = append(out, g.groupMessage(groups[root]))
	}
	out = append(out, rest...)
	if g.verbose {
		out = append(out, hits...)
	}
	return out
}

// resultErrors is the build's errors for the guard's stubs that survived into
// its output, summarized per entry.
func (g *unavailableGuard) resultErrors(res api.BuildResult) []api.Message {
	return g.summarize(g.errors(res.OutputFiles), res.Metafile)
}

// groupMessage writes the summary of one entry's imports of built-ins.
func (g *unavailableGuard) groupMessage(grp *builtinGroup) api.Message {
	specs := map[string]bool{}
	files := map[string]bool{}
	units := map[string]int{}
	for _, h := range grp.hits {
		hit := h.Detail.(builtinHit)
		specs[hit.spec] = true
		files[hit.importer] = true
		unit := packageOf(hit.importer)
		if unit == "" {
			unit = g.rel(hit.importer)
		}
		units[unit]++
	}
	names := make([]string, 0, len(specs))
	for s := range specs {
		names = append(names, s)
	}
	sort.Strings(names)
	unitNames := make([]string, 0, len(units))
	for u := range units {
		unitNames = append(unitNames, u)
	}
	sort.Strings(unitNames)

	var b strings.Builder
	fmt.Fprintf(&b, "Could not resolve %s: Node built-ins do not exist in the browser", quoteList(names, 6))
	fmt.Fprintf(&b, " (%d %s in %d %s)", len(grp.hits), plural(len(grp.hits), "import", "imports"), len(files), plural(len(files), "file", "files"))
	if len(grp.chain) > 0 {
		fmt.Fprintf(&b, "\n  import chain: %s", joinChain(grp.chain))
	}
	if len(unitNames) > 1 || len(grp.chain) == 0 || strings.TrimSuffix(grp.chain[len(grp.chain)-1].label, " (package root)") != unitNames[0] {
		fmt.Fprintf(&b, "\n  imported by: %s", strings.Join(limit(unitNames, 6), ", "))
	}
	msg := api.Message{Text: b.String()}
	msg.Location = g.chainLocation(grp)
	return msg
}

// chainLocation points at the import in project code that starts the chain
// into the first package: the line to change. A built-in imported by project
// code itself points at that import.
func (g *unavailableGuard) chainLocation(grp *builtinGroup) *api.Location {
	for i, n := range grp.chain {
		if n.local != "" && i+1 < len(grp.chain) && grp.chain[i+1].local == "" {
			next := grp.chain[i+1]
			spec := next.via.Original
			if spec == "" {
				spec = strings.TrimSuffix(strings.TrimSuffix(next.label, " (package root)"), "")
			}
			return findImportLocation(filepath.Join(g.root, filepath.FromSlash(n.local)), spec)
		}
	}
	if len(grp.hits) > 0 {
		return grp.hits[0].Location
	}
	return nil
}

// findImportLocation finds the quoted specifier in a file.
func findImportLocation(file, spec string) *api.Location {
	data, err := os.ReadFile(file)
	if err != nil || spec == "" {
		return nil
	}
	src := string(data)
	for _, q := range []string{`"`, `'`} {
		i := strings.Index(src, q+spec)
		if i < 0 {
			continue
		}
		line := 1 + strings.Count(src[:i], "\n")
		lineStart := strings.LastIndex(src[:i], "\n") + 1
		lineEnd := len(src)
		if j := strings.Index(src[i:], "\n"); j >= 0 {
			lineEnd = i + j
		}
		return &api.Location{File: file, Line: line, Column: i - lineStart, LineText: strings.TrimRight(src[lineStart:lineEnd], "\r")}
	}
	return nil
}

func quoteList(names []string, max int) string {
	q := make([]string, 0, len(names))
	for _, n := range limit(names, max) {
		q = append(q, fmt.Sprintf("%q", n))
	}
	s := strings.Join(q, ", ")
	if len(names) > max {
		s += fmt.Sprintf(" and %d more", len(names)-max)
	}
	return s
}

// limit returns the first n elements.
func limit(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
