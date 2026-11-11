package jsruntime

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// unsupportedAPIs returns "api@line:col" for each unsupported reference.
func unsupportedAPIs(s CompatScan) []string {
	var out []string
	for _, r := range s.Unsupported() {
		out = append(out, r.API+"@"+strconv.Itoa(r.Line)+":"+strconv.Itoa(r.Col))
	}
	sort.Strings(out)
	return out
}

func TestScanCompat(t *testing.T) {
	cases := []struct {
		name string
		file string
		src  string
		want []string
	}{
		{"globals", "a.ts", "const t = new TransformStream();\nconst ws = new WebSocket(url);\n", []string{"TransformStream@1:15", "WebSocket@2:16"}},
		{"import shadows", "a.ts", "import { WebSocket } from \"ws\";\nimport TransformStream from \"t\";\nnew WebSocket(); new TransformStream();\n", nil},
		{"local shadows", "a.js", "const Buffer = make();\nBuffer.from(x);\nfunction f(TransformStream) { return new TransformStream(); }\nconst g = ({ WebSocket }) => new WebSocket();\n", nil},
		{"block and catch locals", "a.js", "try { x() } catch (CompressionStream) { new CompressionStream() }\nfor (const WritableStream of list) new WritableStream();\n", nil},
		{"shadow is scoped", "a.js", "function f() { const Buffer = 1; return Buffer; }\nBuffer.from(\"x\");\n", []string{"Buffer@2:1"}},
		{"typeof", "a.js", "if (typeof Buffer !== \"undefined\") { Buffer.from(x); }\nconst hasWS = typeof WebSocket === \"function\";\n", nil},
		{"typeof ternary and logical", "a.js", "const B = typeof Buffer === \"undefined\" ? null : Buffer;\nconst t = typeof TransformStream !== \"undefined\" && new TransformStream();\n", nil},
		{"in globalThis guard", "a.js", "const W = \"WebSocket\" in globalThis ? new WebSocket(u) : null;\n", nil},
		{"globalThis guard", "a.js", "if (globalThis.CompressionStream) { new CompressionStream(\"gzip\"); }\n", nil},
		{"globalThis member", "a.js", "const T = globalThis.TransformStream;\n", []string{"TransformStream@1:11"}},
		{"self member", "a.js", "self.Buffer.from(x);\n", []string{"Buffer@1:1", "self@1:1"}},
		{"member chain", "a.ts", "await crypto.subtle.digest(\"SHA-256\", d);\nawait crypto.subtle.encrypt(alg, key, d);\n", []string{"crypto.subtle.encrypt@2:7"}},
		{"computed literal chain", "a.js", "crypto[\"subtle\"][\"decrypt\"](a, k, d);\n", []string{"crypto.subtle.decrypt@1:1"}},
		{"property names are not references", "a.js", "obj.WebSocket(); const o = { TransformStream: 1 };\nclass A { Buffer() {} }\nlabel: for (;;) break label;\n", nil},
		{"shorthand is a reference", "a.js", "export const apis = { WebSocket };\n", []string{"WebSocket@1:23"}},
		{"types are not references", "a.ts", "let s: TransformStream<string, string> | undefined;\nconst w = x as unknown as WritableStream;\nenum E { A }\ninterface I { b: Buffer }\n", nil},
		{"node globals", "a.ts", "process.nextTick(cb);\nconst env = process.env.KEY;\nconsole.table(rows);\n", []string{"console.table@3:1", "process.nextTick@1:1"}},
		{"one per file and api", "a.js", "new WebSocket(a);\nnew WebSocket(b);\n", []string{"WebSocket@1:5"}},
		{"jsx attribute", "a.tsx", "export const X = () => <div onClick={() => new WebSocket(u)} />;\n", []string{"WebSocket@1:48"}},
		{"class members", "a.ts", "class S extends EventTarget { ws = new WebSocket(u); m(x = Buffer) { return x; } }\n", []string{"Buffer@1:60", "WebSocket@1:40"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := ScanCompatSource(c.file, c.src)
			if !s.Complete {
				t.Fatalf("scan incomplete: skipped %v", s.Skipped)
			}
			if got := unsupportedAPIs(s); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestScanCompatFeatures(t *testing.T) {
	s := ScanCompatSource("a.ts", `export async function GET(req: Request) {
  const u = new URL(req.url);
  if (typeof Buffer !== "undefined") Buffer.from("x");
  const id = crypto.randomUUID();
  return Response.json({ id, q: u.searchParams.get("q") });
}
`)
	want := []string{"crypto", "fetch", "node", "url"}
	if got := s.Features(); !reflect.DeepEqual(got, want) {
		t.Errorf("features = %v, want %v", got, want)
	}
	if len(s.Unsupported()) != 0 {
		t.Errorf("unexpected unsupported refs %v", s.Unsupported())
	}
	if !s.Complete {
		t.Error("scan should be complete")
	}
}

func TestScanCompatIncomplete(t *testing.T) {
	for name, src := range map[string]string{
		"dynamic global": "const C = globalThis[name];\n",
		"eval":           "eval(code);\n",
		"syntax error":   "const = ;\n",
	} {
		if s := ScanCompatSource("a.js", src); s.Complete {
			t.Errorf("%s: scan should be incomplete", name)
		}
	}
	if s := ScanCompatSource("a.js", "const eval = f; eval(x); globalThis['URL'];\n"); !s.Complete {
		t.Error("a local eval and a literal key keep the scan complete")
	}
}

func TestCompatDiagnosticsGroupsPackages(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) string {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	files := []string{
		write("src/api/a.ts", "export const GET = () => new Response(new TransformStream().readable);\n"),
		write("src/lib/b.ts", "export const t = () => new TransformStream();\n"),
		write("node_modules/ws-lib/index.js", "module.exports = () => new WebSocket(u);\n"),
		write("node_modules/ws-lib/other.js", "module.exports = () => new WebSocket(v);\n"),
		write("node_modules/.pnpm/@scope+x@1.0.0/node_modules/@scope/x/a.js", "exports.b = Buffer.from('a');\n"),
		write("node_modules/.pnpm/@scope+x@1.0.0/node_modules/@scope/x/b.js", "exports.c = Buffer.alloc(1);\n"),
	}
	scan := ScanCompatFiles(files)
	ds := CompatDiagnostics(scan, root, CompatAPIRuntime)
	var got []string
	for _, d := range ds {
		if d.Code != diag.CodeAPIRuntimeUnsupported || !d.IsWarning() || d.Hint == "" || d.Source == "" {
			t.Errorf("bad diagnostic %+v", d)
		}
		got = append(got, d.File+":"+strconv.Itoa(d.Line)+":"+strconv.Itoa(d.Col))
	}
	want := []string{
		"node_modules/.pnpm/@scope+x@1.0.0/node_modules/@scope/x/a.js:1:13",
		"node_modules/ws-lib/index.js:1:28",
		"src/api/a.ts:1:43",
		"src/lib/b.ts:1:28",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if !strings.Contains(ds[0].Message, "dependency @scope/x uses Buffer") {
		t.Errorf("package message = %q", ds[0].Message)
	}
	if !strings.Contains(ds[2].Error(), "[KR7160] TransformStream is not supported by the embedded API runtime") {
		t.Errorf("message = %q", ds[2].Error())
	}
	mw := CompatDiagnostics(ScanCompatFiles(files[:1]), root, CompatMiddlewareRuntime)
	if len(mw) != 1 || !strings.Contains(mw[0].Hint, "ssr.middlewareRuntime") || strings.Contains(mw[0].Hint, "ssr.apiRuntime") {
		t.Errorf("middleware hint = %+v", mw)
	}
}

func TestNodeModulesPackage(t *testing.T) {
	for in, want := range map[string]string{
		"/p/node_modules/ws/lib/a.js":                   "ws",
		"node_modules/@a/b/c.js":                        "@a/b",
		"/p/node_modules/.pnpm/x@1/node_modules/y/z.js": "y",
		"/p/src/a.ts":                                   "",
		"/p/node_modules/a.js":                          "",
		"/p/my_node_modules/a/b":                        "",
	} {
		if got := NodeModulesPackage(in); got != want {
			t.Errorf("NodeModulesPackage(%q) = %q, want %q", in, got, want)
		}
	}
}
