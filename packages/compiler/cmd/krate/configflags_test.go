package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProjectConfig(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "krate.config.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Every command that loads a config goes through loadConfig; a mistyped
// --config must fail there instead of using the defaults.
func TestLoadConfigRejectsMissingConfigFlag(t *testing.T) {
	dir := writeProjectConfig(t, "export default { outDir: 'real-out' }\n")
	for _, cmd := range []string{"build", "dev", "serve", "start", "check", "inspect", "types", "typecheck", "deploy", "clean", "config", "mcp"} {
		_, _, err := loadConfig(cliFlags{ConfigPath: "typo.config.ts"}, []string{cmd, dir})
		if err == nil || !strings.Contains(err.Error(), "typo.config.ts") || !strings.Contains(err.Error(), "not found") {
			t.Errorf("%s: err = %v, want a not-found error naming the path", cmd, err)
		}
	}

	// A real explicit path, and no flag at all, still load.
	if err := os.WriteFile(filepath.Join(dir, "other.config.ts"), []byte("export default { outDir: 'other-out' }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, cfg, err := loadConfig(cliFlags{ConfigPath: "other.config.ts"}, []string{"build", dir})
	if err != nil || !strings.HasSuffix(filepath.ToSlash(cfg.OutDir), "/other-out") {
		t.Errorf("explicit config: cfg=%v err=%v", cfg, err)
	}
	if _, _, err := loadConfig(cliFlags{}, []string{"build", t.TempDir()}); err != nil {
		t.Errorf("no config file and no flag must stay valid: %v", err)
	}
}
