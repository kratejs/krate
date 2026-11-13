package hostts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native/runtimes"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// islandRule is one entry of native.host.islands.
type islandRule struct {
	key  string
	rt   runtimes.Runtime
	pkg  bool           // a package name; otherwise a path or glob relative to the root
	re   *regexp.Regexp // path rules
	used bool
}

var scriptExts = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts", ".go"}

// isPackageKey tells a package name ("sharp", "@scope/name") from a path.
func isPackageKey(k string) bool {
	if k == "" || strings.HasPrefix(k, ".") || strings.HasPrefix(k, "/") || strings.ContainsAny(k, "*?[\\") {
		return false
	}
	for _, e := range scriptExts {
		if strings.HasSuffix(k, e) {
			return false
		}
	}
	if strings.HasPrefix(k, "@") {
		return strings.Count(k, "/") == 1
	}
	return !strings.Contains(k, "/")
}

func parseIslands(m map[string]string) []*islandRule {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []*islandRule
	for _, k := range keys {
		r := &islandRule{key: k, rt: runtimes.Runtime(m[k])}
		if isPackageKey(k) {
			r.pkg = true
		} else {
			r.re = globRegexp(strings.TrimPrefix(filepath.ToSlash(k), "./"))
		}
		out = append(out, r)
	}
	return out
}

