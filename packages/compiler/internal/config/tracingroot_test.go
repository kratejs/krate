package config

import (
	"path/filepath"
	"testing"
)

func TestOutputFileTracingRootAllowsParentGlobs(t *testing.T) {
	cfg := Default()
	cfg.OutputFileTracingIncludes = map[string][]string{"*": {"../sibling/dist/x.mjs"}}
	if _, err := cfg.Validate(); err == nil {
		t.Fatal("a glob that climbs out of the project needs outputFileTracingRoot")
	}
	cfg.OutputFileTracingRoot = ".."
	if _, err := cfg.Validate(); err != nil {
		t.Fatalf("with a tracing root the glob is valid: %v", err)
	}
	cfg.OutputFileTracingIncludes = map[string][]string{"*": {"/etc/passwd"}}
	if _, err := cfg.Validate(); err == nil {
		t.Fatal("absolute globs stay invalid")
	}
}

func TestOutputFileTracingRootResolvesAgainstProject(t *testing.T) {
	cfg := Default()
	cfg.OutputFileTracingRoot = "../.."
	root := filepath.Join(string(filepath.Separator), "repo", "apps", "web")
	cfg.Resolve(root)
	if want := filepath.Join(string(filepath.Separator), "repo"); cfg.OutputFileTracingRoot != want {
		t.Errorf("outputFileTracingRoot = %q, want %q", cfg.OutputFileTracingRoot, want)
	}
	if !knownTopLevelKeys["outputFileTracingRoot"] {
		t.Error("outputFileTracingRoot must be a known key")
	}
}
