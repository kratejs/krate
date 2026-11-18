package build

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// A runtime package whose krate-features.json lists the ES modules behind
// each piece (core.modules, features[name].modules, suspense.modules and the
// exports of each module in esm) lets a production build bundle the runtime
// for the site instead of concatenating the prebuilt pieces. The entry
// imports only the exports the site's code can reach and assigns them to
// window, so esbuild drops the rest, and the pieces share one copy of the
// module helpers. Older packages keep the concatenated pieces.

// runtimeModuleRe matches the relative imports between runtime modules
// (tsc output in @krate/runtime/dist).
var runtimeModuleRe = regexp.MustCompile(`(?:\bfrom|\bimport)\s*['"]\./([A-Za-z0-9_.-]+\.js)['"]`)

// globalMemberRe matches a read of a global through the global object
// (globalThis.x, window.x, self["x"]); globalAliasRe a local that holds the
// global object (const g = globalThis;).
var (
	globalMemberRe = regexp.MustCompile(`\b(globalThis|window|self)\s*(?:\?\.|\.)\s*([A-Za-z_$][\w$]*)|\b(globalThis|window|self)\s*\[\s*['"]([A-Za-z_$][\w$]*)['"]\s*\]`)
	globalDeclRe   = regexp.MustCompile(`\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=([^;\n]*)`)
	globalObjectRe = regexp.MustCompile(`\b(?:globalThis|window|self)\s*(?:$|[^\s.\[\w$])`)
)

// globalReads returns the names src reads through the global object,
// including through a local alias of it. The runtime reads optional
// exports that way (typeof g.disposeAll === 'function'), so such an export
// must stay defined on window for the code to behave as before.
func globalReads(src string) map[string]bool {
	out := map[string]bool{}
	for _, m := range globalMemberRe.FindAllStringSubmatch(src, -1) {
		if m[2] != "" {
			out[m[2]] = true
		} else {
			out[m[4]] = true
		}
	}
	for _, a := range globalDeclRe.FindAllStringSubmatch(src, -1) {
		// const g = globalThis; or const target = (api ?? globalThis);
		if !globalObjectRe.MatchString(a[2]) {
			continue
		}
		re := regexp.MustCompile(`(?:^|[^\w$.])` + regexp.QuoteMeta(a[1]) + `\s*(?:\?\.|\.)\s*([A-Za-z_$][\w$]*)`)
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			out[m[1]] = true
		}
	}
	return out
}

// runtimeModulesAvailable reports whether the manifest describes the ES
// modules of every piece a shaken runtime may need.
func runtimeModulesAvailable(m *runtimeFeatureManifest) bool {
	if len(m.Core.Modules) == 0 || len(m.ESM) == 0 {
		return false
	}
	for _, f := range m.Feature {
		if len(f.Modules) == 0 {
			return false
		}
	}
	return m.Suspense != nil && len(m.Suspense.Modules) > 0
}

// shakenRuntimeInput is what bundleShakenRuntime needs to build a runtime.
type shakenRuntimeInput struct {
	// Modules are the runtime modules the runtime is made of, in order.
	Modules []string
	// Lite maps imports of the SPA router to router-lite.js.
	Lite bool
	// Expose are the export names the bundle assigns to window.
	Expose []string
	// ExportOf maps each exposed name to the module that exports it.
	ExportOf map[string]string
}

