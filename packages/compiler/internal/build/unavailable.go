package build

import (
	"fmt"
	"os"
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
	// verbose lists every import of a built-in instead of one summary.
	verbose bool
	mu      sync.Mutex
	msgs    map[string]api.Message // stub key -> error
}

func (b *Builder) newUnavailableGuard() *unavailableGuard {
	return &unavailableGuard{root: b.Root, verbose: b.Verbose, msgs: map[string]api.Message{}}
}

func (g *unavailableGuard) note(key string, msg api.Message) {
	g.mu.Lock()
	if _, ok := g.msgs[key]; !ok {
		g.msgs[key] = msg
	}
	g.mu.Unlock()
}

// importMessage is the error for an import of a Node built-in. It points at
// the import specifier in the importing file when it can be found there.
func (g *unavailableGuard) importMessage(importer, spec string) api.Message {
	text := fmt.Sprintf("Could not resolve %q: Node built-ins do not exist in the browser", spec)
	if data, err := os.ReadFile(importer); err == nil {
		src := string(data)
		for _, q := range []string{`"`, `'`} {
			i := strings.Index(src, q+spec+q)
			if i < 0 {
				continue
			}
			line := 1 + strings.Count(src[:i], "\n")
			lineStart := strings.LastIndex(src[:i], "\n") + 1
			lineEnd := len(src)
			if j := strings.Index(src[i:], "\n"); j >= 0 {
				lineEnd = i + j
			}
			return api.Message{Text: text, Detail: builtinHit{importer: importer, spec: spec}, Location: &api.Location{
				File: importer, Line: line, Column: i - lineStart, LineText: strings.TrimRight(src[lineStart:lineEnd], "\r"),
			}}
		}
	}
	return api.Message{Text: g.rel(importer) + ": " + text, Detail: builtinHit{importer: importer, spec: spec}}
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
				// One stub per importer, so a stub that survives names the
				// file that really reaches it.
				key := args.Importer + "|" + args.Path
				g.note(key, g.importMessage(args.Importer, args.Path))
				return api.OnResolveResult{Path: key, Namespace: "krate-unavailable", SideEffects: api.SideEffectsFalse}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-unavailable"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				src := unavailableStub(args.Path)
				return api.OnLoadResult{Contents: &src, Loader: api.LoaderJS}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `\.node$`}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				g.note(args.Path, api.Message{Text: fmt.Sprintf("%s: native addon %s cannot run in the browser", g.rel(args.Path), filepath.Base(args.Path))})
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
				out = append(out, g.msgs[k])
				break
			}
		}
	}
	return out
}

func unavailableStub(key string) string {
	// module.exports makes it a CommonJS module, so a named import of it is a
	// property read that keeps the stub only when code really uses it.
	return "module.exports = {};\nthrow new Error(\"" + unavailableMarker + hashContent([]byte(key)) + "\");\n"
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
