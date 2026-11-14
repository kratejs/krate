package build

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// A fake node and shell show what RunNativeTest passes on without a window.
func TestRunNativeTestRunsNodeWithEnvironment(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil || runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	if testing.Short() {
		t.Skip("builds an app")
	}
	ex := e2eRequireDeps(t)
	// A copy of nothing: use the example, but stop before launching anything
	// by pointing node at a script that records its arguments.
	work := t.TempDir()
	rec := filepath.Join(work, "args.txt")
	node := filepath.Join(work, "node")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo v22.1.0; exit 0; fi\n" +
		"echo \"$@\" > " + rec + "\necho \"$KRATE_TEST_SHELL|$KRATE_TEST_APP|$KRATE_TEST\" >> " + rec + "\nexit 3\n"
	if err := os.WriteFile(node, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	shell := e2eBuildShell(t, work)
	t.Setenv("DISPLAY", ":0")
	var out bytes.Buffer
	code, err := RunNativeTest(NativeTestOptions{
		LoadConfig: func() (string, *config.Config, map[string]string, error) {
			cfg, err := config.Load(ex)
			return ex, cfg, nil, err
		},
		Patterns: []string{"tasks"},
		Shell:    shell,
		Grep:     "adds",
		Workers:  2,
		Node:     node,
		Stdout:   &out,
		Stderr:   &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 3 {
		t.Errorf("exit code %d, want node's 3\n%s", code, out.String())
	}
	b, err := os.ReadFile(rec)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{"--test", "--test-concurrency=2", "--test-name-pattern=adds", "tasks.native.test.mjs", shell + "|", filepath.Join("native", "test", "app") + "|1"} {
		if !strings.Contains(got, want) {
			t.Errorf("node was run as:\n%s\nmissing %q", got, want)
		}
	}
}
