//go:build js && wasm

// Command krate-wasm is the Krate compiler built for the browser. It exposes
// globalThis.krate.build(files, opts), which runs the same Builder as
// `krate build` (static output only) over an in-memory file system.
//
// The host page or worker must install a Node-style globalThis.fs (see
// packages/web/public/playground/memfs.js) before the module starts.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/build"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/version"
)

// projectRoot is the virtual project directory. node_modules and .krate stay
// between builds: the first holds the preloaded runtime, the second the
// build cache.
const projectRoot = "/app"

var keepOnClean = map[string]bool{"node_modules": true, ".krate": true}

// buildVersion is set with -ldflags "-X main.buildVersion=..." like the CLI's.
var buildVersion string

func main() {
	if buildVersion != "" {
		version.Value = buildVersion
	}
	api := js.Global().Get("Object").New()
	api.Set("version", version.Value)
	api.Set("build", js.FuncOf(jsBuild))
	js.Global().Set("krate", api)
	if ready := js.Global().Get("__krateReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}
	select {}
}

// buildMu serializes builds: the Builder keeps process-wide state.
var buildMu sync.Mutex

// jsBuild is krate.build(files, opts). It returns a Promise because a build
// blocks on file system callbacks, which a JS event handler must not do.
func jsBuild(_ js.Value, args []js.Value) any {
	var files, opts js.Value = js.Undefined(), js.Undefined()
	if len(args) > 0 {
		files = args[0]
	}
	if len(args) > 1 {
		opts = args[1]
	}
	// Read the arguments now: the values belong to the caller's turn.
	in, perr := readFiles(files)
	o := readOptions(opts)
	executor := js.FuncOf(func(_ js.Value, pa []js.Value) any {
		resolve := pa[0]
		go func() {
			buildMu.Lock()
			defer buildMu.Unlock()
			var res result
			if perr != nil {
				res = result{Diagnostics: []diag.Diagnostic{{Message: perr.Error()}}}
			} else {
				res = runBuild(in, o)
			}
			resolve.Invoke(res.toJS())
		}()
		return nil
	})
	defer executor.Release()
	return js.Global().Get("Promise").New(executor)
}

type options struct {
	Minify bool
	Dev    bool
}

func readOptions(v js.Value) options {
	var o options
	if v.Type() != js.TypeObject {
		return o
	}
	if m := v.Get("minify"); m.Type() == js.TypeBoolean {
		o.Minify = m.Bool()
	}
	if d := v.Get("dev"); d.Type() == js.TypeBoolean {
		o.Dev = d.Bool()
	}
	return o
}

type inputFile struct {
	path string
	data []byte
}

func readFiles(v js.Value) ([]inputFile, error) {
	if v.Type() != js.TypeObject {
		return nil, errors.New("build(files, opts): files must be an object mapping paths to contents")
	}
	keys := js.Global().Get("Object").Call("keys", v)
	var out []inputFile
	for i := 0; i < keys.Length(); i++ {
		name := keys.Index(i).String()
		clean, err := cleanRel(name)
		if err != nil {
			return nil, err
		}
		val := v.Get(name)
		var data []byte
		switch {
		case val.Type() == js.TypeString:
			data = []byte(val.String())
		case val.Type() == js.TypeObject && val.InstanceOf(js.Global().Get("Uint8Array")):
			data = make([]byte, val.Length())
			js.CopyBytesToGo(data, val)
		default:
			return nil, fmt.Errorf("file %q: contents must be a string or a Uint8Array", name)
		}
		out = append(out, inputFile{path: clean, data: data})
	}
	return out, nil
}

// cleanRel validates a project-relative path.
func cleanRel(name string) (string, error) {
	p := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if p == "." || path.IsAbs(p) || p == ".." || strings.HasPrefix(p, "../") {
		return "", fmt.Errorf("file path %q must be relative to the project and stay inside it", name)
	}
	if top, _, _ := strings.Cut(p, "/"); top == "dist" || top == "node_modules" || top == ".krate" {
		return "", fmt.Errorf("file path %q is in a directory the compiler owns (%s)", name, top)
	}
	return p, nil
}

type result struct {
	OK          bool
	Files       map[string][]byte
	Diagnostics []diag.Diagnostic
	Timings     map[string]float64
	Log         string
}

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func runBuild(in []inputFile, o options) (res result) {
	start := time.Now()
	res.Timings = map[string]float64{}
	var log strings.Builder
	defer func() {
		// A compiler bug must come back as a diagnostic, not end the instance.
		if rec := recover(); rec != nil {
			res.OK = false
			res.Diagnostics = append(res.Diagnostics, diag.Diagnostic{Message: fmt.Sprintf("internal compiler error: %v", rec), Hint: "this is a bug in Krate; the playground can keep building"})
		}
		res.Log = ansi.ReplaceAllString(log.String(), "")
		res.Timings["total"] = ms(time.Since(start))
	}()

	if err := cleanProject(); err != nil {
		res.Diagnostics = append(res.Diagnostics, diag.Diagnostic{Message: "preparing the project: " + err.Error()})
		return res
	}
	for _, f := range in {
		full := filepath.Join(projectRoot, f.path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			res.Diagnostics = append(res.Diagnostics, diag.Diagnostic{File: f.path, Message: "writing the file: " + err.Error()})
			return res
		}
		if err := os.WriteFile(full, f.data, 0o644); err != nil {
			res.Diagnostics = append(res.Diagnostics, diag.Diagnostic{File: f.path, Message: "writing the file: " + err.Error()})
			return res
		}
	}
	res.Timings["write"] = ms(time.Since(start))

	cfg := config.Default()
	cfg.Minify = o.Minify
	off := false
	cfg.Compress.Brotli, cfg.Compress.Gzip = &off, &off
	cfg.Resolve(projectRoot)

	build.SetOutput(&log)
	build.SetErrOutput(&log)
	builder := build.New(projectRoot, cfg)
	builder.OneShot = true
	builder.DevMode = o.Dev
	builder.Env = map[string]string{}
	builder.SetStderr(&log)

	buildStart := time.Now()
	err := builder.BuildAll()
	res.Timings["build"] = ms(time.Since(buildStart))
	if err != nil {
		res.Diagnostics = append(res.Diagnostics, build.DiagnosticsFromError(err)...)
	}
	if o.Dev {
		res.Diagnostics = append(res.Diagnostics, builder.DevWarnings()...)
	}
	res.Diagnostics = dedupe(addNodeHint(res.Diagnostics))
	hasError := false
	for _, d := range res.Diagnostics {
		if !d.IsWarning() {
			hasError = true
		}
	}
	if err != nil && !hasError {
		res.Diagnostics = append(res.Diagnostics, diag.Diagnostic{Message: err.Error()})
		hasError = true
	}
	res.OK = err == nil && !hasError

	readStart := time.Now()
	out, rerr := readOutput(cfg.OutDir)
	if rerr != nil && res.OK {
		res.OK = false
		res.Diagnostics = append(res.Diagnostics, diag.Diagnostic{Message: "reading the build output: " + rerr.Error()})
	}
	res.Files = out
	res.Timings["read"] = ms(time.Since(readStart))
	return res
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// addNodeHint explains the diagnostics of features that run Node.js, which the
// in-browser build cannot start.
func addNodeHint(ds []diag.Diagnostic) []diag.Diagnostic {
	for i, d := range ds {
		if d.Hint == "" && strings.Contains(d.Message, "executable file not found") {
			ds[i].Hint = "this feature needs Node.js or another host process (generateStaticParams, server components, API routes), which the in-browser playground cannot run"
		}
	}
	return ds
}

func dedupe(ds []diag.Diagnostic) []diag.Diagnostic {
	seen := map[diag.Diagnostic]bool{}
	var out []diag.Diagnostic
	for _, d := range ds {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

// cleanProject empties the project directory except the entries in
// keepOnClean, creating it when missing.
func cleanProject() error {
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(projectRoot)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if keepOnClean[e.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(projectRoot, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// readOutput returns every file under dir by path relative to dir.
func readOutput(dir string) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = data
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	return out, err
}

var textExt = map[string]bool{
	".html": true, ".htm": true, ".js": true, ".mjs": true, ".css": true, ".json": true,
	".svg": true, ".txt": true, ".xml": true, ".map": true, ".webmanifest": true, ".md": true,
}

func (r result) toJS() js.Value {
	g := js.Global()
	obj := g.Get("Object").New()
	obj.Set("ok", r.OK)

	files := g.Get("Object").New()
	names := make([]string, 0, len(r.Files))
	for n := range r.Files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		data := r.Files[n]
		if textExt[strings.ToLower(path.Ext(n))] {
			files.Set(n, string(data))
			continue
		}
		u8 := g.Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(u8, data)
		files.Set(n, u8)
	}
	obj.Set("files", files)

	ds := g.Get("Array").New(len(r.Diagnostics))
	for i, d := range r.Diagnostics {
		o := g.Get("Object").New()
		sev := "error"
		if d.IsWarning() {
			sev = "warning"
		}
		o.Set("severity", sev)
		o.Set("message", d.Message)
		o.Set("file", relToProject(d.File))
		o.Set("line", d.Line)
		o.Set("col", d.Col)
		o.Set("hint", d.Hint)
		o.Set("source", d.Source)
		o.Set("code", string(d.Code))
		ds.SetIndex(i, o)
	}
	obj.Set("diagnostics", ds)

	t := g.Get("Object").New()
	for k, v := range r.Timings {
		t.Set(k, v)
	}
	obj.Set("timingsMs", t)
	obj.Set("log", r.Log)
	return obj
}

// relToProject makes a diagnostic's file name project-relative.
func relToProject(p string) string {
	if p == "" {
		return ""
	}
	if rel, err := filepath.Rel(projectRoot, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return p
}
