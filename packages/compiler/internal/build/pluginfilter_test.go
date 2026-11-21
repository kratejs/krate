package build

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
)

type capturedResolve struct {
	opts api.OnResolveOptions
	cb   func(api.OnResolveArgs) (api.OnResolveResult, error)
}

// captureResolves runs the plugin's Setup against a stub PluginBuild and
// returns the OnResolve callbacks it registers, in order. resolve answers
// pb.Resolve calls.
func captureResolves(t *testing.T, p api.Plugin, resolve func(string, api.ResolveOptions) api.ResolveResult) []capturedResolve {
	t.Helper()
	var out []capturedResolve
	pb := api.PluginBuild{
		InitialOptions: &api.BuildOptions{},
		Resolve:        resolve,
		OnResolve: func(o api.OnResolveOptions, cb func(api.OnResolveArgs) (api.OnResolveResult, error)) {
			out = append(out, capturedResolve{o, cb})
		},
		OnLoad:  func(api.OnLoadOptions, func(api.OnLoadArgs) (api.OnLoadResult, error)) {},
		OnStart: func(func() (api.OnStartResult, error)) {},
		OnEnd:   func(func(*api.BuildResult) (api.OnEndResult, error)) {},
	}
	p.Setup(pb)
	return out
}

// applies mirrors esbuild: the filter matches the specifier and the namespace
// option, when set, matches the importer's namespace.
func (c capturedResolve) applies(t *testing.T, spec, importerNS string) bool {
	t.Helper()
	re, err := regexp.Compile(c.opts.Filter)
	if err != nil {
		t.Fatalf("filter %q does not compile: %v", c.opts.Filter, err)
	}
	return (c.opts.Namespace == "" || c.opts.Namespace == importerNS) && re.MatchString(spec)
}

func resolveHandled(r api.OnResolveResult, err error) bool {
	return err != nil || r.Path != "" || r.External
}

