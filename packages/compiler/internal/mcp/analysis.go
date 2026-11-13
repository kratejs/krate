package mcp

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/astjson"
	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// Static source analysis shared by explain_page, why_hydrated, why_in_bundle
// and list_islands. It reads sources only (no build), follows local imports
// with the same extension and tsconfig-alias rules the bundler uses, and
// classifies each module's component tier with the documented priority:
// decorator, leading directive, file name, config lists, directories, async,
// default client.

type importRef struct {
	Spec     string   `json:"spec"`
	Kind     string   `json:"kind"` // local, package, style, asset, unresolved
	Resolved string   `json:"resolved,omitempty"`
	Line     int      `json:"line,omitempty"`
	Names    []string `json:"names,omitempty"`
}

type hydrationReason struct {
	Kind   string `json:"kind"` // hook, event-handler, island-directive, browser-api
	Name   string `json:"name"`
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Col    int    `json:"col,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type jsxUse struct {
	Tag       string
	Line      int
	Directive string // client:visible, client:idle, ...
}

type moduleInfo struct {
	Path       string            `json:"path"`
	Tier       string            `json:"tier"`
	TierSource string            `json:"tierSource"`
	UseClient  bool              `json:"useClient,omitempty"`
	UseServer  bool              `json:"useServer,omitempty"`
	Bytes      int64             `json:"bytes"`
	Imports    []importRef       `json:"imports,omitempty"`
	Reasons    []hydrationReason `json:"reasons,omitempty"`
	Exports    []string          `json:"exports,omitempty"`
	ParseError string            `json:"parseError,omitempty"`

	jsx      []jsxUse
	localOf  map[string]string // local binding -> import spec
	defaults map[string]string
}

type insightState struct {
	mu    sync.Mutex
	cache map[string]cachedModule
}

type cachedModule struct {
	size  int64
	mtime int64
	info  *moduleInfo
}

var hookNames = map[string]bool{
	"useState": true, "useReducer": true, "useEffect": true, "useLayoutEffect": true, "useInsertionEffect": true,
	"useRef": true, "useMemo": true, "useCallback": true, "useContext": true, "useId": true,
	"useSyncExternalStore": true, "useTransition": true, "useDeferredValue": true, "useImperativeHandle": true,
	"useEffectEvent": true, "useOptimistic": true, "useActionState": true, "useFormStatus": true,
	"createSignal": true, "createEffect": true, "createMemo": true, "createReducer": true,
	"createEffectDeps": true, "createMemoDeps": true, "createCallbackDeps": true,
}

var browserGlobals = map[string]bool{"window": true, "document": true, "localStorage": true, "sessionStorage": true, "navigator": true}

var sourceExts = []string{"", ".tsx", ".ts", ".jsx", ".js", ".mjs", "/index.tsx", "/index.ts", "/index.jsx", "/index.js"}

func posOf(m map[string]any) (line, col int) {
	if p, ok := m["position"].(map[string]any); ok {
		l, _ := p["line"].(float64)
		c, _ := p["col"].(float64)
		return int(l), int(c)
	}
	return 0, 0
}

func walkAST(n any, fn func(m map[string]any)) {
	switch v := n.(type) {
	case map[string]any:
		fn(v)
		for _, c := range v {
			switch c.(type) {
			case map[string]any, []any:
				walkAST(c, fn)
			}
		}
	case []any:
		for _, c := range v {
			walkAST(c, fn)
		}
	}
}

func unquoteSpec(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"' || s[0] == '`') {
		return s[1 : len(s)-1]
	}
	return s
}

func isStyleSpec(spec string) bool {
	switch strings.ToLower(path.Ext(strings.SplitN(spec, "?", 2)[0])) {
	case ".css", ".scss", ".sass", ".less", ".styl":
		return true
	}
	return false
}

func isAssetSpec(spec string) bool {
	switch strings.ToLower(path.Ext(strings.SplitN(spec, "?", 2)[0])) {
	case ".svg", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".ico", ".woff", ".woff2", ".json", ".md", ".mdx", ".txt":
		return true
	}
	return false
}

