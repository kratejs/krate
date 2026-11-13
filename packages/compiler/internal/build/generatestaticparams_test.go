package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestExecuteGenerateStaticParams(t *testing.T) {
	// Create a temp directory with a TSX file that has generateStaticParams
	tmpDir := t.TempDir()
	pagesDir := filepath.Join(tmpDir, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a page with generateStaticParams
	pageContent := `
export default function VideoPage({ params }: { params: { id: string } }) {
  return <div>Video {params.id}</div>;
}

export function generateStaticParams() {
  return [
    { id: 'abc-123' },
    { id: 'def-456' },
  ];
}
`
	pagePath := filepath.Join(pagesDir, "video", "[id].tsx")
	if err := os.MkdirAll(filepath.Dir(pagePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pagePath, []byte(pageContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(tmpDir, "dist")
	b := New(tmpDir, cfg)

	paramSets, err := b.executeGenerateStaticParams(pagePath, b.newBundler())
	if err != nil {
		t.Fatalf("executeGenerateStaticParams: %v", err)
	}

	if len(paramSets) != 2 {
		t.Fatalf("expected 2 param sets, got %d", len(paramSets))
	}

	if paramSets[0]["id"] != "abc-123" {
		t.Errorf("paramSets[0][id] = %q, want %q", paramSets[0]["id"], "abc-123")
	}
	if paramSets[1]["id"] != "def-456" {
		t.Errorf("paramSets[1][id] = %q, want %q", paramSets[1]["id"], "def-456")
	}
}

// TestExecuteGenerateStaticParamsRuntime exercises the bundled fallback under
// each supported build-time runtime (node, bun, deno). CI installs Bun and Deno
// so this is the runtime matrix gate.
func TestExecuteGenerateStaticParamsRuntime(t *testing.T) {
	for _, rt := range []string{"node", "bun", "deno"} {
		t.Run(rt, func(t *testing.T) {
			if _, err := exec.LookPath(rt); err != nil {
				t.Skipf("%s not available", rt)
			}
			tmpDir := t.TempDir()
			pagesDir := filepath.Join(tmpDir, "src", "pages")
			pagePath := filepath.Join(pagesDir, "video", "[id].tsx")
			if err := os.MkdirAll(filepath.Dir(pagePath), 0755); err != nil {
				t.Fatal(err)
			}
			pageContent := `import { homedir } from "node:os";
export function generateStaticParams() {
  return [{ id: homedir() ? "ok" : "no" }];
}
`
			if err := os.WriteFile(pagePath, []byte(pageContent), 0644); err != nil {
				t.Fatal(err)
			}
			cfg := config.Default()
			cfg.PagesDir = pagesDir
			cfg.OutDir = filepath.Join(tmpDir, "dist")
			cfg.Runtime = rt
			b := New(tmpDir, cfg)
			paramSets, err := b.executeGenerateStaticParams(pagePath, b.newBundler())
			if err != nil {
				t.Fatalf("executeGenerateStaticParams (%s): %v", rt, err)
			}
			if len(paramSets) != 1 || paramSets[0]["id"] != "ok" {
				t.Fatalf("%s param sets: %+v", rt, paramSets)
			}
		})
	}
}
