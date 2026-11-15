package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/migrate"
)

// TestMigratedDynamicSSRFalseRendersFallbackThenIsland migrates a Next app whose
// viewer is dynamic<Props>(import, { ssr: false, loading }) and builds the
// result: the page markup shows the loading fallback, and the component is a
// separate chunk that mounts in the browser.
func TestMigratedDynamicSSRFalseRendersFallbackThenIsland(t *testing.T) {
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
