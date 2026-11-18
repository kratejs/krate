package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// monorepoApp writes packages/web (the Next app) next to packages/ui (a
// workspace package linked into the app's node_modules) and returns the app.
func monorepoApp(t *testing.T, nextConfig string, extra map[string]string) string {
	t.Helper()
	top := t.TempDir()
	app := filepath.Join(top, "packages", "web")
	writeNextFile(t, app, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, app, "next.config.ts", nextConfig)
	writeNextFile(t, app, "package.json", `{"name":"web","scripts":{"dev":"next dev","build":"next build","start":"node .next/standalone/packages/web/server.js"},"dependencies":{"next":"16.0.0","react":"19.0.0"}}`)
	writeNextFile(t, top, "packages/ui/package.json", `{"name":"@acme/ui","main":"src/index.ts"}`)
	writeNextFile(t, top, "packages/ui/src/index.ts", `export const x = 1;`)
	writeNextFile(t, top, "packages/dist-only/package.json", `{"name":"@acme/dist-only","main":"dist/index.js"}`)
	writeNextFile(t, top, "node_modules/compiled/package.json", `{"name":"compiled","main":"index.js"}`)
	writeNextFile(t, top, "node_modules/compiled/index.js", `module.exports = 1;`)
	if err := os.MkdirAll(filepath.Join(app, "node_modules", "@acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"ui": "../../../ui", "dist-only": "../../../dist-only"} {
		if err := os.Symlink(target, filepath.Join(app, "node_modules", "@acme", name)); err != nil {
			t.Skipf("no symlinks: %v", err)
		}
	}
	for name, body := range extra {
		writeNextFile(t, app, name, body)
	}
	return app
}

func scanApp(t *testing.T, app string, opts ScanOptions) *Report {
	t.Helper()
	rep, err := ScanNextWithOptions(app, opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

const monorepoConfig = `import path from 'node:path';
import type { NextConfig } from 'next';

const nextConfig: NextConfig = {
  output: 'standalone',
  outputFileTracingRoot: path.resolve(import.meta.dirname, '../..'),
  // An apostrophe in a later comment (Next's tracer) must not confuse the scan.
  transpilePackages: ['@acme/ui', 'compiled'],
  serverExternalPackages: ['@napi-rs/canvas'],
  outputFileTracingIncludes: {
    '/api/preview': ['../render-service/dist/scramble-worker.mjs'],
    '**': ['../../node_modules/.pnpm/@fastpkg+image@*/node_modules/@fastpkg/image-*/*'],
  },
  turbopack: { root: path.resolve(import.meta.dirname, '../..') },
};
export default nextConfig;
`

func TestMigrateStandaloneMonorepoBecomesNodeDeploy(t *testing.T) {
	app := monorepoApp(t, monorepoConfig, nil)
	rep := scanApp(t, app, ScanOptions{UpdatePackage: true})
	body := configBody(t, rep)
	for _, want := range []string{
		`"outputFileTracingRoot": "../.."`,
		`"deploy": {`, `"target": "node"`,
		`"/api/preview": [`, `"../render-service/dist/scramble-worker.mjs"`,
		`"**": [`, `"../../node_modules/.pnpm/@fastpkg+image@*/node_modules/@fastpkg/image-*/*"`,
		`"@napi-rs/canvas"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("config lacks %s:\n%s", want, body)
		}
	}
	for _, m := range rep.Manual {
		for _, bad := range []string{"output", "tracing", "transpile", "turbopack", "Tracing"} {
			if strings.Contains(m.Reason, bad) {
				t.Errorf("unexpected manual item: %+v", m)
			}
		}
	}
	for _, want := range []string{"transpilePackages: 2 of 2", "turbopack.root", "output: 'standalone' became deploy.target", "packages/web/server.mjs", "docker", "copies the files of every route"} {
		if !hasInfo(rep, want) {
			t.Errorf("no info line with %q: %+v", want, rep.Info)
		}
	}
	pkg := findPlan(rep, "package.json")
	if pkg == nil {
		t.Fatal("package.json not planned")
	}
	text := string(pkg.Content)
	if !strings.Contains(text, `"build": "krate deploy"`) || !strings.Contains(text, `"start": "node dist/.krate/server/packages/web/server.mjs"`) || !strings.Contains(text, `"dev": "krate dev"`) {
		t.Errorf("scripts:\n%s", text)
	}
}

func TestMigrateStandaloneWithoutRootUsesFlatServerPath(t *testing.T) {
	rep := scanConfig(t, map[string]string{
		"next.config.js": `module.exports = { output: 'standalone' };`,
		"package.json":   `{"scripts":{"build":"next build","start":"next start"},"dependencies":{"next":"16.0.0"}}`,
	})
	if len(rep.Manual) != 0 && hasManual(rep, "next.config.js", "standalone") {
		t.Errorf("standalone is translated: %+v", rep.Manual)
	}
	if !strings.Contains(configBody(t, rep), `"target": "node"`) {
		t.Errorf("deploy target missing:\n%s", configBody(t, rep))
	}
	root := rep.Root
	rep2, err := ScanNextWithOptions(root, ScanOptions{UpdatePackage: true})
	if err != nil {
		t.Fatal(err)
	}
	pkg := findPlan(rep2, "package.json")
	if pkg == nil || !strings.Contains(string(pkg.Content), `"start": "node dist/.krate/server/server.mjs"`) || !strings.Contains(string(pkg.Content), `"build": "krate deploy"`) {
		t.Errorf("package plan: %+v", pkg)
	}
}

func TestMigrateTracingRootVariants(t *testing.T) {
	for _, expr := range []string{
		`path.resolve(__dirname, '../..')`,
		`path.join(process.cwd(), '..', '..')`,
		`path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')`,
		`'../..'`,
	} {
		app := monorepoApp(t, "import path from 'node:path';\nimport { fileURLToPath } from 'node:url';\nexport default { outputFileTracingRoot: "+expr+" };", nil)
		rep := scanApp(t, app, ScanOptions{})
		if !strings.Contains(configBody(t, rep), `"outputFileTracingRoot": "../.."`) {
			t.Errorf("%s was not translated:\n%s\n%+v", expr, configBody(t, rep), rep.Manual)
		}
	}
	// A root the migrator cannot evaluate stays a manual item.
	app := monorepoApp(t, "export default { outputFileTracingRoot: process.env.ROOT };", nil)
	rep := scanApp(t, app, ScanOptions{})
	if !hasManual(rep, "next.config.ts", "outputFileTracingRoot") {
		t.Errorf("a computed root must be manual: %+v", rep.Manual)
	}
}

func TestMigrateIncludesOutsideTheRootStayManual(t *testing.T) {
	app := monorepoApp(t, `export default {
  outputFileTracingRoot: '../..',
  outputFileTracingIncludes: { '/x': ['../../../escape/**'] },
};`, nil)
	rep := scanApp(t, app, ScanOptions{})
	if !hasManual(rep, "next.config.ts", "outputFileTracingIncludes") {
		t.Errorf("a glob that leaves the root must be manual: %+v", rep.Manual)
	}
	// Without a root a parent glob is not translatable either.
	app = monorepoApp(t, `export default { outputFileTracingIncludes: { '/x': ['../sib/**'] } };`, nil)
	rep = scanApp(t, app, ScanOptions{})
	if !hasManual(rep, "next.config.ts", "outputFileTracingIncludes") {
		t.Errorf("a parent glob without a root must be manual: %+v", rep.Manual)
	}
}

func TestMigrateTranspilePackagesReportsUnresolved(t *testing.T) {
	app := monorepoApp(t, `export default { transpilePackages: ['@acme/ui', '@acme/dist-only', '@acme/missing'] };`, nil)
	rep := scanApp(t, app, ScanOptions{})
	var gotDist, gotMissing bool
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, `"@acme/dist-only"`) && strings.Contains(m.Reason, "does not exist") {
			gotDist = true
		}
		if strings.Contains(m.Reason, `"@acme/missing"`) && strings.Contains(m.Reason, "not installed") {
			gotMissing = true
		}
		if strings.Contains(m.Reason, `"@acme/ui"`) {
			t.Errorf("a resolvable package must not be reported: %+v", m)
		}
	}
	if !gotDist || !gotMissing {
		t.Errorf("dist-only=%v missing=%v: %+v", gotDist, gotMissing, rep.Manual)
	}
	if !hasInfo(rep, "transpilePackages: 1 of 3") {
		t.Errorf("info for the resolved package: %+v", rep.Info)
	}
}

func TestMigrateTurbopackKeys(t *testing.T) {
	app := monorepoApp(t, `export default { turbopack: {
  root: '/somewhere/else',
  resolveAlias: { underscore: 'lodash', '@x': './src/x' },
  rules: { '*.svg': { loaders: ['@svgr/webpack'], as: '*.js' } },
  debugIds: true,
} };`, nil)
	rep := scanApp(t, app, ScanOptions{})
	if !hasManual(rep, "next.config.ts", "turbopack.rules") {
		t.Errorf("rules must stay manual: %+v", rep.Manual)
	}
	if !hasManual(rep, "next.config.ts", "turbopack.root") {
		t.Errorf("an unrelated root must be manual: %+v", rep.Manual)
	}
	var alias string
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "resolveAlias") {
			alias = m.Hint
		}
	}
	if !strings.Contains(alias, `"underscore": ["lodash"]`) || !strings.Contains(alias, "paths") {
		t.Errorf("alias hint: %q", alias)
	}
	if !hasWarning(rep, "turbopack.debugIds") {
		t.Errorf("unused turbopack key warning: %+v", rep.Warnings)
	}
}

func TestMigrateTurbopackRootOnlyLeavesNoItem(t *testing.T) {
	app := monorepoApp(t, `import path from 'node:path';
export default { turbopack: { root: path.resolve(import.meta.dirname) } };`, nil)
	rep := scanApp(t, app, ScanOptions{})
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "turbopack") {
			t.Errorf("unexpected: %+v", m)
		}
	}
}
