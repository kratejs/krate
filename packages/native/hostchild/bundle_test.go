package hostchild

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

var update = flag.Bool("update", false, "rewrite hostchild.mjs from src/runtime.ts")

const header = "// Code generated from src/runtime.ts and packages/app/src/wire.ts. DO NOT EDIT.\n// Regenerate: go test ./hostchild -run TestBundleUpToDate -update\n"

func build(t *testing.T) []byte {
	t.Helper()
	r := api.Build(api.BuildOptions{
		EntryPoints: []string{filepath.Join("src", "runtime.ts")},
		Bundle:      true,
		Format:      api.FormatESModule,
		Platform:    api.PlatformNode,
		Target:      api.ES2022,
		External:    []string{"node:*"},
		LogLevel:    api.LogLevelSilent,
		Write:       false,
	})
	if len(r.Errors) > 0 {
		t.Fatalf("esbuild: %v", r.Errors[0].Text)
	}
	if len(r.OutputFiles) != 1 {
		t.Fatalf("esbuild produced %d files", len(r.OutputFiles))
	}
	return append([]byte(header), r.OutputFiles[0].Contents...)
}

func TestBundleUpToDate(t *testing.T) {
	want := build(t)
	if *update {
		if err := os.WriteFile("hostchild.mjs", want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile("hostchild.mjs")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("hostchild.mjs is stale: run go test ./hostchild -run TestBundleUpToDate -update")
	}
	if string(got) != Source {
		t.Fatal("embedded Source differs from the file")
	}
}