// globRegexp compiles a path pattern: ** matches any characters including
// slashes, * and ? stay inside a segment. A pattern without wildcards matches
// that file or everything below that directory.
func globRegexp(p string) *regexp.Regexp {
	p = strings.TrimSuffix(p, "/")
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(p); i++ {
		switch c := p[i]; {
		case c == '*' && i+1 < len(p) && p[i+1] == '*':
			sb.WriteString(".*")
			i++
			if i+1 < len(p) && p[i+1] == '/' {
				i++
				sb.WriteString("(?:/)?") // "a/**/b" also matches "a/b"
			}
		case c == '*':
			sb.WriteString("[^/]*")
		case c == '?':
			sb.WriteString("[^/]")
		default:
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	sb.WriteString("(?:/.*)?$")
	return regexp.MustCompile(sb.String())
}

// nodeModulesPackage returns the package a metafile input path belongs to.
func nodeModulesPackage(key string) string {
	i := strings.LastIndex(key, "node_modules/")
	if i < 0 {
		return ""
	}
	return packageOf(key[i+len("node_modules/"):])
}

// packageOf returns the package part of an import specifier or path inside
// node_modules: "sharp/lib/x" is "sharp", "@a/b/c" is "@a/b".
func packageOf(spec string) string {
	parts := strings.Split(spec, "/")
	if strings.HasPrefix(spec, "@") && len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

func (r *islandRule) matchInput(key string) bool {
	if r.pkg {
		return nodeModulesPackage(key) == r.key
	}
	if strings.HasPrefix(key, virtualNS+":") {
		return false
	}
	return r.re.MatchString(key)
}

func (r *islandRule) matchExternal(spec string) bool {
	return r.pkg && !strings.HasPrefix(spec, ".") && !strings.HasPrefix(spec, "node:") && packageOf(spec) == r.key
}

// assign decides which runtime runs each registration. Without islands every
// registration runs on the default runtime. With islands, one analysis bundle
// of all registrations gives the import graph; a registration runs on an island
// runtime when anything in its transitive imports matches an islands entry.
func (b *builder) assign(regs []regEntry, def runtimes.Runtime, islands map[string]string, gens map[string]*genModule) (map[runtimes.Runtime][]regEntry, error) {
	out := map[runtimes.Runtime][]regEntry{}
	if len(islands) == 0 || len(regs) == 0 {
		out[def] = regs
		return out, nil
	}
	rules := parseIslands(islands)
	graph, ok := b.importGraph(regs, gens)
	if !ok {
		return out, nil
	}
	memo := map[string]map[int]bool{}
	var visit func(key string, onPath map[string]bool) map[int]bool
	visit = func(key string, onPath map[string]bool) map[int]bool {
		if m, ok := memo[key]; ok {
			return m
		}
		if onPath[key] {
			return nil
		}
		onPath[key] = true
		defer delete(onPath, key)
		hit := map[int]bool{}
		for i, r := range rules {
			if r.matchInput(key) {
				hit[i] = true
			}
		}
		for _, im := range graph[key] {
			if im.external {
				for i, r := range rules {
					if r.matchExternal(im.path) {
						hit[i] = true
					}
				}
				continue
			}
			for i := range visit(im.path, onPath) {
				hit[i] = true
			}
		}
		memo[key] = hit
		return hit
	}
	for _, r := range regs {
		hit := visit(inputKey(r.importFrom), map[string]bool{})
		var idx []int
		for i := range hit {
			idx = append(idx, i)
		}
		sort.Ints(idx)
		target := def
		conflict := false
		for _, i := range idx {
			rules[i].used = true
			rt := rules[i].rt
			if rt == def {
				continue
			}
			if target != def && target != rt {
				conflict = true
			}
			target = rt
		}
		if conflict {
			var names []string
			for _, i := range idx {
				if rules[i].rt != def {
					names = append(names, fmt.Sprintf("%q (%s)", rules[i].key, rules[i].rt))
				}
			}
			b.diags = append(b.diags, diag.New(r.File, r.Line, 1,
				r.ID+" imports code assigned to different host runtimes by native.host.islands: "+strings.Join(names, ", "),
				"split the export so each part imports one island, or give those entries the same runtime", "").WithCode(diag.CodeHostIslandConflict))
			continue
		}
		out[target] = append(out[target], r)
	}
	for _, r := range rules {
		if !r.used {
			d := diag.New("krate.config", 1, 1,
				"native.host.islands entry "+fmt.Sprintf("%q", r.key)+" matches no host code",
				"a package name must match an imported package; a path is relative to the project root and may use * and **", "").WithCode(diag.CodeHostIslandUnused)
			d.Severity = diag.SeverityWarning
			b.diags = append(b.diags, d)
		}
	}
	return out, nil
}

type importEdge struct {
	path     string
	external bool
}

// inputKey turns the importFrom of a registration into a metafile input key.
func inputKey(importFrom string) string {
	if strings.HasPrefix(importFrom, "./") {
		return filepath.ToSlash(importFrom[2:])
	}
	return importFrom
}

// importGraph bundles every registration once (for a child runtime, which has
// the widest module resolution) and returns the import edges of each input.
func (b *builder) importGraph(regs []regEntry, gens map[string]*genModule) (map[string][]importEdge, bool) {
	entry := entrySource(regs)
	external := []string{"node:*"}
	external = append(external, nodeBuiltins...)
	opts := api.BuildOptions{
		EntryPoints:   []string{virtualEntry},
		AbsWorkingDir: b.root,
		Bundle:        true,
		Format:        api.FormatESModule,
		Target:        api.ES2022,
		Platform:      api.PlatformNode,
		Conditions:    []string{"node", "import", "default"},
		External:      external,
		Outfile:       filepath.Join(b.opts.OutDir, "host", "islands-analysis.mjs"),
		Metafile:      true,
		Write:         false,
		LogLevel:      api.LogLevelSilent,
		Define:        map[string]string{"process.env.NODE_ENV": `"production"`},
		Plugins:       []api.Plugin{b.virtualPlugin(entry, gens, false)},
	}
	tsstrip.HandleTierDecorators(&opts)
	r := api.Build(opts)
	if len(r.Errors) > 0 {
		for _, e := range r.Errors {
			b.diags = append(b.diags, b.bundleDiag(e, gens))
		}
		return nil, false
	}
	var meta metafile
	if err := jsonUnmarshal(r.Metafile, &meta); err != nil {
		return nil, false
	}
	g := map[string][]importEdge{}
	for k, in := range meta.Inputs {
		for _, im := range in.Imports {
			g[k] = append(g[k], importEdge{path: im.Path, external: im.External})
		}
	}
	return g, true
}

// nodeBuiltinCheck reports the Node built-in modules a QuickJS bundle imports
// (KR7230): the embedded runtime has none, so the call would fail at run time.
func (b *builder) nodeBuiltinCheck(out *bundleOutput, gens map[string]*genModule) {
	type hit struct {
		importer string
		specs    []string
	}
	byImporter := map[string]*hit{}
	var order []string
	keys := make([]string, 0, len(out.meta.Inputs))
	for k := range out.meta.Inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, im := range out.meta.Inputs[k].Imports {
			if !im.External || !isNodeBuiltin(im.Path) {
				continue
			}
			unit := k
			if pkg := nodeModulesPackage(k); pkg != "" {
				unit = "node_modules/" + pkg
			}
			h := byImporter[unit]
			if h == nil {
				h = &hit{importer: k}
				byImporter[unit] = h
				order = append(order, unit)
			}
			if !contains(h.specs, im.Path) {
				h.specs = append(h.specs, im.Path)
			}
		}
	}
	for _, unit := range order {
		h := byImporter[unit]
		spec := h.specs[0]
		file, line, col, src := b.locateImport(h.importer, spec, gens)
		var msg, island string
		if strings.HasPrefix(unit, "node_modules/") {
			pkg := strings.TrimPrefix(unit, "node_modules/")
			msg = fmt.Sprintf("host code imports the package %s, which uses the Node built-in %s; the embedded QuickJS runtime has no Node built-ins", pkg, quoteList(h.specs))
			island = pkg
		} else {
			msg = fmt.Sprintf("host code imports the Node built-in %s; the embedded QuickJS runtime has no Node built-ins", quoteList(h.specs))
			island = file
		}
		hint := fmt.Sprintf("run this code on a full runtime: add { %q: \"node\" } to native.host.islands, or set native.host.runtime to \"node\" (or \"bun\"/\"deno\"); a Go host function works too", island)
		b.diags = append(b.diags, diag.New(file, line, col, msg, hint, src).WithCode(diag.CodeHostNodeBuiltin))
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func quoteList(l []string) string {
	q := make([]string, len(l))
	for i, s := range l {
		q[i] = fmt.Sprintf("%q", s)
	}
	if len(q) > 3 {
		return strings.Join(q[:3], ", ") + fmt.Sprintf(" and %d more", len(q)-3)
	}
	return strings.Join(q, ", ")
}

func isNodeBuiltin(spec string) bool {
	if strings.HasPrefix(spec, "node:") {
		return true
	}
	for _, n := range nodeBuiltins {
		if n == spec {
			return true
		}
	}
	return false
}

// locateImport finds the import of spec in the importer's source.
func (b *builder) locateImport(importer, spec string, gens map[string]*genModule) (file string, line, col int, src string) {
	re := regexp.MustCompile(`["'` + "`" + `]` + regexp.QuoteMeta(spec) + `["'` + "`" + `]`)
	find := func(text string) (int, int) {
		loc := re.FindStringIndex(text)
		if loc == nil {
			return 1, 1
		}
		before := text[:loc[0]]
		l := strings.Count(before, "\n") + 1
		c := loc[0] - strings.LastIndex(before, "\n")
		return l, c
	}
	if strings.HasPrefix(importer, virtualNS+":"+inlinePrefix) {
		if g := gens[strings.TrimPrefix(importer, virtualNS+":"+inlinePrefix)]; g != nil {
			l, c := find(g.split.HostSrc)
			ol := genOrigin(g, l)
			return g.rel, ol, c, lineText(g.src, ol)
		}
	}
	file = filepath.ToSlash(importer)
	abs := filepath.Join(b.root, filepath.FromSlash(importer))
	if data, err := os.ReadFile(abs); err == nil {
		l, c := find(string(data))
		return file, l, c, lineText(string(data), l)
	}
	return file, 1, 1, ""
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
