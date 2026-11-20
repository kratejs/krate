package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/migrate"
)

// TestMigratedDynamicSSRFalseRendersFallbackThenIsland migrates a Next app whose
// viewer is dynamic<Props>(import, { ssr: false, loading }) and builds the
// result: the page markup shows the loading fallback, and the component is a
// separate chunk that mounts in the browser.
func TestMigratedDynamicSSRFalseRendersFallbackThenIsland(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for rel, content := range map[string]string{
		"package.json":       `{"name":"dyn","private":true,"dependencies":{"next":"16.0.0","react":"19.0.0"}}`,
		"src/app/layout.tsx": `export default function RootLayout({ children }) { return <html><body>{children}</body></html>; }`,
		"src/app/page.tsx": `import { VehicleViewer } from '../components/viewer';
export default function Home() { return <main><h1>Home</h1><VehicleViewer color="red" /></main>; }`,
		"src/components/viewer/types.ts": `export type VehicleViewerProps = { color: string };`,
		"src/components/viewer/viewer.tsx": `import type { VehicleViewerProps } from './types';
export default function Viewer({ color }: VehicleViewerProps) { return <div id="viewer" data-color={color}>VIEWER-READY</div>; }`,
		"src/components/viewer/index.tsx": `'use client';
import dynamic from 'next/dynamic';
import type { VehicleViewerProps } from './types';

export const VehicleViewer = dynamic<VehicleViewerProps>(() => import('./viewer'), {
  ssr: false,
  loading: () => (
    <div id="viewer-loading" className="skeleton">Loading viewer</div>
  ),
});
`,
	} {
		writeFileRel(t, root, rel, content)
	}
	rep, err := migrate.ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Apply(rep, migrate.ApplyOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	prepareMigratedRuntime(t, root)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	b := New(root, cfg)
	b.SkipQualityChecks = true
	if err := b.BuildAll(); err != nil {
		t.Fatalf("build: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	visible := html
	if i := strings.Index(visible, "<template"); i >= 0 {
		visible = visible[:i]
	}
	if !strings.Contains(visible, `<div id=viewer-loading class=skeleton>Loading viewer</div>`) {
		t.Errorf("the server markup must render the loading fallback:\n%s", html)
	}
	if strings.Contains(visible, "VIEWER-READY") {
		t.Errorf("the viewer must not render in the server markup:\n%s", html)
	}
	if chunks, _ := filepath.Glob(filepath.Join(cfg.OutDir, "lazy-*.js")); len(chunks) != 1 {
		t.Errorf("expected the viewer chunk, got %v", chunks)
	}
}

// migrateAndBuild migrates the Next project in root and builds the result.
func migrateAndBuild(t *testing.T, root string) *config.Config {
	t.Helper()
	rep, err := migrate.ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Apply(rep, migrate.ApplyOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	prepareMigratedRuntime(t, root)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	b := New(root, cfg)
	b.SkipQualityChecks = true
	if cfg.EnvDir != "" {
		// Same as the CLI: envDir moves where the .env files are read from.
		environ.Dir = cfg.EnvDir
		t.Cleanup(func() { environ.Dir = "" })
		if b.Env, err = environ.Load(root, "production"); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("build: %v", err)
	}
	return cfg
}

// TestMigratedRouteSegmentsNamedLikeSpecialFiles migrates segments named error
// and loading (normal pages in Next) next to a real error.tsx boundary: the
// pages must stay pages and the boundary must stay a boundary.
func TestMigratedRouteSegmentsNamedLikeSpecialFiles(t *testing.T) {
	root := t.TempDir()
	for rel, content := range map[string]string{
		"package.json":                 `{"name":"seg","private":true,"dependencies":{"next":"16.0.0","react":"19.0.0"}}`,
		"src/app/layout.tsx":           `export default function RootLayout({ children }) { return <html><body>{children}</body></html>; }`,
		"src/app/page.tsx":             `export default function Home() { return <main>HOME</main>; }`,
		"src/app/auth/error/page.tsx":  `export default function ErrorPage() { return <h1>AUTH-ERROR-PAGE</h1>; }`,
		"src/app/(x)/loading/page.tsx": `export default function LoadingPage() { return <h1>LOADING-PAGE</h1>; }`,
		"src/app/dashboard/page.tsx":   `export default function Dash() { return <h1>DASH</h1>; }`,
		"src/app/dashboard/error.tsx": `'use client';
export default function DashError({ error }: { error: Error }) { return <p>DASH-BOUNDARY</p>; }`,
	} {
		writeFileRel(t, root, rel, content)
	}
	cfg := migrateAndBuild(t, root)
	for route, want := range map[string]string{
		"auth/error/index.html": "AUTH-ERROR-PAGE",
		"loading/index.html":    "LOADING-PAGE",
		"dashboard/index.html":  "DASH",
	} {
		data, err := os.ReadFile(filepath.Join(cfg.OutDir, route))
		if err != nil {
			t.Errorf("%s: %v", route, err)
			continue
		}
		if !strings.Contains(string(data), want) {
			t.Errorf("%s does not render %q:\n%s", route, want, data)
		}
	}
	for _, rel := range []string{"src/pages/auth/error/index.tsx", "src/pages/(x)/loading/index.tsx", "src/pages/dashboard/error.tsx"} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("expected migrated file %s: %v", rel, err)
		}
	}
}

// TestMigratedLoadEnvConfigSetsEnvDir migrates a monorepo app whose next.config
// loads the repo-root .env with loadEnvConfig and checks the built page sees
// the root variable.
func TestMigratedLoadEnvConfigSetsEnvDir(t *testing.T) {
	top := t.TempDir()
	root := filepath.Join(top, "packages", "web")
	for rel, content := range map[string]string{
		".env":                      "KRATE_PUBLIC_FROM_ROOT=root-env-value\n",
		"packages/web/package.json": `{"name":"web","private":true,"dependencies":{"next":"16.0.0","react":"19.0.0"}}`,
		"packages/web/next.config.ts": `import { loadEnvConfig } from '@next/env';
import path from 'node:path';

loadEnvConfig(path.resolve(import.meta.dirname, '../..'), process.env.NODE_ENV !== 'production', undefined, true);

export default { reactStrictMode: true };
`,
		"packages/web/src/app/layout.tsx": `export default function RootLayout({ children }) { return <html><body>{children}</body></html>; }`,
		"packages/web/src/app/page.tsx":   `export default function Home() { return <main id="v">{import.meta.env.KRATE_PUBLIC_FROM_ROOT}</main>; }`,
	} {
		writeFileRel(t, top, rel, content)
	}
	rep, err := migrate.ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "loadEnvConfig") {
			t.Errorf("a resolvable loadEnvConfig path must not be a manual item: %+v", m)
		}
	}
	cfg := migrateAndBuild(t, root)
	if cfg.EnvDir != top {
		t.Errorf("envDir = %q, want %q", cfg.EnvDir, top)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "root-env-value") {
		t.Errorf("the page must see the repo-root .env:\n%s", data)
	}
}

