package build

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native"
	"github.com/kratejs/krate/packages/compiler/internal/native/capability"
	"github.com/kratejs/krate/packages/compiler/internal/native/hostts"
	"github.com/kratejs/krate/packages/compiler/internal/native/pkg"
	"github.com/kratejs/krate/packages/compiler/internal/version"
)

// A release build links one executable per target: the platform layer, the
// scheme handler, the SDK areas the app reaches, the app's Go host code
// (registered in process) and, only when the app has TypeScript host code, the
// QuickJS host. The built UI, the host bundle and the manifest travel inside it
// as one embedded zip archive (go:embed rejects the "[id]" file names dynamic
// routes produce, and the archive keeps the assets compressed).
//
// Generated main package, in the app's own Go module when it has Go host code
// (.krate/native/release/pkg) and in a module of its own otherwise
// (.krate/native/release/mod):
//
//	//go:embed app.zip
//	var appZip []byte
//	func init() { runtime.LockOSThread() }
//	func main() { shell.Main(shell.MustZipFS(appZip), func(r *scheme.Registry) { glue0.Register(r) }) }
//
// The generated module requires github.com/kratejs/krate/packages/native: a
// replace to packages/native inside the Krate monorepo, otherwise the version
// matching this compiler (NativeModuleVersion).

//go:embed nativemod/go.mod.txt
var nativeModGoMod string

//go:embed nativemod/go.sum.txt
var nativeModGoSum string

// NativeModuleVersion is the version of the native Go module a release build
// of an app without Go code requires outside the monorepo. It is the
// compiler's own version ("v" + version, the module is tagged
// packages/native/v<version> with every release); KRATE_NATIVE_VERSION
// overrides it. A development build of the compiler has none.
func NativeModuleVersion() (string, bool) {
	if v := os.Getenv("KRATE_NATIVE_VERSION"); v != "" {
		return "v" + strings.TrimPrefix(v, "v"), true
	}
	v := version.Value
	if v == "" || v == "dev" || !nativeSemver.MatchString(strings.TrimPrefix(v, "v")) {
		return "", false
	}
	return "v" + strings.TrimPrefix(v, "v"), true
}

var nativeSemver = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// NativeReleaseOptions tune BuildNativeRelease.
type NativeReleaseOptions struct {
	// OutDir is the native output directory (default <root>/dist-native).
	OutDir string
	// Targets are "<os>/<arch>" platforms; default this machine.
	Targets []string
	// ExtraTags are go build tags added to the SDK shaking tags (tests use
	// krate_devcontrol to drive a release executable through --dev).
	ExtraTags []string
	// KeepSymbols leaves the symbol table and DWARF in the executables
	// (no -s -w), so go tool nm and debuggers work.
	KeepSymbols bool
}

// NativeBinary is one executable of a release build.
type NativeBinary struct {
	Target string `json:"target"`
	// Path is absolute; the report holds it relative to the output directory.
	Path string `json:"-"`
	Size int64  `json:"size"`
}

// NativeReleaseResult describes a release build.
type NativeReleaseResult struct {
	OutDir string
	// Compiled is true when the build produced executables; false for the
	// prebuilt-shell layout (AppDir).
	Compiled bool
	Binaries []NativeBinary
	AppDir   string
	Manifest NativeManifest
	Report   *NativeReport
	// Tags are the go build tags of the executables (the shaken SDK areas).
	Tags []string
	// PGO is the profile passed to go build, "" when none.
	PGO   string
	Notes []string
}

