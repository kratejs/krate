package main

import (
	"errors"
	"os"
	"os/exec"
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

// An unknown checks.failOn must fail config loading for every command; it used
// to become "error" and let a CI gate pass.
func TestLoadConfigRejectsBadFailOn(t *testing.T) {
	for _, v := range []string{"bogus", "off"} {
		dir := writeProjectConfig(t, `export default { checks: { failOn: "`+v+`" } }`+"\n")
		for _, cmd := range []string{"build", "check", "dev"} {
			_, _, err := loadConfig(cliFlags{}, []string{cmd, dir})
			if err == nil || !strings.Contains(err.Error(), "checks.failOn") || !strings.Contains(err.Error(), `"warning"`) {
				t.Errorf("%s failOn=%q: err = %v", cmd, v, err)
			}
		}
	}
	dir := writeProjectConfig(t, `export default { checks: { failOn: "warning" } }`+"\n")
	if _, _, err := loadConfig(cliFlags{}, []string{"check", dir}); err != nil {
		t.Errorf("valid failOn rejected: %v", err)
	}
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

// The commands that accept --config without reading a config (fix, migrate,
// plugin add, doctor) must also refuse a missing file. They exit, so run the
// real entry point in a subprocess.
func TestMissingConfigFlagExitsNonZeroForEveryCommand(t *testing.T) {
	if os.Getenv("KRATE_TEST_RUN_MAIN") == "1" {
		os.Args = append([]string{"krate"}, strings.Split(os.Getenv("KRATE_TEST_ARGS"), "\n")...)
		main()
		os.Exit(0)
	}
	dir := writeProjectConfig(t, "export default { outDir: 'real-out' }\n")
	cmds := map[string][]string{
		"fix":          {"fix", dir},
		"migrate":      {"migrate", "nextjs", dir},
		"plugin add":   {"plugin", "add", "some-package", dir},
		"doctor":       {"doctor", dir},
		"build":        {"build", dir},
		"check":        {"check", dir},
		"inspect":      {"inspect", dir},
		"clean":        {"clean", dir},
		"config":       {"config", dir},
		"serve":        {"serve", dir},
		"start":        {"start", dir},
		"types":        {"types", dir},
		"dev":          {"dev", dir},
		"go:init":      {"go:init", dir},
		"typecheck":    {"typecheck", dir},
		"deploy":       {"deploy", dir},
		"mcp (stdio)":  {"mcp", dir},
		"serve --port": {"serve", dir, "--port", "4999"},
	}
	for name, args := range cmds {
		args = append(args, "--config", "typo.config.ts")
		c := exec.Command(os.Args[0], "-test.run=^TestMissingConfigFlagExitsNonZeroForEveryCommand$")
		c.Env = append(os.Environ(), "KRATE_TEST_RUN_MAIN=1", "KRATE_TEST_ARGS="+strings.Join(args, "\n"))
		out, err := c.CombinedOutput()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			t.Errorf("%s: want exit status 1, got err=%v\n%s", name, err, out)
			continue
		}
		if !strings.Contains(string(out), "typo.config.ts") {
			t.Errorf("%s: output should name the path:\n%s", name, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "dist")); err == nil {
		t.Error("a command ran against the default config and wrote dist")
	}
}
