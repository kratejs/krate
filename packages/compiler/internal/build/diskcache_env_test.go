package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A cached page was built with the env values of that run. Changing a public
// value (shell or .env file) must rebuild it, not replay the old HTML.
func TestDiskCacheInvalidatesOnPublicEnvChange(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <h1>v={process.env.KRATE_PUBLIC_VAL}</h1>; }`,
	})
	read := func() string {
		data, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	build := func(env map[string]string) string {
		nb := New(root, b.Cfg)
		nb.Env = env
		if _, err := captureOutput(func() error { return nb.BuildAll() }); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
		return read()
	}

	t.Setenv("KRATE_PUBLIC_VAL", "shell-one")
	if got := build(nil); !strings.Contains(got, "v=shell-one") {
		t.Fatalf("first build: %s", got)
	}
	t.Setenv("KRATE_PUBLIC_VAL", "shell-two")
	if got := build(nil); !strings.Contains(got, "v=shell-two") {
		t.Errorf("shell change replayed a stale page: %s", got)
	}

	os.Unsetenv("KRATE_PUBLIC_VAL")
	if got := build(map[string]string{"KRATE_PUBLIC_VAL": "file-one"}); !strings.Contains(got, "v=file-one") {
		t.Errorf("env file value: %s", got)
	}
	if got := build(map[string]string{"KRATE_PUBLIC_VAL": "file-two"}); !strings.Contains(got, "v=file-two") {
		t.Errorf("env file change replayed a stale page: %s", got)
	}
}