// resolveImport resolves spec as imported from file fromRel to a project file.
func (s *Service) resolveImport(fromRel, spec string) (kind, resolved string) {
	spec = strings.SplitN(spec, "?", 2)[0]
	switch {
	case isStyleSpec(spec):
		kind = "style"
	case isAssetSpec(spec):
		kind = "asset"
	}
	try := func(base string) string {
		for _, ext := range sourceExts {
			cand := base + ext
			abs, rel, err := s.resolveProjectPath(cand)
			if err != nil {
				continue
			}
			if info, err := os.Stat(abs); err == nil && !info.IsDir() {
				return rel
			}
		}
		return ""
	}
	if strings.HasPrefix(spec, ".") {
		base := path.Join(path.Dir(fromRel), spec)
		if r := try(base); r != "" {
			return orKind(kind, "local"), r
		}
		return orKind(kind, "unresolved"), ""
	}
	baseDir := s.root
	if s.cfg.TSBaseURL != "" {
		baseDir = s.cfg.TSBaseURL
	}
	for _, a := range s.cfg.PathAliases {
		prefix := strings.TrimSuffix(a.Prefix, "*")
		if prefix == "" || !strings.HasPrefix(spec, prefix) {
			continue
		}
		if a.Prefix == prefix && spec != prefix { // exact alias without wildcard
			continue
		}
		rest := strings.TrimPrefix(spec, prefix)
		for _, t := range a.Targets {
			target := strings.Replace(t, "*", rest, 1)
			abs := target
			if !filepath.IsAbs(target) {
				abs = filepath.Join(baseDir, filepath.FromSlash(target))
			}
			rel := s.relToRoot(abs)
			if rel == "" {
				continue
			}
			if r := try(rel); r != "" {
				return orKind(kind, "local"), r
			}
		}
		return orKind(kind, "unresolved"), ""
	}
	return orKind(kind, "package"), ""
}

func orKind(k, def string) string {
	if k != "" {
		return k
	}
	return def
}

// packageName reduces an import specifier to its npm package name.
func packageName(spec string) string {
	spec = strings.TrimPrefix(spec, "node:")
	parts := strings.Split(spec, "/")
	if strings.HasPrefix(spec, "@") && len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

// leadingDirective reads a `// @server`-style tier comment from the first
// comment lines of a file.
func leadingDirective(src string) string {
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "" || strings.HasPrefix(t, "\"use ") || strings.HasPrefix(t, "'use "):
			continue
		case strings.HasPrefix(t, "//"):
			switch strings.TrimSpace(strings.TrimPrefix(t, "//")) {
			case "@server":
				return "server"
			case "@static":
				return "static"
			case "@runtime":
				return "runtime"
			case "@client":
				return "client"
			}
			continue
		case strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*"):
			continue
		}
		return ""
	}
	return ""
}

func (s *Service) configTier(rel string) (tier string) {
	base := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	match := func(list []string) bool {
		for _, e := range list {
			e = strings.TrimSpace(filepath.ToSlash(e))
			if e == "" {
				continue
			}
			if e == base || e == rel || strings.HasSuffix(rel, "/"+strings.TrimPrefix(e, "./")) {
				return true
			}
		}
		return false
	}
	inDir := func(list []string) bool {
		for _, d := range list {
			d = strings.Trim(strings.TrimPrefix(filepath.ToSlash(d), "./"), "/")
			if d != "" && (rel == d || strings.HasPrefix(rel, d+"/")) {
				return true
			}
		}
		return false
	}
	switch {
	case match(s.cfg.ServerComponents):
		return "server"
	case match(s.cfg.RuntimeComponents):
		return "runtime"
	case inDir(s.cfg.ServerDirs):
		return "server"
	case inDir(s.cfg.RuntimeDirs):
		return "runtime"
	}
	return ""
}

