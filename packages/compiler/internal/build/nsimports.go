package build

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// A library can export a namespace through an index module that only
// re-exports: zod's index is `import * as z from "./v4/classic/external.js";
// export { z }`. esbuild tree shakes the members of a namespace import used in
// the same file (z.string()), but a namespace that reaches the importer
// through another module is built as an object holding every export, so
// `import { z } from "zod"` ships all of zod, every locale included.
//
// withNamespaceImports makes a client build rewrite such an import into a
// namespace import of the module itself:
//
//	import { z } from "zod"  ->  import * as z from "./node_modules/zod/v4/classic/external.js"
//
// The binding is the same namespace object either way, so behaviour does not
// change, and esbuild can drop the exports the file never reads. Only an
// index that does nothing but import and re-export one module is skipped
// over, so no module code stops running.
func withNamespaceImports(opts *api.BuildOptions) {
	conds := resolver.ClientConditions(false)
	if opts.Platform == api.PlatformNode {
		conds = resolver.ServerConditions(false)
	}
	rw := &nsRewriter{loaders: opts.Loader, conds: append(conds, opts.Conditions...)}
	wrapped := make([]api.Plugin, 0, len(opts.Plugins)+1)
	for _, p := range opts.Plugins {
		wrapped = append(wrapped, rw.wrap(p))
	}
	opts.Plugins = append(wrapped, rw.loader())
}

type nsRewriter struct {
	loaders map[string]api.Loader
	conds   resolver.Conditions

	mu       sync.Mutex
	resolved map[string]string            // resolve dir + "\x00" + specifier -> path ("" when not a barrel)
	seen     map[string]map[string]string // barrels checked in this build
	hops     map[nsHop]bool               // hops esbuild resolves the same way
}

// nsBarrel is what one module re-exports as namespaces of other modules
// (export name -> the module whose namespace it is), with the files the
// answer was read from.
type nsBarrel struct {
	deps []nsStamp
	hops []nsHop
	ns   map[string]string
}

// nsHop is a package import an index re-exports from, with the file Krate's
// resolver found for it; a build uses the answer only if esbuild agrees.
type nsHop struct {
	importer, spec, path string
}

type nsStamp struct {
	path  string
	mtime time.Time
	size  int64
}

// nsBarrels keeps barrel answers across builds: path -> nsBarrel. An answer
// depends only on the files in its deps, so it stays good while they do.
var nsBarrels sync.Map

// nsBarrelMaxSize bounds the modules read as possible barrels: an index that
// only re-exports one module is small.
const nsBarrelMaxSize = 64 << 10

// nsHopConditions pick a package entry while reading an index; a build checks
// each pick against esbuild, so these only need to agree usually.
var nsHopConditions = resolver.ClientConditions(false)

// nsNamedImportRe matches `import [D,] { ... } from "spec"`; type-only imports
// (`import type { ... }`) do not match.
var nsNamedImportRe = regexp.MustCompile(`^import\s*(?:([A-Za-z_$][\w$]*)\s*,\s*)?\{([^{}]*)\}\s*from\s*(["'])([^"'\n]+)["']`)

// nsDefaultImportRe matches `import D from "spec"`.
var nsDefaultImportRe = regexp.MustCompile(`^import\s+([A-Za-z_$][\w$]*)\s+from\s*(["'])([^"'\n]+)["']`)

// nsImportWindow bounds how far past `import` a statement is matched.
const nsImportWindow = 16 << 10

