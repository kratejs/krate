package environ

import (
	"os"
	"path/filepath"
	"testing"
)

// setEnv sets key and restores the previous value (or unset) afterwards.
func setEnv(t *testing.T, key, value string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
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

func TestMode(t *testing.T) {
	unsetEnv(t, "KRATE_ENV")
	unsetEnv(t, "NODE_ENV")

	if got := Mode("production"); got != "production" {
		t.Errorf("Mode default = %q, want production", got)
	}

	setEnv(t, "KRATE_ENV", "staging")
	unsetEnv(t, "NODE_ENV")
	if got := Mode("production"); got != "staging" {
		t.Errorf("Mode with KRATE_ENV = %q, want staging", got)
	}

	unsetEnv(t, "KRATE_ENV")
	setEnv(t, "NODE_ENV", "test")
	if got := Mode("development"); got != "test" {
		t.Errorf("Mode with NODE_ENV = %q, want test", got)
	}

	// KRATE_ENV beats NODE_ENV.
	setEnv(t, "KRATE_ENV", "edge")
	setEnv(t, "NODE_ENV", "test")
	if got := Mode("production"); got != "edge" {
		t.Errorf("Mode with both = %q, want edge", got)
	}
}

func TestParseBasics(t *testing.T) {
	unsetEnv(t, "HOME_BASE")
	tests := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{"simple", "FOO=bar", map[string]string{"FOO": "bar"}},
		{"empty value", "EMPTY=", map[string]string{"EMPTY": ""}},
		{"spaces around", "  A  =  spaced  ", map[string]string{"A": "spaced"}},
		{"blank lines and comments", "# c\n\nA=1\n\t\nB=2\n", map[string]string{"A": "1", "B": "2"}},
		{"inline comment", "A=1 # note", map[string]string{"A": "1"}},
		{"hash inside value preserved", "URL=http://x/#frag", map[string]string{"URL": "http://x/#frag"}},
		{"export prefix", "export B=2", map[string]string{"B": "2"}},
		{"single quote literal", "S='$HOME'", map[string]string{"S": "$HOME"}},
		{"single quoted spaces", "S='a b c'", map[string]string{"S": "a b c"}},
		{"double quoted keeps hash", "Q=\"a # b\"", map[string]string{"Q": "a # b"}},
		{"no equals is skipped", "NOVALUE", map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := map[string]string{}
			if err := parse([]byte(tt.input), m); err != nil {
				t.Fatalf("parse: %v", err)
			}
			for k, want := range tt.want {
				if m[k] != want {
					t.Errorf("%s: key %s = %q, want %q", tt.name, k, m[k], want)
				}
			}
		})
	}
}

func TestParseExpansion(t *testing.T) {
	unsetEnv(t, "EXPAND_ONE")
	unsetEnv(t, "EXPAND_MISSING")
	setEnv(t, "EXPAND_ONE", "one")

	m := map[string]string{}
	if err := parse([]byte("A=$EXPAND_ONE\nB=${EXPAND_ONE}-x\nC=pre-$EXPAND_MISSING\nD=$A\n"), m); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m["A"] != "one" {
		t.Errorf("A = %q, want %q", m["A"], "one")
	}
	if m["B"] != "one-x" {
		t.Errorf("B = %q, want %q", m["B"], "one-x")
	}
	if m["C"] != "pre-" {
		t.Errorf("C = %q, want %q (missing var expands to empty)", m["C"], "pre-")
	}
	if m["D"] != "one" {
		t.Errorf("D = %q, want %q (in-file chaining)", m["D"], "one")
	}
	if _, ok := m["EXPAND_ONE"]; ok {
		t.Error("file should only define its own keys, not the shell var")
	}
}

func TestParseDedupeLaterWins(t *testing.T) {
	unsetEnv(t, "DEDUPE_KEY")
	m := map[string]string{}
	if err := parse([]byte("DEDUPE_KEY=first\nDEDUPE_KEY=second\n"), m); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m["DEDUPE_KEY"] != "second" {
		t.Errorf("DEDUPE_KEY = %q, want %q (later line wins)", m["DEDUPE_KEY"], "second")
	}
}

func TestParseShellEnvWins(t *testing.T) {
	setEnv(t, "SHELL_KEY", "shell-value")
	m := map[string]string{}
	if err := parse([]byte("SHELL_KEY=file-value\n"), m); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := m["SHELL_KEY"]; ok {
		t.Errorf("SHELL_KEY present in merged map = %q; shell env must win", m["SHELL_KEY"])
	}
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

func TestLoadDevelopmentModeSelectsDevFile(t *testing.T) {
	unsetEnv(t, "MODE_KEY")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env.development"), []byte("MODE_KEY=dev\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root, "development")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got["MODE_KEY"] != "dev" {
		t.Errorf("MODE_KEY = %q, want dev", got["MODE_KEY"])
	}
}

func TestLoadShellEnvWinsAcrossFiles(t *testing.T) {
	setEnv(t, "LIVE_KEY", "shell")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("LIVE_KEY=base\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env.production"), []byte("LIVE_KEY=prod\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root, "production")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := got["LIVE_KEY"]; ok {
		t.Errorf("LIVE_KEY = %q; shell env must win over every file", got["LIVE_KEY"])
	}
}

func TestLoadMissingFiles(t *testing.T) {
	got, err := Load(t.TempDir(), "production")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d keys from an empty project, want 0", len(got))
	}
}

func TestKVListSorted(t *testing.T) {
	got := KVList(map[string]string{"b": "2", "a": "1", "c": "3"})
	want := []string{"a=1", "b=2", "c=3"}
	if len(got) != len(want) {
		t.Fatalf("KVList length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("KVList[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
