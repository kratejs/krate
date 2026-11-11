package runtimes

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/gohost"
	"github.com/kratejs/krate/packages/native/hostchild"
)

// writeHostBundle writes a tiny host child bundle to dir/host.mjs the way the
// host build does: the hostchild runtime, then the registrations, then start.
func writeHostBundle(t *testing.T, dir string) string {
	t.Helper()
	entry := filepath.Join(dir, "host.mjs")
	body := hostchild.Source + "\nglobalThis.__krateHost.register(\"t.Where\", () => \"compiled:\" + typeof process.platform);\nstart();\n"
	if err := os.WriteFile(entry, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return entry
}

// runCompiled starts the executable as the shell would and calls t.Where.
func runCompiled(t *testing.T, exe string) string {
	t.Helper()
	child := gohost.NewChild(gohost.ChildOptions{Logf: t.Logf, Name: "rt", ReadyAnyLine: true, ReadyTimeout: 60 * time.Second})
	if err := child.Start(context.Background(), exe, nil); err != nil {
		t.Fatalf("starting %s: %v", exe, err)
	}
	defer child.Stop()
	body := `{"calls":[{"id":1,"fn":"t.Where","args":[]}]}`
	frame := make([]byte, 0, len(body)+8)
	frame = append(frame, byte(len(body)>>24), byte(len(body)>>16), byte(len(body)>>8), byte(len(body)))
	frame = append(frame, body...)
	frame = append(frame, 0, 0)
	resp, err := child.Client().Post(child.Base()+"/__krate/call", "application/x-krate-frames", bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestReleaseBunCompiled(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil || runtime.GOOS == "windows" {
		t.Skip("bun is not on PATH")
	}
	dir := t.TempDir()
	exe, err := BuildRelease(Bun, true, HostTarget(), filepath.Join(dir, "out"), ReleaseInput{Entry: writeHostBundle(t, dir)})
	if err != nil {
		t.Fatal(err)
	}
	if got := runCompiled(t, exe); !strings.Contains(got, "compiled:string") {
		t.Fatalf("response %q", got)
	}
}

func TestReleaseNodeSEA(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil || runtime.GOOS != "linux" {
		t.Skip("needs node on Linux")
	}
	if _, err := exec.LookPath("postject"); err != nil {
		if out, err := exec.Command("npx", "--yes", "postject", "--help").CombinedOutput(); err != nil {
			t.Skipf("postject is not installed and npx cannot fetch it: %v\n%s", err, out)
		}
	}
	dir := t.TempDir()
	exe, err := BuildRelease(Node, true, HostTarget(), filepath.Join(dir, "out"), ReleaseInput{Entry: writeHostBundle(t, dir)})
	if err != nil {
		t.Fatal(err)
	}
	if got := runCompiled(t, exe); !strings.Contains(got, "compiled:string") {
		t.Fatalf("response %q", got)
	}
	// The executable carries its own script: it runs without the bundle.
	if err := os.Remove(filepath.Join(dir, "host.mjs")); err != nil {
		t.Fatal(err)
	}
	if got := runCompiled(t, exe); !strings.Contains(got, "compiled:string") {
		t.Fatalf("response without the bundle %q", got)
	}
}