// NativeReport is dist-native/report.json.
type NativeReport struct {
	AppID       string `json:"appId"`
	ProductName string `json:"productName"`
	Version     string `json:"version"`
	// Layout is "executable" or "app-dir" (native.release.compile never, or
	// auto without Go).
	Layout string `json:"layout"`
	// Permissions are written to app.json, each with the sites that need it.
	Permissions []NativeReportPermission `json:"permissions"`
	// Allowlist is native.permissions when set.
	Allowlist []string `json:"allowlist,omitempty"`
	// WindowPermissions is the main window's scope, when set.
	WindowPermissions *[]string `json:"windowPermissions,omitempty"`
	// Areas lists the SDK areas the executables link and leave out.
	Areas NativeReportAreas `json:"areas"`
	// GoImports are the Go host imports of SDK areas (they decide linking, not
	// permissions).
	GoImports []capability.Site `json:"goImports,omitempty"`
	HostJS    bool              `json:"hostJs"`
	GoHost    bool              `json:"goHost"`
	// Tags are the go build tags of the executables.
	Tags    []string       `json:"tags,omitempty"`
	PGO     string         `json:"pgo,omitempty"`
	Targets []NativeTarget `json:"targets,omitempty"`
}

// NativeReportPermission is a permission with the code that needs it.
type NativeReportPermission struct {
	Name  string            `json:"name"`
	Sites []capability.Site `json:"sites"`
}

// NativeReportAreas lists SDK areas.
type NativeReportAreas struct {
	Linked []string `json:"linked"`
	Shaken []string `json:"shaken"`
}

// NativeTarget is one executable in the report.
type NativeTarget struct {
	Target string `json:"target"`
	// Binary is relative to the output directory.
	Binary string `json:"binary"`
	Size   int64  `json:"size"`
	// Host lists the compiled host children (node, bun, deno, scriptc) that
	// must stay next to the executable, relative to the output directory.
	Host []string `json:"host,omitempty"`
}

