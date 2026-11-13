package build

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/native"
	"github.com/kratejs/krate/packages/compiler/internal/native/capability"
	"github.com/kratejs/krate/packages/compiler/internal/native/plugins"
	"github.com/kratejs/krate/packages/compiler/internal/version"
)

// Native plugins are Go packages linked into the shell process, so an app with
// plugins runs under a shell built for it: the generated module
// <root>/.krate/native/pluginshell (krate.local/pluginshell) holds the glue of
// every plugin, a registry package listing them, a dev main (cmd/shell) and,
// for a release, the main of the one-process executable. The module requires
// the native module and each plugin's module, replaced by directory (or by
// version for a Go module plugin).

const pluginConfigFile = "krate.config.ts"

// HasNativePlugins reports whether the config enables native plugins.
func HasNativePlugins(cfg *config.Config) bool {
	return cfg != nil && cfg.Native != nil && len(cfg.Native.Plugins) > 0
}

// nativePluginState is what the last native build knows about its plugins.
type nativePluginState struct {
	list  []*plugins.Plugin
	out   *plugins.Output
	used  plugins.Used
	perms []string // plugin permissions the code needs, sorted
	res   *capability.Result
}

// NativePluginNames lists the names of the plugins of the last native build.
func (b *Builder) NativePluginNames() []string {
	if b.nativePlugins == nil {
		return nil
	}
	var out []string
	for _, p := range b.nativePlugins.list {
		out = append(out, p.Manifest.Name)
	}
	sort.Strings(out)
	return out
}

// ResolveNativePlugins resolves native.plugins and checks the plugin permissions
// of the config (KR7250 to KR7255). It caches the result for the Builder.
func (b *Builder) ResolveNativePlugins() ([]*plugins.Plugin, []diag.Diagnostic) {
	if b.nativePluginResolved != nil {
		return b.nativePluginResolved.list, b.nativePluginResolved.ds
	}
	var specs []string
	if b.Cfg.Native != nil {
		specs = b.Cfg.Native.Plugins
	}
	list, ds := plugins.Resolve(b.Root, specs, pluginConfigFile, version.Value)
	if len(ds) == 0 {
		ds = append(ds, checkPluginPermissions(b.Cfg, list)...)
	}
	b.nativePluginResolved = &pluginResolution{list: list, ds: ds}
	return list, ds
}

type pluginResolution struct {
	list []*plugins.Plugin
	ds   []diag.Diagnostic
}

// checkPluginPermissions reports plugin permissions of the allowlist and of the
// window scope that no enabled plugin declares.
func checkPluginPermissions(cfg *config.Config, list []*plugins.Plugin) []diag.Diagnostic {
	if cfg.Native == nil {
		return nil
	}
	declared := map[string]bool{}
	for _, p := range list {
		for _, pm := range p.Manifest.Permissions {
			declared[plugins.PermissionName(p.Manifest.Name, pm.Name)] = true
		}
	}
	var ds []diag.Diagnostic
	check := func(where string, names []string) {
		for _, n := range names {
			if strings.HasPrefix(n, "plugin:") && !declared[n] {
				ds = append(ds, diag.New(pluginConfigFile, 1, 1,
					fmt.Sprintf("%s lists %q, which no enabled plugin declares", where, n),
					"add the plugin to native.plugins, or fix the name (plugin:<plugin>.<permission>); declared: "+joinOrNone(sortedKeys(declared)), "").WithCode(diag.CodePluginPermission))
			}
		}
	}
	check("native.permissions", cfg.Native.Permissions)
	if wp := cfg.Native.Window.Permissions; wp != nil {
		check("native.window.permissions", *wp)
	}
	return ds
}

// PluginInfos describes the resolved plugins to the capability analysis.
func PluginInfos(list []*plugins.Plugin) []capability.PluginInfo {
	var out []capability.PluginInfo
	for _, p := range list {
		info := capability.PluginInfo{Name: p.Manifest.Name}
		for _, o := range p.Manifest.Ops {
			perm := ""
			if o.Permission != "" {
				perm = plugins.PermissionName(p.Manifest.Name, o.Permission)
			}
			info.Ops = append(info.Ops, capability.PluginOp{Name: o.Name, Permission: perm})
		}
		out = append(out, info)
	}
	return out
}