// nsImports returns the matches of the anchored named and default import
// patterns at each word `import` in src, as offsets into src. Matching only
// where the word occurs is much cheaper than scanning a whole bundle-sized
// file with an unanchored pattern.
func nsImports(src string) (named, def [][]int) {
	for i := 0; ; {
		j := strings.Index(src[i:], "import")
		if j < 0 {
			return named, def
		}
		at := i + j
		i = at + len("import")
		if at > 0 && isIdentByte(src[at-1]) || i < len(src) && isIdentByte(src[i]) {
			continue
		}
		window := src[at:min(len(src), at+nsImportWindow)]
		for _, p := range []struct {
			re  *regexp.Regexp
			out *[][]int
		}{{nsNamedImportRe, &named}, {nsDefaultImportRe, &def}} {
			if m := p.re.FindStringSubmatchIndex(window); m != nil {
				for k := range m {
					if m[k] >= 0 {
						m[k] += at
					}
				}
				*p.out = append(*p.out, m)
				i = m[1]
				break
			}
		}
	}
}

func (rw *nsRewriter) reset() {
	rw.mu.Lock()
	rw.resolved = map[string]string{}
	rw.seen = map[string]map[string]string{}
	rw.hops = map[nsHop]bool{}
	rw.mu.Unlock()
}

func (rw *nsRewriter) wrap(p api.Plugin) api.Plugin {
	setup := p.Setup
	p.Setup = func(pb api.PluginBuild) {
		onLoad := pb.OnLoad
		pb.OnLoad = func(o api.OnLoadOptions, cb func(api.OnLoadArgs) (api.OnLoadResult, error)) {
			onLoad(o, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				res, err := cb(args)
				if err == nil && res.Contents != nil && args.Namespace == "file" {
					if out, ok := rw.rewrite(pb, args.Path, *res.Contents); ok {
						res.Contents = &out
					}
				}
				return res, err
			})
		}
		setup(pb)
	}
	return p
}

