package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"krate-compiler/internal/config"
)

func TestBuildTestProject(t *testing.T) {
	// Find the examples project root relative to this package
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// from packages/compiler/internal/build/ -> packages/compiler/ -> examples
	projectRoot := filepath.Clean(filepath.Join(pkgDir, "..", "..", "..", "..", "examples"))
	if _, err := os.Stat(projectRoot); os.IsNotExist(err) {
		t.Skipf("examples not found at %s", projectRoot)
	}

	cfg, err := config.Load(projectRoot)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	b := New(projectRoot, cfg)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	// Verify output directory exists
	outDir := cfg.OutDir
	if !dirExists(outDir) {
		t.Fatalf("output directory %s does not exist", outDir)
	}

	// Verify key output files
	checks := []struct {
		path     string
		contains []string // substrings that must appear in the file
	}{
		{path: filepath.Join(outDir, "index.html"), contains: []string{"<!DOCTYPE html>", "<html", "<head", "<body"}},
		{path: filepath.Join(outDir, "about", "index.html")},
		{path: filepath.Join(outDir, "blog", "index.html")},
		{path: filepath.Join(outDir, "test", "index.html")},
		{path: filepath.Join(outDir, "video", "[id]", "index.html"), contains: []string{"<!DOCTYPE html>", "video"}},
		{path: filepath.Join(outDir, "syntax-robustness", "index.html"), contains: []string{"<!DOCTYPE html>", "Syntax Robustness"}},
		{path: filepath.Join(outDir, "manifest.json"), contains: []string{"\"pages\""}},
	}

	for _, c := range checks {
		info, err := os.Stat(c.path)
		if os.IsNotExist(err) {
			t.Errorf("missing output: %s", c.path)
			continue
		}
		if info.IsDir() {
			t.Errorf("expected file, got directory: %s", c.path)
			continue
		}
		if len(c.contains) > 0 {
			data, err := os.ReadFile(c.path)
			if err != nil {
				t.Errorf("reading %s: %v", c.path, err)
				continue
			}
			content := string(data)
			for _, substr := range c.contains {
				if !strings.Contains(content, substr) {
					t.Errorf("%s missing expected content: %q", c.path, substr)
				}
			}
		}
	}

	// Verify syntax-robustness page renders alias-imported components. The
	// Badge component is imported via `@/components/ui/badge` and
	// `@components/ui/badge`; its `<span class="badge">` markup only appears if
	// the @-alias imports resolved through the bundler.
	syntaxPage := filepath.Join(outDir, "syntax-robustness", "index.html")
	if data, err := os.ReadFile(syntaxPage); err == nil {
		content := string(data)
		if !strings.Contains(content, `class="badge"`) && !strings.Contains(content, "class=badge") {
			t.Errorf("syntax-robustness page did not render @-alias imported Badge component")
		}
		if !strings.Contains(content, "Syntax Robustness") {
			t.Errorf("syntax-robustness page missing heading")
		}
	}

	// Verify JS hydration files exist (hashed names)
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	hasJS := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "index.") && strings.HasSuffix(e.Name(), ".js") && !e.IsDir() {
			hasJS = true
			break
		}
	}
	if !hasJS {
		t.Error("no hashed JS hydration file found in output directory")
	}

	// Verify Tailwind CSS generation: stylesheet should contain utility rules
	twChecks := []struct {
		desc string
		seek string
	}{
		{"padding utility", ".p-6"},
		{"margin utility", ".m-4"},
		{"flex utility", ".flex"},
		{"flex-wrap utility", ".flex-wrap"},
		{"gap utility", ".gap-4"},
		{"border-radius utility", ".rounded-lg"},
		{"rounded-xl utility", ".rounded-xl"},
		{"shadow utility", ".shadow-lg"},
		{"font-bold utility", ".font-bold"},
		{"text color utility", ".text-gray-600"},
		{"bg color utility", ".bg-blue-50"},
		{"text size utility", ".text-2xl"},
	}
	cssFound := false
	for _, c := range entries {
		if strings.HasPrefix(c.Name(), "styles.") && strings.HasSuffix(c.Name(), ".css") && !c.IsDir() {
			data, err := os.ReadFile(filepath.Join(outDir, c.Name()))
			if err != nil {
				t.Fatalf("reading CSS file %s: %v", c.Name(), err)
			}
			css := string(data)
			for _, tw := range twChecks {
				if !strings.Contains(css, tw.seek) {
					t.Errorf("Tailwind CSS missing %s: %q not in stylesheet", tw.desc, tw.seek)
				}
			}
			cssFound = true
			break
		}
	}
	if !cssFound {
		t.Error("no styles.css file found")
	}

	// Verify docs plugin generated pages
	docChecks := []string{
		filepath.Join(outDir, "docs", "index.html"),
		filepath.Join(outDir, "docs", "getting-started", "index.html"),
		filepath.Join(outDir, "docs", "configuration", "index.html"),
		filepath.Join(outDir, "docs", "guides", "advanced", "index.html"),
		filepath.Join(outDir, "docs", "data", "sidebar.json"),
		filepath.Join(outDir, "docs", "data", "search-index.json"),
	}
	for _, docPath := range docChecks {
		if !fileExists(docPath) {
			t.Errorf("docs plugin did not generate: %s", docPath)
		}
	}

	// Verify docs HTML contains expected elements
	docPage := filepath.Join(outDir, "docs", "getting-started", "index.html")
	if data, err := os.ReadFile(docPage); err == nil {
		content := string(data)
		for _, want := range []string{"sidebar", "breadcrumbs", "docs-content", "nav-prev", "nav-next"} {
			if !strings.Contains(content, want) {
				t.Errorf("docs page missing %q", want)
			}
		}
	}

	/* Clean up
	if err := os.RemoveAll(outDir); err != nil {
		t.Errorf("cleanup: %v", err)
	}
	_ = os.MkdirAll(outDir, 0755)*/
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
