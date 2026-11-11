package hostts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
	"github.com/kratejs/krate/packages/compiler/internal/native"
)

// appPkg files: a minimal @krate/app for the host bundle and the UI bundle.
var appFiles = map[string]string{
	"node_modules/@krate/app/package.json": `{"name":"@krate/app","version":"0.0.0","type":"module","exports":{".":"./index.js","./host":"./host.js","./db":"./db.js"}}`,
	"node_modules/@krate/app/index.js": `export function call(fn, args) { return Promise.resolve([fn, args]); }
export function stream(fn, args) { return { fn, args }; }
export function signal(name) { return () => name; }
`,
	"node_modules/@krate/app/host.js": `export function hostSignal(initial) {
  let value = initial;
  const sig = { get: () => value, set(v) { value = v; } };
  Object.defineProperty(sig, "__krateHostSignal", { value: true });
  return sig;
}
`,
	"node_modules/@krate/app/db.js": `export const db = { insert(row) { return { ok: true, row }; } };
`,
	"package.json": `{"name":"t","type":"module"}`,
}

// project writes files into a temp dir and returns the root.
func project(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	all := map[string]string{}
	for k, v := range appFiles {
		all[k] = v
	}
	for k, v := range files {
		all[k] = v
	}
	for name, content := range all {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func buildProject(t *testing.T, files map[string]string, mut func(*config.Config)) (string, *Result, []diag.Diagnostic) {
	t.Helper()
	root := project(t, files)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	if mut != nil {
		mut(cfg)
	}
	cfg.Resolve(root)
	p, err := native.Analyze(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Diagnostics) > 0 {
		t.Fatalf("placement diagnostics: %v", p.Diagnostics)
	}
	res, ds, err := Build(root, cfg, p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return root, res, ds
}

func errorsOf(ds []diag.Diagnostic) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, d := range ds {
		if !d.IsWarning() {
			out = append(out, d)
		}
	}
	return out
}

func mustNoErrors(t *testing.T, ds []diag.Diagnostic) {
	t.Helper()
	if e := errorsOf(ds); len(e) > 0 {
		t.Fatalf("unexpected diagnostics: %+v", e)
	}
}

func ids(res *Result) []string {
	var out []string
	for _, r := range res.Registered {
		out = append(out, r.ID)
	}
	sort.Strings(out)
	return out
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWholeModuleStubAndRegistry(t *testing.T) {
	root, res, ds := buildProject(t, map[string]string{
		"src/lib/sync.host.ts": `export async function syncNow(full: boolean) { return full ? "full" : "partial"; }
export function helper() { return 1; }
`,
		"src/pages/index.tsx": `import { syncNow } from "../lib/sync.host";
export default function Home() { return <button onClick={() => syncNow(true)}>sync</button>; }
`,
	}, nil)
	mustNoErrors(t, ds)
	if got := strings.Join(ids(res), ","); got != "src/lib/sync.host.syncNow" {
		t.Fatalf("registered %s (only imported names are registered)", got)
	}
	stub := res.Overlays[filepath.Join(root, "src/lib/sync.host.ts")]
	if !strings.Contains(stub, `export const syncNow = (...a) => call("src/lib/sync.host.syncNow", a);`) ||
		!strings.Contains(stub, `from "@krate/app"`) {
		t.Fatalf("stub:\n%s", stub)
	}
	js := read(t, res.HostBundle)
	if !strings.Contains(js, `"full"`) || !strings.Contains(js, `__krateHost.register`) && !strings.Contains(js, `.register(`) {
		t.Fatalf("host bundle lacks the real code:\n%s", js)
	}
}

func TestManifest(t *testing.T) {
	root, res, ds := buildProject(t, map[string]string{
		"src/lib/sync.host.ts": "export async function syncNow() { return 1; }\n",
		"src/pages/index.tsx":  `import { syncNow } from "../lib/sync.host";` + "\nexport default function H() { return <p onClick={syncNow}>x</p>; }\n",
	}, nil)
	mustNoErrors(t, ds)
	var m manifest
	if err := json.Unmarshal([]byte(read(t, res.Manifest)), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Registered) != 1 || m.Registered[0].File != "src/lib/sync.host.ts" || m.Registered[0].Line != 1 || m.Registered[0].Kind != "func" {
		t.Fatalf("manifest: %+v", m)
	}
	if res.HostBundle != filepath.Join(root, ".krate/native/host/host.js") {
		t.Fatalf("bundle path %s", res.HostBundle)
	}
	var sm struct {
		Sources        []string `json:"sources"`
		SourcesContent []string `json:"sourcesContent"`
	}
	if err := json.Unmarshal([]byte(read(t, res.SourceMap)), &sm); err != nil {
		t.Fatal(err)
	}
	found := false
	for i, s := range sm.Sources {
		if s == "src/lib/sync.host.ts" {
			found = len(sm.SourcesContent) > i && strings.Contains(sm.SourcesContent[i], "syncNow")
		}
	}
	if !found {
		t.Fatalf("source map sources: %v", sm.Sources)
	}
}

var _ = bundler.New
var _ = jsruntime.NewBare
