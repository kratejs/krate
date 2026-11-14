package build

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
)

// unavailableMarker prefixes the error a stubbed browser-unavailable module
// throws when it is evaluated.
const unavailableMarker = "krate-unavailable: "

// nodeBuiltinRe matches the Node built-in modules by their bare names; the
// node: forms are matched separately.
var nodeBuiltinRe = regexp.MustCompile(`^(node:.+|(assert|async_hooks|buffer|child_process|cluster|console|constants|crypto|dgram|diagnostics_channel|dns|domain|events|fs|http|http2|https|inspector|module|net|os|path|perf_hooks|process|punycode|querystring|readline|repl|stream|string_decoder|sys|timers|tls|trace_events|tty|url|util|v8|vm|wasi|worker_threads|zlib)(/[a-z/]+)?)$`)

// unavailableGuard stands in for what a browser bundle cannot load: Node
// built-ins and native .node addons. Esbuild reports those as errors while it
// scans the graph, even in a module that tree shaking then drops (a server
// helper a barrel re-exports next to a client component). The guard resolves
// them to stubs that throw when evaluated and, after the build, reports an
// error only for a stub that survived into the output, that is, code a client
// module really reaches.
type unavailableGuard struct {
	root string
	// nodeRuntime is set for a bundle that runs on Node: built-ins resolve
	// normally and only native addons, which esbuild cannot load, are stubbed.
	nodeRuntime bool
	mu          sync.Mutex
	msgs        map[string]string // stub key -> error text
}

func (b *Builder) newUnavailableGuard() *unavailableGuard {
	return &unavailableGuard{root: b.Root, msgs: map[string]string{}}
}

func (g *unavailableGuard) note(key, text string) {
	g.mu.Lock()
	if _, ok := g.msgs[key]; !ok {
		g.msgs[key] = text
	}
	g.mu.Unlock()
}

func (g *unavailableGuard) rel(path string) string {
	if r, err := filepath.Rel(g.root, path); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(path)
}

func (g *unavailableGuard) plugin() api.Plugin {
	const mark = "krate-unavailable-check"
	return api.Plugin{
		Name: "krate-unavailable",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: nodeBuiltinRe.String()}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if g.nodeRuntime || args.PluginData == mark {
					return api.OnResolveResult{}, nil
				}
				if !strings.HasPrefix(args.Path, "node:") {
					// A bare name can be an npm package (events, buffer).
					r := pb.Resolve(args.Path, api.ResolveOptions{
						Importer: args.Importer, Namespace: args.Namespace, ResolveDir: args.ResolveDir,
						Kind: args.Kind, PluginData: mark,
					})
					if len(r.Errors) == 0 {
						return api.OnResolveResult{}, nil
					}
				}
				g.note(args.Path, fmt.Sprintf("%s: Could not resolve %q: Node built-ins do not exist in the browser", g.rel(args.Importer), args.Path))
				return api.OnResolveResult{Path: args.Path, Namespace: "krate-unavailable"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-unavailable"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				src := unavailableStub(args.Path)
				return api.OnLoadResult{Contents: &src, Loader: api.LoaderJS}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `\.node$`}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				g.note(args.Path, fmt.Sprintf("%s: native addon %s cannot run in the browser", g.rel(args.Path), filepath.Base(args.Path)))
				src := unavailableStub(args.Path)
				return api.OnLoadResult{Contents: &src, Loader: api.LoaderJS}, nil
			})
		},
	}
}

// errors returns one error per stub that is still in the output.
func (g *unavailableGuard) errors(files []api.OutputFile) []api.Message {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.msgs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(g.msgs))
	for k := range g.msgs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []api.Message
	for _, k := range keys {
		needle := unavailableMarker + hashContent([]byte(k))
		for _, f := range files {
			if strings.Contains(string(f.Contents), needle) {
				out = append(out, api.Message{Text: g.msgs[k]})
				break
			}
		}
	}
	return out
}

func unavailableStub(key string) string {
	return "throw new Error(\"" + unavailableMarker + hashContent([]byte(key)) + "\");\n"
}

// liveOutputs returns the output files a code-split build keeps alive: the
// entry files and every chunk reachable from them by import. Esbuild also
// emits the chunk of a dynamic import() that sits in code tree shaking dropped
// (a lazy() in a barrel's unused re-export); nothing loads such a file.
func liveOutputs(files []api.OutputFile, entries []string) []api.OutputFile {
	byBase := map[string]int{}
	for i, f := range files {
		byBase[filepath.Base(f.Path)] = i
	}
	live := map[int]bool{}
	var queue []int
	for _, e := range entries {
		if i, ok := byBase[e+".js"]; ok && !live[i] {
			live[i] = true
			queue = append(queue, i)
		}
	}
	for len(queue) > 0 {
		cur := files[queue[0]]
		queue = queue[1:]
		text := string(cur.Contents)
		for base, i := range byBase {
			if !live[i] && strings.HasSuffix(base, ".js") && strings.Contains(text, base) {
				live[i] = true
				queue = append(queue, i)
			}
		}
	}
	var out []api.OutputFile
	for i, f := range files {
		if live[i] || !strings.HasSuffix(f.Path, ".js") && !strings.HasSuffix(f.Path, ".js.map") {
			out = append(out, f)
		}
	}
	return out
}