// NativeGoRunner runs the go command of a release build; tests replace it.
var NativeGoRunner = func(dir string, env []string, args ...string) ([]byte, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = env
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

func goOnPath() bool {
	_, err := exec.LookPath("go")
	return err == nil
}

// BuildNativeRelease implements `krate native build --release`.
func (b *Builder) BuildNativeRelease(opts NativeReleaseOptions) (*NativeReleaseResult, error) {
	var targets [][2]string
	seen := map[string]bool{}
	for _, t := range opts.Targets {
		goos, goarch, derr := parseNativeTarget(t)
		if derr != nil {
			return nil, nativeDiagError([]diag.Diagnostic{*derr}, "native target error(s)")
		}
		if k := goos + "/" + goarch; !seen[k] {
			seen[k] = true
			targets = append(targets, [2]string{goos, goarch})
		}
	}
	if len(targets) == 0 {
		targets = [][2]string{{runtime.GOOS, runtime.GOARCH}}
	}
	outDir := opts.OutDir
	if outDir == "" {
		outDir = "dist-native"
	}
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(b.Root, outDir)
	}
	eff := nativeEffective(b.Cfg)
	mode := eff.Release.Compile
	haveGo := goOnPath()
	if mode == "always" && !haveGo {
		return nil, nativeDiagError([]diag.Diagnostic{needGoDiag("native.release.compile is \"always\"")}, "native release error(s)")
	}
	res := &NativeReleaseResult{OutDir: outDir}
	compiled := mode != "never" && haveGo
	if mode == "auto" && !haveGo {
		res.Notes = append(res.Notes, "the go command was not found; building the prebuilt-shell layout (dist-native/app) instead of an executable")
	}

	if !compiled {
		// The prebuilt-shell layout. With several targets the Go host child (if
		// any) is built for the first.
		first := targets[0]
		if len(targets) > 1 {
			res.Notes = append(res.Notes, "the prebuilt-shell layout holds one platform; using "+first[0]+"/"+first[1])
		}
		ar, err := b.BuildNativeApp(NativeAppOptions{OutDir: outDir, Target: first[0] + "/" + first[1], Derive: true})
		if err != nil {
			return nil, err
		}
		res.AppDir, res.Manifest = ar.AppDir, ar.Manifest
		res.Notes = append(res.Notes, ar.Notes...)
		res.Report = b.nativeReport(ar, "app-dir", nil, nil, "")
		return res, b.writeNativeReport(outDir, res)
	}

	stage := filepath.Join(b.nativeOutDir(), "release", "stage")
	ar, err := b.BuildNativeApp(NativeAppOptions{OutDir: stage, Derive: true, SkipGoCompile: true})
	if err != nil {
		return nil, err
	}
	res.Manifest = ar.Manifest
	res.Notes = append(res.Notes, ar.Notes...)
	hostPlan, err := planReleaseHostChildren(ar)
	if err != nil {
		return nil, err
	}
	res.Manifest = ar.Manifest
	hasGo := ar.GoHostMain != ""
	hasTS := ar.Manifest.HostJS != nil
	res.Tags = append(ar.Capabilities.Tags(hasTS), opts.ExtraTags...)

	buildDir, pkgArg, extra, err := b.writeReleasePackage(ar, hasGo)
	if err != nil {
		return nil, err
	}

	if p := b.releasePGO(eff, &res.Notes); p != "" {
		res.PGO = p
	}
	env := os.Environ()
	if !hasGo {
		env = append(env, "GOWORK=off", "GOFLAGS=-mod=mod")
	}
	name := fileSafe(ar.Manifest.ProductName)
	hostFiles := map[string][]string{}
	for _, t := range targets {
		goos, goarch := t[0], t[1]
		files, err := b.buildReleaseHostChildren(hostPlan, goos, goarch, outDir, ar)
		if err != nil {
			return nil, err
		}
		hostFiles[goos+"/"+goarch] = files
		if goos == "windows" {
			// The Go linker picks the .syso up from the main package for this
			// target only (icon, version information, DPI-aware manifest).
			syso := filepath.Join(buildDir, "rsrc_windows_"+goarch+".syso")
			if pkgArg != "." {
				syso = filepath.Join(b.Root, filepath.FromSlash(strings.TrimPrefix(pkgArg, "./")), "rsrc_windows_"+goarch+".syso")
			}
			if err := pkg.WindowsResources(b.Root, b.Cfg, syso); err != nil {
				res.Notes = append(res.Notes, "no Windows resources (icon, version information): "+err.Error())
			}
		}
		exe := name
		if goos == "windows" {
			exe += ".exe"
		}
		out := filepath.Join(outDir, goos+"-"+goarch, exe)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		_ = os.Remove(out)
		args := releaseGoArgs(goos, ar.Manifest, res.Tags, res.PGO, out, pkgArg, extra, !opts.KeepSymbols)
		printf("  %s>%s Compiling %s (%s/%s)\n", cGreen, cReset, exe, goos, goarch)
		tenv := append(append([]string(nil), env...), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
		if msg, err := NativeGoRunner(buildDir, tenv, args...); err != nil {
			text := strings.TrimSpace(string(msg))
			if text == "" {
				text = err.Error()
			}
			hint := "the Go host packages must compile for " + goos + "/" + goarch + " with CGO_ENABLED=0; run go build ./... in the project for the full output"
			if strings.Contains(text, "no required module provides package") || strings.Contains(text, "missing go.sum entry") || strings.Contains(text, "cannot find module providing package") {
				hint = "run go mod tidy in the project, and go get " + nativeGoModulePath + " if go.mod does not require it"
			}
			if strings.Contains(text, "unknown revision") || strings.Contains(text, "invalid version") {
				hint = "the native module version " + nativeModuleRequire(hasGo) + " is not published; set KRATE_NATIVE_VERSION to a released version"
			}
			return nil, nativeDiagError([]diag.Diagnostic{diag.New("go.mod", 1, 1, "go build of the release executable failed:\n"+indent(trimTail(text, 25)), hint, "").WithCode(diag.CodeNativeReleaseBuild)}, "native release error(s)")
		}
		st, err := os.Stat(out)
		if err != nil {
			return nil, err
		}
		res.Binaries = append(res.Binaries, NativeBinary{Target: goos + "/" + goarch, Path: out, Size: st.Size()})
	}
	res.Compiled = true
	res.Report = b.nativeReport(ar, "executable", res.Binaries, res.Tags, res.PGO)
	for i := range res.Report.Targets {
		res.Report.Targets[i].Host = hostFiles[res.Report.Targets[i].Target]
	}
	return res, b.writeNativeReport(outDir, res)
}

func nativeModuleRequire(hasGo bool) string {
	if v, ok := NativeModuleVersion(); ok && !hasGo {
		return v
	}
	return "(see go.mod)"
}

// releaseGoArgs builds the go build command line.
func releaseGoArgs(goos string, m NativeManifest, tags []string, pgo, out, pkg string, extra []string, strip bool) []string {
	ld := ""
	if strip {
		ld = "-s -w"
	}
	if goos == "windows" {
		ld += " -H windowsgui"
	}
	ld = strings.TrimSpace(ld)
	info := strings.NewReplacer("'", "", "\n", " ").Replace(m.ProductName + " " + m.Version)
	ld += " -X '" + nativeGoModulePath + "/shell.BuildInfo=" + info + "'"
	args := append([]string{"build", "-trimpath", "-ldflags=" + ld}, extra...)
	if len(tags) > 0 {
		args = append(args, "-tags="+strings.Join(tags, ","))
	}
	if pgo != "" {
		args = append(args, "-pgo="+pgo)
	}
	return append(args, "-o", out, pkg)
}

// releasePGO returns the absolute profile path when the file exists.
func (b *Builder) releasePGO(eff *config.NativeConfig, notes *[]string) string {
	p := eff.Release.PGO
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(b.Root, p)
	}
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		if b.Cfg.Native != nil && b.Cfg.Native.Release.PGO != "" {
			*notes = append(*notes, "native.release.pgo "+eff.Release.PGO+" does not exist; building without profile-guided optimization (krate native profile creates it)")
		}
		return ""
	}
	return p
}