func TestPurePruneOnlyFileNamespaceImporters(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mod := filepath.Join(root, "util.ts")
	if err := os.WriteFile(mod, []byte("export const x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	importer := filepath.Join(root, "page.tsx")
	stub := func(string, api.ResolveOptions) api.ResolveResult {
		return api.ResolveResult{Path: mod, Namespace: "file"}
	}
	cbs := captureResolves(t, purePrunePlugin(root), stub)
	if len(cbs) != 1 {
		t.Fatalf("want 1 OnResolve, got %d", len(cbs))
	}
	specs := []string{"react", "@scope/pkg/sub", "./util", "../util", mod, `C:\proj\util.ts`, "@/util", "~/util", "node:fs", "https://x.test/a.js"}
	for _, ns := range []string{"file", "", "krate-css", "krate-island-entry", "krate-island-virtual", "krate-go"} {
		for _, spec := range specs {
			args := api.OnResolveArgs{Path: spec, Importer: importer, Namespace: ns, ResolveDir: root, Kind: api.ResolveJSImportStatement}
			r, err := cbs[0].cb(args)
			handled := resolveHandled(r, err)
			if handled && !cbs[0].applies(t, spec, ns) {
				t.Errorf("filter excludes %q from importer namespace %q, which the callback handles", spec, ns)
			}
			if ns == "file" && !handled {
				t.Errorf("pure module import %q from a file importer was not handled", spec)
			}
			if ns != "file" && handled {
				t.Errorf("import %q from namespace %q was handled", spec, ns)
			}
		}
	}
}

func TestPreactExternalsFilterIsExact(t *testing.T) {
	t.Parallel()
	exts := []string{"preact", "preact/hooks", "preact/jsx-runtime"}
	cbs := captureResolves(t, preactExternalsPlugin(exts), nil)
	if len(cbs) != 1 {
		t.Fatalf("want 1 OnResolve, got %d", len(cbs))
	}
	cases := append([]string{
		"preact/compat", "preact-render-to-string", "preact/hooks/x", "xpreact", "preact ", "react", "./preact",
		"@scope/preact", "/abs/preact", `C:\abs\preact`, "", "preact\n",
	}, exts...)
	for _, spec := range cases {
		want := false
		for _, e := range exts {
			want = want || spec == e
		}
		r, err := cbs[0].cb(api.OnResolveArgs{Path: spec})
		if got := resolveHandled(r, err); got != want {
			t.Errorf("callback handled(%q) = %v, want %v", spec, got, want)
		}
		if got := cbs[0].applies(t, spec, "file"); got != want {
			t.Errorf("filter matches(%q) = %v, want %v", spec, got, want)
		}
	}
}

func TestExactSpecFilterEmptyMatchesNothing(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(exactSpecFilter(nil))
	for _, s := range []string{"", "a", "react", "./x", "/abs", `C:\x`, "\x00", "\U0010FFFF"} {
		if re.MatchString(s) {
			t.Errorf("empty filter matched %q", s)
		}
	}
}

func TestIslandServerStubFilterCoversSpecs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "page.tsx")
	stubs := &islandServerStubs{Specs: map[string]*islandStubSpec{
		"./Counter":          {},
		"../ui/Button":       {},
		"@scope/pkg/sub":     {},
		"lucide-react":       {},
		"@/components/Card":  {},
		`C:\proj\Widget.tsx`: {},
		"/abs/Widget.tsx":    {},
		"a.b+c(d)":           {},
	}}
	cbs := captureResolves(t, islandServerStubPlugin(stubs, source), nil)
	if len(cbs) != 2 {
		t.Fatalf("want 2 OnResolve, got %d", len(cbs))
	}
	first := cbs[0]
	cases := []string{
		"./Counter", "../ui/Button", "@scope/pkg/sub", "lucide-react", "@/components/Card", `C:\proj\Widget.tsx`,
		"/abs/Widget.tsx", "a.b+c(d)", "./counter", "./Counter.tsx", "react", "@scope/pkg", "axbxc(d)", "lucide", "", "node:fs",
	}
	for _, spec := range cases {
		for _, importer := range []string{source, filepath.Base(source), "<stdin>"} {
			args := api.OnResolveArgs{Path: spec, Importer: importer, ResolveDir: dir, Kind: api.ResolveJSImportStatement}
			r, err := first.cb(args)
			_, want := stubs.Specs[spec]
			if got := resolveHandled(r, err); got != want {
				t.Errorf("callback handled(%q from %q) = %v, want %v", spec, importer, got, want)
			}
			if got := first.applies(t, spec, "file"); got != want {
				t.Errorf("filter matches(%q) = %v, want %v", spec, got, want)
			}
		}
	}
	empty := &islandServerStubs{Specs: map[string]*islandStubSpec{}}
	if got := captureResolves(t, islandServerStubPlugin(empty, source), nil)[0]; got.applies(t, "react", "file") {
		t.Errorf("filter of a plugin without specs matched react")
	}
}

func TestExternalPackageFilter(t *testing.T) {
	t.Parallel()
	stub := func(string, api.ResolveOptions) api.ResolveResult {
		return api.ResolveResult{External: true}
	}
	cbs := captureResolves(t, externalNodeModulesPlugin(filepath.Join(t.TempDir(), "out.mjs")), stub)
	var target *capturedResolve
	for i := range cbs {
		if cbs[i].opts.Filter == externalPackageFilter {
			target = &cbs[i]
		}
	}
	if target == nil {
		t.Fatal("externalNodeModulesPlugin does not register externalPackageFilter")
	}
	old := regexp.MustCompile(`^[^./]`)
	cases := []string{
		"react", "react-dom/server", "@scope/pkg", "@scope/pkg/sub", "lodash/fp", "./x", "../x", ".hidden", "/abs/x",
		`C:\abs\x`, "C:/abs/x", `\\srv\share`, "node:fs", "node:fs/promises", "https://x.test/a.js", "data:text/javascript,1",
		"virtual:thing", "pkg:with-colon", "a:", "@krate/runtime", "", "x\ny",
	}
	for _, spec := range cases {
		args := api.OnResolveArgs{Path: spec, Importer: "/proj/a.ts", ResolveDir: "/proj", Kind: api.ResolveJSImportStatement}
		r, err := target.cb(args)
		handled := resolveHandled(r, err)
		matches := target.applies(t, spec, "file")
		if handled && old.MatchString(spec) && !matches {
			t.Errorf("filter excludes %q, which the callback handles", spec)
		}
		if matches && !old.MatchString(spec) {
			t.Errorf("filter matches %q, which the old filter did not", spec)
		}
	}
	for _, spec := range []string{"react", "@scope/pkg/sub"} {
		if !target.applies(t, spec, "file") {
			t.Errorf("filter does not match bare package %q", spec)
		}
	}
}

