package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native"
	"github.com/kratejs/krate/packages/compiler/internal/native/capability"
	"github.com/kratejs/krate/packages/compiler/internal/native/features"
	"github.com/kratejs/krate/packages/compiler/internal/native/gobind"
	"github.com/kratejs/krate/packages/compiler/internal/native/goenv"
	"github.com/kratejs/krate/packages/compiler/internal/native/hostts"
	"github.com/kratejs/krate/packages/compiler/internal/native/runtimes"
)

// NativeManifest is app.json, the contract between the build and the shell
// that runs an app directory. Field names are fixed.
type NativeManifest struct {
	AppID       string            `json:"appId"`
	ProductName string            `json:"productName"`
	Version     string            `json:"version"`
	Window      NativeWindowMeta  `json:"window"`
	Assets      string            `json:"assets"`
	Fallbacks   []NativeFallback  `json:"fallbacks"`
	HostJS      *NativeHostJSMeta `json:"hostjs,omitempty"`
	GoHost      *NativeGoHostMeta `json:"gohost,omitempty"`
	// HostChildren are the host code programs that run in a child process on
	// Node, Bun or Deno (see native.host.runtime and native.host.islands).
	HostChildren []NativeHostChildMeta `json:"hostchildren,omitempty"`
	// Permissions is the SDK permission list. A derived build (see
	// NativeAppOptions.Derive) writes the permissions the code needs and sets
	// StrictPermissions so an empty list means nothing; otherwise it is
	// native.permissions, where an empty list means the SDK defaults. The
	// shell validates the names.
	Permissions       []string `json:"permissions,omitempty"`
	StrictPermissions bool     `json:"strictPermissions,omitempty"`
	// Plugins are the native plugins the app uses; the shell that runs the app
	// has to link them (see native.plugins).
	Plugins []string `json:"plugins,omitempty"`
	// Updater is native.updater: the feed URL template and the Ed25519
	// public key that verifies update files.
	Updater *NativeUpdaterMeta `json:"updater,omitempty"`
	// Memory is the memory profile (native.memory): "balanced", "minimal" or
	// "performance". Features says which optional webview features the app
	// uses (detected from the UI, overridden by native.features); the profiles
	// switch the unused ones off.
	Memory   string              `json:"memory,omitempty"`
	Features *NativeFeaturesMeta `json:"features,omitempty"`
	Debug    bool                `json:"debug"`
}

// NativeFeaturesMeta is the features block of app.json.
type NativeFeaturesMeta = features.Set

// NativeUpdaterMeta is the updater block of app.json.
type NativeUpdaterMeta struct {
	URL       string `json:"url"`
	PublicKey string `json:"publicKey"`
}

