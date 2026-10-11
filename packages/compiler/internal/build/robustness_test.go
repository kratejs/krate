package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestRecoverPageConvertsPanic verifies a panic inside a page build becomes an
// error rather than crashing the build.
func TestRecoverPageConvertsPanic(t *testing.T) {
	if os.Getenv("KRATE_DEBUG") != "" {
		t.Skip("KRATE_DEBUG set: panics propagate by design")
	}
	res, css, err := recoverPage(func() (*PageResult, string, error) {
		panic("page boom")
	})
	if err == nil {
		t.Fatal("expected an error from a panicking page build")
	}
	if res != nil || css != "" {
		t.Fatalf("expected zero values on panic, got %v / %q", res, css)
	}
}

// TestRecoverPagePassesThrough verifies a normal result is untouched.
func TestRecoverPagePassesThrough(t *testing.T) {
	want := &PageResult{OutName: "index.html"}
	res, css, err := recoverPage(func() (*PageResult, string, error) {
		return want, "body{}", nil
	})
	if err != nil || res != want || css != "body{}" {
		t.Fatalf("recoverPage altered a clean result: %v %v %q", err, res, css)
	}
}

// TestGuardOutDirHardErrors verifies only truly destructive outDirs are refused.
func TestGuardOutDirHardErrors(t *testing.T) {
	root := t.TempDir()
	unsafe := []string{string(filepath.Separator), root}
	for _, out := range unsafe {
		b := &Builder{Root: root, Cfg: &config.Config{OutDir: out}}
		if err := b.guardOutDir(); err == nil {
			t.Errorf("outDir %q should have been rejected", out)
		}
	}
}

// TestGuardOutDirAllowsAncestorWithWarning verifies an outDir that contains the
// project root warns rather than failing.
func TestGuardOutDirAllowsAncestorWithWarning(t *testing.T) {
	root := t.TempDir()
	out := filepath.Dir(root)
	b := &Builder{Root: root, Cfg: &config.Config{OutDir: out}}
	if err := b.guardOutDir(); err != nil {
		t.Fatalf("ancestor outDir should warn, not error: %v", err)
	}
}

// TestGuardOutDirBypass verifies the escape hatch disables the check.
func TestGuardOutDirBypass(t *testing.T) {
	t.Setenv("KRATE_ALLOW_UNSAFE_OUTDIR", "1")
	root := t.TempDir()
	b := &Builder{Root: root, Cfg: &config.Config{OutDir: root}}
	if err := b.guardOutDir(); err != nil {
		t.Fatalf("bypass should allow any outDir: %v", err)
	}
}

func TestGuardOutDirAllowsNormal(t *testing.T) {
	root := t.TempDir()
	b := &Builder{Root: root, Cfg: &config.Config{OutDir: filepath.Join(root, "dist")}}
	if err := b.guardOutDir(); err != nil {
		t.Fatalf("normal outDir rejected: %v", err)
	}
}
