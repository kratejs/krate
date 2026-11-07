package build

import (
	"os"
	"path/filepath"
	"strings"
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

// An outDir that contains the project root (a monorepo parent, "..") would be
// wiped with the sources inside it, so it is an error, like the root itself.
func TestGuardOutDirRefusesAncestorAndSourceDirs(t *testing.T) {
	root := t.TempDir()
	cfg := func(out string) *config.Config {
		return &config.Config{OutDir: out, PagesDir: filepath.Join(root, "src", "pages"), PublicDir: filepath.Join(root, "public")}
	}
	for name, out := range map[string]string{
		"parent of the project": filepath.Dir(root),
		"src":                   filepath.Join(root, "src"),
		"pages dir":             filepath.Join(root, "src", "pages"),
		"public dir":            filepath.Join(root, "public"),
	} {
		b := &Builder{Root: root, Cfg: cfg(out)}
		err := b.guardOutDir()
		if err == nil || !strings.Contains(err.Error(), "KR8350") {
			t.Errorf("%s: want a KR8350 error, got %v", name, err)
		}
	}
	for name, out := range map[string]string{
		"dist":                  filepath.Join(root, "dist"),
		"sibling outside":       filepath.Join(filepath.Dir(root), "site-dist"),
		"nested under the root": filepath.Join(root, "build", "out"),
	} {
		b := &Builder{Root: root, Cfg: cfg(out)}
		if err := b.guardOutDir(); err != nil {
			t.Errorf("%s: legitimate outDir rejected: %v", name, err)
		}
	}
}

// outDir ".." is refused before the build deletes anything, and the files of
// the parent directory survive; an outDir outside the project that holds no
// sources still builds.
func TestBuildAllRefusesParentOutDirBeforeDeleting(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "proj")
	writeProjectFile(t, root, "src/pages/index.tsx", "export default function P() { return <main>hi</main>; }\n")
	writeProjectFile(t, parent, "keep.txt", "precious")
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.Tailwind.Enabled = false
	cfg.OutDir = parent
	err := New(root, cfg).BuildAll()
	if err == nil || !strings.Contains(err.Error(), "KR8350") {
		t.Fatalf("want a KR8350 error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(parent, "keep.txt")); statErr != nil {
		t.Fatalf("the parent directory was wiped: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "src", "pages", "index.tsx")); statErr != nil {
		t.Fatalf("project sources were wiped: %v", statErr)
	}

	cfg.OutDir = filepath.Join(parent, "site-dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("an outDir outside the project must keep working: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(cfg.OutDir, "index.html")); statErr != nil {
		t.Fatalf("no output in ../site-dist: %v", statErr)
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
