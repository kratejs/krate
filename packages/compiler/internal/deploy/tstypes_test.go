package deploy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestTSDeployTargetsMatchAdapters keeps @krate/core's DeployConfig.target in
// step with the targets krate deploy accepts.
func TestTSDeployTargetsMatchAdapters(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "core", "src", "config.ts"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)export interface DeployConfig \{.*?\n\s*target\?:\s*([^;]+);`).FindSubmatch(data)
	if m == nil {
		t.Fatal("DeployConfig.target not found")
	}
	for _, target := range Targets {
		if !strings.Contains(string(m[1]), `"`+target+`"`) {
			t.Errorf("DeployConfig.target %s lacks %q", m[1], target)
		}
	}
}
