package build

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

// instrumentationExts are the extensions findInstrumentationSource accepts.
var instrumentationExts = []string{".ts", ".js", ".mjs"}

// findInstrumentationSource returns the instrumentation file of a project:
// instrumentation.{ts,js,mjs} in the project root, then in src/, the places
// middleware is looked up. It returns "" when there is none.
func findInstrumentationSource(root string) string {
	for _, dir := range []string{root, filepath.Join(root, "src")} {
		for _, ext := range instrumentationExts {
			p := filepath.Join(dir, "instrumentation"+ext)
			if regularFileExists(p) {
				return p
			}
		}
	}
	return ""
}

// instrumentationBundlePath is where BuildInstrumentation writes the module the
// Node API sidecar runs.
func instrumentationBundlePath(root string) string {
	return filepath.Join(root, ".krate", "instrumentation.mjs")
}

// BuildInstrumentation bundles the instrumentation file, if any, to
// .krate/instrumentation.mjs for the Node API sidecar: ESM, with npm packages
// left to Node like the Node API route bundles. Its `register()` runs once per
// server start (see apiSidecarSharedJS). Without a file, a stale bundle is
// removed.
func (b *Builder) BuildInstrumentation() error {
	src := findInstrumentationSource(b.Root)
	out := instrumentationBundlePath(b.Root)
	if src == "" {
		_ = os.Remove(out)
		_ = os.Remove(out + ".map")
		return nil
	}
	printf("  %s> Instrumentation%s %s\n", cGreen, cReset, filepath.Base(src))
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	_, nodeOpts := b.apiRouteOptions()
	nodeOpts.EntryPoints = []string{src}
	nodeOpts.Outfile = out
	nodeOpts.Write = false
	nodeOpts.Metafile = false
	nodeOpts.Sourcemap = api.SourceMapNone
	nodeOpts.Plugins = append(nodeOpts.Plugins, externalNodeModulesPluginMemo(out, &resolveMemo{}))
	res := esbuildBuild(nodeOpts)
	if len(res.Errors) > 0 {
		return fmt.Errorf("instrumentation build failed:\n%s", formatEsbuildErrors(res.Errors))
	}
	if len(res.OutputFiles) == 0 {
		return fmt.Errorf("instrumentation build produced no output")
	}
	return fsutil.WriteFileAtomic(out, res.OutputFiles[0].Contents, 0o644)
}

// hasInstrumentation reports whether a built instrumentation bundle exists.
func hasInstrumentation(root string) bool {
	return regularFileExists(instrumentationBundlePath(root))
}

// instrumentationFailureMessage prefixes a register() error the way Next does.
const instrumentationFailureMessage = "An error occurred while loading instrumentation hook: "
