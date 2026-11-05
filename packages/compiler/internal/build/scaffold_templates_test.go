package build

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/typecheck"
)

// TestScaffoldedTemplatesBuildAndTypecheck scaffolds every template exactly as
// create-krate-app and create-krate-docs would (placeholders filled in, the
// gitignore file renamed), installs the workspace packages the way
// `npm install` would link them, and runs `krate build` and `krate typecheck`.
// The baseUrl break and the missing .bin both reached users because templates
// were never built against the compiler with the tsconfig as shipped.
func TestScaffoldedTemplatesBuildAndTypecheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links workspace packages with symlinks; covered on Linux and macOS")
	}
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	ensureRuntimeDist(t)
	for _, p := range []string{"core", "plugin"} {
		if _, err := os.Stat(filepath.Join(repo, "packages", p, "dist")); err != nil {
			requireE2E(t, "packages/%s is not built (run pnpm -r build): %v", p, err)
			return
		}
	}
	tsc, _ := filepath.Glob(filepath.Join(repo, "node_modules", ".pnpm", "typescript@*", "node_modules", "typescript"))
	if len(tsc) == 0 {
		requireE2E(t, "typescript is not installed in the workspace")
		return
	}

	templates := []struct{ name, dir, display string }{
		{"app", filepath.Join("create-krate-app", "templates", "app"), "My Site"},
		{"docs", filepath.Join("create-krate-docs", "templates", "docs"), "My Site"},
	}
	for _, tpl := range templates {
		t.Run(tpl.name, func(t *testing.T) {
			root := t.TempDir()
			copyFixtureTree(t, filepath.Join(repo, "packages", tpl.dir), root)
			if err := os.Rename(filepath.Join(root, "gitignore"), filepath.Join(root, ".gitignore")); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			fillPlaceholders(t, root, map[string]string{
				"__PROJECT_NAME__":         "my-site",
				"__PROJECT_DISPLAY_NAME__": tpl.display,
				"__KRATE_VERSION__":        "0.5.0",
			})
			// What `npm install` leaves behind: the @krate packages and typescript.
			nm := filepath.Join(root, "node_modules")
			entries, _ := os.ReadDir(filepath.Join(repo, "packages"))
			for _, e := range entries {
				if _, err := os.Stat(filepath.Join(repo, "packages", e.Name(), "package.json")); err != nil {
					continue
				}
				symlink(t, filepath.Join(repo, "packages", e.Name()), filepath.Join(nm, "@krate", e.Name()))
			}
			symlink(t, tsc[0], filepath.Join(nm, "typescript"))
			symlink(t, filepath.Join(repo, "packages", "runtime"), filepath.Join(root, "packages", "runtime"))

			config.Warnings = nil
			cfg, err := config.Load(root)
			if err != nil {
				t.Fatalf("config.Load: %v", err)
			}
			cfg.Resolve(root)
			for _, w := range config.Warnings {
				t.Errorf("config warning on a fresh scaffold: %s", w)
			}
			if err := New(root, cfg).BuildAll(); err != nil {
				t.Fatalf("krate build on the scaffold: %v", err)
			}
			home, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(home), tpl.display) {
				t.Errorf("index.html does not carry the project name %q", tpl.display)
			}
			if _, err := os.Stat(filepath.Join(cfg.OutDir, ".krate", "manifest.json")); err != nil {
				t.Errorf("dist/.krate/manifest.json missing: %v", err)
			}

			out, ok, err := typecheck.Run(root)
			if err != nil {
				requireE2E(t, "krate typecheck cannot run: %v", err)
				return
			}
			if !ok {
				t.Fatalf("krate typecheck on the scaffold failed:\n%s", out)
			}
		})
	}
}

func fillPlaceholders(t *testing.T, root string, repl map[string]string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		s := string(data)
		for k, v := range repl {
			s = strings.ReplaceAll(s, k, v)
		}
		if s == string(data) {
			return nil
		}
		return os.WriteFile(path, []byte(s), 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}
