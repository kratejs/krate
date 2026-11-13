package crashreport

import (
	"errors"
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

func TestWriteListReadDelete(t *testing.T) {
	setup(t)
	a, err := Write(Report{Kind: "webview", Reason: "crashed", Window: "main", Time: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Write(Report{Kind: "panic", Reason: "boom", Stack: "goroutine 1 [running]:\nmain.main()"})
	if err != nil {
		t.Fatal(err)
	}
	l, err := List()
	if err != nil || len(l) != 2 {
		t.Fatalf("list = %+v, %v", l, err)
	}
	if l[0].ID != b || l[1].ID != a {
		t.Errorf("newest first: %+v", l)
	}
	r, err := Read(b)
	if err != nil {
		t.Fatal(err)
	}
	if r.AppID != "com.acme.crash" || r.AppVersion != "1.2.3" || r.OSVersion != "TestOS 1" || r.Process != "shell" ||
		r.OS == "" || r.Arch == "" || !strings.HasPrefix(r.GoVersion, "go") || r.Kind != "panic" || r.Reason != "boom" ||
		!strings.Contains(r.Stack, "main.main") {
		t.Errorf("report = %+v", r)
	}
	if _, err := time.Parse(time.RFC3339, r.Time); err != nil {
		t.Errorf("time %q: %v", r.Time, err)
	}
	if err := Delete(a); err != nil {
		t.Fatal(err)
	}
	if err := Delete(a); err != nil {
		t.Errorf("deleting twice: %v", err)
	}
	if _, err := Read(a); !errors.Is(err, ErrNotFound) {
		t.Errorf("read deleted: %v", err)
	}
	if l, _ := List(); len(l) != 1 {
		t.Errorf("after delete: %+v", l)
	}
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

func TestNoDir(t *testing.T) {
	Configure(Config{})
	t.Setenv(DirEnv, "")
	if _, err := Write(Report{Kind: "panic"}); !errors.Is(err, ErrNoDir) {
		t.Errorf("write: %v", err)
	}
	if _, err := List(); !errors.Is(err, ErrNoDir) {
		t.Errorf("list: %v", err)
	}
	// The environment names the directory for child processes.
	dir := filepath.Join(t.TempDir(), "c")
	t.Setenv(DirEnv, dir)
	if _, err := Write(Report{Kind: "panic", Reason: "x"}); err != nil {
		t.Errorf("env dir: %v", err)
	}
}

func TestPrunesOldReports(t *testing.T) {
	setup(t)
	for i := 0; i < maxReports+5; i++ {
		if _, err := Write(Report{Kind: "webview", Reason: "r"}); err != nil {
			t.Fatal(err)
		}
	}
	l, _ := List()
	if len(l) != maxReports {
		t.Errorf("kept %d reports", len(l))
	}
}

func TestStackIsTruncated(t *testing.T) {
	setup(t)
	id, _ := Write(Report{Kind: "panic", Stack: strings.Repeat("x", maxStack*2)})
	r, _ := Read(id)
	if len(r.Stack) > maxStack+100 || !strings.HasSuffix(r.Stack, "(truncated)\n") {
		t.Errorf("stack length %d", len(r.Stack))
	}
}

func TestParseCrashLog(t *testing.T) {
	for _, tc := range []struct {
		in, kind, reason string
		ok               bool
	}{
		{"panic: boom\n\ngoroutine 1 [running]:\nmain.main()\n", "panic", "boom", true},
		{"panic: runtime error: index out of range [3] with length 2\n\ngoroutine 1", "panic", "runtime error: index out of range [3] with length 2", true},
		{"fatal error: all goroutines are asleep - deadlock!\n\ngoroutine 1", "fatal", "all goroutines are asleep - deadlock!", true},
		{"runtime: out of memory\nfatal error: out of memory\n", "fatal", "out of memory", true},
		{"SIGSEGV: segmentation violation\nPC=0x1 m=0 sigcode=1\n", "fatal", "SIGSEGV: segmentation violation", true},
		{"  \n", "", "", false},
	} {
		kind, reason, _, ok := ParseCrashLog(tc.in)
		if ok != tc.ok || kind != tc.kind || reason != tc.reason {
			t.Errorf("%q -> %q %q %v", tc.in, kind, reason, ok)
		}
	}
}

func TestCollectSkipsLiveProcessesAndEmptyLogs(t *testing.T) {
	dir := setup(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("pending-111.log", "panic: dead one\n\ngoroutine 1 [running]:\nmain.main()\n")
	write("pending-222.log", "panic: alive one\n")
	write("pending-333.log", "")
	write("pending-notapid.log", "panic: x\n")
	got := collectIn(dir, Config{AppID: "a", Process: "host"}, func(pid int) bool { return pid == 222 })
	if len(got) != 1 || got[0].Reason != "dead one" || got[0].Kind != "panic" {
		t.Fatalf("collected %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "pending-111.log")); !os.IsNotExist(err) {
		t.Error("the collected log must be deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, "pending-222.log")); err != nil {
		t.Error("the live process's log must stay")
	}
	if _, err := os.Stat(filepath.Join(dir, "pending-333.log")); !os.IsNotExist(err) {
		t.Error("an empty log of a dead process is just removed")
	}
	r, _ := Read(got[0].ID)
	if r.Process != "host" || !strings.Contains(r.Stack, "main.main") {
		t.Errorf("%+v", r)
	}
}

func TestGuardReportsAndRepanics(t *testing.T) {
	setup(t)
	func() {
		defer func() {
			if v := recover(); v != "kaput" {
				t.Errorf("recovered %v", v)
			}
		}()
		Guard("shell", func() { panic("kaput") })
	}()
	l, _ := List()
	if len(l) != 1 || l[0].Reason != "kaput" || l[0].Kind != "panic" {
		t.Fatalf("%+v", l)
	}
	r, _ := Read(l[0].ID)
	if !strings.Contains(r.Stack, "TestGuardReportsAndRepanics") {
		t.Errorf("stack:\n%s", r.Stack)
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

func TestCleanExitLeavesNothing(t *testing.T) {
	dir := setup(t)
	un := Install()
	un()
	if left, _ := filepath.Glob(filepath.Join(dir, "pending-*.log")); len(left) != 0 {
		t.Errorf("clean exit left %v", left)
	}
}
