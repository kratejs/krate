package build

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

var jsIdentRe = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

var jsReserved = map[string]bool{
	"break": true, "case": true, "catch": true, "class": true, "const": true, "continue": true,
	"debugger": true, "default": true, "delete": true, "do": true, "else": true, "enum": true,
	"export": true, "extends": true, "false": true, "finally": true, "for": true, "function": true,
	"if": true, "import": true, "in": true, "instanceof": true, "new": true, "null": true,
	"return": true, "super": true, "switch": true, "this": true, "throw": true, "true": true,
	"try": true, "typeof": true, "var": true, "void": true, "while": true, "with": true,
	"yield": true, "let": true, "static": true, "await": true, "implements": true,
	"interface": true, "package": true, "private": true, "protected": true, "public": true,
}

// moduleMappingJS renders a CSS Module class mapping as an ES module: the
// default export is the mapping and every key that is a valid identifier is
// also a named export.
func moduleMappingJS(mapping map[string]string) string {
	data, err := json.Marshal(mapping)
	if err != nil {
		data = []byte("{}")
	}
	var sb strings.Builder
	sb.WriteString("export default " + string(data) + ";\n")
	keys := make([]string, 0, len(mapping))
	for k := range mapping {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if jsIdentRe.MatchString(k) && !jsReserved[k] {
			v, _ := json.Marshal(mapping[k])
			fmt.Fprintf(&sb, "export const %s = %s;\n", k, v)
		}
	}
	return sb.String()
}

// serverCSSPlugin makes stylesheet imports usable in server-side bundles.
// esbuild's own CSS handling would name module classes differently from
// Krate's scoper and write a stray .css file, so a `*.module.css` import
// becomes the Krate class mapping and any other stylesheet import becomes an
// empty module (the page links its CSS separately).
func serverCSSPlugin(root string) api.Plugin {
	return api.Plugin{
		Name: "krate-server-css",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `\.css$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				path := args.Path
				switch {
				case filepath.IsAbs(path):
				case strings.HasPrefix(path, "."):
					path = filepath.Join(args.ResolveDir, filepath.FromSlash(path))
				default:
					if p := resolver.NodeModule(args.ResolveDir, path); p != "" {
						path = p
					} else if !strings.HasSuffix(path, ".module.css") {
						// An unresolvable package stylesheet still contributes
						// nothing to the server render.
						return api.OnResolveResult{Path: path, Namespace: "krate-css"}, nil
					} else {
						return api.OnResolveResult{}, fmt.Errorf("cannot resolve CSS module %q from %s", args.Path, args.ResolveDir)
					}
				}
				return api.OnResolveResult{Path: filepath.Clean(path), Namespace: "krate-css"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-css"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				contents := "export default {};\n"
				if strings.HasSuffix(args.Path, ".module.css") && filepath.IsAbs(args.Path) {
					mapping, _, err := bundler.CSSModuleMapping(root, args.Path)
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
