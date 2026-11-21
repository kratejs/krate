package build

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func actionBatchFiles() map[string]string {
	return map[string]string{
		"src/actions/todo.ts":                 "\"use server\";\nimport { save } from \"../lib/store\";\nexport async function saveTodo(text: string) { return save(text); }\n",
		"src/actions/users/profile.ts":        "\"use server\";\nimport { save } from \"../../lib/store\";\nimport { tag } from \"@fake/ext\";\nexport async function saveProfile(name: string) { return save(name + tag); }\n",
		"src/actions/plain.ts":                "\"use server\";\nexport async function ping() { return \"pong\"; }\n",
		"src/lib/store.ts":                    "import { hostname } from \"node:os\";\nexport function save(v: string) { return v + hostname(); }\nexport function unused() { return 1; }\n",
		"src/pages/index.tsx":                 "export default function Home() {\n  return <form action={async () => { 'use server'; return 1; }}><button>go</button></form>;\n}\n",
		"node_modules/@fake/ext/package.json": `{"name":"@fake/ext","version":"1.0.0","main":"index.js"}`,
		"node_modules/@fake/ext/index.js":     `exports.tag = "ext";`,
	}
}

// Action modules bundled in a batch get the files separate builds write.
func TestActionBatchMatchesSeparateBuilds(t *testing.T) {
	t.Parallel()
	for _, sourcemap := range []bool{false, true} {
		var trees [2]map[string][]byte
		for k, separate := range []bool{false, true} {
			root := t.TempDir()
			writeTree(t, root, actionBatchFiles())
			cfg := config.Default()
			cfg.OutDir = filepath.Join(root, "dist")
			cfg.Sourcemap = sourcemap
			b := New(root, cfg)
			b.Prof = NewProfiler(true)
			if err := b.prepareActions(); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, ".krate", "actions")
			if n := len(b.actions.Order); n < 4 {
				t.Fatalf("modules = %d", n)
			}
			if !separate {
				var rep strings.Builder
				b.Prof.Report(&rep)
				if strings.Contains(rep.String(), "actions-unbatched") {
					t.Fatalf("a module was not batched:\n%s", rep.String())
				}
			} else {
				for _, mod := range b.actions.Order {
					if err := b.compileActionModule(dir, mod, &resolveMemo{}); err != nil {
						t.Fatal(err)
					}
				}
			}
			trees[k] = readArtifactTree(t, dir, root)
		}
		if !reflect.DeepEqual(slices.Sorted(maps.Keys(trees[0])), slices.Sorted(maps.Keys(trees[1]))) {
			t.Fatalf("files: batched %v, separate %v", slices.Sorted(maps.Keys(trees[0])), slices.Sorted(maps.Keys(trees[1])))
		}
		for name, data := range trees[1] {
			if name != "manifest.json" && !bytes.Equal(trees[0][name], data) {
				t.Errorf("sourcemap %v: %s differs:\nbatched:\n%s\nseparate:\n%s", sourcemap, name, trees[0][name], data)
			}
		}
	}
}

// A broken action module is reported on its own; the others still build.
func TestActionBatchFallsBackOnBrokenModule(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := actionBatchFiles()
	files["src/actions/broken.ts"] = "\"use server\";\nimport { x } from \"./missing\";\nexport async function bad() { return x; }\n"
	writeTree(t, root, files)
	cfg := config.Default()
	cfg.OutDir = filepath.Join(root, "dist")
	b := New(root, cfg)
	err := b.prepareActions()
	if err == nil || !strings.Contains(err.Error(), "src/actions/broken.ts") || !strings.Contains(err.Error(), "./missing") {
		t.Fatalf("err = %v", err)
	}
	if strings.Count(err.Error(), "compiling server action") != 1 {
		t.Errorf("only the broken module may fail: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, ".krate", "actions"))
	var mjs int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".mjs") {
			mjs++
		}
	}
	if mjs != 4 {
		t.Errorf("want the 4 working modules bundled, got %d files: %v", mjs, entries)
	}
}
