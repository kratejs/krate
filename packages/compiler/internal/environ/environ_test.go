package environ

import (
	"os"
	"path/filepath"
	"testing"
)

// unsetEnv removes key and restores the previous value (or unset) afterwards.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			os.Setenv(key, old)
		} else {
			os.Unsetenv(key)
		}
	})
}

func TestLoadPrecedence(t *testing.T) {
	unsetEnv(t, "A")
	unsetEnv(t, "B")
	unsetEnv(t, "C")
	unsetEnv(t, "D")
	unsetEnv(t, "Z")

	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(".env", "A=base\nB=base\nC=base\n")
	write(".env.production", "B=prod\nC=prod\n")
	write(".env.local", "C=local\nD=local\n")
	write(".env.production.local", "D=prodlocal\nZ=z\n")

	got, err := Load(root, "production")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := map[string]string{"A": "base", "B": "prod", "C": "local", "D": "prodlocal", "Z": "z"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("production: %s = %q, want %q", k, got[k], v)
		}
	}

	// A different mode must not pick up .env.production-style files.
	dev, err := Load(root, "development")
	if err != nil {
		t.Fatalf("Load(development): %v", err)
	}
	if _, hasZ := dev["Z"]; hasZ {
		t.Error("development mode leaked a production file")
	}
}
