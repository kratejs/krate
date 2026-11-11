package hostts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
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
}

// hostPlugin serves the virtual entry module and the generated inline host
// modules to esbuild.
func (b *builder) hostPlugin(entry string, gens map[string]*genModule) api.Plugin {
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
	}}
}

// emit writes the host bundle, manifest, types, and fills the result.
func (b *builder) emit(regs []regEntry) error {
	entry := entrySource(regs)
	gens := map[string]*genModule{}
	for _, g := range b.genList {
		gens[g.modID] = g
	}

	plugin := b.hostPlugin(entry, gens)

	external := []string{"node:*"}
	external = append(external, nodeBuiltins...)
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
		Outfile:        filepath.Join(b.opts.OutDir, filepath.FromSlash(hostBundleRel)),
		Sourcemap:      api.SourceMapExternal,
		SourcesContent: api.SourcesContentInclude,
		Metafile:       true,
		Write:          false,
		LogLevel:       api.LogLevelSilent,
		Define:         map[string]string{"process.env.NODE_ENV": `"production"`},
		Plugins:        []api.Plugin{plugin},
	}
	tsstrip.HandleTierDecorators(&opts)
	r := api.Build(opts)
	if len(r.Errors) > 0 {
		for _, e := range r.Errors {
			b.diags = append(b.diags, b.bundleDiag(e, gens))
		}
		return nil
	}

	var code, smap []byte
	for _, f := range r.OutputFiles {
		switch {
		case strings.HasSuffix(f.Path, ".map"):
			smap = f.Contents
		case strings.HasSuffix(f.Path, ".js"):
			code = f.Contents
		}
	}
	if code == nil {
		return nil
	}
	smap = b.relativeSources(smap, filepath.Dir(opts.Outfile))

	var meta struct {
		Inputs  map[string]json.RawMessage `json:"inputs"`
		Outputs map[string]struct {
			Imports []struct {
				Path     string `json:"path"`
				External bool   `json:"external"`
			} `json:"imports"`
		} `json:"outputs"`
	}
	_ = json.Unmarshal([]byte(r.Metafile), &meta)
	ext := map[string]bool{}
	for _, o := range meta.Outputs {
		for _, im := range o.Imports {
			if im.External {
				ext[im.Path] = true
			}
		}
	}
	externals := sortedKeys(ext)

	b.compatScan(sortedKeys(meta.Inputs), gens)

	outDir := b.opts.OutDir
	hostDir := filepath.Join(outDir, "host")
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		return err
	}
	bundle := filepath.Join(hostDir, "host.js")
	mapPath := bundle + ".map"
	manPath := filepath.Join(hostDir, "manifest.json")
	if err := os.WriteFile(bundle, code, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(mapPath, smap, 0o644); err != nil {
		return err
	}
	registered := make([]Registration, len(regs))
	for i, r := range regs {
		registered[i] = r.Registration
	}
	man, _ := json.MarshalIndent(manifest{
		Version: manifestVer, Bundle: "host.js", SourceMap: "host.js.map", Target: "es2020",
		Registered: registered, Externals: nonNil(externals),
	}, "", "  ")
	if err := os.WriteFile(manPath, append(man, '\n'), 0o644); err != nil {
		return err
	}
	b.res.HostBundle, b.res.SourceMap, b.res.Manifest = bundle, mapPath, manPath
	b.res.Registered = registered
	b.res.Externals = externals

	return b.writeTypes(outDir)
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
		if str == "" || strings.HasPrefix(str, "krate-host:") {
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
