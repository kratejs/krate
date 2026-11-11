package gobind

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func nativeModuleDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "native"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatalf("host module not found at %s", dir)
	}
	return dir
}

func needGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not available")
	}
}

// newApp copies testdata/<fixture> into a temp dir and gives it a go.mod that
// requires the host module from this worktree, so no network is needed.
func newApp(t *testing.T, fixture string) string {
	t.Helper()
	needGo(t)
	root := t.TempDir()
	src := filepath.Join("testdata", fixture)
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(root, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	writeGoMod(t, root)
	return root
}

func writeGoMod(t *testing.T, root string) {
	t.Helper()
	native := nativeModuleDir(t)
	mod := "module example.com/app\n\ngo 1.26.1\n\n" +
		"require github.com/kratejs/krate/packages/native v0.0.0\n\n" +
		"replace github.com/kratejs/krate/packages/native => " + native + "\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(native, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.sum"), sum, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOWORK", "off")
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run with -update): %v", path, err)
	}
	if string(want) != string(got) {
		t.Errorf("%s differs from the golden file (run with -update to accept)\n--- got ---\n%s", name, got)
	}
}
