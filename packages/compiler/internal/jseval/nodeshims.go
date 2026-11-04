package jseval

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

// ErrorGlobal is the global a script sets to a message when it catches its own
// failure; Eval turns a non-empty value into a ThrownError.
const ErrorGlobal = "__krateEvalError"

// ThrownError is a JavaScript error raised by the evaluated script itself, as
// opposed to a bundling or host failure.
type ThrownError struct{ Text string }

func (e *ThrownError) Error() string { return e.Text }

const nodeShimNamespace = "krate-node"

// nodeShimSources are the Node built-ins the embedded runtime provides for
// config evaluation. They cover the path/url/fs helpers configs use to locate
// files; anything else resolves to a stub that fails with a precise message.
var nodeShimSources = map[string]string{
	"path": `
function normalizeArray(parts, allowAbove) {
  var res = [];
  for (var i = 0; i < parts.length; i++) {
    var p = parts[i];
    if (!p || p === '.') continue;
    if (p === '..') {
      if (res.length && res[res.length - 1] !== '..') res.pop();
      else if (allowAbove) res.push('..');
    } else res.push(p);
  }
  return res;
}
export const sep = '/';
export const delimiter = ':';
export function isAbsolute(p) { return p.charAt(0) === '/'; }
export function normalize(p) {
  var abs = isAbsolute(p);
  var trail = p.slice(-1) === '/';
  var out = normalizeArray(p.split('/'), !abs).join('/');
  if (!out && !abs) out = '.';
  if (out && trail) out += '/';
  return (abs ? '/' : '') + out;
}
export function join() {
  var parts = [];
  for (var i = 0; i < arguments.length; i++) { if (arguments[i]) parts.push(arguments[i]); }
  return parts.length ? normalize(parts.join('/')) : '.';
}
export function resolve() {
  var resolved = '', abs = false;
  for (var i = arguments.length - 1; i >= -1 && !abs; i--) {
    var p = i >= 0 ? arguments[i] : globalThis.__krateCwd || '/';
    if (!p) continue;
    resolved = p + '/' + resolved;
    abs = p.charAt(0) === '/';
  }
  var out = normalizeArray(resolved.split('/'), !abs).join('/');
  return ((abs ? '/' : '') + out) || '.';
}
export function dirname(p) {
  var i = p.replace(/\/+$/, '').lastIndexOf('/');
  if (i < 0) return '.';
  if (i === 0) return '/';
  return p.slice(0, i);
}
export function basename(p, ext) {
  var b = p.replace(/\/+$/, '');
  b = b.slice(b.lastIndexOf('/') + 1);
  if (ext && b.slice(-ext.length) === ext && b !== ext) b = b.slice(0, -ext.length);
  return b;
}
export function extname(p) {
  var b = basename(p);
  var i = b.lastIndexOf('.');
  return i <= 0 ? '' : b.slice(i);
}
export function relative(from, to) {
  var f = resolve(from).split('/').filter(Boolean), t = resolve(to).split('/').filter(Boolean);
  var i = 0;
  while (i < f.length && i < t.length && f[i] === t[i]) i++;
  return f.slice(i).map(function () { return '..'; }).concat(t.slice(i)).join('/');
}
export var posix = { sep: sep, delimiter: delimiter, isAbsolute: isAbsolute, normalize: normalize, join: join, resolve: resolve, dirname: dirname, basename: basename, extname: extname, relative: relative };
export default posix;
`,
	"url": `
export function fileURLToPath(u) {
  var p = String(u && u.href ? u.href : u).replace(/^file:\/\//, '');
  try { p = decodeURIComponent(p); } catch (e) {}
  if (p.charAt(0) === '/' && p.charAt(2) === ':' && /[A-Za-z]/.test(p.charAt(1))) p = p.slice(1);
  return p;
}
export function pathToFileURL(p) {
  var href = 'file://' + (p.charAt(0) === '/' ? '' : '/') + encodeURI(p);
  return { href: href, pathname: p, protocol: 'file:', toString: function () { return href; } };
}
export var URL = globalThis.URL;
export default { fileURLToPath: fileURLToPath, pathToFileURL: pathToFileURL, URL: URL };
`,
	"fs": `
export function readFileSync(p) {
  var r = globalThis.__krateFsRead(String(p));
  if (r === null || r === undefined) { var e = new Error("ENOENT: no such file or directory, open '" + p + "'"); e.code = 'ENOENT'; throw e; }
  return r;
}
export function existsSync(p) { return !!globalThis.__krateFsExists(String(p)); }
export function readdirSync(p) {
  var r = globalThis.__krateFsReaddir(String(p));
  if (r === null || r === undefined) { var e = new Error("ENOENT: no such file or directory, scandir '" + p + "'"); e.code = 'ENOENT'; throw e; }
  return r;
}
export default { readFileSync: readFileSync, existsSync: existsSync, readdirSync: readdirSync };
`,
	"os": `
export var EOL = '\n';
export function platform() { return 'linux'; }
export default { EOL: EOL, platform: platform };
`,
}

// nodeShimPlugin resolves Node built-ins for config evaluation: the shimmed
// ones load real implementations, all others a stub that throws a clear error
// when the config module is evaluated.
func nodeShimPlugin() api.Plugin {
	return api.Plugin{
		Name: "krate-node-shims",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^(?:path|url|fs|os)$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: args.Path, Namespace: nodeShimNamespace}, nil
			})
			pb.OnResolve(api.OnResolveOptions{Filter: `^node:`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: strings.TrimPrefix(args.Path, "node:"), Namespace: nodeShimNamespace}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: nodeShimNamespace}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				name := strings.TrimPrefix(args.Path, "node:")
				src, ok := nodeShimSources[name]
				if !ok {
					names := make([]string, 0, len(nodeShimSources))
					for n := range nodeShimSources {
						names = append(names, "node:"+n)
					}
					sort.Strings(names)
					src = "throw new Error(" + jsQuote("krate.config imports node:"+name+", which the embedded config runtime does not provide (available: "+strings.Join(names, ", ")+"). Compute the value in a script and read it from the environment, or move the logic into a plugin.") + ");\nexport default {};\n"
				}
				return api.OnLoadResult{Contents: &src, Loader: api.LoaderJS}, nil
			})
		},
	}
}

func jsQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// registerNodeHost installs the host functions the fs shim calls.
func registerNodeHost(rt *jsruntime.Runtime, cwd string) {
	_ = rt.SetGlobal("__krateCwd", filepath.ToSlash(cwd))
	_ = rt.RegisterFunc("__krateFsRead", func(args []any) (any, error) {
		p, _ := args[0].(string)
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, nil
		}
		return string(data), nil
	})
	_ = rt.RegisterFunc("__krateFsExists", func(args []any) (any, error) {
		p, _ := args[0].(string)
		_, err := os.Stat(p)
		return err == nil, nil
	})
	_ = rt.RegisterFunc("__krateFsReaddir", func(args []any) (any, error) {
		p, _ := args[0].(string)
		ents, err := os.ReadDir(p)
		if err != nil {
			return nil, nil
		}
		out := make([]any, 0, len(ents))
		for _, e := range ents {
			out = append(out, e.Name())
		}
		return out, nil
	})
}
