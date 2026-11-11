package hostts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
	"github.com/kratejs/krate/packages/compiler/internal/native/runtimes"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
	"github.com/kratejs/krate/packages/native/hostchild"
)

const (
	virtualNS     = "krate-host"
	virtualEntry  = "krate-host:entry"
	inlinePrefix  = "inline/"
	manifestVer   = 1
	hostBundleRel = "host/host.js"
)

// nodeBuiltins are the bare names of Node built-in modules. They are left
// external like the node: forms.
var nodeBuiltins = []string{
	"assert", "async_hooks", "buffer", "child_process", "cluster", "console", "constants", "crypto",
	"dgram", "diagnostics_channel", "dns", "dns/promises", "domain", "events", "fs", "fs/promises",
	"http", "http2", "https", "inspector", "module", "net", "os", "path", "path/posix", "path/win32",
	"perf_hooks", "process", "punycode", "querystring", "readline", "readline/promises", "repl",
	"stream", "stream/promises", "stream/web", "string_decoder", "sys", "timers", "timers/promises",
	"tls", "trace_events", "tty", "url", "util", "util/types", "v8", "vm", "wasi", "worker_threads", "zlib",
}

type manifest struct {
	Version    int            `json:"version"`
	Bundle     string         `json:"bundle"`
	SourceMap  string         `json:"sourceMap"`
	Target     string         `json:"target"`
	Registered []Registration `json:"registered"`
	Externals  []string       `json:"externals"`
	// Groups lists every bundle: the QuickJS one (the fields above) and one
	// per child runtime.
	Groups []manifestGroup `json:"groups,omitempty"`
}

type manifestGroup struct {
	Runtime   string   `json:"runtime"`
	Bundle    string   `json:"bundle"`
	SourceMap string   `json:"sourceMap"`
	Ids       []string `json:"ids"`
}

// job is one bundle to write: the registrations that run in one runtime.
type job struct {
	rt   runtimes.Runtime // quickjs, node, bun, deno or scriptc
	regs []regEntry
	// quickjs marks the in-process bundle (an IIFE for the embedded runtime);
	// otherwise the bundle is an ES module for a child process.
	quickjs bool
}

func (j job) relBundle() string {
	if j.quickjs {
		return hostBundleRel
	}
	return "host/" + string(j.rt) + "/host.mjs"
}

// emit writes the host bundles, manifest, types, and fills the result.
func (b *builder) emit(regs []regEntry) error {
	gens := map[string]*genModule{}
	for _, g := range b.genList {
		gens[g.modID] = g
	}
	def, islands := b.hostConfig()
	assigned, err := b.assign(regs, def, islands, gens)
	if err != nil {
		return err
	}
	if b.hasErrors() {
		return nil
	}

	var jobs []job
	switch def {
	case runtimes.QuickJS:
		jobs = append(jobs, job{rt: runtimes.QuickJS, regs: assigned[runtimes.QuickJS], quickjs: true})
	case runtimes.Scriptc:
		// Development runs scriptc code on QuickJS; the scriptc variant is the
		// child bundle a release build compiles.
		jobs = append(jobs,
			job{rt: runtimes.QuickJS, regs: assigned[runtimes.Scriptc], quickjs: true},
			job{rt: runtimes.Scriptc, regs: assigned[runtimes.Scriptc]})
	default:
		jobs = append(jobs, job{rt: def, regs: assigned[def]})
	}
	if def != runtimes.QuickJS && len(assigned[runtimes.QuickJS]) > 0 {
		jobs = append(jobs, job{rt: runtimes.QuickJS, regs: assigned[runtimes.QuickJS], quickjs: true})
	}
	for _, rt := range []runtimes.Runtime{runtimes.Node, runtimes.Bun, runtimes.Deno} {
		if rt != def && len(assigned[rt]) > 0 {
			jobs = append(jobs, job{rt: rt, regs: assigned[rt]})
		}
	}

	var man manifest
	var all []Registration
	for _, j := range jobs {
		ok, err := b.emitJob(j, gens, &man)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	}
	// Registered lists each id once, in id order. The scriptc variant repeats
	// the QuickJS ids.
	seen := map[string]bool{}
	for _, g := range b.res.Groups {
		for _, r := range g.Registered {
			if !seen[r.ID] {
				seen[r.ID] = true
				all = append(all, r)
			}
		}
	}
	sort.Slice(all, func(i, k int) bool { return all[i].ID < all[k].ID })
	b.res.Registered = all
	man.Registered = nonNilRegs(all)
	if man.Externals == nil {
		man.Externals = []string{}
	}
	man.Version, man.Target = manifestVer, "es2020"
	hostDir := filepath.Join(b.opts.OutDir, "host")
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		return err
	}
	manPath := filepath.Join(hostDir, "manifest.json")
	data, _ := json.MarshalIndent(man, "", "  ")
	if err := os.WriteFile(manPath, append(data, '\n'), 0o644); err != nil {
		return err
	}
	b.res.Manifest = manPath
	b.res.Origin = func(source string, line int) (string, int) {
		if strings.HasPrefix(source, virtualNS+":"+inlinePrefix) {
			if g := gens[strings.TrimPrefix(source, virtualNS+":"+inlinePrefix)]; g != nil {
				return g.rel, genOrigin(g, line)
			}
		}
		return source, line
	}
	return b.writeTypes(b.opts.OutDir)
}

