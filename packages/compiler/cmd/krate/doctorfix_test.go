package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitignoreCovers(t *testing.T) {
	for _, l := range []string{".krate", ".krate/", "/.krate", "/.krate/", ".krate/*", "  .krate/  "} {
		if !gitignoreCovers([]string{"node_modules", l}, ".krate") {
			t.Errorf("%q should cover .krate", l)
		}
	}
	for _, l := range []string{".krate-other", "# .krate", "krate", "!.krate"} {
		if gitignoreCovers([]string{l}, ".krate") {
			t.Errorf("%q must not cover .krate", l)
		}
	}
	if !gitignoreCovers([]string{"build/out/"}, "build/out") {
		t.Error("nested output directory not matched")
	}
}

func TestGitignoreFixAddsOnlyMissingEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, ".gitignore"), "node_modules\ndist")

	fix := gitignoreFix(root, []string{".krate", "dist"})
	if fix == nil || !strings.Contains(fix.Problem, ".krate") || strings.Contains(fix.Problem, "dist") {
		t.Fatalf("unexpected fix %+v", fix)
	}
	if _, err := fix.Apply(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	want := "node_modules\ndist\n\n# krate\n.krate/\n"
	if string(data) != want {
		t.Errorf(".gitignore = %q, want %q", data, want)
	}
	if again := gitignoreFix(root, []string{".krate", "dist"}); again != nil {
		t.Error("fix is not idempotent")
	}
}

func TestGitignoreFixSkipsNonGitProjects(t *testing.T) {
	if fix := gitignoreFix(t.TempDir(), []string{".krate"}); fix != nil {
		t.Errorf("a project outside a git repo needs no .gitignore: %+v", fix)
	}
}

func TestGitignoreFixCreatesFile(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	fix := gitignoreFix(root, []string{".krate"})
	if fix == nil {
		t.Fatal("expected a fix")
	}
	if _, err := fix.Apply(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if string(data) != "# krate\n.krate/\n" {
		t.Errorf(".gitignore = %q", data)
	}
}

func TestVersionMismatchFix(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "package.json"), `{"dependencies":{"@krate/core":"^0.4.0"}}`)
	writeTestFile(t, filepath.Join(root, "node_modules", "@krate", "core", "package.json"), `{"name":"@krate/core","version":"0.4.2"}`)
	installed := installedKratePackages(root)

	var ran []string
	run := func(dir string, c upgradeCommand) error { ran = append(ran, c.String()); return nil }
	fix := versionMismatchFix(root, installed, "0.5.1", run)
	if fix == nil {
		t.Fatal("an older @krate/core should be fixable")
	}
	if _, err := fix.Apply(); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || !strings.Contains(ran[0], "@krate/core@0.5.1") {
		t.Errorf("ran %v", ran)
	}

	if versionMismatchFix(root, installed, "dev", run) != nil {
		t.Error("a dev build has no version to align to")
	}
	if versionMismatchFix(root, installed, "0.4.0", run) != nil {
		t.Error("a newer installed package must not be downgraded")
	}
}

func TestRunDoctorFixesDryRunAndApply(t *testing.T) {
	applied := 0
	fixes := []doctorFix{
		{Name: "a", Problem: "broken a", Apply: func() (string, error) { applied++; return "fixed a", nil }},
		{Name: "b", Problem: "broken b", Apply: func() (string, error) { return "", errors.New("nope") }},
	}
	var out bytes.Buffer
	if failed := runDoctorFixes(&out, fixes, false); failed != 0 || applied != 0 {
		t.Fatalf("a dry run must not apply anything (failed=%d applied=%d)", failed, applied)
	}
	if !strings.Contains(out.String(), "krate doctor --fix") || !strings.Contains(out.String(), "broken b") {
		t.Errorf("dry run output:\n%s", out.String())
	}
	out.Reset()
	if failed := runDoctorFixes(&out, fixes, true); failed != 1 || applied != 1 {
		t.Fatalf("failed=%d applied=%d", failed, applied)
	}
	if !strings.Contains(out.String(), "fixed a") || !strings.Contains(out.String(), "nope") {
		t.Errorf("apply output:\n%s", out.String())
	}
}

func TestOutDirIgnoreName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	for out, want := range map[string]string{
		filepath.Join(root, "dist"):              "dist",
		filepath.Join(root, "build", "out"):      "build/out",
		root:                                     "",
		filepath.Join(filepath.Dir(root), "out"): "",
	} {
		if got := outDirIgnoreName(root, out); got != want {
			t.Errorf("outDirIgnoreName(%q) = %q, want %q", out, got, want)
		}
	}
}
