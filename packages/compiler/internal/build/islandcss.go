package build

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
)

// islandCSSSink collects the stylesheets a compiled package island imports.
type islandCSSSink struct {
	root  string
	mu    sync.Mutex
	files []string
	seen  map[string]bool
}

func (s *islandCSSSink) add(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if !s.seen[path] {
		s.seen[path] = true
		s.files = append(s.files, path)
	}
}

// islandStyles is the CSS pulled in by a page's compiled package islands.
type islandStyles struct {
	CSS    string
	Assets map[string]string // asset source path -> hashed site URL
	Deps   []string          // stylesheets and every file they read
}

// islandCSSPlugin turns stylesheet imports of an island bundle into empty
// modules (esbuild cannot write CSS from an in-memory build) and records the
// resolved files so they can be built through the normal CSS path. A CSS
// Module import yields its Krate class mapping.
func islandCSSPlugin(sink *islandCSSSink) api.Plugin {
	const skip = "krate-island-css-skip"
	return api.Plugin{
		Name: "krate-island-css",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `\.css$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.PluginData == skip {
					return api.OnResolveResult{}, nil
				}
				res := pb.Resolve(args.Path, api.ResolveOptions{
					Kind:       args.Kind,
					ResolveDir: args.ResolveDir,
					Importer:   args.Importer,
					PluginData: skip,
				})
				if len(res.Errors) > 0 {
					return api.OnResolveResult{}, fmt.Errorf("%s", res.Errors[0].Text)
				}
				if res.External {
					return api.OnResolveResult{Path: args.Path, External: true}, nil
				}
				sink.add(res.Path)
				return api.OnResolveResult{Path: res.Path, Namespace: "krate-island-css"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-island-css"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				contents := "export default {};\n"
				if strings.HasSuffix(args.Path, ".module.css") {
					mapping, _, err := bundler.CSSModuleMapping(sink.root, args.Path)
					if err != nil {
						return api.OnLoadResult{}, err
					}
					contents = moduleMappingJS(mapping)
				}
				return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS}, nil
			})
		},
	}
}

// buildIslandStyles builds the collected stylesheets with the page pipeline.
func (b *Builder) buildIslandStyles(sink *islandCSSSink) (*islandStyles, error) {
	if len(sink.files) == 0 {
		return nil, nil
	}
	out := &islandStyles{Assets: map[string]string{}}
	var sb strings.Builder
	for _, file := range sink.files {
		res, err := bundler.BuildStylesheet(b.Root, file)
		if err != nil {
			return nil, fmt.Errorf("island stylesheet %s: %w", file, err)
		}
		sb.WriteString("\n")
		sb.WriteString(res.CSS)
		for src, url := range res.Assets {
			out.Assets[src] = url
		}
		out.Deps = append(out.Deps, file)
		out.Deps = append(out.Deps, res.Deps...)
		for _, d := range res.Warnings {
			fmt.Fprintf(os.Stderr, "  %sWarning:%s %s\n", cYellow, cReset, d.Error())
		}
	}
	out.CSS = sb.String()
	return out, nil
}
