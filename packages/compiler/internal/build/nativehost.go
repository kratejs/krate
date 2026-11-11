package build

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native"
	"github.com/kratejs/krate/packages/compiler/internal/native/gobind"
	"github.com/kratejs/krate/packages/compiler/internal/native/hostts"
)

// NativeHost returns the host code split of the last native build, or nil
// for a web build or a project without TypeScript host code.
func (b *Builder) NativeHost() *hostts.Result { return b.nativeHost }

// NativeGo returns the Go host bindings of the last native build, or nil when
// the UI imports no Go host code.
func (b *Builder) NativeGo() *gobind.Result { return b.nativeGo }

// nativeOutDir is where the native build keeps its generated files.
func (b *Builder) nativeOutDir() string { return filepath.Join(b.Root, ".krate", "native") }

// prepareNativeHost splits the host code of a native build: TypeScript host
// code goes to a host bundle, Go host code gets generated glue, and the UI
// bundlers are armed with stub overlays for both. A web build is untouched.
func (b *Builder) prepareNativeHost() error {
	b.nativeHost, b.nativeGo, b.nativeOverlays = nil, nil, nil
	if !b.NativeTarget {
		return nil
	}
	// Overlays and the page script differ from a web build, so cached web
	// pages must not be replayed (and native pages must not be stored).
	if b.diskCache != nil {
		b.diskCache.enabled = false
	}
	p, err := native.Analyze(b.Root, b.Cfg)
	if err != nil {
		return err
	}
	fail := func(ds []diag.Diagnostic, what string) error {
		errs := make([]error, len(ds))
		for i, d := range ds {
			errs[i] = d
			fmt.Fprintf(os.Stderr, "  %sHost code error:%s %s\n", cRed, cReset, strings.ReplaceAll(diag.FormatDiagnostics([]error{d}), "\n", "\n    "))
		}
		return &multiError{summary: fmt.Sprintf("build failed: %d %s", len(ds), what), errs: errs}
	}
	if len(p.Diagnostics) > 0 {
		return fail(p.Diagnostics, "host code placement error(s)")
	}
	hasTS := false
	for _, m := range p.Modules {
		if m.Rel != "" && !strings.HasSuffix(m.Rel, ".go") {
			hasTS = true
		}
	}
	overlays := map[string]string{}
	if hasTS {
		res, ds, err := hostts.Build(b.Root, b.Cfg, p, hostts.Options{Resolve: b.importResolver().Resolve})
		if err != nil {
			return err
		}
		var errs, warns []diag.Diagnostic
		for _, d := range ds {
			if d.IsWarning() {
				warns = append(warns, d)
			} else {
				errs = append(errs, d)
			}
		}
		if len(warns) > 0 {
			printWarningsOnce(os.Stderr, warns)
		}
		if len(errs) > 0 {
			return fail(errs, "host code error(s)")
		}
		b.nativeHost = res
		for path, src := range res.Overlays {
			overlays[path] = src
		}
	}

	goRes, goOverlays, ds, err := b.prepareNativeGo(p)
	if err != nil {
		return err
	}
	if len(ds) > 0 {
		return fail(ds, "Go host code error(s)")
	}
	b.nativeGo = goRes
	for path, src := range goOverlays {
		overlays[path] = src
	}

	if len(overlays) > 0 {
		appMod, err := b.writeNativeAppModule()
		if err != nil {
			return err
		}
		for path, src := range overlays {
			rel, err := filepath.Rel(filepath.Dir(path), appMod)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if !strings.HasPrefix(rel, ".") {
				rel = "./" + rel
			}
			overlays[path] = bootStub(src, rel)
		}
		b.nativeOverlays = overlays
		if b.importResolver().Resolve(filepath.Join(b.Root, "index.ts"), nativeAppPkg) == "" {
			return fail([]diag.Diagnostic{diag.New("package.json", 1, 1, "the UI calls host code, but "+nativeAppPkg+" cannot be resolved from this project",
				"install it next to @krate/core: npm i "+nativeAppPkg, "").WithCode(diag.CodeNativeAppMissing)}, "native setup error(s)")
		}
	}
	return nil
}