// analyzeModule parses one project source file. Results are cached by size
// and mtime.
func (s *Service) analyzeModule(rel string) (*moduleInfo, error) {
	abs, rel, err := s.resolveProjectPath(rel)
	if err != nil {
		return nil, err
	}
	if s.policy != nil && s.policy.denied(rel) {
		return nil, &policyError{"refusing to read " + rel + ": it " + s.policy.deniedReason(rel)}
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	st := &s.ins
	st.mu.Lock()
	if c, ok := st.cache[rel]; ok && c.size == info.Size() && c.mtime == info.ModTime().UnixNano() {
		st.mu.Unlock()
		return c.info, nil
	}
	st.mu.Unlock()

	maxBytes := int64(2 << 20)
	if s.policy != nil && s.policy.MaxFileBytes > 0 {
		maxBytes = s.policy.MaxFileBytes
	}
	if info.Size() > maxBytes {
		return nil, &policyError{rel + " is larger than the read limit"}
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	mi := s.parseModule(rel, string(data))
	mi.Bytes = info.Size()
	st.mu.Lock()
	if st.cache == nil {
		st.cache = map[string]cachedModule{}
	}
	st.cache[rel] = cachedModule{size: info.Size(), mtime: info.ModTime().UnixNano(), info: mi}
	st.mu.Unlock()
	return mi, nil
}

func (s *Service) parseModule(rel, src string) *moduleInfo {
	mi := &moduleInfo{Path: rel, Tier: "client", TierSource: "default", localOf: map[string]string{}, defaults: map[string]string{}}
	ext := strings.ToLower(path.Ext(rel))
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".mts", ".cjs", ".cts":
	default:
		mi.Tier, mi.TierSource = "none", "not a script"
		return mi
	}
	// Tier decorators are invisible to the parser (the build removes them
	// first), so read the file-level tier from the source and parse a copy
	// with the decorators blanked.
	decoTier := tsstrip.ModuleTier(rel, src)
	prog, perrs, _ := astprint.Parse(tsstrip.BlankTierDecorators(rel, src))
	if len(perrs) > 0 || prog == nil {
		if len(perrs) > 0 {
			mi.ParseError = perrs[0].Error()
		}
		return mi
	}
	raw, err := astjson.EncodeProgram(prog)
	if err != nil {
		mi.ParseError = err.Error()
		return mi
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return mi
	}

	var declTiers []string
	var defaultTier string
	var asyncDefault bool
	exports := map[string]bool{}
	body, _ := doc["body"].([]any)
	for i, st := range body {
		m, _ := st.(map[string]any)
		switch m["kind"] {
		case "ExprStmt":
			if lit, ok := m["expression"].(map[string]any); ok && lit["kind"] == "Literal" {
				switch lit["value"] {
				case "use client":
					mi.UseClient = true
				case "use server":
					mi.UseServer = true
				}
			}
			_ = i
		case "ImportStmt":
			spec := unquoteSpec(str(m["source"]))
			line, _ := posOf(m)
			ref := importRef{Spec: spec, Line: line}
			if d := str(m["default"]); d != "" {
				ref.Names = append(ref.Names, d)
				mi.localOf[d] = spec
				mi.defaults[d] = spec
			}
			if ns := str(m["namespace"]); ns != "" {
				ref.Names = append(ref.Names, "* as "+ns)
				mi.localOf[ns] = spec
			}
			if named, ok := m["named"].([]any); ok {
				for _, n := range named {
					nm, _ := n.(map[string]any)
					local := str(nm["local"])
					ref.Names = append(ref.Names, local)
					mi.localOf[local] = spec
				}
			}
			ref.Kind, ref.Resolved = s.resolveImport(rel, spec)
			mi.Imports = append(mi.Imports, ref)
		case "ExportStmt":
			isDefault, _ := m["default"].(bool)
			if src := str(m["reexportSource"]); src != "" {
				spec := unquoteSpec(src)
				line, _ := posOf(m)
				ref := importRef{Spec: spec, Line: line}
				ref.Kind, ref.Resolved = s.resolveImport(rel, spec)
				mi.Imports = append(mi.Imports, ref)
			}
			if named, ok := m["named"].([]any); ok {
				for _, n := range named {
					if nm, ok := n.(map[string]any); ok {
						exports[str(nm["remote"])] = true
					}
				}
			}
			decl, _ := m["declaration"].(map[string]any)
			if decl == nil {
				continue
			}
			switch decl["kind"] {
			case "FnDecl":
				name := str(decl["name"])
				if isDefault {
					defaultTier = tierName(decl["tier"])
					asyncDefault, _ = decl["async"].(bool)
					exports["default"] = true
				} else if name != "" {
					exports[name] = true
				}
				if t := tierName(decl["tier"]); t != "" {
					declTiers = append(declTiers, t)
				}
			case "VarStmt":
				if ds, ok := decl["decls"].([]any); ok {
					for _, d := range ds {
						dm, _ := d.(map[string]any)
						if n := str(dm["name"]); n != "" {
							exports[n] = true
						}
						if t := tierName(dm["tier"]); t != "" {
							declTiers = append(declTiers, t)
						}
					}
				}
			default:
				if isDefault {
					exports["default"] = true
				}
			}
		}
	}
	mi.Exports = sortedKeys(exports)

	// Tier priority: decorator, directive, file name, config, dirs, async.
	if defaultTier == "" {
		defaultTier = decoTier
	}
	switch {
	case defaultTier != "":
		mi.Tier, mi.TierSource = defaultTier, "decorator"
	case len(declTiers) == 1 && len(mi.Exports) == 1:
		mi.Tier, mi.TierSource = declTiers[0], "decorator"
	default:
		base := path.Base(rel)
		if d := leadingDirective(src); d != "" {
			mi.Tier, mi.TierSource = d, "directive (// @"+d+")"
		} else if t := fileNameTier(base); t != "" {
			mi.Tier, mi.TierSource = t, "file name (*."+t+")"
		} else if t := s.configTier(rel); t != "" {
			mi.Tier, mi.TierSource = t, "krate.config (serverComponents/runtimeComponents or dirs)"
		} else if asyncDefault {
			mi.Tier, mi.TierSource = "runtime", "async component"
		} else if mi.UseServer {
			mi.Tier, mi.TierSource = "server", `"use server" actions module`
		}
	}

	// Hydration reasons and JSX usage.
	walkAST(doc, func(n map[string]any) {
		line, col := posOf(n)
		switch n["kind"] {
		case "CallExpr":
			callee, _ := n["callee"].(map[string]any)
			name := ""
			switch callee["kind"] {
			case "Identifier":
				name = str(callee["name"])
			case "MemberExpr":
				name = str(callee["property"])
				if p, ok := callee["property"].(map[string]any); ok {
					name = str(p["name"])
				}
			}
			if hookNames[name] {
				mi.Reasons = append(mi.Reasons, hydrationReason{Kind: "hook", Name: name, File: rel, Line: line, Col: col})
			}
		case "JSXOpening":
			tag := str(n["name"])
			l, c := line, col
			attrs, _ := n["attributes"].([]any)
			directive := ""
			lower := tag != "" && tag[0] >= 'a' && tag[0] <= 'z' && !strings.Contains(tag, ".")
			for _, a := range attrs {
				am, _ := a.(map[string]any)
				name := str(am["name"])
				al, ac := posOf(am)
				if al == 0 {
					al, ac = l, c
				}
				switch {
				case strings.HasPrefix(name, "client:"):
					directive = name
					mi.Reasons = append(mi.Reasons, hydrationReason{Kind: "island-directive", Name: name, File: rel, Line: al, Col: ac, Detail: "on <" + tag + ">"})
				case lower && len(name) > 2 && strings.HasPrefix(name, "on") && name[2] >= 'A' && name[2] <= 'Z':
					mi.Reasons = append(mi.Reasons, hydrationReason{Kind: "event-handler", Name: name, File: rel, Line: al, Col: ac, Detail: "on <" + tag + ">"})
				}
			}
			if tag != "" && (tag[0] >= 'A' && tag[0] <= 'Z' || strings.Contains(tag, ".")) {
				mi.jsx = append(mi.jsx, jsxUse{Tag: tag, Line: l, Directive: directive})
			}
		case "Identifier":
			if name := str(n["name"]); browserGlobals[name] {
				mi.Reasons = append(mi.Reasons, hydrationReason{Kind: "browser-api", Name: name, File: rel, Line: line, Col: col})
			}
		}
	})
	sort.SliceStable(mi.Reasons, func(i, j int) bool { return mi.Reasons[i].Line < mi.Reasons[j].Line })
	return mi
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func fileNameTier(base string) string {
	for _, t := range []string{"server", "runtime", "static"} {
		if strings.Contains(base, "."+t+".") {
			return t
		}
	}
	return ""
}

// interactive reports whether a module has code that needs the browser.
func (m *moduleInfo) interactive() bool {
	// A client:* directive on a child only defers that child; it does not make
	// the module that writes it hydrate.
	for _, r := range m.Reasons {
		if r.Kind == "hook" || r.Kind == "event-handler" {
			return true
		}
	}
	return false
}

// layoutChain returns the _layout files that wrap a page source, outermost
// first.
func (s *Service) layoutChain(pageRel string) []string {
	pagesRel := s.relToRoot(s.cfg.PagesDir)
	if pagesRel == "" || !strings.HasPrefix(pageRel, pagesRel+"/") {
		return nil
	}
	var out []string
	dir := pagesRel
	segs := strings.Split(path.Dir(strings.TrimPrefix(pageRel, pagesRel+"/")), "/")
	check := func(d string) {
		for _, ext := range []string{".tsx", ".jsx", ".ts", ".js"} {
			cand := path.Join(d, "_layout"+ext)
			if abs, _, err := s.resolveProjectPath(cand); err == nil {
				if _, err := os.Stat(abs); err == nil {
					out = append(out, cand)
					return
				}
			}
		}
	}
	check(dir)
	for _, seg := range segs {
		if seg == "." || seg == "" {
			continue
		}
		dir = path.Join(dir, seg)
		check(dir)
	}
	return out
}

// moduleGraph is the set of project modules reachable from a page via static
// imports, with the shortest import chain to each.
type moduleGraph struct {
	Entry     string
	Modules   map[string]*moduleInfo
	Parent    map[string]string // module -> importer on the shortest chain
	Order     []string
	Packages  map[string][]string // package name -> importing modules
	Truncated bool
}

const maxGraphModules = 400

func (s *Service) buildModuleGraph(entries []string) *moduleGraph {
	g := &moduleGraph{Modules: map[string]*moduleInfo{}, Parent: map[string]string{}, Packages: map[string][]string{}}
	if len(entries) > 0 {
		g.Entry = entries[len(entries)-1]
	}
	var queue []string
	for _, e := range entries {
		if _, seen := g.Modules[e]; seen {
			continue
		}
		mi, err := s.analyzeModule(e)
		if err != nil {
			continue
		}
		g.Modules[e] = mi
		g.Order = append(g.Order, e)
		g.Parent[e] = ""
		queue = append(queue, e)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, imp := range g.Modules[cur].Imports {
			switch imp.Kind {
			case "package":
				g.Packages[packageName(imp.Spec)] = append(g.Packages[packageName(imp.Spec)], cur)
			case "local":
				if _, seen := g.Modules[imp.Resolved]; seen {
					continue
				}
				if len(g.Modules) >= maxGraphModules {
					g.Truncated = true
					continue
				}
				mi, err := s.analyzeModule(imp.Resolved)
				if err != nil {
					continue
				}
				g.Modules[imp.Resolved] = mi
				g.Parent[imp.Resolved] = cur
				g.Order = append(g.Order, imp.Resolved)
				queue = append(queue, imp.Resolved)
			}
		}
	}
	for k := range g.Packages {
		g.Packages[k] = uniqueSorted(g.Packages[k])
	}
	return g
}

// chain returns the import chain from an entry to module (entry first).
func (g *moduleGraph) chain(module string) []string {
	var rev []string
	seen := map[string]bool{}
	for cur := module; cur != "" && !seen[cur]; cur = g.Parent[cur] {
		seen[cur] = true
		rev = append(rev, cur)
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// tierName reads a declaration's tier, ignoring `host`, which marks native
// host code and is not a rendering tier.
func tierName(v any) string {
	if t := str(v); t != "host" {
		return t
	}
	return ""
}
