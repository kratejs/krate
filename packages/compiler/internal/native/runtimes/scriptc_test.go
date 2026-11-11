package runtimes

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func TestScriptcArgs(t *testing.T) {
	if got := strings.Join(ScriptcCoverageArgs("/p/host.mjs", ""), " "); got != "coverage /p/host.mjs --print=diagnostics --fail-on=blockers" {
		t.Errorf("coverage args %s", got)
	}
	if got := strings.Join(ScriptcCoverageArgs("/p/host.mjs", "divergences"), " "); !strings.HasSuffix(got, "--fail-on=divergences") {
		t.Errorf("coverage args %s", got)
	}
	args, env, err := ScriptcBuildArgs(HostTarget(), "/p/host.mjs", "/o/host")
	if err != nil || strings.Join(args, " ") != "build /p/host.mjs -o /o/host --strip --optimization release" || env != nil {
		t.Errorf("build args %v %v %v", args, env, err)
	}
	other := Target{"linux", "arm64"}
	if HostTarget() == other {
		other = Target{"linux", "amd64"}
	}
	args, env, err = ScriptcBuildArgs(other, "/p/host.mjs", "/o/host")
	if other.OS == "linux" {
		if err != nil || len(env) != 1 || !strings.HasPrefix(env[0], "SCRIPTC_TARGET=") || !strings.HasSuffix(env[0], "-linux-gnu") {
			t.Errorf("cross build %v %v %v", args, env, err)
		}
	}
	if _, err := ScriptcTriple(Target{"darwin", "arm64"}); err == nil {
		t.Error("darwin cross triple accepted")
	}
}

func TestScriptcToolchain(t *testing.T) {
	fakeTools(t, map[string]string{"scriptc": "/bin/scriptc"}, nil)
	err := ScriptcToolchain(HostTarget())
	var d diag.Diagnostic
	if HostTarget().OS == "windows" {
		return
	}
	if !errors.As(err, &d) || d.Code != diag.CodeScriptcToolchain || !strings.Contains(d.Message, "clang") {
		t.Fatalf("missing clang: %v", err)
	}
	fakeTools(t, map[string]string{"scriptc": "/bin/scriptc", "clang": "/bin/clang"}, nil)
	if err := ScriptcToolchain(HostTarget()); err != nil {
		t.Errorf("with clang: %v", err)
	}
	// A cross build needs zig.
	cross := Target{"linux", "arm64"}
	if HostTarget() == cross {
		cross = Target{"linux", "amd64"}
	}
	err = ScriptcToolchain(cross)
	if !errors.As(err, &d) || !strings.Contains(d.Message, "zig") {
		t.Errorf("cross without zig: %v", err)
	}
	fakeTools(t, nil, nil)
	err = ScriptcToolchain(HostTarget())
	if !errors.As(err, &d) || d.Code != diag.CodeScriptcToolchain || !strings.Contains(d.Hint, "npm install -g scriptc") {
		t.Errorf("missing scriptc: %v", err)
	}
	t.Setenv("SCRIPTC_LINKER", "x")
	envGet = os.Getenv
	fakeTools(t, map[string]string{"scriptc": "/bin/scriptc"}, nil)
	envGet = func(k string) string {
		if k == "SCRIPTC_LINKER" {
			return "my-linker"
		}
		return ""
	}
	if err := ScriptcToolchain(HostTarget()); err != nil {
		t.Errorf("with SCRIPTC_LINKER: %v", err)
	}
}

func TestParseScriptcReport(t *testing.T) {
	ok, err := os.ReadFile("testdata/scriptc_coverage_ok.json")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := ParseScriptcReport(ok)
	if err != nil || !rep.Success || rep.Phase != "complete" || len(rep.Diagnostics) != 0 {
		t.Fatalf("ok report %+v, %v", rep, err)
	}
	if _, err := ParseScriptcReport([]byte(`{"schema":"other","schemaVersion":1}`)); err == nil {
		t.Error("foreign schema accepted")
	}
	if _, err := ParseScriptcReport([]byte(`{"schema":"scriptc-diagnostics","schemaVersion":2}`)); err == nil {
		t.Error("newer schema accepted")
	}
	if _, err := ParseScriptcReport([]byte(`not json`)); err == nil {
		t.Error("garbage accepted")
	}
}

// esbuild-style map: generated line 3 maps to src/lib/x.host.ts line 12, col 5;
// line 1 maps to the host child runtime.
func sampleMap() []byte {
	// Segments: line 1: [0,1,0,0] -> source 1 line 1 col 1(0). line 3: [6,-1,11,4] -> source 0, line 12, col 5.
	return []byte(`{"version":3,"sources":["src/lib/x.host.ts","krate-hostchild:hostchild-runtime"],"mappings":"ACAA;;MDWI"}`)
}