var unsafeName = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)

func fileSafe(s string) string {
	s = strings.TrimSpace(unsafeName.ReplaceAllString(s, "-"))
	s = strings.Trim(s, ". ")
	if s == "" {
		return "app"
	}
	return s
}

// writeReleasePackage writes the generated main package and the embedded
// archive. It returns the package directory, the directory to run go build in
// and the package argument.
func (b *Builder) writeReleasePackage(ar *NativeAppResult, hasGo bool) (buildDir, pkgArg string, extra []string, err error) {
	var pkgDir string
	base := filepath.Join(b.nativeOutDir(), "release")
	name := "mod"
	if hasGo {
		name = "pkg"
	}
	pkgDir = filepath.Join(base, name)
	if err = os.RemoveAll(pkgDir); err != nil {
		return
	}
	if err = os.MkdirAll(pkgDir, 0o755); err != nil {
		return
	}
	if err = writeAppZip(ar, filepath.Join(pkgDir, "app.zip")); err != nil {
		return
	}
	var glues []string
	if hasGo {
		for _, g := range b.NativeGo().Glue {
			glues = append(glues, g.ImportPath)
		}
	}
	if err = os.WriteFile(filepath.Join(pkgDir, "main.go"), []byte(releaseMain(glues)), 0o644); err != nil {
		return
	}
	if hasGo {
		extra, err = b.writeReleaseModfile(base)
		return b.Root, "./" + filepath.ToSlash(mustRel(b.Root, pkgDir)), extra, err
	}
	if err = b.writeReleaseModule(pkgDir); err != nil {
		return
	}
	return pkgDir, ".", nil, nil
}

func mustRel(base, p string) string {
	r, err := filepath.Rel(base, p)
	if err != nil {
		return p
	}
	return r
}

