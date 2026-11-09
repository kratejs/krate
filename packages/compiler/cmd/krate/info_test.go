package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPackageManagerFor(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"field wins over lockfile": {map[string]string{"package.json": `{"packageManager":"yarn@4.1.0"}`, "pnpm-lock.yaml": ""}, "yarn"},
		"pnpm lock":                {map[string]string{"pnpm-lock.yaml": ""}, "pnpm"},
		"bun lock":                 {map[string]string{"bun.lock": ""}, "bun"},
		"npm lock":                 {map[string]string{"package-lock.json": "{}"}, "npm"},
		"nothing":                  {map[string]string{}, "npm"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			for f, c := range tc.files {
				writeTestFile(t, filepath.Join(root, f), c)
			}
			if got := packageManagerFor(root); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPackageManagerForFindsWorkspaceLock(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "pnpm-lock.yaml"), "")
	sub := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := packageManagerFor(sub); got != "pnpm" {
		t.Errorf("got %q, want pnpm", got)
	}
}

func TestInstalledKratePackages(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "node_modules", "@krate", "runtime", "package.json"), `{"name":"@krate/runtime","version":"0.5.1"}`)
	writeTestFile(t, filepath.Join(root, "node_modules", "@krate", "core", "package.json"), `{"name":"@krate/core","version":"0.5.0"}`)
	writeTestFile(t, filepath.Join(root, "node_modules", "@krate", "broken", "package.json"), `{`)
	got := installedKratePackages(root)
	if len(got) != 2 || got[0].Name != "@krate/core" || got[1].Version != "0.5.1" {
		t.Fatalf("unexpected packages %+v", got)
	}
}

func TestInfoReportIsPasteReady(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "krate.config.ts"), "export default { output: \"static\" }\n")
	writeTestFile(t, filepath.Join(root, "node_modules", "@krate", "core", "package.json"), `{"name":"@krate/core","version":"0.5.0"}`)
	rep := collectInfo(root, cliFlags{})
	var out bytes.Buffer
	writeInfo(&out, rep)
	for _, want := range []string{"### Krate environment", "- krate: v", "- OS: ", "@krate/core 0.5.0", "output: static"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report misses %q:\n%s", want, out.String())
		}
	}
}

func TestInfoReportsBrokenConfig(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "krate.config.ts"), "export default {")
	rep := collectInfo(root, cliFlags{})
	if rep.ConfigError == "" || rep.Config != nil {
		t.Fatalf("want a config error, got %+v", rep)
	}
}
