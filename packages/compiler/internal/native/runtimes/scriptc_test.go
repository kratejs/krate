package runtimes

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func stubScriptc(t *testing.T, out string, runErr error) *[][]string {
	t.Helper()
	oldRun, oldLook := run, lookPath
	var calls [][]string
	run = func(dir string, env []string, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return []byte(out), runErr
	}
	lookPath = func(string) (string, error) { return "scriptc", nil }
	t.Cleanup(func() { run, lookPath = oldRun, oldLook })
	return &calls
}

const coverageBlocked = `{"schema":"scriptc-diagnostics","schemaVersion":1,"command":"coverage","success":false,"phase":"complete",
"diagnostics":[{"code":"SC2020","severity":"error","scope":"reached","file":"/p/src/api/a.ts","line":3,"column":7,"message":"nope","hint":"use X"}]}`

func TestScriptcCoverageReportBlocked(t *testing.T) {
	stubScriptc(t, "progress\n"+coverageBlocked, errors.New("exit status 1"))
	rep, ds, err := ScriptcCoverageReport("/p", "/p/.krate/api-scriptc/main.ts", "blockers", Origin{Root: "/p"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Success || len(ds) != 1 {
		t.Fatalf("report %+v, diagnostics %v", rep, ds)
	}
	d := ds[0]
	if d.File != "src/api/a.ts" || d.Line != 3 || d.Col != 7 || d.Code != diag.CodeScriptcUnsupported || d.Hint != "use X" {
		t.Errorf("diagnostic = %+v", d)
	}
}

func TestScriptcCoverageReportClean(t *testing.T) {
	calls := stubScriptc(t, `{"schema":"scriptc-diagnostics","schemaVersion":1,"success":true,"diagnostics":[]}`, nil)
	rep, ds, err := ScriptcCoverageReport("/p", "main.ts", "", Origin{})
	if err != nil || !rep.Success || len(ds) != 0 {
		t.Fatalf("%+v %v %v", rep, ds, err)
	}
	if got := strings.Join((*calls)[0], " "); got != "scriptc coverage main.ts --print=diagnostics --fail-on=blockers" {
		t.Errorf("command = %s", got)
	}
}

func TestScriptcCoverageReportUnreadableOutput(t *testing.T) {
	stubScriptc(t, "boom", errors.New("exit status 2"))
	if _, _, err := ScriptcCoverageReport("/p", "main.ts", "blockers", Origin{}); err == nil {
		t.Fatal("scriptc failing without a report must be an error")
	}
}

func TestScriptcBuildRunsReleaseBuild(t *testing.T) {
	calls := stubScriptc(t, "", nil)
	t.Setenv("SCRIPTC_LINKER", "x")
	dir := t.TempDir()
	exe := filepath.Join(dir, "server")
	if err := ScriptcBuild(HostTarget(), filepath.Join(dir, "main.ts"), exe); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join((*calls)[0], " "); !strings.Contains(got, "build "+filepath.Join(dir, "main.ts")+" -o "+exe+" --strip --optimization release") {
		t.Errorf("command = %s", got)
	}
}

func TestScriptcBuildToolchainMissing(t *testing.T) {
	oldLook := lookPath
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookPath = oldLook })
	err := ScriptcBuild(HostTarget(), "main.ts", "out")
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code != diag.CodeScriptcToolchain {
		t.Fatalf("want KR7236, got %v", err)
	}
}