// releaseMain is the generated main package.
func releaseMain(glues []string) string {
	var sb strings.Builder
	sb.WriteString("// Code generated by krate. DO NOT EDIT.\n\npackage main\n\nimport (\n\t_ \"embed\"\n\t\"runtime\"\n\n")
	for i, g := range glues {
		fmt.Fprintf(&sb, "\tglue%d %q\n", i, g)
	}
	if len(glues) > 0 {
		fmt.Fprintf(&sb, "\t%q\n", nativeGoModulePath+"/scheme")
	}
	fmt.Fprintf(&sb, "\t%q\n)\n\n", nativeGoModulePath+"/shell")
	sb.WriteString("//go:embed app.zip\nvar appZip []byte\n\n")
	sb.WriteString("// The platform layer needs the main goroutine on the main OS thread.\nfunc init() { runtime.LockOSThread() }\n\n")
	if len(glues) == 0 {
		sb.WriteString("func main() { shell.Main(shell.MustZipFS(appZip), nil) }\n")
		return sb.String()
	}
	sb.WriteString("func main() {\n\tshell.Main(shell.MustZipFS(appZip), func(r *scheme.Registry) {\n")
	for i := range glues {
		fmt.Fprintf(&sb, "\t\tglue%d.Register(r)\n", i)
	}
	sb.WriteString("\t})\n}\n")
	return sb.String()
}

// writeReleaseModfile gives the release build its own copy of the project's
// go.mod and go.sum (go build -modfile), completed with the requirements and
// checksums of the native module's dependencies, which the project's own
// go.mod does not list until it is tidied for release builds. The project's
// files stay untouched. With an active go.work there is no copy: the
// workspace owns the module graph.
func (b *Builder) writeReleaseModfile(dir string) ([]string, error) {
	out, _ := exec.Command("go", "env", "GOWORK").Output()
	if w := strings.TrimSpace(string(out)); w != "" && w != "off" {
		return nil, nil
	}
	gomod, err := os.ReadFile(filepath.Join(b.Root, "go.mod"))
	if err != nil {
		return nil, err
	}
	sum, _ := os.ReadFile(filepath.Join(b.Root, "go.sum"))
	nativeSum := nativeModGoSum
	if mod := FindMonorepoNative(b.Root); mod != "" {
		if data, err := os.ReadFile(filepath.Join(mod, "go.sum")); err == nil {
			nativeSum = string(data)
		}
	}
	merged := mergeLines(string(sum), nativeSum)
	modfile := filepath.Join(dir, "go.release.mod")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(modfile, gomod, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.release.sum"), []byte(merged), 0o644); err != nil {
		return nil, err
	}
	return []string{"-modfile=" + modfile, "-mod=mod"}, nil
}

// mergeLines joins the lines of two go.sum files without duplicates, sorted.
func mergeLines(a, b string) string {
	set := map[string]bool{}
	for _, l := range strings.Split(a+"\n"+b, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			set[l] = true
		}
	}
	lines := make([]string, 0, len(set))
	for l := range set {
		lines = append(lines, l)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

// writeReleaseModule writes go.mod and go.sum of the module that holds the
// generated main of an app without Go host code.
func (b *Builder) writeReleaseModule(dir string) error {
	mod := nativeModGoMod
	var require, replace string
	native := FindMonorepoNative(b.Root)
	if native == "" {
		// A local checkout of the native module, for development outside the monorepo.
		if d := os.Getenv("KRATE_NATIVE_DIR"); d != "" {
			if data, err := os.ReadFile(filepath.Join(d, "go.mod")); err == nil && bytes.Contains(data, []byte("module "+nativeGoModulePath)) {
				native = d
			}
		}
	}
	if native != "" {
		require = "v0.0.0"
		replace = "\nreplace " + nativeGoModulePath + " => " + filepath.ToSlash(native) + "\n"
	} else {
		v, ok := NativeModuleVersion()
		if !ok {
			return nativeDiagError([]diag.Diagnostic{diag.New("go.mod", 1, 1,
				"this build of krate has no release version, so the version of "+nativeGoModulePath+" to link is unknown",
				"set KRATE_NATIVE_VERSION to a released version (for example v0.5.0), or use a released krate", "").WithCode(diag.CodeNativeModuleVersion)}, "native release error(s)")
		}
		require = v
	}
	lines := strings.Split(mod, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "module ") {
			lines[i] = "module krate.local/release\n\nrequire " + nativeGoModulePath + " " + require
		}
	}
	gomod := strings.Join(lines, "\n") + replace
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "go.sum"), []byte(nativeModGoSum), 0o644)
}