// planShakenRuntime decides the modules and exported names of a shaken
// runtime. refs are the names the site's code references; scripts are the
// classic scripts that run next to the runtime (the bootstrap and compat
// helpers). Every export of an included module is exposed when all is set.
func planShakenRuntime(m *runtimeFeatureManifest, routerName string, features []string, suspense bool, refs map[string]bool, all bool, load func(string) string, scripts ...string) (shakenRuntimeInput, error) {
	in := shakenRuntimeInput{Lite: routerName == "router-lite", ExportOf: map[string]string{}}
	in.Modules = append(in.Modules, m.Core.Modules...)
	pieces := append([]string{routerName}, features...)
	for _, name := range pieces {
		f, ok := m.Feature[name]
		if !ok {
			return in, fmt.Errorf("runtime manifest has no %s feature", name)
		}
		in.Modules = append(in.Modules, f.Modules...)
	}
	if suspense {
		in.Modules = append(in.Modules, m.Suspense.Modules...)
	}
	for _, mod := range in.Modules {
		for _, name := range m.ESM[mod] {
			if _, ok := in.ExportOf[name]; !ok {
				in.ExportOf[name] = mod
			}
		}
	}

	want := map[string]bool{}
	for name := range refs {
		want[name] = true
	}
	// The router API is public: user scripts the build does not see (and
	// the native host) call krateRouter and navigate.
	for _, name := range m.Feature[routerName].Exports {
		want[name] = true
	}
	// Names the runtime itself reads from window, in every module it is
	// built from (imports between modules included).
	seen := map[string]bool{}
	queue := append([]string(nil), in.Modules...)
	for len(queue) > 0 {
		mod := in.mapModule(queue[0])
		queue = queue[1:]
		if seen[mod] {
			continue
		}
		seen[mod] = true
		src := load(mod)
		if src == "" {
			return in, fmt.Errorf("runtime module %s is missing", mod)
		}
		for name := range globalReads(src) {
			want[name] = true
		}
		for _, sm := range runtimeModuleRe.FindAllStringSubmatch(src, -1) {
			queue = append(queue, sm[1])
		}
	}
	for name := range scriptRefs(want, scripts...) {
		want[name] = true
	}
	for name := range in.ExportOf {
		if all || want[name] {
			in.Expose = append(in.Expose, name)
		}
	}
	sort.Strings(in.Expose)
	return in, nil
}

// scriptRefs returns the names the bootstrap scripts reference when the
// code next to them uses the names in used. A line that defines a helper
// (window.kbindText=function...) counts only when the helper is used,
// directly or by another used helper; every other line runs at load and
// always counts.
func scriptRefs(used map[string]bool, scripts ...string) map[string]bool {
	out := map[string]bool{}
	helpers := map[string]string{}
	var always []string
	for _, script := range scripts {
		for _, line := range strings.Split(script, "\n") {
			if rest, ok := strings.CutPrefix(line, "window."); ok {
				if name, _, found := strings.Cut(rest, "="); found && isJSIdent(name) {
					helpers[name] += line + "\n"
					continue
				}
			}
			always = append(always, line)
		}
	}
	var queue []string
	add := func(src string) {
		refs := jsRefs(src)
		for name := range globalReads(src) {
			refs[name] = true
		}
		for name := range refs {
			if !out[name] {
				out[name] = true
				queue = append(queue, name)
			}
		}
	}
	add(strings.Join(always, "\n"))
	for name := range used {
		queue = append(queue, name)
	}
	done := map[string]bool{}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if src, ok := helpers[name]; ok && !done[name] {
			done[name] = true
			add(src)
		}
	}
	return out
}

// mapModule returns the module an import of mod loads.
func (in shakenRuntimeInput) mapModule(mod string) string {
	if in.Lite && mod == "router.js" {
		return "router-lite.js"
	}
	return mod
}

// Late lists the modules that run after the others have assigned their
// exports to window. The Suspense runtime connects to the actions and
// boundary features through window when it loads, as it did when it was a
// separate bundle appended to the runtime.
var lateRuntimeModules = map[string]bool{"suspense.js": true}

// baseEntryPath is the generated module that loads every runtime module
// except the late ones and assigns their exports.
const baseEntryPath = "krate-runtime-base.js"

// entry returns the generated entry module: it imports every runtime
// module (their load-time side effects, such as the router's listeners,
// stay) and assigns the exposed names to window, the late modules last.
func (in shakenRuntimeInput) entry() string {
	return in.entryFor(true)
}

// baseEntry returns the module that loads the modules that are not late.
func (in shakenRuntimeInput) baseEntry() string {
	return in.entryFor(false)
}

func (in shakenRuntimeInput) entryFor(late bool) string {
	byModule := map[string][]string{}
	for _, name := range in.Expose {
		mod := in.ExportOf[name]
		byModule[mod] = append(byModule[mod], name)
	}
	var sb strings.Builder
	if late {
		fmt.Fprintf(&sb, "import %q;\n", "./"+baseEntryPath)
	}
	var assign []string
	n := 0
	for _, mod := range in.Modules {
		if lateRuntimeModules[mod] != late {
			continue
		}
		names := byModule[mod]
		delete(byModule, mod)
		if len(names) == 0 {
			fmt.Fprintf(&sb, "import %q;\n", "./"+mod)
			continue
		}
		var specs []string
		for _, name := range names {
			local := fmt.Sprintf("k%d", n)
			n++
			specs = append(specs, name+" as "+local)
			assign = append(assign, name+": "+local)
		}
		fmt.Fprintf(&sb, "import { %s } from %q;\n", strings.Join(specs, ", "), "./"+mod)
	}
	if len(assign) > 0 {
		fmt.Fprintf(&sb, "Object.assign(window, { %s });\n", strings.Join(assign, ", "))
	}
	return sb.String()
}

