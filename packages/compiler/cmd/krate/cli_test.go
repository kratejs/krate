package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestKrateMainHelper is the child side of runKrate: it runs the real entry
// point with the arguments the parent passed.
func TestKrateMainHelper(t *testing.T) {
	if os.Getenv("KRATE_TEST_HELPER_MAIN") != "1" {
		t.Skip("child process only")
	}
	os.Args = append([]string{"krate"}, strings.Split(os.Getenv("KRATE_TEST_HELPER_ARGS"), "\n")...)
	main()
	os.Exit(0)
}

func runKrate(t *testing.T, args ...string) (int, string) {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^TestKrateMainHelper$")
	c.Env = append(os.Environ(), "KRATE_TEST_HELPER_MAIN=1", "KRATE_TEST_HELPER_ARGS="+strings.Join(args, "\n"))
	out, err := c.CombinedOutput()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), string(out)
	}
	if err != nil {
		t.Fatalf("running krate %v: %v", args, err)
	}
	return 0, string(out)
}

func TestParseFlagsValuedForms(t *testing.T) {
	flags, rest, err := parseFlagsChecked([]string{"build", "site", "--config=my.config.ts", "--out-dir", "out", "--target=vercel", "--mode=staging", "--port=4000", "--host", "0.0.0.0", "--root", "a:b", "--root=c:d"})
	if err != nil {
		t.Fatal(err)
	}
	if flags.ConfigPath != "my.config.ts" || flags.OutDir != "out" || flags.Target != "vercel" || flags.Mode != "staging" {
		t.Errorf("flags = %+v", flags)
	}
	if flags.Port != 4000 || !flags.PortSet || flags.Host != "0.0.0.0" {
		t.Errorf("port/host = %d %v %q", flags.Port, flags.PortSet, flags.Host)
	}
	if len(flags.Roots) != 2 || flags.Roots[0] != "a:b" || flags.Roots[1] != "c:d" {
		t.Errorf("roots = %v", flags.Roots)
	}
	if strings.Join(rest, " ") != "build site" {
		t.Errorf("remaining = %v", rest)
	}
}

// A valued flag at the end of the line used to be reported as an unknown flag.
func TestParseFlagsMissingValueNamesTheFlag(t *testing.T) {
	for _, name := range []string{"--config", "--out-dir", "--target", "--mode", "--report", "--token", "--root", "--cpuprofile", "--memprofile", "--host", "--port"} {
		_, _, err := parseFlagsChecked([]string{"build", name})
		if err == nil || !strings.Contains(err.Error(), name+" requires a value") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, _, err := parseFlagsChecked([]string{"serve", "--port=abc"}); err == nil || !strings.Contains(err.Error(), "--port") {
		t.Errorf("--port=abc: err = %v", err)
	}
}

func TestCheckDeployTarget(t *testing.T) {
	for _, ok := range []string{"netlify", "vercel", "cloudflare", "gh-pages", "docker", "node"} {
		if err := checkDeployTarget(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	err := checkDeployTarget("vercell")
	if err == nil || !strings.Contains(err.Error(), `"vercell"`) || !strings.Contains(err.Error(), "vercel") {
		t.Errorf("err = %v", err)
	}
}

// An unknown target must fail before the build, not after minutes of work.
func TestDeployUnknownTargetFailsBeforeBuilding(t *testing.T) {
	dir := writeProjectConfig(t, "export default { outDir: 'dist' }\n")
	code, out := runKrate(t, "deploy", dir, "--target", "vercell")
	if code != 1 || !strings.Contains(out, `unknown deploy target "vercell"`) {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	if strings.Contains(out, "Building") {
		t.Errorf("the build started before the target was checked:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist")); err == nil {
		t.Error("deploy wrote dist for an unknown target")
	}
}

func TestDoctorExitStatus(t *testing.T) {
	good := writeProjectConfig(t, "export default { outDir: 'dist' }\n")
	if code, out := runKrate(t, "doctor", good); code != 0 {
		t.Errorf("healthy project: exit %d\n%s", code, out)
	}
	bad := writeProjectConfig(t, "export default { outDir: 'dist' }\nsyntax error here\n")
	code, out := runKrate(t, "doctor", bad)
	if code != 1 || !strings.Contains(out, "config") || !strings.Contains(out, "problem(s)") {
		t.Errorf("broken config: exit %d, want 1\n%s", code, out)
	}
}

func TestCoreVersionNote(t *testing.T) {
	if got := coreVersionNote(`{"version":"0.5.0"}`, "0.5.0"); got != "" {
		t.Errorf("matching versions: %q", got)
	}
	if got := coreVersionNote(`{"version":"0.5.0"}`, "dev"); got != "" {
		t.Errorf("dev build: %q", got)
	}
	if got := coreVersionNote(`not json`, "0.5.0"); got != "" {
		t.Errorf("unreadable manifest: %q", got)
	}
	got := coreVersionNote(`{"version":"0.5.0-beta.3"}`, "0.5.0")
	if !strings.Contains(got, "v0.5.0-beta.3") || !strings.Contains(got, "v0.5.0") {
		t.Errorf("mismatch note = %q", got)
	}
}