// writeAppZip packs the app directory into a zip, with app.json rewritten for
// the executable (the Go host is linked in, so it has no gohost section).
func writeAppZip(ar *NativeAppResult, path string) error {
	m := ar.Manifest
	m.GoHost = nil
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	stamp := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	add := func(name string, data []byte, compress bool) error {
		h := &zip.FileHeader{Name: name, Method: zip.Store, Modified: stamp}
		if compress {
			h.Method = zip.Deflate
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	if err := add("app.json", append(manifest, '\n'), true); err != nil {
		f.Close()
		return err
	}
	var files []string
	err = filepath.WalkDir(ar.AppDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			rel := filepath.ToSlash(mustRel(ar.AppDir, p))
			if rel != "app.json" {
				files = append(files, rel)
			}
		}
		return nil
	})
	if err != nil {
		f.Close()
		return err
	}
	sort.Strings(files)
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(ar.AppDir, filepath.FromSlash(rel)))
		if err != nil {
			f.Close()
			return err
		}
		if err := add(rel, data, !alreadyCompressed(rel)); err != nil {
			f.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func alreadyCompressed(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".woff", ".woff2", ".zip", ".gz", ".br", ".mp3", ".mp4", ".webm", ".ogg":
		return true
	}
	return false
}

// nativeReport assembles report.json from a build.
func (b *Builder) nativeReport(ar *NativeAppResult, layout string, bins []NativeBinary, tags []string, pgo string) *NativeReport {
	m := ar.Manifest
	r := &NativeReport{
		AppID: m.AppID, ProductName: m.ProductName, Version: m.Version, Layout: layout,
		HostJS: m.HostJS != nil, GoHost: ar.GoHostMain != "", Tags: tags, PGO: mustRelOrSelf(b.Root, pgo),
		Permissions: []NativeReportPermission{},
	}
	if b.Cfg.Native != nil {
		r.Allowlist = append([]string(nil), b.Cfg.Native.Permissions...)
	}
	r.WindowPermissions = m.Window.Permissions
	if c := ar.Capabilities; c != nil {
		for _, p := range c.Permissions {
			entry := NativeReportPermission{Name: p, Sites: append([]capability.Site{}, c.Needs[p]...)}
			if p == "fs" {
				entry.Sites = append(entry.Sites, c.Needs["fs:read"]...)
			}
			r.Permissions = append(r.Permissions, entry)
		}
		r.Areas = NativeReportAreas{Linked: nonNil(c.Linked()), Shaken: nonNil(c.Shaken())}
		r.GoImports = c.GoSites
	}
	for _, bin := range bins {
		r.Targets = append(r.Targets, NativeTarget{Target: bin.Target, Binary: bin.Path, Size: bin.Size})
	}
	return r
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func mustRelOrSelf(root, p string) string {
	if p == "" {
		return ""
	}
	return filepath.ToSlash(mustRel(root, p))
}

// writeNativeReport writes dist-native/report.json.
func (b *Builder) writeNativeReport(outDir string, res *NativeReleaseResult) error {
	for i := range res.Report.Targets {
		res.Report.Targets[i].Binary = filepath.ToSlash(mustRel(outDir, res.Binaries[i].Path))
	}
	data, err := json.MarshalIndent(res.Report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "report.json"), append(data, '\n'), 0o644)
}

