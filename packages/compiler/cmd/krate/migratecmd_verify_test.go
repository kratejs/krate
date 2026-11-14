package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/migrate"
)

func TestUnresolvedKratePackages(t *testing.T) {
	root := t.TempDir()
	rep := &migrate.Report{
		Root: root,
		Files: []migrate.PlannedFile{
			{Target: "krate.config.ts", Content: []byte("import { defineConfig } from '@krate/core';")},
			{Target: "src/pages/index.tsx", Content: []byte("import { signal } from '@krate/runtime';")},
		},
	}
	got := unresolvedKratePackages(rep)
	if len(got) != 2 || got[0] != "@krate/core" || got[1] != "@krate/runtime" {
		t.Fatalf("nothing installed: got %v, want core and runtime", got)
	}

	// Install a minimal @krate/core; only the runtime stays missing.
	pkg := filepath.Join(root, "node_modules", "@krate", "core")
	if err := os.MkdirAll(pkg, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"@krate/core","main":"index.js"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "index.js"), []byte("export {};"), 0644); err != nil {
		t.Fatal(err)
	}
	got = unresolvedKratePackages(rep)
	if len(got) != 1 || got[0] != "@krate/runtime" {
		t.Fatalf("core installed: got %v, want only runtime", got)
	}
}

func TestMigrateVerifyStopsBeforeWritingWhenPackagesAreMissing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"dependencies":{"next":"16.0.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "app"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app", "page.tsx"), []byte("export default function P() { return <main/>; }"), 0644); err != nil {
		t.Fatal(err)
	}
	code, out := runKrate(t, "migrate", "nextjs", root, "--apply", "--verify")
	if code == 0 {
		t.Fatalf("want a non-zero exit, output:\n%s", out)
	}
	if !strings.Contains(out, "@krate/core") || !strings.Contains(out, "--update-package") || !strings.Contains(out, "nothing was written") {
		t.Errorf("message should name the package and the fix:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "krate.config.ts")); !os.IsNotExist(err) {
		t.Errorf("krate.config.ts was written before the check: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "app", "page.tsx")); err != nil {
		t.Errorf("original app tree must be untouched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".krate")); !os.IsNotExist(err) {
		t.Errorf("no backup directory should exist: %v", err)
	}
}
