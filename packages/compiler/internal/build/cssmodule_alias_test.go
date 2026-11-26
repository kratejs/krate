package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
)

// The server CSS plugin resolves a CSS module imported through a tsconfig
// paths alias to its Krate class mapping.
func TestServerCSSPluginResolvesPathAlias(t *testing.T) {
	root := t.TempDir()
	writeProject(t, root, map[string]string{
		"components/box.module.css": ".box { color: red; }\n",
		"entry.js":                  "import s from \"@/components/box.module.css\";\nconsole.log(s.box);\n",
	})
	res := bundler.NewImportResolver([]string{"@/*"}, [][]string{{"./*"}}, root)
	result := api.Build(api.BuildOptions{
		AbsWorkingDir: root,
		EntryPoints:   []string{filepath.Join(root, "entry.js")},
		Bundle:        true,
		Write:         false,
		Outdir:        filepath.Join(root, "out"),
		Plugins:       []api.Plugin{serverCSSPlugin(root, res)},
		LogLevel:      api.LogLevelSilent,
	})
	if len(result.Errors) > 0 {
		t.Fatalf("build failed: %v", result.Errors[0].Text)
	}
	mapping, _, err := bundler.CSSModuleMapping(root, filepath.Join(root, "components", "box.module.css"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(result.OutputFiles[0].Contents); !strings.Contains(got, mapping["box"]) {
		t.Errorf("output lacks the mapped class %q:\n%s", mapping["box"], got)
	}
}

// A client chunk that imports a CSS module builds on its own, and a chunk that
// fails for another reason reports that error, not esbuild's missing output
// path complaint.
func TestClientChunkAloneHandlesCSSModuleImport(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx":           "export default function P() { return <p>ok</p>; }",
		"tsconfig.json":                 `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["./*"]}}}`,
		"src/components/box.module.css": ".box { color: red; }\n",
		"src/components/Good.tsx": `"use client";
import s from "./box.module.css";
import t from "@/src/components/box.module.css";
export default function Good() { return <p class={s.box + " " + t.box}>x</p>; }`,
		"src/components/Bad.tsx": `"use client";
import s from "./box.module.css";
import { nope } from "./missing";
export default function Bad() { return <p class={s.box}>{nope}</p>; }`,
	})
	withPreact(t, root)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	alone := New(root, b.Cfg)
	if r := alone.buildClientChunk(chunkReq(root, "src/components/Good.tsx")); r.err != nil {
		t.Fatalf("chunk with a CSS module import failed: %v", r.err)
	}
	r := alone.buildClientChunk(chunkReq(root, "src/components/Bad.tsx"))
	if r.err == nil {
		t.Fatal("expected the missing import to fail the chunk")
	}
	if msg := r.err.Error(); strings.Contains(msg, "output path") || !strings.Contains(msg, "missing") {
		t.Errorf("unexpected error: %v", r.err)
	}
}
