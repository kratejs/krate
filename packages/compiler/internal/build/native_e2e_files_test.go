package build

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// e2eStartShellArgs starts the shell with an explicit command line.
func e2eStartShellArgs(t *testing.T, bin string, argv ...string) *e2eShell {
	t.Helper()
	// e2eStartShell puts the app directory last; the file arguments of this
	// test must follow it, so the command line is given whole: "--dev" first.
	return e2eStartShell(t, bin, "", append([]string{}, argv...)...)
}

func e2eWaitList(t *testing.T, s *e2eShell, sel string, want ...string) []string {
	t.Helper()
	var items []string
	var err error
	for deadline := time.Now().Add(45 * time.Second); ; time.Sleep(150 * time.Millisecond) {
		err = s.query(fmt.Sprintf(`[...document.querySelectorAll(%q)].map((li) => li.textContent)`, sel+" li"), &items)
		if err == nil && len(items) >= len(want) {
			break
		}
		if time.Now().After(deadline) {
			var errs, problems string
			_ = s.query(`JSON.stringify(globalThis.__krateErrs ?? [], (k, v) => v instanceof Error ? String(v) + " " + v.stack : v)`, &errs)
			_ = s.query(`document.getElementById("problems")?.textContent ?? ""`, &problems)
			t.Fatalf("%s never listed %d items: have %q (last query error: %v)\n  page errors: %s\n  problems: %s", sel, len(want), items, err, errs, problems)
		}
	}
	for i, w := range want {
		if !strings.Contains(items[i], w) {
			t.Errorf("%s item %d = %q, want it to contain %q", sel, i, items[i], w)
		}
	}
	return items
}

// TestNativeE2EFilesGUI runs examples/native-files in the real webview: the
// file it is launched with, a file a second launch hands to the running
// instance, and a file dropped on the window all reach the page, which reads
// each one through the fs API (the grant a dialog pick gets).
func TestNativeE2EFilesGUI(t *testing.T) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the native GUI end-to-end test")
	}
	e2eRequireDeps(t)
	ex := filepath.Join(repoRoot(t), "examples", "native-files")
	if _, err := os.Stat(filepath.Join(ex, "node_modules", "@krate", "app", "dist", "index.js")); err != nil {
		requireE2E(t, "examples/native-files has no built @krate/app (run pnpm install and pnpm build)")
	}
	work := t.TempDir()
	appDir := e2eBuildNative(t, ex, filepath.Join(work, "dist-native"))
	bin := e2eBuildShell(t, work)

	// The build marks an app with file associations as single-instance.
	var man struct {
		SingleInstance bool `json:"singleInstance"`
	}
	raw, err := os.ReadFile(filepath.Join(appDir, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &man); err != nil || !man.SingleInstance {
		t.Fatalf("app.json singleInstance = %v (%v)", man.SingleInstance, err)
	}

	// The files live outside every app directory: only the open or the drop
	// makes them readable.
	files := t.TempDir()
	mk := func(name, body string) string {
		p := filepath.Join(files, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	first := mk("first.krnote", "from the command line")
	second := mk("second.krnote", "from a second launch")
	third := mk("third with space.krnote", "from a drop")

	// Single-instance sockets live in the runtime directory; keep this test's
	// out of the way of a real instance of the example.
	sockDir, err := os.MkdirTemp("", "kfiles")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	t.Setenv("XDG_RUNTIME_DIR", sockDir)

	s := e2eStartShellArgs(t, bin, appDir, first)
	s.waitReady(60 * time.Second)
	e2eWaitList(t, s, "#opened", "first.krnote => from the command line")

	// A second launch, as the operating system does for a double click while
	// the app runs, hands its file over and exits.
	cmd := exec.Command(bin, appDir, second)
	cmd.Stderr = &testLogWriter{t: t}
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the second launch exited with %v; it should hand over and exit 0", err)
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the second launch did not exit")
	}
	items := e2eWaitList(t, s, "#opened", "first.krnote", "second.krnote => from a second launch")
	if len(items) != 2 {
		t.Errorf("opened %q; each file should be listed once", items)
	}

	// A drop onto the window reaches the page with real paths.
	if _, err := s.send("drop "+third, 10*time.Second); err != nil {
		t.Fatalf("drop: %v", err)
	}
	e2eWaitList(t, s, "#dropped", "third with space.krnote => from a drop")

	var problems string
	if err := s.query(`document.getElementById("problems").textContent`, &problems); err != nil || problems != "" {
		t.Errorf("page problems %q (%v)", problems, err)
	}
	if _, err := s.send("quit", 10*time.Second); err != nil {
		t.Errorf("quit: %v", err)
	}
}