// prepareNativePlugins generates the plugins' glue and TypeScript modules and
// adds their stubs to overlays. A release build (nativeShake) links only the
// ops the code uses.
func (b *Builder) prepareNativePlugins(p *native.Placement, overlays map[string]string) error {
	b.nativePlugins, b.nativePluginMods = nil, nil
	if !HasNativePlugins(b.Cfg) {
		if ds := checkPluginPermissions(b.Cfg, nil); len(ds) > 0 {
			return nativeDiagError(ds, "native plugin error(s)")
		}
		return nil
	}
	list, ds := b.ResolveNativePlugins()
	if len(ds) > 0 {
		return nativeDiagError(ds, "native plugin error(s)")
	}
	if !goOnPath() {
		return nativeDiagError([]diag.Diagnostic{needGoDiag("native plugins are Go code linked into a shell built for the app")}, "native plugin error(s)")
	}
	res, err := capability.Analyze(b.Root, b.Cfg, p, capability.Options{Plugins: PluginInfos(list)})
	if err != nil {
		return err
	}
	var used plugins.Used
	if b.nativeShake {
		used = plugins.Used{}
		for name, ops := range res.PluginOps {
			used[name] = map[string]bool{}
			for _, op := range ops {
				used[name][op] = true
			}
		}
	}
	st := &nativePluginState{list: list, used: used, res: res}
	// The shell module comes first: the plugins' packages are type-checked in it.
	shellDir := plugins.ShellDir(b.Root)
	if err := b.writePluginShellModule(st, shellDir); err != nil {
		return err
	}
	out, ds, err := plugins.Generate(b.Root, list, used, pluginGoEnv(runtime.GOOS, runtime.GOARCH))
	if err != nil {
		return err
	}
	if len(ds) > 0 {
		return nativeDiagError(ds, "native plugin error(s)")
	}
	st.out = out
	b.cleanPluginGlue(st)
	for perm := range res.Needs {
		if strings.HasPrefix(perm, "plugin:") {
			st.perms = append(st.perms, perm)
		}
	}
	sort.Strings(st.perms)
	b.nativePlugins = st
	b.nativePluginMods = map[string]string{}
	for _, l := range out.Linked {
		overlays[filepath.Clean(l.StubPath)] = l.StubJS
		b.nativePluginMods[l.Spec] = l.StubPath
	}
	return nil
}

// nativeModuleRequire says how generated modules require the native module:
// the version, and the directory it is replaced by (inside a Krate checkout or
// with KRATE_NATIVE_DIR), or an error when this build of krate has no version.
func (b *Builder) nativeModuleRequire() (require, replaceDir string, err error) {
	native := FindMonorepoNative(b.Root)
	if native == "" {
		if d := os.Getenv("KRATE_NATIVE_DIR"); d != "" {
			if data, rerr := os.ReadFile(filepath.Join(d, "go.mod")); rerr == nil && strings.Contains(string(data), "module "+nativeGoModulePath) {
				native = d
			}
		}
	}
	if native != "" {
		return "v0.0.0", native, nil
	}
	v, ok := NativeModuleVersion()
	if !ok {
		return "", "", nativeDiagError([]diag.Diagnostic{diag.New("go.mod", 1, 1,
			"this build of krate has no release version, so the version of "+nativeGoModulePath+" to link is unknown",
			"set KRATE_NATIVE_VERSION to a released version (for example v0.5.0), or use a released krate", "").WithCode(diag.CodeNativeModuleVersion)}, "native release error(s)")
	}
	return v, "", nil
}