const runtimeModuleNamespace = "krate-runtime-module"

// bundleShakenRuntime bundles the runtime described by in, loading the
// runtime modules with load.
func bundleShakenRuntime(in shakenRuntimeInput, load func(string) string) (string, error) {
	plugin := api.Plugin{
		Name: "krate-runtime-modules",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				name, ok := strings.CutPrefix(args.Path, "./")
				if !ok || strings.ContainsAny(name, "/\\") || !strings.HasSuffix(name, ".js") {
					return api.OnResolveResult{}, fmt.Errorf("unexpected import %q in the runtime", args.Path)
				}
				return api.OnResolveResult{Path: in.mapModule(name), Namespace: runtimeModuleNamespace}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: runtimeModuleNamespace}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				if args.Path == baseEntryPath {
					src := in.baseEntry()
					return api.OnLoadResult{Contents: &src, Loader: api.LoaderJS}, nil
				}
				src := load(args.Path)
				if src == "" {
					return api.OnLoadResult{}, fmt.Errorf("runtime module %s is missing", args.Path)
				}
				return api.OnLoadResult{Contents: &src, Loader: api.LoaderJS}, nil
			})
		},
	}
	result := api.Build(api.BuildOptions{
		Stdin: &api.StdinOptions{
			Contents:   in.entry(),
			Sourcefile: "krate-runtime-entry.js",
			Loader:     api.LoaderJS,
		},
		Bundle:            true,
		Write:             false,
		Format:            api.FormatIIFE,
		Platform:          api.PlatformBrowser,
		Target:            api.ES2020,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		TreeShaking:       api.TreeShakingTrue,
		Charset:           api.CharsetUTF8,
		LegalComments:     api.LegalCommentsNone,
		LogLevel:          api.LogLevelSilent,
		Plugins:           []api.Plugin{plugin},
	})
	if len(result.Errors) > 0 {
		return "", fmt.Errorf("bundling the runtime: %s", result.Errors[0].Text)
	}
	if len(result.OutputFiles) == 0 {
		return "", fmt.Errorf("bundling the runtime produced no output")
	}
	return string(result.OutputFiles[0].Contents), nil
}

// shakenRuntimeMemo caches bundled runtimes by project root and entry within
// a build; the runtime chunk is written on more than one build path.
var shakenRuntimeMemo = struct {
	mu sync.Mutex
	m  map[string]string
}{m: make(map[string]string)}

// shakenRuntime bundles the trimmed runtime for the given pieces from the
// runtime's ES modules. It returns "" when the runtime package does not ship
// them or bundling fails; the caller then concatenates the prebuilt pieces.
func shakenRuntime(projectRoot string, m *runtimeFeatureManifest, routerName string, features []string, opts runtimeChunkOptions, refs map[string]bool) string {
	if !runtimeModulesAvailable(m) {
		return ""
	}
	load := func(name string) string { return loadNamedBundleFromDisk(projectRoot, name) }
	scripts := []string{renderer.HydrationBootstrapJS}
	if opts.SPA {
		scripts = append(scripts, routerStartJS)
	}
	if opts.Compat {
		scripts = append(scripts, renderer.CompatBootstrapJS)
	}
	in, err := planShakenRuntime(m, routerName, features, opts.Suspense, refs, opts.Usage.Unknown, load, scripts...)
	if err != nil {
		return ""
	}
	key := projectRoot + "\x00" + strings.Join(in.Modules, ",") + "\x00" + strings.Join(in.Expose, ",")
	shakenRuntimeMemo.mu.Lock()
	js, ok := shakenRuntimeMemo.m[key]
	shakenRuntimeMemo.mu.Unlock()
	if ok {
		return js
	}
	js, err = bundleShakenRuntime(in, load)
	if err != nil {
		js = ""
	}
	shakenRuntimeMemo.mu.Lock()
	shakenRuntimeMemo.m[key] = js
	shakenRuntimeMemo.mu.Unlock()
	return js
}
