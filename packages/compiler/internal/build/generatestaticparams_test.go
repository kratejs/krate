package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"krate-compiler/internal/config"
)

func TestExtractParamNames(t *testing.T) {
	tmpDir := t.TempDir()
	pagesDir := filepath.Join(tmpDir, "src", "pages")
	tests := []struct {
		pagePath string
		pagesDir string
		want     []string
	}{
		{
			pagePath: filepath.Join(pagesDir, "video", "[id].tsx"),
			pagesDir: pagesDir,
			want:     []string{"id"},
		},
		{
			pagePath: filepath.Join(pagesDir, "user", "[username]", "posts", "[postId].tsx"),
			pagesDir: pagesDir,
			want:     []string{"username", "postId"},
		},
		{
			pagePath: filepath.Join(pagesDir, "about.tsx"),
			pagesDir: pagesDir,
			want:     nil,
		},
		{
			pagePath: filepath.Join(pagesDir, "index.tsx"),
			pagesDir: pagesDir,
			want:     nil,
		},
	}

	for _, tt := range tests {
		got := extractParamNames(tt.pagePath, tt.pagesDir)
		if len(got) != len(tt.want) {
			t.Errorf("extractParamNames(%q, %q) = %v, want %v", tt.pagePath, tt.pagesDir, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("extractParamNames(%q, %q)[%d] = %q, want %q", tt.pagePath, tt.pagesDir, i, got[i], tt.want[i])
			}
		}
	}
}

func TestIsDynamicRoute(t *testing.T) {
	tmpDir := t.TempDir()
	pagesDir := filepath.Join(tmpDir, "src", "pages")
	tests := []struct {
		pagePath string
		pagesDir string
		want     bool
	}{
		{filepath.Join(pagesDir, "video", "[id].tsx"), pagesDir, true},
		{filepath.Join(pagesDir, "user", "[username]", "posts", "[postId].tsx"), pagesDir, true},
		{filepath.Join(pagesDir, "about.tsx"), pagesDir, false},
		{filepath.Join(pagesDir, "index.tsx"), pagesDir, false},
	}

	for _, tt := range tests {
		got := isDynamicRoute(tt.pagePath, tt.pagesDir)
		if got != tt.want {
			t.Errorf("isDynamicRoute(%q, %q) = %v, want %v", tt.pagePath, tt.pagesDir, got, tt.want)
		}
	}
}

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

	paramSets, err := executeGenerateStaticParams(pagePath)
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

func TestResolveStaticParamsPages(t *testing.T) {
	tmpDir := t.TempDir()
	pagesDir := filepath.Join(tmpDir, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a static page (no generateStaticParams)
	aboutContent := `export default function About() { return <div>About</div>; }`
	if err := os.WriteFile(filepath.Join(pagesDir, "about.tsx"), []byte(aboutContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a dynamic page with generateStaticParams
	videoContent := `
export default function VideoPage({ params }: { params: { id: string } }) {
  return <div>Video {params.id}</div>;
}
export function generateStaticParams() {
  return [
    { id: 'abc' },
    { id: 'xyz' },
  ];
}
`
	pagePath := filepath.Join(pagesDir, "video", "[id].tsx")
	if err := os.MkdirAll(filepath.Dir(pagePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pagePath, []byte(videoContent), 0644); err != nil {
		t.Fatal(err)
	}

	b := New(tmpDir, &config.Config{
		PagesDir: filepath.Join(tmpDir, "src", "pages"),
	})

	pages := []string{
		filepath.Join(pagesDir, "about.tsx"),
		pagePath,
	}

	expanded := b.resolveStaticParamsPages(pages)

	if len(expanded) != 2 {
		t.Fatalf("expected 2 expanded pages, got %d", len(expanded))
	}

	// Check that outPath has the params substituted
	for _, ep := range expanded {
		if !strings.Contains(ep.OutPath, "video/") {
			t.Errorf("expected outPath to contain 'video/', got %q", ep.OutPath)
		}
	}

	if expanded[0].Params["id"] != "abc" {
		t.Errorf("expanded[0].Params[id] = %q, want %q", expanded[0].Params["id"], "abc")
	}
	if expanded[1].Params["id"] != "xyz" {
		t.Errorf("expanded[1].Params[id] = %q, want %q", expanded[1].Params["id"], "xyz")
	}
}