// nativeClientEntry is bundled with @krate/app into .krate/native/app.js, the
// module the host stubs import their call, stream and signal functions from.
// It installs, on first use, a host client whose signals are Krate runtime
// signals, so reading a host signal in JSX or an effect is tracked and the UI
// updates when the host pushes a new value. Stubs create their signals lazily
// and never call at import time, so the client is installed before the first
// host access.
const nativeClientEntry = `import { getClient, setClient, createClient } from "@krate/app";

let booted = false;
function boot() {
  if (booted) return;
  booted = true;
  // The Krate runtime chunk defines createSignal as a global.
  setClient(createClient({ createSignal: globalThis.createSignal }));
}

export function call(fn, args, opts) {
  boot();
  return getClient().call(fn, args, opts);
}

export function stream(fn, args, opts) {
  boot();
  return getClient().stream(fn, args, opts);
}

export function signal(name) {
  boot();
  return getClient().signal(name);
}
`

var stubAppImportRe = regexp.MustCompile(`(?m)^(import\s*\{)([^}]*)(\}\s*from\s*)"@krate/app"`)

// stubWrapped maps the @krate/app exports the generated stubs use to the
// wrapper's. The wrapper's names differ from the package's on purpose: the
// bundler places every module in one scope, where two functions of one name
// would clash.
var stubWrapped = map[string]string{"call": "__krateHostCall", "stream": "__krateHostStream", "signal": "__krateHostSignal"}

// bootStub points the @krate/app import of a generated stub at the wrapper,
// given as an import specifier relative to the stub.
// An import that names anything besides call, stream and signal is the
// project's own and stays untouched.
func bootStub(src, spec string) string {
	return stubAppImportRe.ReplaceAllStringFunc(src, func(m string) string {
		sub := stubAppImportRe.FindStringSubmatch(m)
		var parts []string
		for _, part := range strings.Split(sub[2], ",") {
			f := strings.Fields(part)
			if len(f) == 0 {
				continue
			}
			wrapped, ok := stubWrapped[f[0]]
			if !ok {
				return m
			}
			if len(f) == 3 && f[1] == "as" {
				parts = append(parts, wrapped+" as "+f[2])
			} else {
				parts = append(parts, wrapped+" as "+f[0])
			}
		}
		return sub[1] + " " + strings.Join(parts, ", ") + " " + sub[3] + strconv.Quote(spec)
	})
}

// writeNativeAppModule bundles the client entry with @krate/app into one
// self-contained module and returns its path. The bundle keeps everything in
// a function scope, so the page bundler only sees three exports and none of
// the package's internals (it would otherwise inline them into every page's
// scope, name by name).
func (b *Builder) writeNativeAppModule() (string, error) {
	res := api.Build(api.BuildOptions{
		Stdin:      &api.StdinOptions{Contents: nativeClientEntry, ResolveDir: b.Root, Sourcefile: "krate-native-client.js", Loader: api.LoaderJS},
		Bundle:     true,
		Format:     api.FormatIIFE,
		GlobalName: "__krateHost",
		Platform:   api.PlatformBrowser,
		Target:     api.ES2020,
		Write:      false,
		LogLevel:   api.LogLevelSilent,
	})
	if len(res.Errors) > 0 || len(res.OutputFiles) == 0 {
		msg := "bundling " + nativeAppPkg + " failed"
		if len(res.Errors) > 0 {
			msg += ": " + res.Errors[0].Text
		}
		return "", nativeDiagError([]diag.Diagnostic{diag.New("package.json", 1, 1, msg, "check that "+nativeAppPkg+" is installed and builds", "").WithCode(diag.CodeNativeAppMissing)}, "native setup error(s)")
	}
	src := string(res.OutputFiles[0].Contents) + `
export function __krateHostCall(fn, args, opts) { return __krateHost.call(fn, args, opts); }
export function __krateHostStream(fn, args, opts) { return __krateHost.stream(fn, args, opts); }
export function __krateHostSignal(name) { return __krateHost.signal(name); }
`
	path := filepath.Join(b.nativeOutDir(), "app.js")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if old, err := os.ReadFile(path); err == nil && string(old) == src {
		return path, nil
	}
	return path, os.WriteFile(path, []byte(src), 0o644)
}