// pluginModFiles returns the go.mod text and go.sum text of the pluginshell module.
func (b *Builder) pluginModFiles(list []*plugins.Plugin) (gomod, gosum string, err error) {
	nrequire, nreplace, err := b.nativeModuleRequire()
	if err != nil {
		return "", "", err
	}
	requires := []string{nativeGoModulePath + " " + nrequire}
	var replaces []string
	if nreplace != "" {
		replaces = append(replaces, nativeGoModulePath+" => "+filepath.ToSlash(nreplace))
	}
	seen := map[string]bool{nativeGoModulePath: true}
	sum := nativeModGoSum
	if nreplace != "" {
		if data, rerr := os.ReadFile(filepath.Join(nreplace, "go.sum")); rerr == nil {
			sum = string(data)
		}
	}
	for _, p := range list {
		if seen[p.ModulePath] {
			continue
		}
		seen[p.ModulePath] = true
		if p.Kind == "go" {
			requires = append(requires, p.ModulePath+" "+p.Version)
		} else {
			requires = append(requires, p.ModulePath+" v0.0.0")
			replaces = append(replaces, p.ModulePath+" => "+filepath.ToSlash(p.ModuleDir))
		}
		if data, rerr := os.ReadFile(filepath.Join(p.ModuleDir, "go.sum")); rerr == nil {
			sum = mergeLines(sum, string(data))
		}
	}
	goLine := "go 1.26.1"
	for _, l := range strings.Split(nativeModGoMod, "\n") {
		if strings.HasPrefix(l, "go ") {
			goLine = l
		}
	}
	var sb strings.Builder
	sb.WriteString("module " + plugins.ShellModule + "\n\n" + goLine + "\n\nrequire (\n")
	for _, r := range requires {
		sb.WriteString("\t" + r + "\n")
	}
	sb.WriteString(")\n")
	for _, r := range replaces {
		sb.WriteString("\nreplace " + r + "\n")
	}
	return sb.String(), mergeLines(sum, ""), nil
}

// pluginReleaseModLines are the lines the go.mod of the app's own module gets
// when a release executable that links Go host code also links plugins: a
// requirement on the pluginshell module and the requires and replaces of the
// plugin modules. A plugin that lives in the app's module needs none.
func (b *Builder) pluginReleaseModLines(st *nativePluginState) (string, error) {
	_, appModule := governingModuleOf(b.Root)
	var sb strings.Builder
	sb.WriteString("\nrequire " + plugins.ShellModule + " v0.0.0\n")
	sb.WriteString("replace " + plugins.ShellModule + " => " + filepath.ToSlash(st.out.Dir) + "\n")
	seen := map[string]bool{nativeGoModulePath: true, appModule: true}
	for _, p := range st.list {
		if seen[p.ModulePath] {
			continue
		}
		seen[p.ModulePath] = true
		if p.Kind == "go" {
			sb.WriteString("require " + p.ModulePath + " " + p.Version + "\n")
			continue
		}
		sb.WriteString("require " + p.ModulePath + " v0.0.0\n")
		sb.WriteString("replace " + p.ModulePath + " => " + filepath.ToSlash(p.ModuleDir) + "\n")
	}
	return sb.String(), nil
}

// governingModuleOf returns the directory and path of the Go module at dir.
func governingModuleOf(dir string) (string, string) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", ""
	}
	for _, l := range strings.Split(string(data), "\n") {
		if f := strings.Fields(l); len(f) == 2 && f[0] == "module" {
			return dir, strings.Trim(f[1], "\"")
		}
	}
	return "", ""
}

const devShellMain = `// Code generated by krate. DO NOT EDIT.

package main

import (
	"os"
	"runtime"

	"github.com/kratejs/krate/packages/native/shell"
	"krate.local/pluginshell/registry"
)

// The platform layer needs the main goroutine on the main OS thread.
func init() { runtime.LockOSThread() }

func main() { os.Exit(shell.CLI(os.Args[1:], registry.All())) }
`

