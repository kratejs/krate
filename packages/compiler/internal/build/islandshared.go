package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// Preact islands and real-React islands are bundled in two esbuild graphs,
// because react resolves to preact/compat in one and to the project's React in
// the other. A module both reach that does not depend on either React (a cache,
// a store built on zustand/vanilla, an event emitter) would otherwise be copied
// into each graph and evaluated twice, so its module-level state would split.
// Such modules are bundled once into isn-* chunks that both graphs import.

const islandNeutralStem = "isn-"

// islandFlavour is the build of the island entries of one React flavour.
type islandFlavour struct {
	real   bool
	opts   api.BuildOptions
	names  []string // entry output stems
	guard  *unavailableGuard
	stem   string // shared chunk prefix
	result api.BuildResult
}

type islandMetafile struct {
	Inputs map[string]struct {
		Format  string             `json:"format"`
		Imports []islandMetaImport `json:"imports"`
	} `json:"inputs"`
	Outputs map[string]struct {
		EntryPoint string `json:"entryPoint"`
	} `json:"outputs"`
}

type islandMetaImport struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Original string `json:"original"`
	External bool   `json:"external"`
}

// metaNamespaceRe matches the namespace prefix of a metafile path of a module
// a plugin loaded ("krate-island-entry:island_x"); two or more characters, so
// a Windows drive letter is not one.
var metaNamespaceRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]+:`)

// neutralKey turns a metafile path into an absolute slash path (or keeps a
// plugin namespace path as it is).
func neutralKey(root, p string) string {
	if strings.HasPrefix(p, "(") || metaNamespaceRe.MatchString(p) {
		return p
	}
	fp := filepath.FromSlash(p)
	if !filepath.IsAbs(fp) {
		fp = filepath.Join(root, fp)
	}
	return filepath.ToSlash(filepath.Clean(fp))
}

// neutralEdgeKey keys one import of one module. Paths compare without case
// on Windows, where the drive letter's case may differ between the two.
func neutralEdgeKey(importer, spec string) string {
	if runtime.GOOS == "windows" {
		importer = strings.ToLower(importer)
	}
	return importer + "\x00" + spec
}

// neutralImporterKey is the canonical key of an OnResolve importer: esbuild
// names a file by its real path, while metafile paths are relative to the
// project root, which may be a symlink.
func neutralImporterKey(args api.OnResolveArgs) string {
	if args.Namespace == "file" || args.Namespace == "" {
		return filepath.ToSlash(canonicalPath(args.Importer))
	}
	return args.Namespace + ":" + args.Importer
}

var neutralScriptExts = map[string]bool{".js": true, ".mjs": true, ".cjs": true, ".jsx": true, ".ts": true, ".mts": true, ".cts": true, ".tsx": true}

// islandSharePlan is the set of modules both graphs import from isn-* chunks.
type islandSharePlan struct {
	shared   map[string]bool     // canonical key of every shared module
	boundary []string            // shared modules a graph imports directly, sorted
	edges    []map[string]string // per graph: importer key + "\x00" + specifier -> boundary key
	// cycles are the import cycles among the shared modules; cycleOf maps a
	// member to its cycle. Each cycle is bundled into its own entry chunk
	// (unit), and each member imported from outside its cycle gets a facade
	// chunk (facades, member -> file stem) re-exporting it from the unit.
	cycles  []islandCycle
	cycleOf map[string]int
	facades map[string]string
	// inner maps a shared module's import of a member of another cycle
	// (importer key + "\x00" + specifier) to that member.
	inner map[string]string
}

// islandShareCheck classifies the modules of two graphs. own holds, for each
// module that cannot be shared by itself, why: an import that resolves
// differently in the two graphs (react is preact/compat in one), an external
// import, a file that is not a script, a global only one graph injects. A
// module whose imports reach such a module cannot be shared either.
type islandShareCheck struct {
	root    string
	graphs  []map[string]metaNode
	own     map[string]string
	ok      map[string]bool
	globals *regexp.Regexp // names only some graphs inject (the boundary Link shim)
}

func newIslandShareCheck(root string, metas []*islandMetafile, globals []string) *islandShareCheck {
	c := &islandShareCheck{root: root, own: map[string]string{}, ok: map[string]bool{}}
	for _, m := range metas {
		c.graphs = append(c.graphs, canonicalGraph(root, m))
	}
	if len(globals) > 0 {
		quoted := make([]string, len(globals))
		for i, g := range globals {
			quoted[i] = regexp.QuoteMeta(g)
		}
		c.globals = regexp.MustCompile(`\b(` + strings.Join(quoted, "|") + `)\b`)
	}
	for p := range c.graphs[0] {
		if reason := c.ownReason(p); reason != "" {
			c.own[p] = reason
		} else {
			c.ok[p] = true
		}
	}
	return c
}

// ownReason says why module p cannot be shared on its own account, or "".
func (c *islandShareCheck) ownReason(p string) string {
	if strings.HasPrefix(p, "(") || metaNamespaceRe.MatchString(p) {
		return "generated by a build plugin"
	}
	if !neutralScriptExts[strings.ToLower(filepath.Ext(p))] {
		return "not a script module"
	}
	n := c.graphs[0][p]
	for _, g := range c.graphs[1:] {
		o, found := g[p]
		if !found {
			return "resolves differently for the two kinds of island"
		}
		if spec, differ := firstImportDifference(n.imports, o.imports); differ {
			return strconv.Quote(spec)
		}
	}
	for _, imp := range n.imports {
		if imp.External && !syntheticImport(imp) && !strings.HasPrefix(imp.Path, "./"+islandNeutralEntryPrefix) {
			return strconv.Quote(imp.Original)
		}
	}
	if c.globals != nil && !strings.Contains(p, "/node_modules/") {
		if name := freeGlobalUse(filepath.FromSlash(p), c.globals); name != "" {
			return "uses the compiler-provided " + name + ", which differs between the two kinds of island"
		}
	}
	return ""
}

// freeGlobalUse returns the first of the names that the module at path
// references as a free identifier, which an inject file then provides. A
// comment, a string, an object key, a member name (x.Link), a type and a
// name the module declares or imports at top level are not uses.
func freeGlobalUse(path string, names *regexp.Regexp) string {
	data, err := os.ReadFile(path)
	if err != nil || !names.Match(data) {
		return ""
	}
	prog, errs := tsstrip.Parse(path, string(data), tsstrip.Options{})
	if len(errs) > 0 || prog == nil {
		// Unparsable: assume the worst, as a copy per flavour is always safe.
		return string(names.Find(data))
	}
	declared := map[string]bool{}
	for _, stmt := range prog.Body {
		if ex, ok := stmt.(*ast.ExportStmt); ok && ex.Declaration != nil {
			stmt = ex.Declaration
		}
		switch d := stmt.(type) {
		case *ast.ImportStmt:
			declared[d.Default] = true
			declared[d.Namespace] = true
			for _, n := range d.Named {
				declared[n.Local] = true
			}
		case *ast.FnDecl:
			declared[d.Name] = true
		case *ast.ClassDecl:
			declared[d.Name] = true
		case *ast.VarStmt:
			for _, v := range d.Decls {
				declared[v.Name] = true
				for _, n := range v.Names {
					declared[n] = true
				}
			}
		}
	}
	skip := map[ast.Expr]bool{}
	found := ""
	walkProgramExprs(prog, func(e ast.Expr) bool {
		switch v := e.(type) {
		case *ast.MemberExpr:
			if !v.Computed {
				skip[v.Property] = true
			}
		case *ast.Identifier:
			if !skip[e] && !declared[v.Name] && names.MatchString(v.Name) && names.FindString(v.Name) == v.Name {
				found = v.Name
				return false
			}
		}
		return true
	})
	return found
}

// firstImportDifference returns the first specifier the two import lists of a
// module resolve differently. The imports esbuild adds for defines and inject
// files are left out: a graph may inject a file the other does not (the
// boundary Link shim), into every module, used or not.
func firstImportDifference(a, b []islandMetaImport) (string, bool) {
	index := func(list []islandMetaImport) ([]islandMetaImport, map[string]islandMetaImport) {
		var real []islandMetaImport
		m := map[string]islandMetaImport{}
		for _, imp := range list {
			if syntheticImport(imp) {
				continue
			}
			real = append(real, imp)
			m[imp.Kind+"\x00"+imp.Original] = imp
		}
		return real, m
	}
	ra, ma := index(a)
	rb, mb := index(b)
	for _, imp := range ra {
		if o, ok := mb[imp.Kind+"\x00"+imp.Original]; !ok || o != imp {
			return imp.Original, true
		}
	}
	for _, imp := range rb {
		if _, ok := ma[imp.Kind+"\x00"+imp.Original]; !ok {
			return imp.Original, true
		}
	}
	return "", false
}

// settle removes every module whose imports reach one that cannot be shared,
// until nothing changes. A module in an import cycle is shared with the rest
// of its cycle (see sharedCycles).
func (c *islandShareCheck) settle() {
	c.close()
}

// islandCycle is a strongly connected component of the shared modules: an
// import cycle, shared as a whole. root is the member an island enters it
// at, so the shared chunk evaluates the cycle in the order the islands did.
type islandCycle struct {
	members []string
	root    string
	unit    string // chunk file stem
}

// staticSuccessors lists the modules p imports with import statements that
// are in set.
func staticSuccessors(g map[string]metaNode, p string, set map[string]bool) []string {
	var out []string
	for _, imp := range g[p].imports {
		if imp.Kind == "import-statement" && !syntheticImport(imp) && !imp.External && set[imp.Path] {
			out = append(out, imp.Path)
		}
	}
	return out
}

// sharedCycles returns the import cycles among the shared modules (Tarjan's
// strongly connected components), in Tarjan's order: a cycle comes after
// every cycle it imports.
func sharedCycles(g map[string]metaNode, shared map[string]bool) []islandCycle {
	keys := make([]string, 0, len(shared))
	for p := range shared {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var cycles []islandCycle
	next := 0
	var strong func(v string)
	strong = func(v string) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range staticSuccessors(g, v, shared) {
			if _, seen := index[w]; !seen {
				strong(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] != index[v] {
			return
		}
		var comp []string
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			comp = append(comp, w)
			if w == v {
				break
			}
		}
		if len(comp) == 1 {
			self := false
			for _, w := range staticSuccessors(g, v, shared) {
				self = self || w == v
			}
			if !self {
				return
			}
		}
		sort.Strings(comp)
		cycles = append(cycles, islandCycle{members: comp})
	}
	for _, k := range keys {
		if _, seen := index[k]; !seen {
			strong(k)
		}
	}
	return cycles
}

// cycleRoots sets the root of each cycle: the member a depth-first walk of
// the import statements from the island entries reaches first, which is
// where ES module evaluation enters the cycle. A cycle only import() reaches
// is entered at the member the walk over every import finds first.
func cycleRoots(graphs []map[string]metaNode, entries [][]string, cycles []islandCycle) {
	of := map[string]int{}
	for i, cy := range cycles {
		for _, m := range cy.members {
			of[m] = i
		}
	}
	for _, static := range []bool{true, false} {
		for gi, g := range graphs {
			seen := map[string]bool{}
			var walk func(p string)
			walk = func(p string) {
				if seen[p] {
					return
				}
				seen[p] = true
				if i, ok := of[p]; ok && cycles[i].root == "" {
					cycles[i].root = p
				}
				for _, imp := range g[p].imports {
					if syntheticImport(imp) || imp.External || static && imp.Kind != "import-statement" {
						continue
					}
					walk(imp.Path)
				}
			}
			for _, e := range entries[gi] {
				walk(e)
			}
		}
	}
	for i := range cycles {
		if cycles[i].root == "" {
			cycles[i].root = cycles[i].members[0]
		}
	}
}

// metaEntries lists the canonical keys of a metafile's entry points, sorted.
func metaEntries(root string, m *islandMetafile) []string {
	var out []string
	for _, o := range m.Outputs {
		if o.EntryPoint != "" {
			out = append(out, canonicalMetaKey(root, o.EntryPoint))
		}
	}
	sort.Strings(out)
	return out
}

// close removes every module whose imports reach one that cannot be shared.
func (c *islandShareCheck) close() {
	for changed := true; changed; {
		changed = false
		for p := range c.ok {
			for _, imp := range c.graphs[0][p].imports {
				if !syntheticImport(imp) && !c.ok[imp.Path] {
					delete(c.ok, p)
					changed = true
					break
				}
			}
		}
	}
}

// chain is the shortest import chain from module key to the reason it cannot
// be shared, for example `src/a.ts -> src/b.ts -> "react"`, and whether that
// reason is React itself.
func (c *islandShareCheck) chain(key string) (string, bool) {
	parent := map[string]string{key: ""}
	queue := []string{key}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if reason, ok := c.own[p]; ok {
			var parts []string
			for q := p; q != ""; q = parent[q] {
				parts = append([]string{c.display(q)}, parts...)
			}
			react := false
			if spec, err := strconv.Unquote(reason); err == nil {
				pkg := packageNameOf(spec)
				react = pkg == "react" || pkg == "react-dom" || pkg == "preact"
				return strings.Join(parts, " -> ") + " -> " + reason, react
			}
			return strings.Join(parts, " -> ") + " (" + reason + ")", false
		}
		for _, imp := range c.graphs[0][p].imports {
			if syntheticImport(imp) || imp.External {
				continue
			}
			if _, seen := parent[imp.Path]; !seen {
				if _, in := c.graphs[0][imp.Path]; in {
					parent[imp.Path] = p
					queue = append(queue, imp.Path)
				}
			}
		}
	}
	return c.display(key), false
}

// display names a module for a message: project-relative, or from the last
// node_modules segment for a package file.
func (c *islandShareCheck) display(key string) string {
	if i := strings.LastIndex(key, "/node_modules/"); i >= 0 {
		return key[i+len("/node_modules/"):]
	}
	abs := filepath.FromSlash(key)
	for _, root := range []string{canonicalPath(c.root), c.root} {
		if withinDir(root, abs) {
			return canonicalRel(root, abs)
		}
	}
	// A workspace package next to the app: relative to it (../shared/x.ts).
	if rel, err := filepath.Rel(canonicalPath(c.root), abs); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(abs)
}

// planSharedIslandModules finds the modules two graphs can share: script
// modules in both graphs, whose imports resolve the same in both and are all
// shareable too. A module a graph reaches directly must be ESM and imported
// by import statements or import(), so the graph can import it from a chunk.
func planSharedIslandModules(root string, metas []*islandMetafile, globals []string) islandSharePlan {
	c := newIslandShareCheck(root, metas, globals)
	graphs := c.graphs
	for {
		c.settle()
		// A module a graph reaches from outside the shared set is imported
		// from its chunk; that needs ESM and static or dynamic imports.
		bad := false
		for _, g := range graphs {
			for p, n := range g {
				if c.ok[p] {
					continue
				}
				for _, imp := range n.imports {
					if !c.ok[imp.Path] || syntheticImport(imp) {
						continue
					}
					if graphs[0][imp.Path].format != "esm" {
						c.own[imp.Path] = "CommonJS, which cannot be loaded from a shared chunk"
					} else if imp.Kind != "import-statement" && imp.Kind != "dynamic-import" {
						c.own[imp.Path] = "loaded with require(), which cannot load a shared chunk"
					} else {
						continue
					}
					delete(c.ok, imp.Path)
					bad = true
				}
			}
		}
		if !bad {
			// A shared module imports a member of another cycle from the
			// cycle's chunk too (see buildNeutralChunks), with the same needs.
			cycles := sharedCycles(graphs[0], c.ok)
			for p, to := range crossCycleImports(graphs[0], c.ok, cycles) {
				for _, imp := range to {
					if graphs[0][imp.Path].format != "esm" {
						c.own[imp.Path] = "CommonJS in an import cycle, which cannot be loaded from a shared chunk"
					} else if imp.Kind != "import-statement" && imp.Kind != "dynamic-import" {
						c.own[imp.Path] = "in an import cycle and loaded with require() by " + c.display(p) + ", which cannot load a shared chunk"
					} else {
						continue
					}
					delete(c.ok, imp.Path)
					bad = true
				}
			}
		}
		if !bad {
			break
		}
	}
	ok := c.ok
	plan := islandSharePlan{shared: map[string]bool{}, edges: make([]map[string]string, len(graphs))}
	if len(ok) == 0 {
		return plan
	}
	for p := range ok {
		plan.shared[p] = true
	}
	boundary := map[string]bool{}
	for i, g := range graphs {
		edges := map[string]string{}
		for p, n := range g {
			if ok[p] {
				continue
			}
			for _, imp := range n.imports {
				if ok[imp.Path] && !syntheticImport(imp) {
					edges[neutralEdgeKey(p, imp.Original)] = imp.Path
					boundary[imp.Path] = true
				}
			}
		}
		plan.edges[i] = edges
	}
	for k := range boundary {
		plan.boundary = append(plan.boundary, k)
	}
	sort.Strings(plan.boundary)
	cycles := sharedCycles(graphs[0], ok)
	if len(cycles) == 0 {
		return plan
	}
	entries := make([][]string, len(metas))
	for i, m := range metas {
		entries[i] = metaEntries(root, m)
	}
	cycleRoots(graphs, entries, cycles)
	plan.cycles = cycles
	plan.cycleOf = map[string]int{}
	for i, cy := range cycles {
		plan.cycles[i].unit = cycleUnitName(root, cy.root)
		for _, m := range cy.members {
			plan.cycleOf[m] = i
		}
	}
	plan.facades = map[string]string{}
	for _, k := range plan.boundary {
		if _, in := plan.cycleOf[k]; in {
			plan.facades[k] = neutralEntryName(root, k)
		}
	}
	plan.inner = map[string]string{}
	for p, to := range crossCycleImports(graphs[0], ok, cycles) {
		for _, imp := range to {
			plan.inner[neutralEdgeKey(p, imp.Original)] = imp.Path
			plan.facades[imp.Path] = neutralEntryName(root, imp.Path)
		}
	}
	return plan
}

// crossCycleImports lists, per shared module, its imports of a member of an
// import cycle it is not in itself.
func crossCycleImports(g map[string]metaNode, shared map[string]bool, cycles []islandCycle) map[string][]islandMetaImport {
	of := map[string]int{}
	for i, cy := range cycles {
		for _, m := range cy.members {
			of[m] = i
		}
	}
	out := map[string][]islandMetaImport{}
	for p := range shared {
		pc, pin := of[p]
		for _, imp := range g[p].imports {
			if syntheticImport(imp) || imp.External || !shared[imp.Path] {
				continue
			}
			if ic, in := of[imp.Path]; in && (!pin || ic != pc) {
				out[p] = append(out[p], imp)
			}
		}
	}
	return out
}

// cycleUnitName names the chunk that holds an import cycle, after its root.
func cycleUnitName(root, cycleRoot string) string {
	rel := cycleRoot
	if r, err := filepath.Rel(canonicalPath(root), filepath.FromSlash(cycleRoot)); err == nil {
		rel = filepath.ToSlash(r)
	}
	sum := sha256.Sum256([]byte("cycle\x00" + rel))
	return islandNeutralEntryPrefix + hex.EncodeToString(sum[:6])
}

// metaNode is one module of a graph, keyed by its canonical path: the two
// graphs may reach one file by different paths (a symlinked directory, or
// macOS's /var and /private/var).
type metaNode struct {
	format  string
	imports []islandMetaImport // Path is canonical, except for externals
}

func canonicalMetaKey(root, p string) string {
	k := neutralKey(root, p)
	if strings.HasPrefix(k, "(") || metaNamespaceRe.MatchString(k) {
		return k
	}
	return filepath.ToSlash(canonicalPath(filepath.FromSlash(k)))
}

func canonicalGraph(root string, m *islandMetafile) map[string]metaNode {
	g := make(map[string]metaNode, len(m.Inputs))
	for p, in := range m.Inputs {
		imports := make([]islandMetaImport, len(in.Imports))
		for i, imp := range in.Imports {
			if !imp.External {
				imp.Path = canonicalMetaKey(root, imp.Path)
			}
			imports[i] = imp
		}
		g[canonicalMetaKey(root, p)] = metaNode{format: in.Format, imports: imports}
	}
	return g
}

// syntheticImport reports an import esbuild adds for a define or an inject
// file. Both graphs have the same ones (the imports are compared), and the
// shared build gets the same defines and the injects both use.
func syntheticImport(imp islandMetaImport) bool {
	return imp.External && imp.Original == ""
}

// neutralImports is the plugin state that sends a graph's imports of shared
// modules to their isn-* chunks. It is set between two rebuilds and only read
// while esbuild runs.
type neutralImports struct {
	root    string
	edges   map[string]string   // as in islandSharePlan.edges
	files   map[string]string   // boundary key -> chunk file name
	exports map[string][]string // boundary key -> its export names, for a stub
}

// sharedStubNamespace holds the stand-ins for shared modules: a stub
// re-exports every export of the module from its chunk, by name. Importing
// the stub instead of the chunk itself lets esbuild resolve a namespace
// import's members (z.string()) to named imports from the chunk, so the
// chunk is tree shaken to the names the islands use.
const sharedStubNamespace = "krate-island-shared-stub"

// virtualName is the path a module the build generates for a shared module is
// known by in the output (comments of an unminified build): relative to the
// project, as esbuild prints a file's path.
func virtualName(root, key string) string {
	if rel, err := filepath.Rel(canonicalPath(root), filepath.FromSlash(key)); err == nil {
		return filepath.ToSlash(rel)
	}
	return key
}

func sharedStubSource(names []string, file string) string {
	return "export { " + strings.Join(names, ", ") + " } from " + strconv.Quote("./"+file) + ";\n"
}

// sharedStubs resolves the chunk a stub imports as external and loads stubs.
func sharedStubs(pb api.PluginBuild, source func(key string) (string, bool)) {
	pb.OnResolve(api.OnResolveOptions{Filter: `.*`, Namespace: sharedStubNamespace}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
		return api.OnResolveResult{Path: args.Path, External: true}, nil
	})
	pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: sharedStubNamespace}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
		key, _ := args.PluginData.(string)
		src, ok := source(key)
		if !ok {
			return api.OnLoadResult{}, fmt.Errorf("unknown shared island module %s", args.Path)
		}
		return api.OnLoadResult{Contents: &src, Loader: api.LoaderJS}, nil
	})
}

func (n *neutralImports) plugin() api.Plugin {
	return api.Plugin{
		Name: "krate-island-shared",
		Setup: func(pb api.PluginBuild) {
			sharedStubs(pb, func(key string) (string, bool) {
				names, ok := n.exports[key]
				if !ok || n.files[key] == "" {
					return "", false
				}
				return sharedStubSource(names, n.files[key]), true
			})
			pb.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				// Only an import statement or import() can load an ES module
				// chunk; a require of the same specifier stays bundled.
				if len(n.edges) == 0 || args.Kind != api.ResolveJSImportStatement && args.Kind != api.ResolveJSDynamicImport {
					return api.OnResolveResult{}, nil
				}
				target, ok := n.edges[neutralEdgeKey(neutralImporterKey(args), args.Path)]
				if !ok {
					return api.OnResolveResult{}, nil
				}
				if file := n.files[target]; file != "" {
					if _, ok := n.exports[target]; ok {
						return api.OnResolveResult{Path: virtualName(n.root, target), Namespace: sharedStubNamespace, PluginData: target}, nil
					}
					return api.OnResolveResult{Path: "./" + file, External: true}, nil
				}
				return api.OnResolveResult{}, nil
			})
		},
	}
}

func parseIslandMetafile(s string) (*islandMetafile, error) {
	var m islandMetafile
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// buildIslandFlavours runs the island builds. With both flavours present it
// builds each once to learn its module graph, bundles the modules both can
// share into isn-* chunks (returned), and rebuilds both importing them.
func (b *Builder) buildIslandFlavours(flavours []*islandFlavour, chunksDir string) []api.OutputFile {
	if len(flavours) < 2 {
		for _, f := range flavours {
			f.result = esbuildBuild(f.opts)
		}
		return nil
	}
	var preact *islandFlavour
	for _, f := range flavours {
		if !f.real {
			preact = f
		}
	}
	neutralBase := preact.opts
	states := make([]*neutralImports, len(flavours))
	ctxs := make([]api.BuildContext, len(flavours))
	defer func() {
		for _, c := range ctxs {
			if c != nil {
				c.Dispose()
			}
		}
	}()
	for i, f := range flavours {
		states[i] = &neutralImports{root: b.Root}
		opts := f.opts
		opts.Metafile = true
		opts.Plugins = append([]api.Plugin{states[i].plugin()}, opts.Plugins...)
		tsstrip.HandleTierDecorators(&opts)
		withLoadCounter(&opts)
		ctx, cerr := api.Context(opts)
		if cerr != nil {
			f.result = api.BuildResult{Errors: cerr.Errors}
			return nil
		}
		ctxs[i] = ctx
	}
	metas := make([]*islandMetafile, len(flavours))
	rebuildAll(flavours, ctxs)
	for i, f := range flavours {
		if len(f.result.Errors) > 0 {
			return nil
		}
		m, err := parseIslandMetafile(f.result.Metafile)
		if err != nil {
			return nil
		}
		metas[i] = m
	}
	globals := flavourGlobals(flavours)
	plan := planSharedIslandModules(b.Root, metas, globals)
	if len(plan.boundary) == 0 {
		b.warnDuplicatedIslandModules(metas, globals)
		return nil
	}
	files := make(map[string]string, len(plan.boundary))
	for _, k := range plan.boundary {
		files[k] = neutralEntryName(b.Root, k) + ".js"
	}
	// The export names of every module a graph or a shared module imports
	// from a chunk, for its stub. One the build cannot list (it re-exports
	// a CommonJS module) is imported from its chunk as a whole.
	listKeys := append([]string(nil), plan.boundary...)
	for m := range plan.facades {
		if files[m] == "" {
			listKeys = append(listKeys, m)
		}
	}
	exports, _ := listModuleExports(neutralBase, listKeys, chunksDir)
	for i := range flavours {
		states[i].edges = plan.edges[i]
		states[i].files = files
		states[i].exports = exports
	}
	rebuild := func() bool {
		rebuildAll(flavours, ctxs)
		for i, f := range flavours {
			if len(f.result.Errors) > 0 {
				return false
			}
			trimSharedImports(f.result.OutputFiles)
			if m, err := parseIslandMetafile(f.result.Metafile); err == nil {
				metas[i] = m
			}
		}
		return true
	}
	unshare := func() {
		for i := range flavours {
			states[i].edges = nil
			states[i].exports = nil
		}
		rebuild()
		b.warnDuplicatedIslandModules(metas, globals)
	}
	if !rebuild() {
		// Never fail a build for sharing: bundle a copy per graph instead.
		unshare()
		return nil
	}
	var outputs [][]api.OutputFile
	for _, f := range flavours {
		outputs = append(outputs, liveOutputs(f.result.OutputFiles, f.names))
	}
	used := sharedImportsUsed(outputs, files)
	nres := b.buildNeutralChunks(neutralBase, sharedInjects(flavours), files, used, exports, plan, chunksDir)
	if len(nres.Errors) > 0 {
		// Never ship graphs that import chunks that were not built.
		unshare()
		return nil
	}
	b.warnDuplicatedIslandModules(metas, globals)
	return nres.OutputFiles
}

// rebuildAll rebuilds every flavour's context at once: the flavours share no
// build state, and each context keeps its own parse cache.
func rebuildAll(flavours []*islandFlavour, ctxs []api.BuildContext) {
	var wg sync.WaitGroup
	for i, f := range flavours {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.result = ctxs[i].Rebuild()
		}()
	}
	wg.Wait()
}

const islandNeutralEntryPrefix = "isn_"

// neutralEntryName names the chunk of a shared module after its path, so the
// graphs that import it can name it before it is built. Like an island entry,
// it is revalidated, never cached as immutable.
func neutralEntryName(root, key string) string {
	rel := key
	if r, err := filepath.Rel(canonicalPath(root), filepath.FromSlash(key)); err == nil {
		rel = filepath.ToSlash(r)
	}
	sum := sha256.Sum256([]byte(rel))
	return islandNeutralEntryPrefix + hex.EncodeToString(sum[:6])
}

// neutralUse is what the graphs import from one shared module.
type neutralUse struct {
	all   bool
	names map[string]bool
}

var (
	neutralStaticRe  = regexp.MustCompile(`(import|export)\s*([^;'"]*?)\s*from\s*["']\./(` + islandNeutralEntryPrefix + `[0-9a-f]{12})\.js["']`)
	neutralBareRe    = regexp.MustCompile(`import\s*["']\./(` + islandNeutralEntryPrefix + `[0-9a-f]{12})\.js["']`)
	neutralSpecRe    = regexp.MustCompile(`^\s*([A-Za-z_$][\w$]*|"[^"]*")(?:\s+as\s+[^,]+)?\s*$`)
	neutralDefaultRe = regexp.MustCompile(`^\s*[A-Za-z_$][\w$]*\s*$`)
)

// sharedImportsUsed reads, from the built graphs, the names each shared
// chunk must export. Anything it cannot read (a namespace import, import(),
// export *) keeps every export of the module.
func sharedImportsUsed(outputs [][]api.OutputFile, files map[string]string) map[string]*neutralUse {
	byFile := make(map[string]string, len(files))
	for k, f := range files {
		byFile[strings.TrimSuffix(f, ".js")] = k
	}
	used := map[string]*neutralUse{}
	get := func(stem string) *neutralUse {
		k, ok := byFile[stem]
		if !ok {
			return nil
		}
		u := used[k]
		if u == nil {
			u = &neutralUse{names: map[string]bool{}}
			used[k] = u
		}
		return u
	}
	for _, files := range outputs {
		for _, f := range files {
			if !strings.HasSuffix(f.Path, ".js") {
				continue
			}
			text := string(f.Contents)
			if !strings.Contains(text, islandNeutralEntryPrefix) {
				continue
			}
			counted := map[string]int{}
			// A stub imports every export of a module by name, and esbuild
			// keeps the whole clause, so an imported name is used when its
			// local is referenced outside the import statements.
			refs := neutralRefs(neutralStaticRe.ReplaceAllStringFunc(text, func(m string) string { return strings.Repeat(" ", len(m)) }))
			for _, m := range neutralStaticRe.FindAllStringSubmatch(text, -1) {
				u := get(m[3])
				if u == nil {
					continue
				}
				counted[m[3]]++
				addNeutralClause(u, m[1], m[2], refs)
			}
			for _, m := range neutralBareRe.FindAllStringSubmatch(text, -1) {
				if get(m[1]) != nil {
					counted[m[1]]++
				}
			}
			// A reference the patterns above did not account for (import())
			// needs the whole module.
			for stem := range byFile {
				if n := strings.Count(text, "./"+stem+".js"); n > counted[stem] {
					get(stem).all = true
				}
			}
		}
	}
	return used
}

func addNeutralClause(u *neutralUse, kw, clause string, refs map[string]bool) {
	clause = strings.TrimSpace(clause)
	if strings.Contains(clause, "*") {
		u.all = true
		return
	}
	// An export clause re-exports each name; an import clause needs the
	// names whose local is referenced.
	used := func(local string) bool { return kw != "import" || refs[local] }
	open, close := strings.Index(clause, "{"), strings.LastIndex(clause, "}")
	if open < 0 || close < open {
		if kw == "import" && neutralDefaultRe.MatchString(clause) {
			if used(clause) {
				u.names["default"] = true
			}
			return
		}
		u.all = true
		return
	}
	if head := strings.TrimSuffix(strings.TrimSpace(clause[:open]), ","); head != "" {
		if kw != "import" || !neutralDefaultRe.MatchString(head) {
			u.all = true
			return
		}
		if used(strings.TrimSpace(head)) {
			u.names["default"] = true
		}
	}
	for _, spec := range strings.Split(clause[open+1:close], ",") {
		if strings.TrimSpace(spec) == "" {
			continue
		}
		m := neutralSpecRe.FindStringSubmatch(spec)
		if m == nil {
			u.all = true
			return
		}
		local := m[1]
		if i := strings.Index(spec, " as "); i >= 0 {
			local = strings.TrimSpace(spec[i+len(" as "):])
		}
		if used(local) {
			u.names[m[1]] = true
		}
	}
}

// trimSharedImports drops, from each import of a shared chunk in the built
// files, the names whose local is never referenced: an import from a stub
// lists every export of the module, and a chunk exports only the names some
// file uses. The statement keeps its length (padded before "from"), so a
// source map of the file stays right.
func trimSharedImports(files []api.OutputFile) {
	for i, f := range files {
		if !strings.HasSuffix(f.Path, ".js") || !bytes.Contains(f.Contents, []byte(islandNeutralEntryPrefix)) {
			continue
		}
		text := string(f.Contents)
		refs := neutralRefs(neutralStaticRe.ReplaceAllStringFunc(text, func(m string) string { return strings.Repeat(" ", len(m)) }))
		changed := false
		out := neutralStaticRe.ReplaceAllStringFunc(text, func(stmt string) string {
			m := neutralStaticRe.FindStringSubmatch(stmt)
			if m[1] != "import" {
				return stmt
			}
			clause := strings.TrimSpace(m[2])
			open, close := strings.Index(clause, "{"), strings.LastIndex(clause, "}")
			if strings.Contains(clause, "*") || open < 0 || close < open {
				return stmt
			}
			var keep []string
			for _, spec := range strings.Split(clause[open+1:close], ",") {
				spec = strings.TrimSpace(spec)
				if spec == "" {
					continue
				}
				local := spec
				if j := strings.Index(spec, " as "); j >= 0 {
					local = strings.TrimSpace(spec[j+len(" as "):])
				}
				if refs[local] || !neutralDefaultRe.MatchString(local) {
					keep = append(keep, spec)
				}
			}
			head := strings.TrimSuffix(strings.TrimSpace(clause[:open]), ",")
			if head != "" && !refs[strings.TrimSpace(head)] {
				head = ""
			}
			lead := "import"
			if head != "" {
				lead += " " + head + ","
			}
			next := lead + "{" + strings.Join(keep, ",") + "}"
			rest := stmt[strings.LastIndex(stmt, "from"):]
			if len(next)+len(rest) > len(stmt) {
				return stmt
			}
			changed = true
			return next + strings.Repeat(" ", len(stmt)-len(next)-len(rest)) + rest
		})
		if changed {
			files[i].Contents = []byte(out)
		}
	}
}

// neutralRefs returns the identifiers in JavaScript text that are not
// property names (after a "."; a spread "..." is a reference). Strings and
// comments are read as code, which can only add names.
func neutralRefs(text string) map[string]bool {
	refs := map[string]bool{}
	isPart := func(c byte) bool {
		return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
	}
	for i := 0; i < len(text); {
		c := text[i]
		if !isPart(c) {
			i++
			continue
		}
		j := i
		for j < len(text) && isPart(text[j]) {
			j++
		}
		if c < '0' || c > '9' {
			property := i > 0 && text[i-1] == '.' && !(i >= 3 && text[i-3:i] == "...")
			if !property {
				refs[text[i:j]] = true
			}
		}
		i = j
	}
	return refs
}

const neutralEntryNamespace = "krate-island-shared-entry"

// buildNeutralChunks bundles the shared modules once. Each module the graphs
// import gets an entry named for it that re-exports only the names they use,
// so a large library both reach is still tree shaken.
//
// Code splitting orders the modules of a shared chunk by their distance from
// the entry points, so a chunk several entries reach could run an import
// cycle from another member than the islands did, and a member would read a
// binding of one that has not run yet. So each cycle is bundled into an entry
// chunk of its own (the unit), which imports the cycle's root first: esbuild
// then runs the cycle from the root, as the islands do. Every other module,
// an island or a shared module, imports a member through the member's facade
// chunk, which re-exports it from the unit. The names a facade needs from
// shared modules are read from the build's own output, so it is rebuilt until
// they settle.
func (b *Builder) buildNeutralChunks(base api.BuildOptions, injects []string, files map[string]string, used map[string]*neutralUse, exports map[string][]string, plan islandSharePlan, chunksDir string) api.BuildResult {
	need := map[string]*neutralUse{}
	for k, u := range used {
		need[k] = u.clone()
	}
	facadeFiles := map[string]string{}
	for m, stem := range plan.facades {
		facadeFiles[m] = stem + ".js"
	}
	for pass := 0; ; pass++ {
		result := b.buildNeutralOnce(base, injects, files, need, exports, plan, chunksDir)
		if len(result.Errors) > 0 || len(plan.cycles) == 0 {
			return result
		}
		inner := sharedImportsUsed([][]api.OutputFile{result.OutputFiles}, facadeFiles)
		grew := false
		for m, u := range inner {
			if need[m] == nil {
				need[m] = &neutralUse{names: map[string]bool{}}
			}
			grew = need[m].merge(u) || grew
		}
		if !grew {
			return result
		}
		if pass == 8 {
			return api.BuildResult{Errors: []api.Message{{Text: "the shared island chunks did not settle"}}}
		}
	}
}

func (u *neutralUse) clone() *neutralUse {
	c := &neutralUse{all: u.all, names: make(map[string]bool, len(u.names))}
	for n := range u.names {
		c.names[n] = true
	}
	return c
}

// merge adds o to u and reports whether u grew.
func (u *neutralUse) merge(o *neutralUse) bool {
	grew := false
	if o.all && !u.all {
		u.all, grew = true, true
	}
	for n := range o.names {
		if !u.names[n] {
			u.names[n], grew = true, true
		}
	}
	return grew
}

func (u *neutralUse) sortedNames() []string {
	names := make([]string, 0, len(u.names))
	for n := range u.names {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (b *Builder) buildNeutralOnce(base api.BuildOptions, injects []string, files map[string]string, need map[string]*neutralUse, exports map[string][]string, plan islandSharePlan, chunksDir string) api.BuildResult {
	sources := map[string]string{} // entry path -> source
	dirs := map[string]string{}    // entry path -> the directory it resolves from
	var points []api.EntryPoint
	keys := make([]string, 0, len(files))
	for k := range files {
		if _, in := plan.cycleOf[k]; !in {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		u := need[k]
		if u == nil {
			continue
		}
		spec := strconv.Quote(k)
		var src string
		switch {
		case u.all:
			src = "export * from " + spec + ";\n"
			if moduleHasDefaultExport(k) {
				src += "export { default } from " + spec + ";\n"
			}
		case len(u.names) == 0:
			src = "import " + spec + ";\n"
		default:
			src = "export { " + strings.Join(u.sortedNames(), ", ") + " } from " + spec + ";\n"
		}
		id := neutralEntryNamespace + ":" + virtualName(b.Root, k)
		sources[id], dirs[id] = src, k
		points = append(points, api.EntryPoint{InputPath: id, OutputPath: strings.TrimSuffix(files[k], ".js")})
	}
	// The units and the facades of the members other modules import.
	for ci, cy := range plan.cycles {
		var unit strings.Builder
		fmt.Fprintf(&unit, "import * as __krate_cycle_root from %s;\n", strconv.Quote(cy.root))
		used := false
		for mi, m := range cy.members {
			stem, imported := plan.facades[m]
			u := need[m]
			if !imported || u == nil {
				continue
			}
			used = true
			names := u.sortedNames()
			if u.all {
				listed, ok := exports[m]
				if !ok {
					return api.BuildResult{Errors: []api.Message{{Text: "cannot list the exports of " + m + ", which a shared chunk re-exports"}}}
				}
				names = listed
			}
			id := neutralEntryNamespace + ":facade:" + virtualName(b.Root, m)
			if len(names) == 0 {
				sources[id] = "import \"./" + cy.unit + ".js\";\n"
			} else {
				var inner, outer []string
				for ni, n := range names {
					alias := fmt.Sprintf("__krate_c%d_%d_%d", ci, mi, ni)
					inner = append(inner, n+" as "+alias)
					outer = append(outer, alias+" as "+n)
				}
				fmt.Fprintf(&unit, "export { %s } from %s;\n", strings.Join(inner, ", "), strconv.Quote(m))
				sources[id] = "export { " + strings.Join(outer, ", ") + " } from \"./" + cy.unit + ".js\";\n"
			}
			dirs[id] = m
			points = append(points, api.EntryPoint{InputPath: id, OutputPath: stem})
		}
		if used {
			id := neutralEntryNamespace + ":unit:" + virtualName(b.Root, cy.root)
			sources[id], dirs[id] = unit.String(), cy.root
			points = append(points, api.EntryPoint{InputPath: id, OutputPath: cy.unit})
		}
	}
	if len(points) == 0 {
		return api.BuildResult{}
	}
	opts := base
	opts.EntryPoints = nil
	opts.EntryPointsAdvanced = points
	opts.Outdir = chunksDir
	opts.EntryNames = "[name]"
	opts.ChunkNames = islandNeutralStem + "[hash]"
	opts.Splitting = true
	opts.Metafile = false
	opts.Inject = injects
	entry := api.Plugin{
		Name: neutralEntryNamespace,
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^` + neutralEntryNamespace + `:`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: strings.TrimPrefix(args.Path, neutralEntryNamespace+":"), Namespace: neutralEntryNamespace}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: neutralEntryNamespace}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				src, ok := sources[neutralEntryNamespace+":"+args.Path]
				if !ok {
					return api.OnLoadResult{}, fmt.Errorf("unknown shared island module %s", args.Path)
				}
				return api.OnLoadResult{Contents: &src, Loader: api.LoaderJS, ResolveDir: filepath.Dir(filepath.FromSlash(dirs[neutralEntryNamespace+":"+args.Path]))}, nil
			})
		},
	}
	plugins := []api.Plugin{entry}
	if len(plan.cycles) > 0 {
		// A facade's unit, and a shared module's import of a member of
		// another cycle, load the other chunk at run time.
		plugins = append(plugins, api.Plugin{
			Name: "krate-island-shared-cycles",
			Setup: func(pb api.PluginBuild) {
				pb.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
					if args.Namespace == neutralEntryNamespace && strings.HasPrefix(args.Path, "./"+islandNeutralEntryPrefix) {
						return api.OnResolveResult{Path: args.Path, External: true}, nil
					}
					if args.Kind != api.ResolveJSImportStatement && args.Kind != api.ResolveJSDynamicImport {
						return api.OnResolveResult{}, nil
					}
					if m, ok := plan.inner[neutralEdgeKey(neutralImporterKey(args), args.Path)]; ok {
						if _, listed := exports[m]; listed {
							return api.OnResolveResult{Path: virtualName(b.Root, m), Namespace: sharedStubNamespace, PluginData: m}, nil
						}
						return api.OnResolveResult{Path: "./" + plan.facades[m] + ".js", External: true}, nil
					}
					return api.OnResolveResult{}, nil
				})
				sharedStubs(pb, func(key string) (string, bool) {
					names, ok := exports[key]
					if !ok || plan.facades[key] == "" {
						return "", false
					}
					return sharedStubSource(names, plan.facades[key]+".js"), true
				})
			},
		})
	}
	opts.Plugins = append(plugins, base.Plugins...)
	result := esbuildBuild(opts)
	if len(result.Errors) == 0 {
		names := make([]string, len(points))
		for i, p := range points {
			names[i] = p.OutputPath
		}
		result.OutputFiles = liveOutputs(result.OutputFiles, names)
		trimSharedImports(result.OutputFiles)
	}
	return result
}

// listModuleExports lists the names each module exports at run time, from a
// build of `export * from` each, for a facade that must re-export them all.
func listModuleExports(base api.BuildOptions, keys []string, outdir string) (map[string][]string, []api.Message) {
	sort.Strings(keys)
	const ns = "krate-island-shared-exports"
	var points []api.EntryPoint
	for i, k := range keys {
		points = append(points, api.EntryPoint{InputPath: ns + ":" + k, OutputPath: "x" + strconv.Itoa(i)})
	}
	opts := base
	opts.EntryPoints = nil
	opts.EntryPointsAdvanced = points
	opts.Outdir = filepath.Join(outdir, "krate-exports")
	opts.EntryNames = "[name]"
	opts.Splitting = true
	opts.Metafile = true
	opts.Write = false
	opts.MinifyIdentifiers, opts.MinifySyntax, opts.MinifyWhitespace = false, false, false
	opts.Sourcemap = api.SourceMapNone
	opts.Inject = nil
	opts.Plugins = append([]api.Plugin{{
		Name: ns,
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^` + ns + `:`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: strings.TrimPrefix(args.Path, ns+":"), Namespace: ns}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: ns}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				spec := strconv.Quote(args.Path)
				src := "export * from " + spec + ";\n"
				if moduleHasDefaultExport(args.Path) {
					src += "export { default } from " + spec + ";\n"
				}
				return api.OnLoadResult{Contents: &src, Loader: api.LoaderJS, ResolveDir: filepath.Dir(filepath.FromSlash(args.Path))}, nil
			})
		},
	}}, base.Plugins...)
	res := esbuildBuild(opts)
	if len(res.Errors) > 0 {
		return nil, res.Errors
	}
	var meta struct {
		Outputs map[string]struct {
			EntryPoint string   `json:"entryPoint"`
			Exports    []string `json:"exports"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal([]byte(res.Metafile), &meta); err != nil {
		return nil, []api.Message{{Text: "reading the exports of shared island modules: " + err.Error()}}
	}
	// An export * of a CommonJS module is resolved at run time, so its names
	// are not listed: an entry whose code (or a chunk it imports) does that is
	// left out.
	texts := map[string]string{}
	for _, f := range res.OutputFiles {
		texts[filepath.Base(f.Path)] = string(f.Contents)
	}
	dynamic := func(entry string) bool {
		seen := map[string]bool{}
		queue := []string{entry}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if seen[cur] {
				continue
			}
			seen[cur] = true
			text := texts[cur]
			if strings.Contains(text, "__reExport") {
				return true
			}
			for base := range texts {
				if !seen[base] && strings.Contains(text, "./"+base) {
					queue = append(queue, base)
				}
			}
		}
		return false
	}
	out := map[string][]string{}
	for path, o := range meta.Outputs {
		if k := strings.TrimPrefix(o.EntryPoint, ns+":"); k != o.EntryPoint {
			if dynamic(filepath.Base(path)) {
				continue
			}
			names := make([]string, 0, len(o.Exports))
			for _, n := range o.Exports {
				if !neutralDefaultRe.MatchString(n) && n != "default" {
					n = strconv.Quote(n)
				}
				names = append(names, n)
			}
			sort.Strings(names)
			out[k] = names
		}
	}
	return out, nil
}

// moduleHasDefaultExport reports whether a script module has an export
// default (export * does not re-export it).
func moduleHasDefaultExport(path string) bool {
	data, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		return false
	}
	prog, errs := tsstrip.Parse(path, string(data), tsstrip.Options{})
	if len(errs) > 0 || prog == nil {
		return strings.Contains(string(data), "export default")
	}
	for _, stmt := range prog.Body {
		ex, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		if ex.Default {
			return true
		}
		for _, list := range [][]ast.NamedExport{ex.Named, ex.ReexportNamed} {
			for _, n := range list {
				if n.Remote == "default" {
					return true
				}
			}
		}
	}
	return false
}

var injectExportRe = regexp.MustCompile(`export\s+(?:async\s+)?(?:function\*?|const|let|var|class)\s+([A-Za-z_$][\w$]*)|export\s*\{([^}]*)\}`)

// flavourGlobals lists the names an inject file of only some flavours
// provides (the boundary shim's Link): a module that uses one is bundled per
// flavour, since the name means something different in each.
func flavourGlobals(flavours []*islandFlavour) []string {
	shared := map[string]bool{}
	for _, p := range sharedInjects(flavours) {
		shared[p] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range flavours {
		for _, p := range f.opts.Inject {
			if shared[p] || seen[p] {
				continue
			}
			seen[p] = true
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			for _, m := range injectExportRe.FindAllStringSubmatch(string(data), -1) {
				if m[1] != "" {
					out = append(out, m[1])
					continue
				}
				for _, spec := range strings.Split(m[2], ",") {
					fields := strings.Fields(spec)
					if len(fields) > 0 {
						out = append(out, fields[len(fields)-1])
					}
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// sharedInjects returns the inject files every flavour uses (the context-key
// runtime); a flavour-specific one is never needed by a shared module.
func sharedInjects(flavours []*islandFlavour) []string {
	var out []string
	for _, p := range flavours[0].opts.Inject {
		all := true
		for _, f := range flavours[1:] {
			found := false
			for _, q := range f.opts.Inject {
				if q == p {
					found = true
					break
				}
			}
			all = all && found
		}
		if all {
			out = append(out, p)
		}
	}
	return out
}

// warnDuplicatedIslandModules reports each project module that both graphs
// still bundle (its imports reach React, or something else that cannot be
// shared) and that keeps module-level state, which the two copies do not
// share. The message names the import chain.
func (b *Builder) warnDuplicatedIslandModules(metas []*islandMetafile, globals []string) {
	if len(metas) < 2 {
		return
	}
	c := newIslandShareCheck(b.Root, metas, globals)
	c.settle()
	var ds []diag.Diagnostic
	var paths []string
	root, out := canonicalPath(b.Root), canonicalPath(b.Cfg.OutDir)
	for key := range c.graphs[0] {
		if strings.HasPrefix(key, "(") || metaNamespaceRe.MatchString(key) || !neutralScriptExts[strings.ToLower(filepath.Ext(key))] {
			continue
		}
		both := true
		for _, g := range c.graphs[1:] {
			if _, ok := g[key]; !ok {
				both = false
				break
			}
		}
		abs := filepath.FromSlash(key)
		// Generated files (the context-key runtime) keep one copy per
		// flavour on purpose.
		if !both || strings.Contains(key, "/node_modules/") || !withinDir(root, abs) || withinDir(out, abs) {
			continue
		}
		paths = append(paths, abs)
	}
	sort.Strings(paths)
	for _, abs := range paths {
		pos, what, ok := moduleStateAt(abs)
		if !ok {
			continue
		}
		chain, react := c.chain(filepath.ToSlash(abs))
		because := "its imports reach code that cannot be shared: " + chain
		if react {
			because = "its imports reach React: " + chain
		}
		ds = append(ds, diag.Diagnostic{
			File:     abs,
			Line:     pos.Line,
			Col:      pos.Col,
			Severity: diag.SeverityWarning,
			Code:     diag.CodeIslandModuleDuplicated,
			Message: fmt.Sprintf("%s is bundled twice, once for the Preact islands and once for the real-React islands, because %s; %s here is not shared between the two copies",
				canonicalRel(root, abs), because, what),
			Hint: "keep the state in a module whose imports do not reach React: Krate bundles such a module once for both kinds of island",
		})
	}
	if len(ds) > 0 {
		printWarningsOnce(os.Stderr, ds)
	}
}

func canonicalRel(root, abs string) string {
	if rel, err := filepath.Rel(root, abs); err == nil && withinDir(root, abs) {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(abs)
}

// storeFactories are calls whose result is state a module keeps for every
// importer (a zustand or jotai store, an event emitter).
var storeFactories = map[string]bool{
	"create": true, "createStore": true, "createWithEqualityFn": true, "atom": true, "atomWithStorage": true,
	"proxy": true, "mitt": true, "createNanoEvents": true, "writable": true, "signal": true, "observable": true,
}

// moduleStateAt finds the first top-level declaration of mutable module state:
// a let or var, a const holding a new object instance, or a store factory call.
func moduleStateAt(path string) (ast.Pos, string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ast.Pos{}, "", false
	}
	prog, errs := tsstrip.Parse(path, string(data), tsstrip.Options{})
	if len(errs) > 0 || prog == nil {
		return ast.Pos{}, "", false
	}
	for _, stmt := range prog.Body {
		if ex, ok := stmt.(*ast.ExportStmt); ok && ex.Declaration != nil {
			stmt = ex.Declaration
		}
		vs, ok := stmt.(*ast.VarStmt)
		if !ok || len(vs.Decls) == 0 {
			continue
		}
		switch vs.Kind {
		case ast.VarLet, ast.VarVar:
			return vs.Position, "the module-level variable declared", true
		case ast.VarConst:
			for _, d := range vs.Decls {
				switch init := d.Init.(type) {
				case *ast.NewExpr:
					return vs.Position, "the object created", true
				case *ast.CallExpr:
					if storeFactories[storeCalleeName(init.Callee)] {
						return vs.Position, "the store created", true
					}
				}
			}
		}
	}
	return ast.Pos{}, "", false
}

func storeCalleeName(e ast.Expr) string {
	switch c := e.(type) {
	case *ast.Identifier:
		return c.Name
	case *ast.MemberExpr:
		if id, ok := c.Property.(*ast.Identifier); ok && !c.Computed {
			return id.Name
		}
	case *ast.CallExpr:
		// create<T>()(...) curries the factory.
		return storeCalleeName(c.Callee)
	}
	return ""
}
