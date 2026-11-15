package jsruntime

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/jseval"
)

// MiddlewareNeedsNode reports why the middleware at entry cannot run in the
// embedded runtime: its bundle imports Node built-ins (a database driver, fs,
// crypto) or references APIs the KR7160 scan flags. An empty result means the
// embedded runtime is fine, or the bundle could not be analysed (a missing
// dependency): nothing is claimed then. Imports of next and next/* are left
// out because a migration replaces them.
func MiddlewareNeedsNode(root, entry string) []string {
	res := api.Build(api.BuildOptions{
		EntryPoints:   []string{entry},
		AbsWorkingDir: root,
		Outdir:        filepath.Join(root, ".krate"),
		Bundle:        true,
		Platform:      api.PlatformNode,
		Format:        api.FormatESModule,
		Write:         false,
		LogLevel:      api.LogLevelSilent,
		Metafile:      true,
		Plugins:       []api.Plugin{jseval.MarkerPackagesPlugin(), externalNextPlugin()},
	})
	if len(res.Errors) > 0 {
		return nil
	}
	reasons := map[string]bool{}
	var meta struct {
		Inputs  map[string]json.RawMessage `json:"inputs"`
		Outputs map[string]struct {
			Imports []struct {
				Path     string `json:"path"`
				External bool   `json:"external"`
				Kind     string `json:"kind"`
			} `json:"imports"`
		} `json:"outputs"`
	}
	if json.Unmarshal([]byte(res.Metafile), &meta) != nil {
		return nil
	}
	var files []string
	for in := range meta.Inputs {
		files = append(files, filepath.Join(root, filepath.FromSlash(in)))
	}
	for _, o := range meta.Outputs {
		for _, imp := range o.Imports {
			if imp.External && imp.Kind == "import-statement" && imp.Path != "next" && !strings.HasPrefix(imp.Path, "next/") {
				reasons["imports "+imp.Path] = true
			}
		}
	}
	for _, r := range ScanCompatFiles(files).Unsupported() {
		reasons["uses "+r.API] = true
	}
	out := make([]string, 0, len(reasons))
	for r := range reasons {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// externalNextPlugin keeps next and next/* out of the analysis bundle. A
// plugin is used because esbuild's own External globs also swallow tsconfig
// path aliases.
func externalNextPlugin() api.Plugin {
	return api.Plugin{
		Name: "krate-external-next",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^next(/|$)`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: args.Path, External: true}, nil
			})
		},
	}
}