// loader loads the script files no other plugin claimed, rewritten.
func (rw *nsRewriter) loader() api.Plugin {
	return api.Plugin{
		Name: "krate-namespace-imports",
		Setup: func(pb api.PluginBuild) {
			pb.OnStart(func() (api.OnStartResult, error) {
				rw.reset()
				return api.OnStartResult{}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `\.(m?[jt]sx?)$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				data, err := os.ReadFile(args.Path)
				if err != nil {
					return api.OnLoadResult{}, nil
				}
				out, ok := rw.rewrite(pb, args.Path, string(data))
				if !ok {
					return api.OnLoadResult{}, nil
				}
				loader, set := rw.loaders[filepath.Ext(args.Path)]
				if !set {
					loader = loaderForExt(args.Path)
				}
				return api.OnLoadResult{Contents: &out, Loader: loader, ResolveDir: filepath.Dir(args.Path)}, nil
			})
		},
	}
}

// resolve returns the file an import of the file at importer resolves to when
// that file is a pure index re-exporting namespaces, and "" otherwise.
//
// esbuild's own resolver is slow when called from a plugin (it reads every
// directory again), so the import is first resolved with Krate's cached
// resolver, and esbuild is asked only when that lands on such an index, or
// when project code imports something the cached resolver cannot place (a
// tsconfig path alias).
func (rw *nsRewriter) resolve(pb api.PluginBuild, importer, spec string) string {
	dir := filepath.Dir(importer)
	key := dir + "\x00" + spec
	rw.mu.Lock()
	if rw.resolved == nil {
		rw.resolved = map[string]string{}
	}
	if p, ok := rw.resolved[key]; ok {
		rw.mu.Unlock()
		return p
	}
	rw.mu.Unlock()
	candidate := ""
	switch {
	case strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../"):
		candidate = resolver.File(filepath.Join(dir, spec))
	case spec != "" && !strings.HasPrefix(spec, "/") && !strings.HasPrefix(spec, "#") && !strings.Contains(spec, ":"):
		candidate = resolver.NodeModuleWith(dir, spec, rw.conds)
	}
	p := ""
	switch {
	case candidate != "":
		if len(rw.barrel(pb, candidate)) > 0 {
			p = rw.esbuildResolve(pb, importer, spec)
		}
	case !strings.Contains(filepath.ToSlash(importer), "/node_modules/"):
		p = rw.esbuildResolve(pb, importer, spec)
	}
	if p != "" && len(rw.barrel(pb, p)) == 0 {
		p = ""
	}
	rw.mu.Lock()
	rw.resolved[key] = p
	rw.mu.Unlock()
	return p
}

func (rw *nsRewriter) esbuildResolve(pb api.PluginBuild, importer, spec string) string {
	res := pb.Resolve(spec, api.ResolveOptions{Importer: importer, ResolveDir: filepath.Dir(importer), Kind: api.ResolveJSImportStatement, Namespace: "file"})
	if len(res.Errors) == 0 && !res.External && res.Namespace == "file" && filepath.IsAbs(res.Path) {
		return res.Path
	}
	return ""
}

// rewrite returns src with each named import of a namespace that a pure
// index re-exports turned into a namespace import of its module.
func (rw *nsRewriter) rewrite(pb api.PluginBuild, path, src string) (string, bool) {
	if !strings.Contains(src, "import") || !strings.Contains(src, "{") {
		return src, false
	}
	type change struct {
		start, end int
		text       string
	}
	var changes []change
	namedImports, defaultImports := nsImports(src)
	for _, m := range defaultImports {
		local, spec := src[m[2]:m[3]], src[m[6]:m[7]]
		if local == "type" {
			continue
		}
		target := rw.resolve(pb, path, spec)
		if target == "" {
			continue
		}
		if mod, ok := rw.barrel(pb, target)["default"]; ok {
			changes = append(changes, change{m[0], m[1], "import * as " + local + " from " + strconv.Quote(importSpecFor(path, mod))})
		}
	}
	for _, m := range namedImports {
		spec := src[m[8]:m[9]]
		var specs []string
		for _, s := range strings.Split(src[m[4]:m[5]], ",") {
			if s = strings.TrimSpace(s); s != "" {
				specs = append(specs, s)
			}
		}
		target := ""
		var ns map[string]string
		var moved []string // `import * as local from "module"` statements
		var kept []string
		for _, s := range specs {
			fields := strings.Fields(s)
			remote, local := fields[0], fields[0]
			if remote == "type" && len(fields) > 1 {
				kept = append(kept, s)
				continue
			}
			if len(fields) == 3 && fields[1] == "as" {
				local = fields[2]
			} else if len(fields) != 1 {
				kept = append(kept, s)
				continue
			}
			if ns == nil {
				if target = rw.resolve(pb, path, spec); target == "" {
					break
				}
				ns = rw.barrel(pb, target)
				if len(ns) == 0 {
					break
				}
			}
			if mod, ok := ns[remote]; ok {
				moved = append(moved, "import * as "+local+" from "+strconv.Quote(importSpecFor(path, mod))+";")
				continue
			}
			kept = append(kept, s)
		}
		def := ""
		if m[2] >= 0 {
			def = src[m[2]:m[3]]
			if mod, ok := ns["default"]; ok {
				moved = append(moved, "import * as "+def+" from "+strconv.Quote(importSpecFor(path, mod))+";")
				def = ""
			}
		}
		if len(moved) == 0 {
			continue
		}
		text := strings.Join(moved, " ")
		if def != "" || len(kept) > 0 {
			clause := "{ " + strings.Join(kept, ", ") + " }"
			if def != "" && len(kept) == 0 {
				clause = def
			} else if def != "" {
				clause = def + ", " + clause
			}
			text += " import " + clause + " from " + src[m[6]:m[7]] + spec + src[m[6]:m[7]]
		} else if strings.HasSuffix(text, ";") {
			text = strings.TrimSuffix(text, ";")
		}
		// Keep the line count, so the source map of the rest stays right.
		text += strings.Repeat("\n", strings.Count(src[m[0]:m[1]], "\n"))
		changes = append(changes, change{m[0], m[1], text})
	}
	if len(changes) == 0 {
		return src, false
	}
	// Only real import statements: not the text of a string or a comment.
	prog, errs := tsstrip.Parse(path, src, tsstrip.Options{})
	if len(errs) > 0 || prog == nil {
		return src, false
	}
	starts := map[int]bool{}
	for _, stmt := range prog.Body {
		if imp, ok := stmt.(*ast.ImportStmt); ok {
			starts[imp.Position.Offset] = true
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].start < changes[j].start })
	var b strings.Builder
	last := 0
	for _, c := range changes {
		if !starts[c.start] {
			continue
		}
		b.WriteString(src[last:c.start])
		b.WriteString(c.text)
		last = c.end
	}
	if last == 0 {
		return src, false
	}
	b.WriteString(src[last:])
	return b.String(), true
}

// importSpecFor is a relative specifier from the file at importer to target.
func importSpecFor(importer, target string) string {
	rel, err := filepath.Rel(filepath.Dir(importer), target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, "../") {
		rel = "./" + rel
	}
	return rel
}

// barrel returns the names the module at path exports as namespaces of other
// modules, if it is a pure index: nothing but imports and re-exports of one
// module, so importing that module instead skips no code.
func (rw *nsRewriter) barrel(pb api.PluginBuild, path string) map[string]string {
	rw.mu.Lock()
	if rw.seen == nil {
		rw.seen = map[string]map[string]string{}
	}
	if ns, ok := rw.seen[path]; ok {
		rw.mu.Unlock()
		return ns
	}
	rw.mu.Unlock()
	b, _ := nsBarrelOf(path, map[string]bool{})
	ns := b.ns
	for _, h := range b.hops {
		if len(ns) > 0 && !rw.hopHolds(pb, h) {
			ns = nil
		}
	}
	rw.mu.Lock()
	rw.seen[path] = ns
	rw.mu.Unlock()
	return ns
}

// hopHolds compares paths as strings: when the two resolvers spell a file
// differently (a Windows short name), the import is just left alone.
func (rw *nsRewriter) hopHolds(pb api.PluginBuild, h nsHop) bool {
	rw.mu.Lock()
	if rw.hops == nil {
		rw.hops = map[nsHop]bool{}
	}
	ok, done := rw.hops[h]
	rw.mu.Unlock()
	if done {
		return ok
	}
	ok = rw.esbuildResolve(pb, h.importer, h.spec) == h.path
	rw.mu.Lock()
	rw.hops[h] = ok
	rw.mu.Unlock()
	return ok
}

// nsBarrelOf answers barrel for path from the cache or by reading it.
// visiting holds the indexes being read above it; reaching one again (a
// cycle) makes every index on the cycle not pure, whichever was read first.
func nsBarrelOf(path string, visiting map[string]bool) (nsBarrel, bool) {
	if visiting[path] || len(visiting) > 8 {
		return nsBarrel{}, false
	}
	if v, ok := nsBarrels.Load(path); ok {
		if b := v.(nsBarrel); b.fresh() {
			return b, true
		}
	}
	st := resolver.StatPath(path)
	if !st.Exists || st.IsDir {
		return nsBarrel{}, true
	}
	b := nsBarrel{deps: []nsStamp{{path, st.Mtime, st.Size}}}
	if !isScriptPath(path) || st.Size > nsBarrelMaxSize {
		nsBarrels.Store(path, b)
		return b, true
	}
	visiting[path] = true
	ns, deps, hops, ok := analyzeBarrel(path, visiting)
	delete(visiting, path)
	if !ok {
		return nsBarrel{}, false
	}
	b.ns = ns
	b.deps = append(b.deps, deps...)
	b.hops = hops
	nsBarrels.Store(path, b)
	return b, true
}

func (b nsBarrel) fresh() bool {
	for _, d := range b.deps {
		if st := resolver.StatPath(d.path); !st.Exists || !st.Mtime.Equal(d.mtime) || st.Size != d.size {
			return false
		}
	}
	return len(b.deps) > 0
}

func isScriptPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".mjs", ".jsx", ".ts", ".mts", ".tsx":
		return true
	}
	return false
}

// analyzeBarrel reads the module at path as a pure index. It reports false
// when it ran into a cycle of indexes.
func analyzeBarrel(path string, visiting map[string]bool) (map[string]string, []nsStamp, []nsHop, bool) {
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "export") {
		return nil, nil, nil, true
	}
	prog, errs := tsstrip.Parse(path, string(data), tsstrip.Options{})
	if len(errs) > 0 || prog == nil {
		return nil, nil, nil, true
	}
	unquote := func(s string) string { return strings.Trim(s, "\"'`") }
	dir := filepath.Dir(path)
	target := ""
	var hops []nsHop
	one := func(spec string) bool {
		spec = unquote(spec)
		p := ""
		switch {
		case strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../"):
			p = resolver.File(filepath.Join(dir, spec))
		case spec != "" && !strings.HasPrefix(spec, "/") && !strings.HasPrefix(spec, "#") && !strings.Contains(spec, ":"):
			if p = resolver.NodeModuleWith(dir, spec, nsHopConditions); p != "" && target == "" {
				hops = append(hops, nsHop{path, spec, p})
			}
		}
		if p == "" || !isScriptPath(p) || target != "" && p != target {
			return false
		}
		target = p
		return true
	}
	nsLocal := map[string]bool{}      // local bound to the namespace of target
	named := map[string]string{}      // local -> name imported from target
	exported := map[string]string{}   // export name -> local
	reexported := map[string]string{} // export name -> name re-exported from target
	nsExport := map[string]bool{}     // export * as name from target
	star := false
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.ImportStmt:
			if len(s.Attributes) > 0 || !one(s.Source) {
				return nil, nil, nil, true
			}
			if s.Namespace != "" {
				nsLocal[s.Namespace] = true
			}
			if s.Default != "" {
				named[s.Default] = "default"
			}
			for _, n := range s.Named {
				named[n.Local] = n.Remote
			}
		case *ast.ExportStmt:
			if s.Declaration != nil || len(s.Attributes) > 0 {
				return nil, nil, nil, true
			}
			switch {
			case s.Default:
				if s.Local == "" {
					return nil, nil, nil, true
				}
				exported["default"] = s.Local
			case s.ReexportSource != "":
				if !one(s.ReexportSource) {
					return nil, nil, nil, true
				}
				switch {
				case s.Namespace != "":
					nsExport[s.Namespace] = true
				case s.StarReexport:
					star = true
				default:
					for _, n := range s.ReexportNamed {
						reexported[n.Remote] = n.Local
					}
				}
			default:
				for _, n := range s.Named {
					exported[n.Remote] = n.Local
				}
			}
		default:
			return nil, nil, nil, true
		}
	}
	if target == "" {
		return nil, nil, nil, true
	}
	ns := map[string]string{}
	var inner map[string]string
	var deps []nsStamp
	cut := false
	innerNS := func() map[string]string {
		if inner == nil {
			b, ok := nsBarrelOf(target, visiting)
			cut = cut || !ok
			deps = b.deps
			hops = append(hops, b.hops...)
			inner = b.ns
			if inner == nil {
				inner = map[string]string{}
			}
		}
		return inner
	}
	for name := range nsExport {
		ns[name] = target
	}
	for name, local := range exported {
		if nsLocal[local] {
			ns[name] = target
		} else if remote, ok := named[local]; ok {
			if mod, ok := innerNS()[remote]; ok {
				ns[name] = mod
			}
		}
	}
	for name, remote := range reexported {
		if mod, ok := innerNS()[remote]; ok {
			ns[name] = mod
		}
	}
	if star {
		names := make([]string, 0, len(innerNS()))
		for n := range innerNS() {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			_, local := exported[n]
			_, again := reexported[n]
			if _, set := ns[n]; !set && !local && !again && !nsExport[n] && n != "default" {
				ns[n] = innerNS()[n]
			}
		}
	}
	if cut {
		return nil, nil, nil, false
	}
	return ns, deps, hops, true
}
