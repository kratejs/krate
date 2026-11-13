package goenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUses(t *testing.T) {
	src := "go 1.26\n\nuse ./a // first\nuse (\n\t./b\n\t\"./c d\"\n\t// ./skipped\n)\n"
	if got, want := Uses([]byte(src)), []string{"./a", "./b", "./c d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Uses = %q, want %q", got, want)
	}
}

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A workspace above the app that does not use the app's module turns the
// workspace off; one that uses it keeps it.
func TestExcluded(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	t.Setenv("GOWORK", "")
	ws := t.TempDir()
	write(t, filepath.Join(ws, "other", "go.mod"), "module example.com/other\n\ngo 1.21\n")
	write(t, filepath.Join(ws, "app", "go.mod"), "module example.com/app\n\ngo 1.21\n")
	app := filepath.Join(ws, "app")

	write(t, filepath.Join(ws, "go.work"), "go 1.21\n\nuse ./other\n")
	mu.Lock()
	cache = map[string]bool{}
	mu.Unlock()
	if !Excluded(app) {
		t.Fatal("a go.work without the app's module must be excluded")
	}
	env := Env(app)
	if env[len(env)-1] != "GOWORK=off" {
		t.Fatalf("Env does not end with GOWORK=off: %q", env[len(env)-1])
	}

	write(t, filepath.Join(ws, "go.work"), "go 1.21\n\nuse (\n\t./other\n\t./app\n)\n")
	mu.Lock()
	cache = map[string]bool{}
	mu.Unlock()
	if Excluded(app) {
		t.Fatal("a go.work that uses the app's module must stay active")
	}
}

// Env sets PWD like os/exec does for a nil Cmd.Env, so the go command reports
// directories under the path the caller used, not a symlink-resolved one.
func TestEnvSetsPWD(t *testing.T) {
	dir := t.TempDir()
	env := Env(dir)
	want := "PWD=" + dir
	for _, kv := range env {
		if kv == want {
			return
		}
	}
	t.Fatalf("Env(%s) has no %s", dir, want)
}

// Through a symlinked directory go list reports the symlinked path, as it does
// with the implicit PWD of a nil Cmd.Env.
func TestEnvKeepsSymlinkedDirectory(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	real := t.TempDir()
	write(t, filepath.Join(real, "go.mod"), "module example.com/m\n\ngo 1.21\n")
	write(t, filepath.Join(real, "p", "p.go"), "package p\n")
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks not available:", err)
	}
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", "./p")
	cmd.Dir = link
	cmd.Env = Env(link)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(out)), filepath.Join(link, "p"); got != want {
		t.Fatalf("go list Dir = %s, want %s", got, want)
	}
}