// nativeAppPkg is the package the generated host stubs import.
const nativeAppPkg = "@krate/app"

// prepareNativeGo finds the .go files the UI imports, generates their glue,
// stubs and declarations, and returns the overlays that make the UI bundlers
// read the stub in place of each file.
func (b *Builder) prepareNativeGo(p *native.Placement) (*gobind.Result, map[string]string, []diag.Diagnostic, error) {
	imps, err := hostts.GoImports(b.Root, b.Cfg, p, hostts.Options{Resolve: b.importResolver().Resolve})
	if err != nil {
		return nil, nil, nil, err
	}
	if len(imps) == 0 {
		return nil, nil, nil, nil
	}
	var ds []diag.Diagnostic
	var in []gobind.Import
	for _, imp := range imps {
		if imp.Namespace {
			ds = append(ds, diag.New(imp.From, imp.Line, imp.Col, "a namespace import or `export *` of "+filepath.Base(imp.File)+" hides the names the UI uses",
				"import the functions and signals by name: import { List, Count } from \"./"+filepath.Base(imp.File)+"\"", "").WithCode(diag.CodeNativeGoNamespace))
			continue
		}
		in = append(in, gobind.Import{File: imp.File, Names: imp.Names, From: imp.From, Line: imp.Line, Col: imp.Col})
	}
	if len(ds) > 0 {
		return nil, nil, ds, nil
	}
	if !b.onlyGoComponents(imps) {
		if d := checkNativeGoModule(b.Root); d != nil {
			return nil, nil, []diag.Diagnostic{*d}, nil
		}
	}
	res, ds, err := gobind.Generate(b.Root, in, b.nativeOutDir())
	if err != nil || len(ds) > 0 {
		return nil, nil, ds, err
	}
	overlays := map[string]string{}
	for _, m := range res.Modules {
		if m.Stub == "" {
			continue
		}
		data, err := os.ReadFile(m.Stub)
		if err != nil {
			return nil, nil, nil, err
		}
		src := string(data)
		// Go components imported from the same file stay components; the
		// stub only has to export the names so the bundle links.
		for _, c := range m.Components {
			src += "export const " + c + " = () => null;\n"
		}
		overlays[filepath.Clean(m.Source)] = src
	}
	return res, overlays, nil, nil
}

// onlyGoComponents reports whether every imported name is a Go component
// (so no host glue is needed).
func (b *Builder) onlyGoComponents(imps []hostts.GoImport) bool {
	for _, imp := range imps {
		comps := map[string]bool{}
		for _, c := range b.cachedGoComponents(imp.File) {
			comps[c.Name] = true
		}
		for _, n := range imp.Names {
			if !comps[n] {
				return false
			}
		}
	}
	return true
}

// nativeGoModulePath is the module the generated glue and the Go host import.
const nativeGoModulePath = "github.com/kratejs/krate/packages/native"

// checkNativeGoModule checks that the project's go.mod exists and requires
// the native host module.
func checkNativeGoModule(root string) *diag.Diagnostic {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		d := diag.New("go.mod", 1, 1, "the UI imports Go host code, but the project has no go.mod",
			"run go mod init <module> in the project, then go get "+nativeGoModulePath, "").WithCode(diag.CodeNativeGoModule)
		return &d
	}
	if !goModRequires(string(data), nativeGoModulePath) {
		d := diag.New("go.mod", 1, 1, "go.mod does not require "+nativeGoModulePath+", which the generated Go host needs",
			"run go get "+nativeGoModulePath, "").WithCode(diag.CodeNativeGoModule)
		return &d
	}
	return nil
}

// goModRequires reports whether a go.mod requires module path.
func goModRequires(gomod, path string) bool {
	inBlock := false
	for _, line := range strings.Split(gomod, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch {
		case inBlock && f[0] == ")":
			inBlock = false
		case inBlock:
			if f[0] == path {
				return true
			}
		case f[0] == "require" && len(f) >= 2 && f[1] == "(":
			inBlock = true
		case f[0] == "require" && len(f) >= 2 && f[1] == path:
			return true
		}
	}
	return false
}
