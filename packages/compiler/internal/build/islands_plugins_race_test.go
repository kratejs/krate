package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

// esbuild calls OnResolve and OnLoad from many goroutines at once. The compat
// and router-hooks plugins must not share mutable state between those calls
// (run with -race: an unguarded lazy assignment was a data race in CI).
func TestCompatPluginsResolveConcurrently(t *testing.T) {
	root := t.TempDir()
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skip(err)
	}
	var entry strings.Builder
	for i := 0; i < 32; i++ {
		src := "import { useOptimistic } from \"react\";\nimport { usePathname } from \"@krate/runtime\";\n" +
			fmt.Sprintf("export const m%d = [useOptimistic, usePathname];\n", i)
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("m%d.js", i)), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&entry, "export * from \"./m%d.js\";\n", i)
	}
	if err := os.WriteFile(filepath.Join(root, "entry.js"), []byte(entry.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	res := api.Build(api.BuildOptions{
		EntryPoints:   []string{filepath.Join(root, "entry.js")},
		AbsWorkingDir: root,
		Bundle:        true,
		Write:         false,
		Format:        api.FormatESModule,
		External:      []string{"preact", "preact/*", "react", "react-dom"},
		Plugins:       []api.Plugin{reactCompatPlugin(root), routerHooksPlugin(root)},
		LogLevel:      api.LogLevelSilent,
	})
	if len(res.Errors) > 0 {
		t.Fatalf("bundle failed: %v", res.Errors[0].Text)
	}
	if len(res.OutputFiles) == 0 || !strings.Contains(string(res.OutputFiles[0].Contents), "useOptimistic") {
		t.Fatal("the compat module was not bundled")
	}
}