func nonNilRegs(r []Registration) []Registration {
	if r == nil {
		return []Registration{}
	}
	return r
}

// hostConfig returns the default host runtime and the islands of the project.
func (b *builder) hostConfig() (runtimes.Runtime, map[string]string) {
	if b.cfg == nil || b.cfg.Native == nil {
		return runtimes.QuickJS, nil
	}
	n := b.cfg.EffectiveNative()
	if n == nil || n.Host.Runtime == "" {
		return runtimes.QuickJS, n.Host.Islands
	}
	return runtimes.Runtime(n.Host.Runtime), n.Host.Islands
}

// bundleOutput is what esbuild produced for one job.
type bundleOutput struct {
	code, smap []byte
	externals  []string
	meta       metafile
}

type metafile struct {
	Inputs map[string]struct {
		Imports []struct {
			Path     string `json:"path"`
			Kind     string `json:"kind"`
			External bool   `json:"external"`
		} `json:"imports"`
	} `json:"inputs"`
}

// virtualPlugin resolves the generated entry, the generated inline modules and
// the host child runtime.
func (b *builder) virtualPlugin(entry string, gens map[string]*genModule, child bool) api.Plugin {
	return api.Plugin{Name: "krate-host", Setup: func(pb api.PluginBuild) {
		pb.OnResolve(api.OnResolveOptions{Filter: `^krate-host:`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
			return api.OnResolveResult{Path: strings.TrimPrefix(args.Path, "krate-host:"), Namespace: virtualNS}, nil
		})
		pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: virtualNS}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
			if args.Path == "entry" {
				return api.OnLoadResult{Contents: &entry, ResolveDir: b.root, Loader: api.LoaderJS}, nil
			}
			g := gens[strings.TrimPrefix(args.Path, inlinePrefix)]
			if g == nil {
				return api.OnLoadResult{}, os.ErrNotExist
			}
			src := tsstrip.BlankTierDecorators(g.path, g.split.HostSrc)
			loader := map[string]api.Loader{"tsx": api.LoaderTSX, "jsx": api.LoaderJSX, "js": api.LoaderJS}[g.split.Loader]
			if loader == 0 {
				loader = api.LoaderTS
			}
			return api.OnLoadResult{Contents: &src, ResolveDir: g.absDir, Loader: loader}, nil
		})
		if !child {
			return
		}
		pb.OnResolve(api.OnResolveOptions{Filter: `^krate-hostchild:runtime$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
			return api.OnResolveResult{Path: "hostchild-runtime", Namespace: runtimeNS}, nil
		})
		pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: runtimeNS}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
			src := hostchild.Source
			return api.OnLoadResult{Contents: &src, ResolveDir: b.root, Loader: api.LoaderJS}, nil
		})
		// Deno accepts only node:-prefixed built-ins; the prefix is valid
		// everywhere, so normalize bare names.
		pb.OnResolve(api.OnResolveOptions{Filter: bareBuiltinFilter}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
			return api.OnResolveResult{Path: "node:" + args.Path, External: true}, nil
		})
	}}
}

var bareBuiltinFilter = func() string {
	parts := make([]string, len(nodeBuiltins))
	for i, n := range nodeBuiltins {
		parts[i] = regexp.QuoteMeta(n)
	}
	return "^(" + strings.Join(parts, "|") + ")$"
}()

const runtimeNS = "krate-hostchild"

// childBanner lets CommonJS packages bundled into an ES module call require
// for Node built-ins.
const childBanner = `import { createRequire as __krateCreateRequire } from "node:module";
const require = __krateCreateRequire(import.meta.url);`

func (b *builder) buildJob(j job, gens map[string]*genModule) (*bundleOutput, bool) {
	entry := entrySource(j.regs)
	if !j.quickjs {
		entry = childEntrySource(j.regs)
	}
	external := []string{"node:*"}
	external = append(external, nodeBuiltins...)
	outfile := filepath.Join(b.opts.OutDir, filepath.FromSlash(j.relBundle()))
	opts := api.BuildOptions{
		EntryPoints:    []string{virtualEntry},
		AbsWorkingDir:  b.root,
		Bundle:         true,
		Format:         api.FormatIIFE,
		Target:         api.ES2020,
		Platform:       api.PlatformNeutral,
		MainFields:     []string{"module", "main"},
		Conditions:     []string{"import", "default"},
		External:       external,
		Outfile:        outfile,
		Sourcemap:      api.SourceMapExternal,
		SourcesContent: api.SourcesContentInclude,
		Metafile:       true,
		Write:          false,
		LogLevel:       api.LogLevelSilent,
		Define:         map[string]string{"process.env.NODE_ENV": `"production"`},
		Plugins:        []api.Plugin{b.virtualPlugin(entry, gens, !j.quickjs)},
	}
	if !j.quickjs {
		opts.Format = api.FormatESModule
		opts.Target = api.ES2022
		opts.Platform = api.PlatformNode
		opts.MainFields = nil
		opts.Conditions = childConditions(j.rt)
		opts.Banner = map[string]string{"js": childBanner}
		opts.Sourcemap = api.SourceMapLinked
	}
	tsstrip.HandleTierDecorators(&opts)
	r := api.Build(opts)
	if len(r.Errors) > 0 {
		for _, e := range r.Errors {
			b.diags = append(b.diags, b.bundleDiag(e, gens))
		}
		return nil, false
	}
	out := &bundleOutput{}
	for _, f := range r.OutputFiles {
		switch {
		case strings.HasSuffix(f.Path, ".map"):
			out.smap = f.Contents
		case strings.HasSuffix(f.Path, ".js"), strings.HasSuffix(f.Path, ".mjs"):
			out.code = f.Contents
		}
	}
	if out.code == nil {
		return nil, false
	}
	out.smap = b.relativeSources(out.smap, filepath.Dir(outfile))
	var meta struct {
		metafile
		Outputs map[string]struct {
			Imports []struct {
				Path     string `json:"path"`
				External bool   `json:"external"`
			} `json:"imports"`
		} `json:"outputs"`
	}
	_ = json.Unmarshal([]byte(r.Metafile), &meta)
	out.meta = meta.metafile
	ext := map[string]bool{}
	for _, o := range meta.Outputs {
		for _, im := range o.Imports {
			if im.External {
				ext[im.Path] = true
			}
		}
	}
	out.externals = sortedKeys(ext)
	return out, true
}

func childConditions(rt runtimes.Runtime) []string {
	switch rt {
	case runtimes.Bun:
		return []string{"bun", "node", "import", "default"}
	case runtimes.Deno:
		return []string{"deno", "node", "import", "default"}
	}
	return []string{"node", "import", "default"}
}

// emitJob bundles one runtime's registrations and writes the files. It
// reports false when the bundle failed (the diagnostics are in b.diags).
func (b *builder) emitJob(j job, gens map[string]*genModule, man *manifest) (bool, error) {
	out, ok := b.buildJob(j, gens)
	if !ok {
		return false, nil
	}
	inputs := make([]string, 0, len(out.meta.Inputs))
	for k := range out.meta.Inputs {
		inputs = append(inputs, k)
	}
	sort.Strings(inputs)
	if j.quickjs {
		b.compatScan(inputs, gens)
		b.nodeBuiltinCheck(out, gens)
		if b.hasErrors() {
			return false, nil
		}
	}
	rel := j.relBundle()
	bundle := filepath.Join(b.opts.OutDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(bundle), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(bundle, out.code, 0o644); err != nil {
		return false, err
	}
	mapPath := bundle + ".map"
	if err := os.WriteFile(mapPath, out.smap, 0o644); err != nil {
		return false, err
	}
	g := Group{Runtime: string(j.rt), Bundle: bundle, SourceMap: mapPath, Externals: out.externals}
	if j.quickjs {
		g.Runtime = string(runtimes.QuickJS)
	}
	for _, r := range j.regs {
		reg := r.Registration
		if !j.quickjs && j.rt != runtimes.Scriptc {
			reg.Runtime = string(j.rt)
		}
		g.Registered = append(g.Registered, reg)
		switch Kind(r.Kind) {
		case KindFunc:
			g.Funcs = append(g.Funcs, r.ID)
		case KindStream:
			g.Streams = append(g.Streams, r.ID)
		case KindSignal:
			g.Signals = append(g.Signals, r.ID)
		}
	}
	b.res.Groups = append(b.res.Groups, g)
	mg := manifestGroup{Runtime: g.Runtime, Bundle: filepath.Base(bundle), SourceMap: filepath.Base(mapPath)}
	if j.rt == runtimes.Scriptc {
		mg.Runtime = "scriptc"
	}
	for _, r := range g.Registered {
		mg.Ids = append(mg.Ids, r.ID)
	}
	man.Groups = append(man.Groups, mg)
	if j.quickjs {
		b.res.HostBundle, b.res.SourceMap = bundle, mapPath
		man.Bundle, man.SourceMap = "host.js", "host.js.map"
		man.Externals = out.externals
		b.res.Externals = out.externals
	}
	return true, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// writeTypes emits one .d.ts per host module the UI imports.
func (b *builder) writeTypes(outDir string) error {
	typesDir := filepath.Join(outDir, "types")
	if err := os.RemoveAll(typesDir); err != nil {
		return err
	}
	b.res.TypesDir = typesDir
	for _, path := range sortedKeys(b.use) {
		mod, err := b.an.load(path, nil)
		if err != nil {
			continue
		}
		dts := filepath.Join(typesDir, filepath.FromSlash(moduleID(b.rel(path)))+".d.ts")
		if err := os.MkdirAll(filepath.Dir(dts), 0o755); err != nil {
			return err
		}
		src := typesSource(typesImportPath(dts, path), mod.Exports, b.buildNames(path))
		if err := os.WriteFile(dts, []byte(src), 0o644); err != nil {
			return err
		}
		b.res.Types = append(b.res.Types, dts)
	}
	return nil
}

// relativeSources rewrites the source paths of a source map, which esbuild
// writes relative to the output file, to be relative to the project root.
func (b *builder) relativeSources(data []byte, outDir string) []byte {
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return data
	}
	srcs, _ := m["sources"].([]any)
	for i, s := range srcs {
		str, _ := s.(string)
		if str == "" || strings.HasPrefix(str, "krate-host:") || strings.HasPrefix(str, runtimeNS+":") {
			continue
		}
		abs := str
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(outDir, filepath.FromSlash(str))
		}
		srcs[i] = b.rel(filepath.Clean(abs))
	}
	m["sources"] = srcs
	out, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return out
}

// genOrigin maps a line of a generated inline module back to the UI module.
func genOrigin(g *genModule, line int) int {
	for _, f := range g.split.Fns {
		if line >= f.genLine && line < f.genLine+f.lines {
			return f.Line + (line - f.genLine)
		}
	}
	if len(g.split.Fns) > 0 {
		return g.split.Fns[0].Line
	}
	return 1
}

func (b *builder) bundleDiag(e api.Message, gens map[string]*genModule) diag.Diagnostic {
	hint := "host code is bundled as one ES2020 script: fix the error, or move the work to a Go host function"
	if strings.Contains(e.Text, "Could not resolve") {
		hint = "install the package or fix the import path; host code can import packages from node_modules and project files"
	}
	if e.Location == nil {
		return diag.New("", 1, 1, "host bundle: "+e.Text, hint, "").WithCode(diag.CodeHostBundle)
	}
	file, line := e.Location.File, e.Location.Line
	src := strings.TrimRight(e.Location.LineText, "\r\n")
	if strings.HasPrefix(file, virtualNS+":"+inlinePrefix) {
		if g := gens[strings.TrimPrefix(file, virtualNS+":"+inlinePrefix)]; g != nil {
			line = genOrigin(g, line)
			file = g.rel
			src = lineText(g.src, line)
		}
	} else if filepath.IsAbs(file) {
		file = b.rel(file)
	}
	return diag.New(filepath.ToSlash(file), line, e.Location.Column+1, "host bundle: "+e.Text, hint, src).WithCode(diag.CodeHostBundle)
}

// compatScan reports the APIs the host bundle uses that the embedded runtime
// does not provide.
func (b *builder) compatScan(inputs []string, gens map[string]*genModule) {
	var files []string
	scan := jsruntime.CompatScan{Complete: true}
	for _, in := range inputs {
		if strings.HasPrefix(in, virtualNS+":") {
			if g := gens[strings.TrimPrefix(in, virtualNS+":"+inlinePrefix)]; g != nil {
				scan.Merge(jsruntime.ScanCompatSource(g.path, g.split.HostSrc))
			}
			continue
		}
		p := filepath.FromSlash(in)
		if !filepath.IsAbs(p) {
			p = filepath.Join(b.root, p)
		}
		files = append(files, p)
	}
	scan.Merge(jsruntime.ScanCompatFiles(files))
	ds := jsruntime.CompatDiagnostics(scan, b.root, jsruntime.CompatAPIRuntime)
	byRel := map[string]*genModule{}
	for _, g := range b.genList {
		byRel[g.rel] = g
	}
	for _, d := range ds {
		d.Message = strings.Replace(d.Message, "embedded API runtime", "embedded host runtime", 1)
		d.Hint = "host code runs on the embedded QuickJS runtime by default: implement this in a Go host function, or run this module on a full runtime with native.host.islands"
		d.Code = diag.CodeHostUnsupportedAPI
		if g := byRel[d.File]; g != nil {
			d.Line = genOrigin(g, d.Line)
			d.Source = lineText(g.src, d.Line)
		}
		b.diags = append(b.diags, d)
	}
}
