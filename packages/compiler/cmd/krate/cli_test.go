package main

import (
	"errors"
	"os"
	"os/exec"
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
