package build

import (
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

// neutralKey turns a metafile input path into the key the shared-module
// plugin computes from an OnResolve importer.
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

func neutralImporterKey(args api.OnResolveArgs) string {
	if args.Namespace == "file" || args.Namespace == "" {
		return filepath.ToSlash(filepath.Clean(args.Importer))
	}
	return args.Namespace + ":" + args.Importer
}

var neutralScriptExts = map[string]bool{".js": true, ".mjs": true, ".cjs": true, ".jsx": true, ".ts": true, ".mts": true, ".cts": true, ".tsx": true}

// islandSharePlan is the set of modules both graphs import from isn-* chunks.
type islandSharePlan struct {
	shared   map[string]bool     // neutralKey of every shared module
	boundary []string            // shared modules a graph imports directly, sorted
	edges    []map[string]string // per graph: importer key + "\x00" + specifier -> boundary key
}

// planSharedIslandModules finds the modules two graphs can share: script
// modules in both graphs, with the same imports in both, whose imports are all
// shareable too. A module a graph reaches directly must be ESM and imported
// by import statements or import(), so the graph can import it from a chunk.
func planSharedIslandModules(root string, metas []*islandMetafile) islandSharePlan {
	type node struct {
		format  string
		imports []islandMetaImport
	}
	graphs := make([]map[string]node, len(metas))
	for i, m := range metas {
		g := map[string]node{}
		for p, in := range m.Inputs {
			g[p] = node{format: in.Format, imports: in.Imports}
		}
		graphs[i] = g
	}
	ok := map[string]bool{}
	for p, n := range graphs[0] {
		if strings.HasPrefix(p, "(") || metaNamespaceRe.MatchString(p) || !neutralScriptExts[strings.ToLower(filepath.Ext(p))] {
			continue
		}
		same := true
		for _, g := range graphs[1:] {
			o, found := g[p]
			if !found || !sameMetaImports(n.imports, o.imports) {
				same = false
				break
			}
		}
		if same {
			ok[p] = true
		}
	}
	for {
		for changed := true; changed; {
			changed = false
			for p := range ok {
				for _, imp := range graphs[0][p].imports {
					if syntheticImport(imp) {
						continue
					}
					if imp.External || !ok[imp.Path] {
						delete(ok, p)
						changed = true
						break
					}
				}
			}
		}
		// A module a graph reaches from outside the shared set is imported
		// from its chunk; that needs ESM and static or dynamic imports.
		bad := false
		for _, g := range graphs {
			for p, n := range g {
				if ok[p] {
					continue
				}
				for _, imp := range n.imports {
					if !ok[imp.Path] {
						continue
					}
					if graphs[0][imp.Path].format != "esm" || imp.Kind != "import-statement" && imp.Kind != "dynamic-import" {
						delete(ok, imp.Path)
						bad = true
					}
				}
			}
		}
		if !bad {
			break
		}
	}
	plan := islandSharePlan{shared: map[string]bool{}, edges: make([]map[string]string, len(graphs))}
	if len(ok) == 0 {
		return plan
	}
	for p := range ok {
		plan.shared[neutralKey(root, p)] = true
	}
	boundary := map[string]bool{}
	for i, g := range graphs {
		edges := map[string]string{}
		for p, n := range g {
			if ok[p] {
				continue
			}
			for _, imp := range n.imports {
				if ok[imp.Path] {
					key := neutralKey(root, imp.Path)
					edges[neutralEdgeKey(neutralKey(root, p), imp.Original)] = key
					boundary[key] = true
				}
			}
		}
		plan.edges[i] = edges
	}
	for k := range boundary {
		plan.boundary = append(plan.boundary, k)
	}
	sort.Strings(plan.boundary)
	return plan
}

// syntheticImport reports an import esbuild adds for a define or an inject
// file. Both graphs have the same ones (the imports are compared), and the
// shared build gets the same defines and the injects both use.
func syntheticImport(imp islandMetaImport) bool {
	return imp.External && imp.Original == ""
}

func sameMetaImports(a, b []islandMetaImport) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// neutralImports is the plugin state that sends a graph's imports of shared
// modules to their isn-* chunks. It is set between two rebuilds and only read
// while esbuild runs.
type neutralImports struct {
	edges map[string]string // as in islandSharePlan.edges
	files map[string]string // boundary key -> chunk file name
}

func (n *neutralImports) plugin() api.Plugin {
	return api.Plugin{
		Name: "krate-island-shared",
		Setup: func(pb api.PluginBuild) {
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
		states[i] = &neutralImports{}
		opts := f.opts
		opts.Metafile = true
		opts.Plugins = append([]api.Plugin{states[i].plugin()}, opts.Plugins...)
		tsstrip.HandleTierDecorators(&opts)
		ctx, cerr := api.Context(opts)
		if cerr != nil {
			f.result = api.BuildResult{Errors: cerr.Errors}
			return nil
		}
		ctxs[i] = ctx
	}
	metas := make([]*islandMetafile, len(flavours))
	for i, f := range flavours {
		f.result = ctxs[i].Rebuild()
		if len(f.result.Errors) > 0 {
			return nil
		}
		m, err := parseIslandMetafile(f.result.Metafile)
		if err != nil {
			return nil
		}
		metas[i] = m
	}
	plan := planSharedIslandModules(b.Root, metas)
	if len(plan.boundary) == 0 {
		b.warnDuplicatedIslandModules(metas)
		return nil
	}
	files := make(map[string]string, len(plan.boundary))
	for _, k := range plan.boundary {
		files[k] = neutralEntryName(b.Root, k) + ".js"
	}
	for i := range flavours {
		states[i].edges = plan.edges[i]
		states[i].files = files
	}
	rebuild := func() bool {
		for i, f := range flavours {
			f.result = ctxs[i].Rebuild()
			if len(f.result.Errors) > 0 {
				return false
			}
			if m, err := parseIslandMetafile(f.result.Metafile); err == nil {
				metas[i] = m
			}
		}
		return true
	}
	unshare := func() {
		for i := range flavours {
			states[i].edges = nil
		}
		rebuild()
		b.warnDuplicatedIslandModules(metas)
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
	nres := b.buildNeutralChunks(neutralBase, sharedInjects(flavours), files, used, chunksDir)
	if len(nres.Errors) > 0 {
		// Never ship graphs that import chunks that were not built.
		unshare()
		return nil
	}
	b.warnDuplicatedIslandModules(metas)
	return nres.OutputFiles
}

const islandNeutralEntryPrefix = "isn_"

// neutralEntryName names the chunk of a shared module after its path, so the
// graphs that import it can name it before it is built. Like an island entry,
// it is revalidated, never cached as immutable.
func neutralEntryName(root, key string) string {
	rel := key
	if r, err := filepath.Rel(root, filepath.FromSlash(key)); err == nil {
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
			for _, m := range neutralStaticRe.FindAllStringSubmatch(text, -1) {
				u := get(m[3])
				if u == nil {
					continue
				}
				counted[m[3]]++
				addNeutralClause(u, m[1], m[2])
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

func addNeutralClause(u *neutralUse, kw, clause string) {
	clause = strings.TrimSpace(clause)
	if strings.Contains(clause, "*") {
		u.all = true
		return
	}
	open, close := strings.Index(clause, "{"), strings.LastIndex(clause, "}")
	if open < 0 || close < open {
		if kw == "import" && neutralDefaultRe.MatchString(clause) {
			u.names["default"] = true
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
		u.names["default"] = true
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
		u.names[m[1]] = true
	}
}

const neutralEntryNamespace = "krate-island-shared-entry"

// buildNeutralChunks bundles the shared modules once. Each module the graphs
// import gets an entry named for it that re-exports only the names they use,
// so a large library both reach is still tree shaken.
func (b *Builder) buildNeutralChunks(base api.BuildOptions, injects []string, files map[string]string, used map[string]*neutralUse, chunksDir string) api.BuildResult {
	sources := map[string]string{}
	var points []api.EntryPoint
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		u := used[k]
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
			names := make([]string, 0, len(u.names))
			for n := range u.names {
				names = append(names, n)
			}
			sort.Strings(names)
			src = "export { " + strings.Join(names, ", ") + " } from " + spec + ";\n"
		}
		sources[k] = src
		points = append(points, api.EntryPoint{InputPath: neutralEntryNamespace + ":" + k, OutputPath: strings.TrimSuffix(files[k], ".js")})
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
				src, ok := sources[args.Path]
				if !ok {
					return api.OnLoadResult{}, fmt.Errorf("unknown shared island module %s", args.Path)
				}
				return api.OnLoadResult{Contents: &src, Loader: api.LoaderJS, ResolveDir: filepath.Dir(filepath.FromSlash(args.Path))}, nil
			})
		},
	}
	opts.Plugins = append([]api.Plugin{entry}, base.Plugins...)
	result := esbuildBuild(opts)
	if len(result.Errors) == 0 {
		names := make([]string, len(points))
		for i, p := range points {
			names[i] = p.OutputPath
		}
		result.OutputFiles = liveOutputs(result.OutputFiles, names)
	}
	return result
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
// still bundle (it depends on React, so each flavour needs its own copy) and
// that keeps module-level state, which the two copies do not share.
func (b *Builder) warnDuplicatedIslandModules(metas []*islandMetafile) {
	if len(metas) < 2 {
		return
	}
	var ds []diag.Diagnostic
	var paths []string
	for p := range metas[0].Inputs {
		if strings.HasPrefix(p, "(") || metaNamespaceRe.MatchString(p) || !neutralScriptExts[strings.ToLower(filepath.Ext(p))] {
			continue
		}
		both := true
		for _, m := range metas[1:] {
			if _, ok := m.Inputs[p]; !ok {
				both = false
				break
			}
		}
		if !both {
			continue
		}
		abs := filepath.FromSlash(neutralKey(b.Root, p))
		// Generated files (the context-key runtime) keep one copy per
		// flavour on purpose.
		if strings.Contains(filepath.ToSlash(abs), "/node_modules/") || !withinDir(canonicalPath(b.Root), canonicalPath(abs)) ||
			withinDir(canonicalPath(b.Cfg.OutDir), canonicalPath(abs)) {
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
		reason := duplicateReason(b.Root, metas, abs)
		ds = append(ds, diag.Diagnostic{
			File:     abs,
			Line:     pos.Line,
			Col:      pos.Col,
			Severity: diag.SeverityWarning,
			Code:     diag.CodeIslandModuleDuplicated,
			Message: fmt.Sprintf("%s is bundled twice, once for the Preact islands and once for the real-React islands, because %s; %s here is not shared between the two copies",
				b.relPath(abs), reason, what),
			Hint: "keep the state in a module that does not import React (directly or through its imports): Krate bundles such a module once for both kinds of island",
		})
	}
	if len(ds) > 0 {
		printWarningsOnce(os.Stderr, ds)
	}
}

// duplicateReason names the first import of a module that keeps it out of
// the shared chunks.
func duplicateReason(root string, metas []*islandMetafile, abs string) string {
	key := filepath.ToSlash(abs)
	var imports [][]islandMetaImport
	for _, m := range metas {
		for p, in := range m.Inputs {
			if neutralKey(root, p) == key {
				imports = append(imports, in.Imports)
				break
			}
		}
	}
	if len(imports) < 2 {
		return "it depends on React"
	}
	for i := range imports[0] {
		if i >= len(imports[1]) || imports[0][i] != imports[1][i] {
			spec := imports[0][i].Original
			if spec == "" {
				break
			}
			if pkg := packageNameOf(spec); pkg == "react" || pkg == "react-dom" || strings.HasPrefix(spec, "react/") {
				return fmt.Sprintf("it imports %q", spec)
			}
			return fmt.Sprintf("its import %q resolves differently for each (it depends on React)", spec)
		}
	}
	for _, imp := range imports[0] {
		if imp.External && !syntheticImport(imp) {
			return fmt.Sprintf("it imports %q", imp.Original)
		}
	}
	if len(imports[0]) > 0 {
		return fmt.Sprintf("an import of it (%q, directly or further down) depends on React", imports[0][0].Original)
	}
	return "it depends on React"
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
