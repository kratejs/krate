package build

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

var nativeHostAppFiles = map[string]string{
	"package.json":                         `{"name":"t","type":"module"}`,
	"node_modules/@krate/app/package.json": `{"name":"@krate/app","version":"0.0.0","type":"module","exports":{".":"./index.js","./host":"./host.js"}}`,
	"node_modules/@krate/app/index.js":     "export function call(fn, args) { return Promise.resolve([fn, args]); }\nexport function stream(fn, args) { return { fn, args }; }\nexport function signal(name) { return () => name; }\nexport function createClient() { return { call, stream, signal }; }\nexport function setClient() {}\nexport function getClient() { return { call, stream, signal }; }\n",
	"node_modules/@krate/app/host.js":      "export function hostSignal(v) { return { get: () => v }; }\n",
}

func buildNativeFixture(t *testing.T, files map[string]string, native bool) (string, *Builder, error) {
	t.Helper()
	root := t.TempDir()
	all := map[string]string{}
	for k, v := range nativeHostAppFiles {
		all[k] = v
	}
	for k, v := range files {
		all[k] = v
	}
	writeTree(t, root, all)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	// A native build turns view transitions off by default; pin both so the
	// two outputs differ only by the host overlay.
	cfg.ViewTransitions = "off"
	cfg.Resolve(root)
	b := New(root, cfg)
	b.NativeTarget = native
	return root, b, b.BuildAll()
}

func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestNativeBuildSplitsHostCode(t *testing.T) {
	root, b, err := buildNativeFixture(t, map[string]string{
		"src/lib/sync.host.ts": "export async function syncNow() { return 'HOSTONLYSECRET'; }\n",
		"src/pages/index.tsx": `import { syncNow } from "../lib/sync.host";
export default function Home() { return <button onClick={() => syncNow()}>sync</button>; }
`,
	}, true)
	if err != nil {
		t.Fatalf("native build: %v", err)
	}
	res := b.NativeHost()
	if res == nil || len(res.Registered) != 1 || res.Registered[0].ID != "src/lib/sync.host.syncNow" {
		t.Fatalf("host split: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, ".krate", "native", "host", "host.js")); err != nil {
		t.Fatal(err)
	}
	for name, content := range readTree(t, filepath.Join(root, "dist")) {
		if strings.Contains(content, "HOSTONLYSECRET") {
			t.Errorf("host code shipped in dist/%s", name)
		}
	}
	found := false
	for _, content := range readTree(t, filepath.Join(root, "dist")) {
		if strings.Contains(content, "src/lib/sync.host.syncNow") {
			found = true
		}
	}
	if !found {
		t.Error("the UI never calls the host function by id")
	}
}