// TestMigratedNextPublicVarFromRootEnv migrates a monorepo app that reads a
// NEXT_PUBLIC_ variable defined only in the repo-root .env: the migrated app
// keeps the name (envPrefix) and the built page contains the value.
func TestMigratedNextPublicVarFromRootEnv(t *testing.T) {
	top := t.TempDir()
	root := filepath.Join(top, "packages", "web")
	for rel, content := range map[string]string{
		".env":                      "NEXT_PUBLIC_UMAMI_URL=https://umami.example.test\nSECRET_TOKEN=do-not-leak-1234\n",
		"packages/web/package.json": `{"name":"web","private":true,"dependencies":{"next":"16.0.0","react":"19.0.0"}}`,
		"packages/web/next.config.ts": `import { loadEnvConfig } from '@next/env';
import path from 'node:path';

loadEnvConfig(path.resolve(import.meta.dirname, '../..'), process.env.NODE_ENV !== 'production', undefined, true);

export default { reactStrictMode: true };
`,
		"packages/web/src/app/layout.tsx": `export default function RootLayout({ children }) { return <html><body>{children}</body></html>; }`,
		"packages/web/src/app/page.tsx":   `export default function Home() { return <main id="v" data-u={process.env.NEXT_PUBLIC_UMAMI_URL}>{process.env.NEXT_PUBLIC_UMAMI_URL}</main>; }`,
	} {
		writeFileRel(t, top, rel, content)
	}
	cfg := migrateAndBuild(t, root)
	if len(cfg.EnvPrefix) != 2 || cfg.EnvPrefix[1] != "NEXT_PUBLIC_" {
		t.Errorf("envPrefix = %v", cfg.EnvPrefix)
	}
	data, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "https://umami.example.test") {
		t.Errorf("the page must contain the NEXT_PUBLIC_ value from the repo-root .env:\n%s", data)
	}
	if strings.Contains(string(data), "do-not-leak-1234") {
		t.Errorf("a secret reached the page")
	}
}