func TestScriptcDiagnosticsMapToOriginalSource(t *testing.T) {
	raw, err := os.ReadFile("testdata/scriptc_coverage_blockers.json")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	src := strings.Repeat("// line\n", 11) + "    const [a, b] = list;\n"
	if err := os.MkdirAll(filepath.Join(root, "src", "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "lib", "x.host.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := "/work/app/.krate/native/host/scriptc/host.mjs"
	ds, err := ScriptcDiagnostics(raw, Origin{
		Map: sampleMap(), Bundle: bundle, Root: root,
		RuntimeSource: func(s string) bool { return strings.HasPrefix(s, "krate-hostchild:") },
	})
	if err != nil {
		t.Fatal(err)
	}
	// The unreached diagnostic is dropped; two errors and one warning remain.
	if len(ds) != 3 {
		t.Fatalf("got %d diagnostics: %+v", len(ds), ds)
	}
	app := ds[0]
	if app.Code != diag.CodeScriptcUnsupported || app.File != "src/lib/x.host.ts" || app.Line != 12 || app.Col != 5 {
		t.Errorf("app diagnostic %+v", app)
	}
	if !strings.Contains(app.Message, "scriptc SC1031") || app.Hint != "destructure into named variables" {
		t.Errorf("message/hint %q / %q", app.Message, app.Hint)
	}
	if !strings.Contains(app.Source, "const [a, b]") {
		t.Errorf("source line %q", app.Source)
	}
	rt := ds[1]
	if rt.Code != diag.CodeScriptcRuntime || !strings.HasPrefix(rt.File, "krate-hostchild:") {
		t.Errorf("runtime diagnostic %+v", rt)
	}
	if ds[2].Severity != diag.SeverityWarning || !strings.Contains(ds[2].Message, "SC6001") {
		t.Errorf("warning %+v", ds[2])
	}
	if app.IsWarning() || rt.IsWarning() {
		t.Error("blockers must be errors")
	}
}

func TestSourceMap(t *testing.T) {
	sm, err := ParseSourceMap(sampleMap())
	if err != nil {
		t.Fatal(err)
	}
	if src, l, c, ok := sm.Original(3, 7); !ok || src != "src/lib/x.host.ts" || l != 12 || c != 5 {
		t.Errorf("Original(3,7) = %s %d %d %v", src, l, c, ok)
	}
	if src, l, c, ok := sm.Original(1, 1); !ok || src != "krate-hostchild:hostchild-runtime" || l != 1 || c != 1 {
		t.Errorf("Original(1,1) = %s %d %d %v", src, l, c, ok)
	}
	if _, _, _, ok := sm.Original(2, 1); ok {
		t.Error("an empty line mapped")
	}
	if _, _, _, ok := sm.Original(99, 1); ok {
		t.Error("a line past the end mapped")
	}
}

func TestJSONPart(t *testing.T) {
	got := string(jsonPart([]byte("warning: something\n{\"schema\":\"x\"}\ntrailing\n")))
	if got != `{"schema":"x"}` {
		t.Errorf("jsonPart = %q", got)
	}
}

// TestScriptcOnPath runs the real scriptc when it is installed.
func TestScriptcOnPath(t *testing.T) {
	if _, err := exec.LookPath("scriptc"); err != nil {
		t.Skip("scriptc is not on PATH")
	}
	dir := t.TempDir()
	entry := filepath.Join(dir, "ok.ts")
	if err := os.WriteFile(entry, []byte("const who: string = \"world\";\nconsole.log(`hello, ${who}`);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ds, err := ScriptcCoverage(dir, entry, "blockers", Origin{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		if !d.IsWarning() {
			t.Errorf("unexpected blocker: %v", d)
		}
	}
	bad := filepath.Join(dir, "bad.ts")
	if err := os.WriteFile(bad, []byte("const x = eval(\"1\");\nconsole.log(x);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ds, err = ScriptcCoverage(dir, bad, "blockers", Origin{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) == 0 || ds[0].Code != diag.CodeScriptcUnsupported || ds[0].File != "bad.ts" {
		t.Errorf("eval diagnostics %+v", ds)
	}
	if ScriptcToolchain(HostTarget()) == nil {
		out := filepath.Join(dir, "ok")
		args, env, _ := ScriptcBuildArgs(HostTarget(), entry, out)
		if b, err := run(dir, env, "scriptc", args...); err != nil {
			t.Fatalf("scriptc build: %v\n%s", err, b)
		}
	}
}
