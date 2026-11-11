package goenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