func TestPathAliasFilter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, f := range []string{"src/components/Button.tsx", "src/lib.ts", "shared/util.ts"} {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("export {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res := bundler.NewImportResolver(
		[]string{"@/*", "~lib", "#shared/*", "app.v2/*", "star*"},
		[][]string{{"src/*"}, {"src/lib.ts"}, {"shared/*"}, {"src/*"}, {"src/lib.ts"}},
		dir,
	)
	cbs := captureResolves(t, pathAliasPlugin(res), nil)
	if len(cbs) != 1 {
		t.Fatalf("want 1 OnResolve, got %d", len(cbs))
	}
	old := regexp.MustCompile(`^[^./]`)
	cases := []string{
		"@/components/Button", "@/lib", "@", "@/", "@x/lib", "~lib", "~lib/x", "~libx", "#shared/util", "#shared", "#sharedx/util",
		"app.v2/lib", "appxv2/lib", "star*", "starfish", "react", "@scope/pkg", "./components/Button", "../lib", "/abs/lib",
		`C:\abs\lib`, "node:fs", "",
	}
	for _, spec := range cases {
		args := api.OnResolveArgs{Path: spec, Importer: filepath.Join(dir, "src/a.ts"), ResolveDir: dir}
		r, err := cbs[0].cb(args)
		handled := resolveHandled(r, err)
		if handled && !cbs[0].applies(t, spec, "file") {
			t.Errorf("filter excludes %q, which resolves through an alias", spec)
		}
		if cbs[0].applies(t, spec, "file") && !old.MatchString(spec) {
			t.Errorf("filter matches %q, which the old filter did not", spec)
		}
	}
	for _, spec := range []string{"@/components/Button", "~lib", "#shared/util", "app.v2/lib"} {
		if !cbs[0].applies(t, spec, "file") {
			t.Errorf("filter does not match alias import %q", spec)
		}
	}
	for _, r := range []*bundler.ImportResolver{nil, bundler.NewImportResolver(nil, nil, dir)} {
		cb := captureResolves(t, pathAliasPlugin(r), nil)[0]
		if cb.applies(t, "react", "file") || cb.applies(t, "@/x", "file") {
			t.Errorf("filter of a resolver without aliases matched a specifier")
		}
	}
}

func TestBuiltinGuardFilter(t *testing.T) {
	t.Parallel()
	old := regexp.MustCompile(`^(node:)?[a-z_]+(/[a-z_]+)?$`)
	for _, target := range []string{"node", "cloudflare"} {
		cbs := captureResolves(t, builtinGuardPlugin(target), nil)
		if len(cbs) != 1 {
			t.Fatalf("want 1 OnResolve, got %d", len(cbs))
		}
		cases := []string{
			"react", "lodash", "events", "buffer", "events/", "fs/promises", "node:fs/promises", "node:node:fs", "v8", "node:v8",
			"fs/promises/x", "./fs", "/fs", "node:", "node:react", "util/types", "node:util/types", "Fs", "@scope/fs", "path/win32",
			"stream/consumers", "sys", "node:test", "test",
		}
		for name := range nodeBuiltinNames {
			cases = append(cases, name, "node:"+name)
		}
		for _, spec := range cases {
			r, err := cbs[0].cb(api.OnResolveArgs{Path: spec, Importer: "/proj/a.ts"})
			handled := resolveHandled(r, err)
			matches := cbs[0].applies(t, spec, "file")
			if handled && old.MatchString(spec) && !matches {
				t.Errorf("target %s: filter excludes %q, which the callback handles", target, spec)
			}
			if matches && !old.MatchString(spec) {
				t.Errorf("target %s: filter matches %q, which the old filter did not", target, spec)
			}
			if matches && !handled {
				t.Errorf("target %s: filter matches %q, which the callback ignores", target, spec)
			}
		}
	}
}
