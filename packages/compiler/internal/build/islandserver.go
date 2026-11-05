package build

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// A page that is rendered per request (SSR, ISR, streaming regions) runs its
// JSX in Node with Krate's server renderer, which cannot run a component that
// needs React's render model: a store hook, a query hook, a Radix primitive.
// Its islands are therefore replaced in the server bundle by stubs that emit
// the placeholder, with the markup the build rendered for it, and the page
// script mounts them as it does on a static page.

// islandServerStubs describes how the server bundle of one page replaces its
// islands.
type islandServerStubs struct {
	// Attrs are the places in the page source where an island root gets its id.
	Attrs []islandStubAttr
	// HTML is the build-time markup of each island, by id.
	HTML map[string]string
	// Specs lists, per import specifier of the page, the island bindings it
	// provides.
	Specs map[string]*islandStubSpec
}

type islandStubAttr struct {
	Offset int
	ID     string
}

type islandStubSpec struct {
	Default string   // the page binds the module's default export to an island
	Named   []string // exported names bound to islands
	Members []string // members used through a namespace binding
	NS      bool     // a namespace binding is an island root
	// Rest means the page imports other names of the module too (helpers,
	// types), which stay the real exports; RestDefault is its default export.
	Rest        bool
	RestDefault bool
}

// newIslandServerStubs derives the stubs of a page from its islands.
func newIslandServerStubs(prog *ast.Program, exts []irtree.ExternalComponent, html map[string]string) *islandServerStubs {
	if len(exts) == 0 || prog == nil {
		return nil
	}
	roots := map[string]bool{}
	st := &islandServerStubs{HTML: map[string]string{}, Specs: map[string]*islandStubSpec{}}
	for _, e := range exts {
		if e.Subtree == "" || e.Offset <= 0 && e.Line == 0 {
			continue
		}
		roots[e.Local] = true
		st.Attrs = append(st.Attrs, islandStubAttr{Offset: e.Offset, ID: e.ID})
		if h := html[e.ID]; h != "" {
			st.HTML[e.ID] = h
		}
	}
	if len(roots) == 0 {
		return nil
	}
	names := irtree.JSXElementNames(prog.Body)
	for _, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		src := strings.Trim(imp.Source, "\"'")
		spec := &islandStubSpec{}
		hit := false
		if imp.Default != "" && roots[imp.Default] {
			spec.Default, hit = imp.Default, true
		} else if imp.Default != "" {
			spec.Rest, spec.RestDefault = true, true
		}
		if imp.Namespace != "" && roots[imp.Namespace] {
			spec.NS, hit = true, true
			seen := map[string]bool{}
			for _, n := range names {
				if strings.HasPrefix(n, imp.Namespace+".") {
					m := strings.TrimPrefix(n, imp.Namespace+".")
					if i := strings.IndexByte(m, '.'); i >= 0 {
						m = m[:i]
					}
					if !seen[m] {
						seen[m] = true
						spec.Members = append(spec.Members, m)
					}
				}
			}
			sort.Strings(spec.Members)
		} else if imp.Namespace != "" {
			spec.Rest = true
		}
		for _, n := range imp.Named {
			local := n.Local
			if local == "" {
				local = n.Remote
			}
			if roots[local] {
				spec.Named = append(spec.Named, n.Remote)
				hit = true
			} else {
				spec.Rest = true
			}
		}
		if hit {
			st.Specs[src] = spec
		}
	}
	if len(st.Attrs) == 0 || len(st.Specs) == 0 {
		return nil
	}
	return st
}

// applyAttrs puts the id of each island on its root tag in the page source.
func (s *islandServerStubs) applyAttrs(src string) string {
	attrs := append([]islandStubAttr(nil), s.Attrs...)
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Offset > attrs[j].Offset })
	for _, a := range attrs {
		if a.Offset < 0 || a.Offset >= len(src) || src[a.Offset] != '<' {
			continue
		}
		end := a.Offset + 1
		for end < len(src) && (isTagChar(src[end])) {
			end++
		}
		if end == a.Offset+1 {
			continue
		}
		src = src[:end] + ` data-krate-island-id=` + strconv.Quote(a.ID) + src[end:]
	}
	return src
}

func isTagChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '$' || c == '-' || c == ':'
}

// stubModule is the source of the module that replaces an island import.
func (s *islandServerStubs) stubModule(spec string, sp *islandStubSpec) string {
	var sb strings.Builder
	sb.WriteString("const __html = " + jsonLiteral(s.HTML) + ";\n")
	sb.WriteString(`function __stub(name) {
  const C = (props) => (
    <div data-krate-island={props["data-krate-island-id"]} data-krate-island-ssr="" style="display:contents" dangerouslySetInnerHTML={{ __html: __html[props["data-krate-island-id"]] || "" }}></div>
  );
  C.displayName = name;
  return C;
}
`)
	if sp.Default != "" {
		sb.WriteString("export default __stub(\"default\");\n")
	}
	for _, n := range sp.Named {
		sb.WriteString("export const " + n + " = __stub(" + strconv.Quote(n) + ");\n")
	}
	for _, m := range sp.Members {
		sb.WriteString("export const " + m + " = __stub(" + strconv.Quote(m) + ");\n")
	}
	if sp.Rest {
		sb.WriteString("export * from " + strconv.Quote(spec) + ";\n")
		if sp.RestDefault {
			sb.WriteString("export { default } from " + strconv.Quote(spec) + ";\n")
		}
	}
	return sb.String()
}

// islandServerStubPlugin serves the stub modules to the page's server bundle.
func islandServerStubPlugin(s *islandServerStubs, source string) api.Plugin {
	sourcefile, dir := filepath.Base(source), filepath.Dir(source)
	return api.Plugin{
		Name: "krate-island-server-stubs",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if (args.Importer != source && args.Importer != sourcefile && args.Importer != "<stdin>") || args.ResolveDir != dir || args.Kind == api.ResolveEntryPoint {
					return api.OnResolveResult{}, nil
				}
				if _, ok := s.Specs[args.Path]; !ok {
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: args.Path, Namespace: "krate-island-stub", PluginData: args.ResolveDir}, nil
			})
			pb.OnResolve(api.OnResolveOptions{Filter: `.*`, Namespace: "krate-island-stub"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				// The stub re-exports the rest of the real module.
				res := pb.Resolve(args.Path, api.ResolveOptions{Kind: args.Kind, ResolveDir: args.ResolveDir, Importer: "<stub>"})
				if len(res.Errors) > 0 {
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: res.Path, External: res.External}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-island-stub"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				src := s.stubModule(args.Path, s.Specs[args.Path])
				dir, _ := args.PluginData.(string)
				return api.OnLoadResult{Contents: &src, Loader: api.LoaderTSX, ResolveDir: dir}, nil
			})
		},
	}
}

func jsonLiteral(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