// AnalyzeNativeCapabilities runs the capability analysis without building:
// the SDK permissions the project's code needs, the areas it links, and the
// KR7210 diagnostics for permissions native.permissions does not allow.
func (b *Builder) AnalyzeNativeCapabilities() (*capability.Result, []diag.Diagnostic, error) {
	p, err := native.Analyze(b.Root, b.Cfg)
	if err != nil {
		return nil, nil, err
	}
	imps, err := hostts.GoImports(b.Root, b.Cfg, p, hostts.Options{Resolve: b.importResolver().Resolve})
	if err != nil {
		return nil, nil, err
	}
	var goFiles []string
	seen := map[string]bool{}
	for _, imp := range imps {
		if !seen[imp.File] {
			seen[imp.File] = true
			goFiles = append(goFiles, imp.File)
		}
	}
	if len(goFiles) > 0 && !goOnPath() {
		goFiles = nil // the Go imports cannot be listed without go
	}
	res, err := capability.Analyze(b.Root, b.Cfg, p, capability.Options{GoFiles: goFiles})
	if err != nil {
		return nil, nil, err
	}
	return res, permissionDiagnostics(res, nativeEffective(b.Cfg).Permissions), nil
}

// permissionDiagnostics reports each permission the code needs that the
// allowlist does not grant (KR7210). An empty allowlist grants everything.
func permissionDiagnostics(res *capability.Result, allow []string) []diag.Diagnostic {
	if len(allow) == 0 {
		return nil
	}
	var ds []diag.Diagnostic
	perms := make([]string, 0, len(res.Needs))
	for perm := range res.Needs {
		perms = append(perms, perm)
	}
	sort.Strings(perms)
	for _, perm := range perms {
		if capability.Covers(allow, perm) {
			continue
		}
		for _, s := range res.Needs[perm] {
			ds = append(ds, diag.New(s.File, s.Line, s.Col,
				fmt.Sprintf("%s needs the %q permission, which native.permissions does not allow", s.Symbol, perm),
				fmt.Sprintf("add %q to native.permissions in the config, or remove the call", perm), "").WithCode(diag.CodeNativePermission))
		}
	}
	return ds
}

// FormatNativeCapabilities renders the capability report that `krate native
// check` and `krate check` print.
func FormatNativeCapabilities(res *capability.Result, allow []string) string {
	var sb strings.Builder
	if len(res.Permissions) == 0 {
		sb.WriteString("permissions: none needed (app.json grants nothing)\n")
	} else {
		fmt.Fprintf(&sb, "permissions: %d (derived from the code)\n", len(res.Permissions))
		for _, p := range res.Permissions {
			sites := append([]capability.Site{}, res.Needs[p]...)
			if p == "fs" {
				sites = append(sites, res.Needs["fs:read"]...)
			}
			fmt.Fprintf(&sb, "  %s\n", p)
			for i, s := range sites {
				if i == 3 {
					fmt.Fprintf(&sb, "    ... and %d more\n", len(sites)-3)
					break
				}
				fmt.Fprintf(&sb, "    %s:%d  %s\n", s.File, s.Line, s.Symbol)
			}
		}
	}
	if len(allow) > 0 {
		fmt.Fprintf(&sb, "allowlist: %s\n", strings.Join(allow, ", "))
	}
	linked, shaken := res.Linked(), res.Shaken()
	fmt.Fprintf(&sb, "sdk areas linked: %s\n", joinOrNone(append([]string{"os", "permission"}, linked...)))
	fmt.Fprintf(&sb, "sdk areas left out of release builds: %s\n", joinOrNone(shaken))
	for _, s := range res.GoSites {
		fmt.Fprintf(&sb, "  go host import %s:%d  %s\n", s.File, s.Line, s.Symbol)
	}
	return sb.String()
}

func joinOrNone(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	return strings.Join(s, ", ")
}
