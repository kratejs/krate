package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/environ"
)

// envDir points .env loading at a monorepo root; the config itself already
// sees those variables.
func TestLoadConfigEnvDirReadsMonorepoRoot(t *testing.T) {
	t.Cleanup(func() { environ.Dir = ""; environ.Current = nil })
	_ = os.Unsetenv("ENVDIR_TEST_OUT")
	_ = os.Unsetenv("ENVDIR_TEST_SECRET")

	mono := t.TempDir()
	app := filepath.Join(mono, "packages", "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(mono, ".env"), "ENVDIR_TEST_OUT=from-root-env\n")
	write(filepath.Join(mono, ".env.local"), "ENVDIR_TEST_SECRET=s3\n")
	write(filepath.Join(app, ".env"), "ENVDIR_TEST_OUT=project-env\n")
	write(filepath.Join(app, "krate.config.ts"), "export default { envDir: '../..', outDir: process.env.ENVDIR_TEST_OUT || 'unset' }\n")

	_, cfg, err := loadConfig(cliFlags{}, []string{"build", app})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(cfg.OutDir), "/from-root-env") {
		t.Errorf("config must see the envDir variables, outDir = %s", cfg.OutDir)
	}
	if cfg.EnvDir != mono {
		t.Errorf("envDir must resolve against the project: %s", cfg.EnvDir)
	}
	if environ.Current["ENVDIR_TEST_SECRET"] != "s3" || environ.Current["ENVDIR_TEST_OUT"] != "from-root-env" {
		t.Errorf("env = %v", environ.Current)
	}

	write(filepath.Join(app, "krate.config.ts"), "export default { envDir: 'nope' }\n")
	if _, _, err := loadConfig(cliFlags{}, []string{"build", app}); err == nil || !strings.Contains(err.Error(), "envDir") {
		t.Errorf("a missing envDir must be an error, got %v", err)
	}
}