// NativeWindowMeta is the main window of app.json.
type NativeWindowMeta struct {
	Title     string `json:"title"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Resizable bool   `json:"resizable"`
	Path      string `json:"path"`
	// Permissions narrows the SDK permissions of the main window's page
	// (native.window.permissions).
	Permissions *[]string `json:"permissions,omitempty"`
	// KeepAlive is native.window.keepAlive.
	KeepAlive bool `json:"keepAlive,omitempty"`
	// ContextMenu is native.window.contextMenu ("" means default).
	ContextMenu string `json:"contextMenu,omitempty"`
}

// NativeFallback maps a dynamic route to the prerendered shell page the shell
// serves for every URL that matches the pattern and has no file of its own.
type NativeFallback struct {
	Pattern string `json:"pattern"`
	File    string `json:"file"`
}

// NativeHostJSMeta locates the TypeScript host bundle.
type NativeHostJSMeta struct {
	Bundle string `json:"bundle"`
	Map    string `json:"map,omitempty"`
	// MemoryMB is native.host.memoryMB: the QuickJS heap limit. 0 means the
	// profile's default.
	MemoryMB int `json:"memoryMB,omitempty"`
}

// NativeHostChildMeta locates one JavaScript host child and lists what it
// serves. Entry is the script and Command the argv that runs it, with
// "{entry}" for the script path and "{sockdir}" for the shell's socket
// directory; a release build replaces both by an executable (Entry set to the
// compiled file, Command empty).
type NativeHostChildMeta struct {
	Runtime string   `json:"runtime"`
	Entry   string   `json:"entry"`
	Map     string   `json:"map,omitempty"`
	Command []string `json:"command,omitempty"`
	Funcs   []string `json:"funcs"`
	Streams []string `json:"streams"`
	Signals []string `json:"signals"`
}

// NativeGoHostMeta locates the Go host binary and lists what it serves.
type NativeGoHostMeta struct {
	Binary  string   `json:"binary"`
	Funcs   []string `json:"funcs"`
	Streams []string `json:"streams"`
	Signals []string `json:"signals"`
}

// NativeAppOptions tune BuildNativeApp.
type NativeAppOptions struct {
	// OutDir is the native output directory (default <root>/dist-native);
	// the app directory is OutDir/app.
	OutDir string
	// Target is "<os>/<arch>" for the Go host (default: this machine).
	Target string
	// Shell asks for a shell binary next to the app directory: a path to
	// copy, "auto" or "" to use KRATE_SHELL or build the monorepo shell.
	Shell     string
	WantShell bool
	// Distribute is set by `krate native build`: a shell that needs no build
	// (see FindPrebuiltShell) is then provided without being asked for.
	Distribute bool
	// SkipGoCompile writes the Go host program but does not compile it.
	SkipGoCompile bool
	// Dev marks a `krate native dev` build: the manifest enables the
	// webview's developer tools.
	Dev bool
	// Derive works out the SDK permissions from the code (capability
	// analysis), checks them against native.permissions and writes the derived
	// list to app.json. `krate native build` sets it; `krate native dev` does
	// not, so dev keeps the configured list.
	Derive bool
}

// NativeAppResult describes the app directory BuildNativeApp wrote.
type NativeAppResult struct {
	// OutDir and AppDir are absolute.
	OutDir, AppDir string
	Manifest       NativeManifest
	// GoHostMain is the generated Go host program, "" without Go host code.
	GoHostMain string
	// HostGroups are the host bundles of the build, one per runtime, as the
	// host build wrote them under .krate/native/host. A release build compiles
	// the child groups (and the "scriptc" group) into executables with
	// runtimes.BuildRelease.
	HostGroups []hostts.Group
	// ShellPath is the shell binary copied or built next to the app.
	ShellPath string
	// DistExe is the renamed shell of the runnable distributable
	// <out>/<os>-<arch>/ (with app/ next to it), "" without a shell.
	DistExe string
	// Notes are non-fatal remarks (for example a shell that could not be built).
	Notes []string
	// Capabilities is the capability analysis of a Derive build.
	Capabilities *capability.Result
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugOf(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		return "app"
	}
	return s
}

// nativeDiagError prints diagnostics the way the host code errors print and
// returns the aggregate build error.
func nativeDiagError(ds []diag.Diagnostic, what string) error {
	errs := make([]error, len(ds))
	for i, d := range ds {
		errs[i] = d
		fmt.Fprintf(os.Stderr, "  %sError:%s %s\n", cRed, cReset, strings.ReplaceAll(diag.FormatDiagnostics([]error{d}), "\n", "\n    "))
	}
	return &multiError{summary: fmt.Sprintf("build failed: %d %s", len(ds), what), errs: errs}
}

// parseNativeTarget validates "<os>/<arch>".
func parseNativeTarget(t string) (goos, goarch string, d *diag.Diagnostic) {
	if t == "" {
		return runtime.GOOS, runtime.GOARCH, nil
	}
	bad := func() (string, string, *diag.Diagnostic) {
		x := diag.New("--target", 1, 1, fmt.Sprintf("unknown native target %q", t), "use <os>/<arch>, for example linux/amd64, darwin/arm64 or windows/amd64", "").WithCode(diag.CodeNativeTarget)
		return "", "", &x
	}
	os_, arch, ok := strings.Cut(t, "/")
	if !ok {
		return bad()
	}
	switch os_ {
	case "macos":
		os_ = "darwin"
	case "windows", "linux", "darwin":
	default:
		return bad()
	}
	switch arch {
	case "amd64", "arm64":
	default:
		return bad()
	}
	return os_, arch, nil
}

// BuildNativeApp builds the project as a native app directory:
//
//	app/app.json
//	app/dist/...            the prerendered UI
//	app/host/host.js(.map)  when the app has TypeScript host code
//	app/gohost/krate-gohost when the app has Go host code
//
// It sets NativeTarget, gates on native.CheckTarget, builds every route as
// static HTML, then adds the host programs and the manifest.
func (b *Builder) BuildNativeApp(opts NativeAppOptions) (*NativeAppResult, error) {
	goos, goarch, derr := parseNativeTarget(opts.Target)
	if derr != nil {
		return nil, nativeDiagError([]diag.Diagnostic{*derr}, "native target error(s)")
	}
	b.NativeTarget = true
	b.nativeShake = opts.Derive
	// The shell serves the files from the app bundle: .br and .gz siblings
	// would only add to its size.
	b.Cfg.Compress = config.CompressConfig{}

	ds, err := native.CheckTarget(b.Root, b.Cfg)
	if err != nil {
		return nil, err
	}
	if len(ds) > 0 {
		return nil, nativeDiagError(ds, "native target error(s)")
	}

	if !opts.Dev {
		pl, _ := native.Analyze(b.Root, b.Cfg)
		if lean, err := native.CheckLean(b.Root, b.Cfg, pl); err == nil {
			printWarningsOnce(os.Stderr, lean)
		}
	}

	outDir := opts.OutDir
	if outDir == "" {
		outDir = "dist-native"
	}
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(b.Root, outDir)
	}
	appDir := filepath.Join(outDir, "app")
	res := &NativeAppResult{OutDir: outDir, AppDir: appDir}
	if err := os.RemoveAll(appDir); err != nil {
		return nil, fmt.Errorf("cleaning %s: %w", appDir, err)
	}
	distDir := filepath.Join(appDir, "dist")
	b.Cfg.OutDir = distDir

	if err := b.BuildAll(); err != nil {
		return nil, err
	}
	fallbacks, err := nativeFallbacks(distDir)
	if err != nil {
		return nil, err
	}
	// The build manifest and other server files are of no use to the shell.
	_ = os.RemoveAll(filepath.Join(distDir, ".krate"))

	eff := nativeEffective(b.Cfg)
	productName := eff.ProductName
	if productName == "" {
		productName = filepath.Base(b.Root)
	}
	appID := eff.AppID
	if appID == "" {
		appID = "com.example." + strings.ReplaceAll(slugOf(productName), "-", "")
		res.Notes = append(res.Notes, "native.appId is not set; using "+appID)
	}
	version := eff.Version
	if version == "" {
		version = "0.1.0"
	}
	m := NativeManifest{
		AppID: appID, ProductName: productName, Version: version,
		Window: NativeWindowMeta{
			Title: productName, Width: eff.Window.Width, Height: eff.Window.Height,
			Resizable: eff.Window.Resizable != nil && *eff.Window.Resizable, Path: "/",
		},
		Assets:      "dist",
		Fallbacks:   fallbacks,
		Permissions: append([]string(nil), eff.Permissions...),
		Memory:      eff.Memory,
		Debug:       opts.Dev,
	}
	detected, err := features.ScanDir(distDir)
	if err != nil {
		return nil, fmt.Errorf("scanning the UI for webview features: %w", err)
	}
	resolved := detected.Resolve(features.Override{
		Media: eff.Features.Media, WebAudio: eff.Features.WebAudio, WebGL: eff.Features.WebGL,
		WebRTC: eff.Features.WebRTC, SpellCheck: eff.Features.SpellCheck,
	})
	m.Features = &resolved
	m.Window.Permissions = eff.Window.Permissions
	m.Window.KeepAlive = eff.Window.KeepAlive
	m.Window.ContextMenu = eff.Window.ContextMenu
	m.Plugins = b.NativePluginNames()
	if !opts.Derive {
		m.Permissions = b.pluginDevPermissions(m.Permissions)
	}
	if opts.Derive {
		cap, ds, err := b.deriveNativePermissions(eff)
		if err != nil {
			return nil, err
		}
		if len(ds) > 0 {
			return nil, nativeDiagError(ds, "native permission error(s)")
		}
		res.Capabilities = cap
		m.Permissions = append([]string{}, cap.Permissions...)
		m.StrictPermissions = true
		if wp := m.Window.Permissions; wp != nil {
			kept := []string{}
			for _, n := range *wp {
				if capability.Covers(m.Permissions, n) {
					kept = append(kept, n)
				}
			}
			m.Window.Permissions = &kept
		}
	}

	if h := b.NativeHost(); h != nil {
		res.HostGroups = h.Groups
		children, notes, err := writeHostChildren(h, appDir, eff.Permissions, b.InspectHost)
		if err != nil {
			return nil, err
		}
		m.HostChildren = children
		res.Notes = append(res.Notes, notes...)
	}
	if eff.Updater != nil {
		m.Updater = &NativeUpdaterMeta{URL: eff.Updater.URL, PublicKey: eff.Updater.PublicKey}
	}

	if h := b.NativeHost(); h != nil && h.HostBundle != "" {
		if err := copyNativeFile(h.HostBundle, filepath.Join(appDir, "host", "host.js")); err != nil {
			return nil, err
		}
		m.HostJS = &NativeHostJSMeta{Bundle: "host/host.js", MemoryMB: eff.Host.MemoryMB}
		if h.SourceMap != "" {
			if _, err := os.Stat(h.SourceMap); err == nil {
				if err := copyNativeFile(h.SourceMap, filepath.Join(appDir, "host", "host.js.map")); err != nil {
					return nil, err
				}
				m.HostJS.Map = "host/host.js.map"
			}
		}
	}

	if g := b.NativeGo(); g != nil && len(g.Glue) > 0 {
		mainPath, err := b.writeGoHostMain(g)
		if err != nil {
			return nil, err
		}
		res.GoHostMain = mainPath
		bin := filepath.Join("gohost", "krate-gohost")
		if goos == "windows" {
			bin += ".exe"
		}
		gh := &NativeGoHostMeta{Binary: filepath.ToSlash(bin), Funcs: []string{}, Streams: []string{}, Signals: []string{}}
		for _, mod := range g.Modules {
			// Build functions run during the build; the shell never serves them.
			skip := map[string]bool{}
			for _, n := range mod.Build {
				skip[mod.ID+"."+n] = true
			}
			for _, f := range mod.Funcs {
				if !skip[f] {
					gh.Funcs = append(gh.Funcs, f)
				}
			}
			gh.Streams = append(gh.Streams, mod.Streams...)
			gh.Signals = append(gh.Signals, mod.Signals...)
		}
		sort.Strings(gh.Funcs)
		sort.Strings(gh.Streams)
		sort.Strings(gh.Signals)
		m.GoHost = gh
		if !opts.SkipGoCompile {
			if _, err := exec.LookPath("go"); err != nil {
				return nil, nativeDiagError([]diag.Diagnostic{needGoDiag("the app has Go host code, which is compiled with the Go toolchain")}, "Go host error(s)")
			}
			if err := b.compileGoHost(filepath.Join(appDir, bin), goos, goarch, !opts.Dev); err != nil {
				return nil, err
			}
		}
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(appDir, "app.json"), append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	res.Manifest = m

	// A shell that needs no build (an explicit one, $KRATE_SHELL, or the one
	// next to the krate executable) is always provided, so the build leaves a
	// runnable distributable; building one from a checkout needs a request.
	want := opts.WantShell || opts.Shell != ""
	if !want && opts.Distribute {
		if p, _, _ := FindPrebuiltShell("", goos, goarch); p != "" || b.nativePlugins != nil {
			want = true
		}
	}
	if want {
		var path, note string
		if b.nativePlugins != nil {
			// The prebuilt shell links no plugins: the app gets its own.
			built, err := b.PluginShell(goos, goarch)
			if err != nil {
				return nil, err
			}
			path = filepath.Join(outDir, ShellFileName(goos))
			if err := copyNativeFile(built, path); err != nil {
				return nil, err
			}
			_ = os.Chmod(path, 0o755)
		} else {
			path, note = b.provideShell(outDir, opts.Shell, goos, goarch)
		}
		res.ShellPath = path
		if note != "" {
			res.Notes = append(res.Notes, note)
		}
		if path != "" {
			dist, err := assembleShellDist(outDir, appDir, path, m.ProductName, goos, goarch)
			if err != nil {
				return nil, fmt.Errorf("assembling the distributable: %w", err)
			}
			res.DistExe = dist
		}
	}
	return res, nil
}

// nativeEffective returns the native config with defaults, or the defaults
// alone when the project has no native block.
func nativeEffective(cfg *config.Config) *config.NativeConfig {
	if e := cfg.EffectiveNative(); e != nil {
		return e
	}
	tmp := config.Config{Native: &config.NativeConfig{}}
	return tmp.EffectiveNative()
}

// nativeFallbacks lists the prerendered shell page of every dynamic route that
// accepts params the build did not enumerate.
func nativeFallbacks(distDir string) ([]NativeFallback, error) {
	data, err := os.ReadFile(ManifestPath(distDir))
	if err != nil {
		return nil, fmt.Errorf("reading the build manifest: %w", err)
	}
	var man Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return nil, fmt.Errorf("reading the build manifest: %w", err)
	}
	out := []NativeFallback{}
	seen := map[string]bool{}
	for _, p := range man.Pages {
		if !isDynamicPattern(p.Route) || p.StaticOnly || !p.DynamicParams || seen[p.Route] {
			continue
		}
		file := strings.TrimPrefix(p.Route, "/") + "/index.html"
		if _, err := os.Stat(filepath.Join(distDir, filepath.FromSlash(file))); err != nil {
			continue
		}
		seen[p.Route] = true
		out = append(out, NativeFallback{Pattern: p.Route, File: file})
	}
	sort.Slice(out, func(i, j int) bool { return compareRouteSpecificity(out[i].Pattern, out[j].Pattern) < 0 })
	return out, nil
}

// writeGoHostMain writes the program that serves the generated glue.
func (b *Builder) writeGoHostMain(g *gobind.Result) (string, error) {
	var sb strings.Builder
	sb.WriteString("// Code generated by krate. DO NOT EDIT.\n\npackage main\n\nimport (\n")
	for i, gl := range g.Glue {
		fmt.Fprintf(&sb, "\tglue%d %q\n", i, gl.ImportPath)
	}
	sb.WriteString("\n\t\"github.com/kratejs/krate/packages/native/gohost\"\n\t\"github.com/kratejs/krate/packages/native/scheme\"\n)\n\n")
	sb.WriteString("func main() {\n\tgohost.Serve(func(r *scheme.Registry) {\n")
	for i := range g.Glue {
		fmt.Fprintf(&sb, "\t\tglue%d.Register(r)\n", i)
	}
	sb.WriteString("\t})\n}\n")
	dir := filepath.Join(b.nativeOutDir(), "gohost")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "main.go")
	if old, err := os.ReadFile(path); err == nil && string(old) == sb.String() {
		return path, nil
	}
	return path, os.WriteFile(path, []byte(sb.String()), 0o644)
}

// compileGoHost builds the Go host program inside the project's Go module.
// strip drops the symbol table and DWARF (release builds); `krate native dev`
// keeps them for stack traces and debuggers.
func (b *Builder) compileGoHost(out, goos, goarch string, strip bool) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	args := []string{"build"}
	if b.DebugGo && !strip {
		// A debugger needs the optimizer and inliner off, and the source
		// paths in the debug info to be the real ones (no -trimpath) so that
		// a breakpoint in src/lib/tasks.go finds its file.
		args = append(args, "-gcflags=all=-N -l")
	} else {
		args = append(args, "-trimpath")
	}
	if strip {
		args = append(args, "-ldflags=-s -w")
	}
	args = append(args, "-o", out, "./.krate/native/gohost")
	cmd := exec.Command("go", args...)
	cmd.Dir = b.Root
	cmd.Env = append(goenv.Env(b.Root), "GOOS="+goos, "GOARCH="+goarch)
	if goos != runtime.GOOS || goarch != runtime.GOARCH {
		cmd.Env = append(cmd.Env, "CGO_ENABLED=0")
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	printf("  %s>%s Compiling the Go host (%s/%s)\n", cGreen, cReset, goos, goarch)
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(buf.String())
		if msg == "" {
			msg = err.Error()
		}
		hint := "the packages the UI imports must compile for the target; run go build ./... in the project for the full output"
		if strings.Contains(msg, "no required module provides package") || strings.Contains(msg, "missing go.sum entry") || strings.Contains(msg, "cannot find module providing package") {
			hint = "run go mod tidy in the project, and go get " + nativeGoModulePath + " if go.mod does not require it"
		}
		return nativeDiagError([]diag.Diagnostic{diag.New("go.mod", 1, 1, "go build of the Go host failed:\n"+indent(trimTail(msg, 20)), hint, "").WithCode(diag.CodeNativeGoBuild)}, "Go host error(s)")
	}
	return nil
}

func indent(s string) string { return "    " + strings.ReplaceAll(s, "\n", "\n    ") }

func trimTail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + "\n..."
}

// provideShell copies or builds a shell binary into outDir. A failure is
// reported as a note, never as a build error.
func (b *Builder) provideShell(outDir, want, goos, goarch string) (string, string) {
	dst := filepath.Join(outDir, ShellFileName(goos))
	path, _, err := FindPrebuiltShell(want, goos, goarch)
	if err != nil {
		return "", err.Error()
	}
	if path != "" {
		if err := copyNativeFile(path, dst); err != nil {
			return "", "could not copy the shell binary: " + err.Error()
		}
		_ = os.Chmod(dst, 0o755)
		return dst, ""
	}
	mod := FindMonorepoNative(b.Root)
	if mod == "" {
		return "", errNoShell("").Error()
	}
	if _, err := os.Stat(filepath.Join(mod, "cmd", "krate-shell")); err != nil {
		return "", errNoShell("packages/native/cmd/krate-shell does not exist in this checkout").Error()
	}
	cmd := exec.Command("go", "build", "-trimpath", "-o", dst, "./cmd/krate-shell")
	cmd.Dir = mod
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Run(); err != nil {
		return "", "could not build the shell: " + trimTail(strings.TrimSpace(buf.String()), 6)
	}
	return dst, ""
}

// FindMonorepoNative returns packages/native of a Krate checkout containing
// root, or "".
func FindMonorepoNative(root string) string {
	dir := root
	for {
		cand := filepath.Join(dir, "packages", "native")
		if data, err := os.ReadFile(filepath.Join(cand, "go.mod")); err == nil && bytes.Contains(data, []byte("module "+nativeGoModulePath)) {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func copyNativeFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if si, err := in.Stat(); err == nil {
		if di, err := os.Stat(dst); err == nil && os.SameFile(si, di) {
			return nil // os.Create would truncate the source
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// needGoDiag is the diagnostic for a build step that needs the go command.
func needGoDiag(why string) diag.Diagnostic {
	return diag.New("go.mod", 1, 1, "the go command was not found on PATH: "+why,
		"install Go (https://go.dev/dl), or remove the Go host code from the app", "").WithCode(diag.CodeNativeNeedGo)
}

// deriveNativePermissions runs the capability analysis of the last native
// build and checks it against the native.permissions allowlist (KR7210).
func (b *Builder) deriveNativePermissions(eff *config.NativeConfig) (*capability.Result, []diag.Diagnostic, error) {
	p, err := native.Analyze(b.Root, b.Cfg)
	if err != nil {
		return nil, nil, err
	}
	opts := capability.Options{GoFiles: b.nativeGoFiles}
	if st := b.nativePlugins; st != nil {
		opts.Plugins = PluginInfos(st.list)
	}
	res, err := capability.Analyze(b.Root, b.Cfg, p, opts)
	if err != nil {
		return nil, nil, err
	}
	if st := b.nativePlugins; st != nil {
		// The SDK areas the plugins' Go code imports have to stay linked.
		sites, err := capability.GoSDKImportsIn(st.out.Dir, []string{"./registry"}, pluginGoEnv(runtime.GOOS, runtime.GOARCH))
		if err != nil {
			return nil, nil, pluginShellError(err.Error(), err)
		}
		res.AddGoSites(sites)
	}
	ds := permissionDiagnostics(res, eff.Permissions)
	return res, ds, nil
}

// writeHostChildren copies the bundles of the child runtimes (node, bun, deno)
// into the app directory and returns their app.json entries. The scriptc
// variant is not copied: development runs that code on QuickJS, and a release
// build compiles the bundle.
func writeHostChildren(h *hostts.Result, appDir string, perms []string, inspect string) ([]NativeHostChildMeta, []string, error) {
	var out []NativeHostChildMeta
	var notes []string
	inspectAddrs := InspectAddrs(inspect, len(h.Groups))
	for _, g := range h.Groups {
		rt := runtimes.Runtime(g.Runtime)
		if !rt.Child() {
			continue
		}
		rel := "host/" + g.Runtime + "/host.mjs"
		if err := copyNativeFile(g.Bundle, filepath.Join(appDir, filepath.FromSlash(rel))); err != nil {
			return nil, nil, err
		}
		cmd := runtimes.Command(rt, perms)
		if len(inspectAddrs) > len(out) {
			cmd = runtimes.WithInspect(cmd, rt, inspectAddrs[len(out)])
		}
		meta := NativeHostChildMeta{
			Runtime: g.Runtime, Entry: rel, Command: cmd,
			Funcs: append([]string{}, g.Funcs...), Streams: append([]string{}, g.Streams...), Signals: append([]string{}, g.Signals...),
		}
		if _, err := os.Stat(g.SourceMap); err == nil {
			if err := copyNativeFile(g.SourceMap, filepath.Join(appDir, filepath.FromSlash(rel+".map"))); err != nil {
				return nil, nil, err
			}
			meta.Map = rel + ".map"
		}
		sort.Strings(meta.Funcs)
		sort.Strings(meta.Streams)
		sort.Strings(meta.Signals)
		out = append(out, meta)
		if _, err := exec.LookPath(meta.Command[0]); err != nil {
			notes = append(notes, fmt.Sprintf("%s is not on PATH: this app directory needs it to start its host code (a release build bundles the runtime)", meta.Command[0]))
		}
	}
	return out, notes, nil
}
