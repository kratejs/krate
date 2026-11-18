package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pkgJSON(t *testing.T, rep *Report) map[string]any {
	t.Helper()
	f := findPlan(rep, "package.json")
	if f == nil {
		t.Fatalf("package.json not planned: %+v", rep.Files)
	}
	var out map[string]any
	if err := json.Unmarshal(f.Content, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func depOf(pkg map[string]any, name string) (string, bool) {
	for _, key := range []string{"dependencies", "devDependencies"} {
		if m, ok := pkg[key].(map[string]any); ok {
			if v, ok := m[name].(string); ok {
				return v, true
			}
		}
	}
	return "", false
}

func withReleases(t *testing.T, rows ...krateRelease) {
	t.Helper()
	old := krateReleases
	krateReleases = append([]krateRelease(nil), old...)
	for _, row := range rows {
		for i := range krateReleases {
			if krateReleases[i].Name == row.Name {
				krateReleases[i] = row
			}
		}
	}
	t.Cleanup(func() { krateReleases = old })
}

// The generated table must list the version in each package's manifest.
func TestKrateVersionTableMatchesManifests(t *testing.T) {
	if len(krateReleases) == 0 {
		t.Fatal("empty table")
	}
	for _, r := range krateReleases {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", r.Dir, "package.json"))
		if err != nil {
			t.Skipf("not inside a Krate checkout: %v", err)
		}
		var m struct{ Name, Version string }
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if m.Name != r.Name || m.Version != r.Version {
			t.Errorf("table has %s@%s, packages/%s is %s@%s; run node scripts/gen-krate-versions.mjs", r.Name, r.Version, r.Dir, m.Name, m.Version)
		}
	}
}

func TestPackageUpdateIsTheDefaultAndOptOutLeavesItAlone(t *testing.T) {
	root := gapsProject(t, map[string]string{"src/app/page.tsx": "export default function P() { return <main/>; }"})
	rep, err := ScanNextWithOptions(root, ScanOptions{KrateVersion: "1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	pkg := pkgJSON(t, rep)
	if v, _ := depOf(pkg, "@krate/core"); v != "^"+mustRelease(t, "@krate/core").Version {
		t.Errorf("@krate/core = %q", v)
	}
	rep, err = ScanNextWithOptions(root, ScanOptions{KrateVersion: "1.2.3", NoUpdatePackage: true})
	if err != nil {
		t.Fatal(err)
	}
	if findPlan(rep, "package.json") != nil {
		t.Error("package.json planned although updating was switched off")
	}
	if !hasManual(rep, "package.json", "Next.js dependencies and scripts are still present") {
		t.Errorf("opt-out lost the manual item: %+v", rep.Manual)
	}
}

func mustRelease(t *testing.T, name string) krateRelease {
	t.Helper()
	r, ok := krateReleaseFor(name)
	if !ok {
		t.Fatalf("%s missing from the table", name)
	}
	return r
}

func TestMigratedImportsGetTheirOwnPackageVersions(t *testing.T) {
	withReleases(t,
		krateRelease{Name: "@krate/runtime", Dir: "runtime", Version: "0.5.0-rc.4"},
		krateRelease{Name: "@krate/three", Dir: "three", Version: "0.1.0"},
		krateRelease{Name: "@krate/konva", Dir: "konva", Version: "0.2.7"},
		krateRelease{Name: "@krate/auth", Dir: "auth", Version: "0.3.0"},
	)
	root := nextAuthProject(t, nextAuthFixtureConfig)
	writeNextFile(t, root, "src/app/page.tsx", "import { auth } from '@/auth';\nimport '@krate/three';\nimport '@krate/konva';\nexport default async function H() { await auth(); return <main/>; }\n")
	rep, err := ScanNextWithOptions(root, ScanOptions{KrateVersion: "0.5.0-rc.4"})
	if err != nil {
		t.Fatal(err)
	}
	pkg := pkgJSON(t, rep)
	for name, want := range map[string]string{
		"@krate/runtime": "^0.5.0-rc.4", "@krate/three": "^0.1.0", "@krate/konva": "^0.2.7", "@krate/auth": "^0.3.0",
	} {
		if got, _ := depOf(pkg, name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestDevBuildLinksToTheKrateCheckout(t *testing.T) {
	checkout := t.TempDir()
	for _, p := range []string{"runtime", "core", "components", "three"} {
		writeNextFile(t, checkout, "packages/"+p+"/package.json", `{"name":"@krate/`+p+`"}`)
	}
	root := gapsProject(t, map[string]string{"src/app/page.tsx": "import '@krate/three';\nexport default function P() { return <main/>; }"})
	rep, err := ScanNextWithOptions(root, ScanOptions{KrateVersion: "dev", KrateRoot: checkout})
	if err != nil {
		t.Fatal(err)
	}
	pkg := pkgJSON(t, rep)
	rel, _ := filepath.Rel(root, filepath.Join(checkout, "packages", "runtime"))
	if got, _ := depOf(pkg, "@krate/runtime"); got != "link:"+filepath.ToSlash(rel) {
		t.Errorf("@krate/runtime = %q", got)
	}
	if got, _ := depOf(pkg, "@krate/three"); !strings.HasPrefix(got, "link:") || !strings.HasSuffix(got, "/packages/three") {
		t.Errorf("@krate/three = %q", got)
	}
	if hasInfo(rep, "development build") {
		t.Errorf("a checkout was found, no fallback note expected: %v", rep.Info)
	}
}

func TestDevBuildWithoutACheckoutWritesLatestAndSaysSo(t *testing.T) {
	t.Setenv("KRATE_BINARY", "")
	root := gapsProject(t, map[string]string{"src/app/page.tsx": "export default function P() { return <main/>; }"})
	rep, err := ScanNextWithOptions(root, ScanOptions{KrateVersion: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := depOf(pkgJSON(t, rep), "@krate/core"); got != "latest" {
		t.Errorf("@krate/core = %q", got)
	}
	if !hasInfo(rep, `written as "latest"`) {
		t.Errorf("no info line: %v", rep.Info)
	}
}

func TestFindKrateCheckoutFromKrateBinaryAndProjectModules(t *testing.T) {
	checkout := t.TempDir()
	writeNextFile(t, checkout, "packages/core/package.json", `{"name":"@krate/core"}`)
	writeNextFile(t, checkout, "packages/compiler/go.mod", "module x\n")
	t.Setenv("KRATE_BINARY", filepath.Join(checkout, "packages", "core", "bin", "krate"))
	real, _ := filepath.EvalSymlinks(checkout)
	if got := findKrateCheckout(t.TempDir()); got != real {
		t.Errorf("from KRATE_BINARY = %q, want %q", got, real)
	}
	t.Setenv("KRATE_BINARY", "")
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(checkout, "packages", "core"), filepath.Join(proj, "node_modules", "@krate", "core")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if got := findKrateCheckout(proj); got != real {
		t.Errorf("from node_modules = %q, want %q", got, real)
	}
}

func TestNextPackagesAreRemovedOnlyWhenNothingImportsThem(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"package.json":     `{"dependencies":{"next":"16.0.0","next-auth":"5.0.0","@next/env":"16.0.0","react":"19.0.0"},"devDependencies":{"typescript":"5"}}`,
		"src/app/page.tsx": "export default function P() { return <main/>; }",
		"src/lib/env.ts":   "import { loadEnvConfig } from '@next/env';\nexport const load = () => loadEnvConfig('.');\n",
		"next.config.ts":   "import type { NextConfig } from 'next';\nexport default {} satisfies NextConfig;\n",
	})
	rep, err := ScanNextWithOptions(root, ScanOptions{KrateVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	pkg := pkgJSON(t, rep)
	for name, want := range map[string]bool{"next": false, "next-auth": false, "@next/env": true, "react": true} {
		if _, ok := depOf(pkg, name); ok != want {
			t.Errorf("%s present = %v, want %v", name, ok, want)
		}
	}
	if !hasInfo(rep, "kept @next/env in package.json: still imported by src/lib/env.ts") {
		t.Errorf("importer not reported: %v", rep.Info)
	}
}

func TestOnlyTheInstallCommandIsLeftForAPnpmWorkspace(t *testing.T) {
	ws := t.TempDir()
	writeNextFile(t, ws, "pnpm-workspace.yaml", "packages:\n  - packages/*\n")
	writeNextFile(t, ws, "packages/web/package.json", `{"scripts":{"dev":"next dev --turbopack -p 4000","build":"next build","start":"next start","typecheck":"next typegen && tsc --noEmit","lint":"next lint","predev":"node prep.mjs"},"dependencies":{"next":"16.0.0","react":"19.0.0"}}`)
	writeNextFile(t, ws, "packages/web/src/app/page.tsx", "export default function P() { return <main/>; }")
	rep, err := ScanNextWithOptions(filepath.Join(ws, "packages", "web"), ScanOptions{KrateVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Manual) != 1 || rep.Manual[0].Source != "package.json" || !strings.Contains(rep.Manual[0].Hint, "`pnpm install` in ../..") {
		t.Fatalf("manual items = %+v", rep.Manual)
	}
	scripts := pkgJSON(t, rep)["scripts"].(map[string]any)
	for name, want := range map[string]string{
		"dev": "krate dev --port 4000", "build": "krate build", "start": "krate start",
		"typecheck": "krate types && tsc --noEmit", "lint": "next lint", "predev": "node prep.mjs",
	} {
		if scripts[name] != want {
			t.Errorf("scripts.%s = %v, want %q", name, scripts[name], want)
		}
	}
	// next lint still needs the binary.
	if _, ok := depOf(pkgJSON(t, rep), "next"); !ok {
		t.Error("next was removed although a script runs next lint")
	}
	if !hasInfo(rep, "scripts.lint still runs `next lint`") {
		t.Errorf("no info line for next lint: %v", rep.Info)
	}
}

func TestNodeDeployScriptsAndNoRemainingNext(t *testing.T) {
	rep := scanConfig(t, map[string]string{
		"next.config.js": `module.exports = { output: 'standalone' };`,
		"package.json":   `{"scripts":{"build":"next build","start":"node .next/standalone/server.js","typecheck":"next typegen && tsc"},"dependencies":{"next":"16.0.0"}}`,
	})
	scripts := pkgJSON(t, rep)["scripts"].(map[string]any)
	if scripts["build"] != "krate deploy" || scripts["start"] != "node dist/.krate/server/server.mjs" || scripts["typecheck"] != "krate types && tsc" {
		t.Errorf("scripts = %v", scripts)
	}
	if _, ok := depOf(pkgJSON(t, rep), "next"); ok {
		t.Error("next should be gone")
	}
	if len(rep.Manual) != 1 {
		t.Errorf("manual = %+v", rep.Manual)
	}
}

func TestPackageJSONKeepsItsLayout(t *testing.T) {
	orig := `{
  "name": "@dd/web",
  "private": true,
  "scripts": {
    "dev": "next dev",
    "build": "next build && node post.mjs",
    "zeta": "echo a && echo b"
  },
  "dependencies": {
    "next": "16.0.0",
    "react": "19.0.0"
  },
  "pnpm": { "onlyBuiltDependencies": ["esbuild"] },
  "version": "1.0.0"
}
`
	root := gapsProject(t, map[string]string{"package.json": orig, "src/app/page.tsx": "export default function P() { return <main/>; }"})
	rep, err := ScanNextWithOptions(root, ScanOptions{KrateVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	got := string(findPlan(rep, "package.json").Content)
	var order []string
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, `  "`) {
			order = append(order, strings.Fields(line)[0])
		}
	}
	if strings.Join(order, " ") != `"name": "private": "scripts": "dependencies": "pnpm": "version":` {
		t.Errorf("top-level order: %v\n%s", order, got)
	}
	if !strings.Contains(got, "    \"dev\": \"krate dev\",\n    \"build\": \"krate build && node post.mjs\",\n    \"zeta\": \"echo a && echo b\",") {
		t.Errorf("scripts reordered or HTML-escaped:\n%s", got)
	}
	if !strings.Contains(got, `"pnpm": {`) || !strings.Contains(got, `"onlyBuiltDependencies": [`) {
		t.Errorf("nested value lost:\n%s", got)
	}
}
