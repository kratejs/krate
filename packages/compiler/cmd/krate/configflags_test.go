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
