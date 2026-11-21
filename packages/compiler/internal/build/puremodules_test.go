package build

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

// TestPurePruneMemoAcrossPasses: the island builds of a flavour share the
// pure-module plugin's resolutions with later passes that put a plugin ahead
// of it claiming some imports (the shared-chunk stubs). The later pass then
// asks esbuild for nothing again, and its output is the one a fresh plugin
// gives.
func TestPurePruneMemoAcrossPasses(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeProjectFile(t, root, "src/entry.ts", "import { a } from './a';\nimport { b } from './b';\nimport { c } from './lib';\nconsole.log(a(), b(), c());\n")
	writeProjectFile(t, root, "src/a.ts", "export const a = () => 'a-mark';\nexport const unusedA = () => 'unused-a-mark';\n")
	writeProjectFile(t, root, "src/b.ts", "export const b = () => 'b-mark';\n")
	writeProjectFile(t, root, "src/lib/index.ts", "export { c } from './c';\nexport { d } from './d';\n")
	writeProjectFile(t, root, "src/lib/c.ts", "export const c = () => 'c-mark';\n")
	writeProjectFile(t, root, "src/lib/d.ts", "console.log('d-effect-mark');\nexport const d = 1;\n")

	var inner atomic.Int64
	counter := api.Plugin{Name: "count-inner", Setup: func(pb api.PluginBuild) {
		pb.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
			if _, ok := args.PluginData.(pureModuleMark); ok {
				inner.Add(1)
			}
			return api.OnResolveResult{}, nil
		})
	}}
	// Claims ./b from the entry by importer and path, as the shared-chunk
	// plugin claims an import of a shared module.
	entry := filepath.Join(root, "src", "entry.ts")
	claim := api.Plugin{Name: "claim", Setup: func(pb api.PluginBuild) {
		pb.OnResolve(api.OnResolveOptions{Filter: `^\./b$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
			if canonicalPath(args.Importer) != canonicalPath(entry) {
				return api.OnResolveResult{}, nil
			}
			return api.OnResolveResult{Path: "./isn_b.js", External: true}, nil
		})
	}}
	build := func(plugins ...api.Plugin) string {
		t.Helper()
		res := api.Build(api.BuildOptions{
			AbsWorkingDir: root,
			EntryPoints:   []string{entry},
			Bundle:        true,
			Write:         false,
			Format:        api.FormatESModule,
			Outdir:        filepath.Join(root, "out"),
			LogLevel:      api.LogLevelSilent,
			Plugins:       plugins,
		})
		if len(res.Errors) > 0 || len(res.OutputFiles) != 1 {
			t.Fatalf("build: %v", res.Errors)
		}
		return string(res.OutputFiles[0].Contents)
	}

	memo := &resolveMemo{}
	first := build(purePrunePluginMemo(root, memo), counter)
	if inner.Load() == 0 {
		t.Fatal("the first pass asked esbuild for nothing, so the memo is not exercised")
	}
	if strings.Contains(first, "unused-a-mark") || !strings.Contains(first, "d-effect-mark") {
		t.Fatalf("the plugin did not prune as expected:\n%s", first)
	}
	inner.Store(0)
	second := build(claim, purePrunePluginMemo(root, memo), counter)
	if n := inner.Load(); n != 0 {
		t.Errorf("the second pass asked esbuild again %d times; want every answer from the memo", n)
	}
	fresh := build(claim, purePrunePlugin(root), counter)
	if second != fresh {
		t.Errorf("the second pass differs from a build with a fresh plugin:\n%s\nwant:\n%s", second, fresh)
	}
	if !strings.Contains(second, "./isn_b.js") || strings.Contains(second, "b-mark") {
		t.Errorf("the claimed import was not left to the plugin ahead:\n%s", second)
	}
}
