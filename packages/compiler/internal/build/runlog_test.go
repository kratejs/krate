package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestStartRunLogOffByDefault(t *testing.T) {
	root := t.TempDir()
	stop := StartRunLog(root, &config.Config{}, "build")
	if path := stop(); path != "" {
		t.Fatalf("log path = %q, want none when logs are not enabled", path)
	}
	if _, err := os.Stat(filepath.Join(root, ".krate", "logs")); err == nil {
		t.Fatal("logs directory created while logs are disabled")
	}
}

func TestStartRunLogWritesTimestampedFile(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Logs: config.LogsConfig{Enabled: true}}
	oldOut, oldErr := os.Stdout, os.Stderr
	stop := StartRunLog(root, cfg, "build")
	_, _ = os.Stdout.WriteString("\x1b[32mhello from stdout\x1b[0m\n")
	_, _ = os.Stderr.WriteString("oops on stderr\n")
	path := stop()
	if os.Stdout != oldOut || os.Stderr != oldErr {
		t.Fatal("streams not restored")
	}
	if filepath.Dir(path) != filepath.Join(root, ".krate", "logs") || !strings.HasSuffix(path, ".log") {
		t.Fatalf("unexpected log path %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"krate build", "hello from stdout", "oops on stderr"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("log misses %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), "\x1b") {
		t.Errorf("log keeps ANSI codes:\n%q", data)
	}
}

func TestStartRunLogRefusesDirOutsideProject(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Logs: config.LogsConfig{Enabled: true, Dir: ".."}}
	if path := StartRunLog(root, cfg, "build")(); path != "" {
		t.Fatalf("wrote a log to %q outside the project", path)
	}
}

func TestPruneRunLogsKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	names := []string{"2026-01-01T00-00-01.log", "2026-01-01T00-00-02.log", "2026-01-01T00-00-03.log", "notes.txt"}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pruneRunLogs(dir, 2, filepath.Join(dir, names[2]))
	for n, want := range map[string]bool{names[0]: false, names[1]: true, names[2]: true, "notes.txt": true} {
		_, err := os.Stat(filepath.Join(dir, n))
		if (err == nil) != want {
			t.Errorf("%s exists = %v, want %v", n, err == nil, want)
		}
	}
	pruneRunLogs(dir, -1, "")
	if _, err := os.Stat(filepath.Join(dir, names[1])); err != nil {
		t.Error("keep < 0 must keep every log")
	}
}
