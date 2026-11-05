package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/migrate"
)

func TestLoadConfigRejectsMissingProjectDir(t *testing.T) {
	parent := t.TempDir()
	typo := filepath.Join(parent, "typo-dir")
	_, _, err := loadConfig(cliFlags{}, []string{"build", typo})
	if err == nil || !strings.Contains(err.Error(), "project directory "+typo+" does not exist") {
		t.Fatalf("error = %v, want a missing project directory message", err)
	}
	if _, statErr := os.Stat(typo); !os.IsNotExist(statErr) {
		t.Fatalf("the missing directory was created (stat err = %v)", statErr)
	}

	file := filepath.Join(parent, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadConfig(cliFlags{}, []string{"build", file}); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("error = %v, want a not-a-directory message", err)
	}
}

func TestDisplayOutputUnsetIsNotServer(t *testing.T) {
	if got := displayOutput(""); strings.HasPrefix(got, "server") {
		t.Errorf("unset output shown as %q", got)
	}
	if got := displayOutput("static"); got != "static" {
		t.Errorf("static output shown as %q", got)
	}
}

func TestOverwriteCount(t *testing.T) {
	rep := &migrate.Report{Files: []migrate.PlannedFile{
		{Target: "a", Overwrites: true},
		{Target: "b"},
		{Target: "c", Overwrites: true},
	}}
	if got := overwriteCount(rep); got != 2 {
		t.Errorf("overwriteCount = %d, want 2", got)
	}
}