func (b *Builder) writePluginShellModule(st *nativePluginState, dir string) error {
	gomod, gosum, err := b.pluginModFiles(st.list)
	if err != nil {
		return err
	}
	for name, content := range map[string]string{"go.mod": gomod, "go.sum": gosum, filepath.Join("cmd", "shell", "main.go"): devShellMain} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if old, rerr := os.ReadFile(path); rerr == nil && string(old) == content {
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// cleanPluginGlue removes the glue file of an earlier build that had more ops
// for a plugin that has none now.
func (b *Builder) cleanPluginGlue(st *nativePluginState) {
	for _, l := range st.out.Linked {
		if !l.HasGlue {
			_ = os.Remove(filepath.Join(st.out.Dir, "plugins", l.Plugin.Manifest.Name, "glue.go"))
		}
	}
}

// pluginGoEnv is the environment of go commands that build the pluginshell module.
func pluginGoEnv(goos, goarch string) []string {
	return append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
}

// pluginShellKey identifies a build of the per-app shell: the Krate version,
// the native module, the platform and the sources of every plugin.
func (b *Builder) pluginShellKey(goos, goarch string) string {
	h := sha256.New()
	nv, nd, _ := b.nativeModuleRequire()
	fmt.Fprintln(h, "krate", version.Value, "native", nv, nd, goos, goarch)
	h.Write([]byte(devShellMain))
	for _, p := range b.nativePlugins.list {
		fmt.Fprintln(h, p.Manifest.Name, p.Digest())
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:12]
}

// PluginShell builds the shell for the plugins of the last native build (dev
// main, every op linked) and returns its path. Builds are cached in
// <root>/.krate/native/bin by plugin set, plugin sources and Krate version.
func (b *Builder) PluginShell(goos, goarch string) (string, error) {
	st := b.nativePlugins
	if st == nil {
		return "", fmt.Errorf("the app has no native plugins")
	}
	exe := "shell-" + b.pluginShellKey(goos, goarch)
	if goos == "windows" {
		exe += ".exe"
	}
	bin := filepath.Join(b.nativeOutDir(), "bin", exe)
	stale := false
	if _, err := os.Stat(bin); err != nil {
		stale = true
	} else if mod := FindMonorepoNative(b.Root); mod != "" && nativeShellStale(mod, bin) {
		stale = true
	}
	if !stale {
		return bin, nil
	}
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return "", err
	}
	printf("  %s>%s Building the shell for the plugins (%s)\n", cGreen, cReset, strings.Join(b.NativePluginNames(), ", "))
	msg, err := NativeGoRunner(st.out.Dir, pluginGoEnv(goos, goarch), "build", "-trimpath", "-o", bin, "./cmd/shell")
	if err != nil {
		return "", pluginShellError(string(msg), err)
	}
	// Drop the shells of earlier plugin sets.
	if old, _ := filepath.Glob(filepath.Join(filepath.Dir(bin), "shell-*")); len(old) > 0 {
		for _, o := range old {
			if o != bin {
				_ = os.Remove(o)
			}
		}
	}
	return bin, nil
}

func pluginShellError(out string, err error) error {
	text := strings.TrimSpace(out)
	if text == "" {
		text = err.Error()
	}
	hint := "the plugins' Go packages must compile with CGO_ENABLED=0 and their modules must resolve; run go build in the plugin's directory for the full output"
	if strings.Contains(text, "missing go.sum entry") || strings.Contains(text, "no required module provides package") {
		hint = "run go mod tidy in the plugin's module, and check that its dependencies can be downloaded"
	}
	return nativeDiagError([]diag.Diagnostic{diag.New("krate.config.ts", 1, 1, "go build of the shell for the app's plugins failed:\n"+indent(trimTail(text, 25)), hint, "").WithCode(diag.CodePluginShell)}, "native plugin error(s)")
}

// pluginDevManifestPermissions adds, to the permission list of a dev build, the
// permissions of the plugin ops the code uses. An app that lists none keeps
// the SDK defaults next to them; a configured list is respected as written.
func (b *Builder) pluginDevPermissions(configured []string) []string {
	st := b.nativePlugins
	if st == nil || len(st.perms) == 0 || len(configured) > 0 {
		return configured
	}
	return append(append([]string{}, config.NativeDefaultPermissions()...), st.perms...)
}

// pluginDirs are the directories of the local plugins, which a dev session watches.
func (b *Builder) pluginDirs() []string {
	var out []string
	if b.nativePlugins == nil {
		return nil
	}
	for _, p := range b.nativePlugins.list {
		if p.Local {
			out = append(out, p.Dir)
		}
	}
	return out
}
