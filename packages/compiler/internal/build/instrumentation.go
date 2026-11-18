package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
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

// instrumentationExitCode is the exit status of a Node API sidecar whose
// instrumentation failed (EX_CONFIG): the host stops instead of restarting it.
const instrumentationExitCode = 78

// instrumentationReportPath is where the Node API sidecar writes the error of
// a failed instrumentation (see krateRunInstrumentation).
func instrumentationReportPath(root string) string {
	return filepath.Join(root, ".krate", "instrumentation-error.json")
}

// instrumentationFailure describes a failed instrumentation of the Node API
// sidecar: the error the sidecar reported, as a diagnostic on the source file,
// and its stack.
func instrumentationFailure(root string, dev bool) (diag.Diagnostic, string) {
	var rep struct {
		Name    string `json:"name"`
		Message string `json:"message"`
		Stack   string `json:"stack"`
	}
	if data, err := os.ReadFile(instrumentationReportPath(root)); err == nil {
		_ = json.Unmarshal(data, &rep)
	}
	cause := strings.TrimSpace(rep.Message)
	if rep.Name != "" && cause != "" && !strings.HasPrefix(cause, rep.Name) {
		cause = rep.Name + ": " + cause
	}
	if cause == "" {
		cause = "the Node API sidecar exited with status " + strconv.Itoa(instrumentationExitCode) + " (see the error above)"
	}
	file := "instrumentation"
	if src := findInstrumentationSource(root); src != "" {
		file = relOr(root, src)
	}
	hint := "fix the error in " + file + " or a module it imports and start the server again. The server does not start while instrumentation fails: register() must resolve before the first request"
	if dev {
		hint = "fix the error in " + file + " or a module it imports; the Node API sidecar starts again after the next rebuild of the instrumentation file. Until then requests that need it answer 503"
	}
	return diag.Diagnostic{
		File:    file,
		Line:    1,
		Col:     1,
		Message: "instrumentation failed: " + cause,
		Hint:    hint,
	}, rep.Stack
}

// exitStatus is the exit status in a process's Wait error, or -1.
func exitStatus(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// waitForFileChange waits until the size or modification time of path changes
// (or it appears or disappears). It returns false when stop closed first.
func waitForFileChange(path string, stop <-chan struct{}) bool {
	stamp := func() string {
		st, err := os.Stat(path)
		if err != nil {
			return ""
		}
		return fmt.Sprint(st.Size(), st.ModTime().UnixNano())
	}
	was := stamp()
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return false
		case <-t.C:
			if stamp() != was {
				return true
			}
		}
	}
}
