package crashreport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "crashes")
	Configure(Config{Dir: dir, AppID: "com.acme.crash", AppVersion: "1.2.3", OSVersion: "TestOS 1", Process: "shell"})
	t.Cleanup(func() { Configure(Config{}) })
	return dir
}

func TestIDsCannotEscapeTheDirectory(t *testing.T) {
	dir := setup(t)
	secret := filepath.Join(filepath.Dir(dir), "secret.json")
	if err := os.WriteFile(secret, []byte(`{"kind":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../secret", "..", "crash-../../secret", "", "crash-a/b", `crash-a\b`} {
		if _, err := Read(id); err == nil {
			t.Errorf("Read(%q) succeeded", id)
		}
		if err := Delete(id); err == nil {
			t.Errorf("Delete(%q) succeeded", id)
		}
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatal(err)
	}
}

// A process that dies of a panic leaves a log the next process turns into a
// report: the whole path through the real runtime.
func TestRealCrashBecomesAReport(t *testing.T) {
	if os.Getenv("CRASHREPORT_CHILD") == "1" {
		Configure(Config{Dir: os.Getenv("CRASHREPORT_DIR"), AppID: "com.acme.child", Process: "host"})
		Install()
		go func() { panic("child went down") }()
		time.Sleep(10 * time.Second)
		return
	}
	dir := filepath.Join(t.TempDir(), "crashes")
	cmd := exec.Command(os.Args[0], "-test.run=^TestRealCrashBecomesAReport$")
	cmd.Env = append(os.Environ(), "CRASHREPORT_CHILD=1", "CRASHREPORT_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("the child should have crashed:\n%s", out)
	}
	Configure(Config{Dir: dir, AppID: "com.acme.parent"})
	defer Configure(Config{})
	l, err := List()
	if err != nil || len(l) != 1 {
		t.Fatalf("list = %+v, %v", l, err)
	}
	r, _ := Read(l[0].ID)
	if r.Kind != "panic" || r.Reason != "child went down" || !strings.Contains(r.Stack, "goroutine") {
		t.Errorf("report = %+v", r)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "pending-*.log")); len(left) != 0 {
		t.Errorf("logs left: %v", left)
	}
}
