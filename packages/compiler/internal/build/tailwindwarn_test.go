package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestTailwindOffWarningPointsAtTheDirective(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFileRel(t, root, "src/styles/globals.css", "body { margin: 0 }\n\n  @import 'tailwindcss';\n")
	writeFileRel(t, root, "node_modules/x/other.css", "@import 'tailwindcss';\n")
	cfg := config.Default()
	cfg.OutDir = root + "/dist"
	file, line, col := New(root, cfg).findTailwindDirective("@import 'tailwindcss'")
	if filepath.ToSlash(file) != "src/styles/globals.css" || line != 3 || col != 3 {
		t.Errorf("got %s:%d:%d, want src/styles/globals.css:3:3", file, line, col)
	}
}

// buildUntouchedTailwind builds files with the default config, which does not
// set tailwind.enabled unless tweak does, and returns the concatenated output
// CSS and the build warnings.
func buildUntouchedTailwind(t *testing.T, files map[string]string, tweak func(*config.Config)) (sheets, logged string) {
	t.Helper()
	root := t.TempDir()
	for p, c := range files {
		writeFileRel(t, root, p, c)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if tweak != nil {
		tweak(cfg)
	}
	logged, err := captureBuild(New(root, cfg))
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, logged)
	}
	matches, _ := filepath.Glob(filepath.Join(cfg.OutDir, "*.css"))
	var b strings.Builder
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String(), logged
}

var tailwindImportApp = map[string]string{
	"src/pages/index.tsx": `import "../globals.css";
export default function P() {
  return <button class="inline-flex gap-2">Restart</button>;
}`,
	"src/globals.css": "@import \"tailwindcss\";\n.card { color: red }\n",
}

func TestTailwindImportTurnsTailwindOn(t *testing.T) {
	t.Parallel()
	sheets, logged := buildUntouchedTailwind(t, tailwindImportApp, nil)
	for _, want := range []string{"@layer base", ".gap-2", ".inline-flex"} {
		if !strings.Contains(sheets, want) {
			t.Errorf("output CSS lacks %q:\n%s", want, sheets)
		}
	}
	if strings.Contains(sheets, `@import "tailwindcss"`) {
		t.Errorf("the Tailwind import was left in the output:\n%s", sheets)
	}
	if strings.Contains(logged, "KR7140") {
		t.Errorf("unexpected Tailwind-off warning:\n%s", logged)
	}
}

func TestTailwindExplicitlyOffWarnsOnce(t *testing.T) {
	t.Parallel()
	sheets, logged := buildUntouchedTailwind(t, tailwindImportApp, func(c *config.Config) {
		c.Tailwind.Enabled = false
		c.Tailwind.EnabledSet = true
	})
	if strings.Contains(sheets, "@layer base") {
		t.Errorf("Tailwind ran although the config turned it off:\n%s", sheets)
	}
	if n := strings.Count(logged, "Tailwind is not enabled"); n != 1 {
		t.Errorf("got %d Tailwind-off warnings, want 1:\n%s", n, logged)
	}
	if strings.Contains(logged, "needs Tailwind") {
		t.Errorf("the import was also reported without a location:\n%s", logged)
	}
	if !strings.Contains(logged, "tailwind.enabled: false") {
		t.Errorf("the hint does not point at the config setting:\n%s", logged)
	}
}

func TestProjectUsesTailwind(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		files map[string]string
		want  bool
	}{
		"import":       {map[string]string{"src/a.css": "@import 'tailwindcss';"}, true},
		"subpath":      {map[string]string{"styles/a.css": `@import "tailwindcss/utilities";`}, true},
		"v3 directive": {map[string]string{"src/a.css": "@tailwind utilities;"}, true},
		"plain css":    {map[string]string{"src/a.css": "body { margin: 0 }"}, false},
		"node_modules": {map[string]string{"node_modules/x/a.css": `@import "tailwindcss";`}, false},
		"out dir":      {map[string]string{"dist/a.css": `@import "tailwindcss";`}, false},
	} {
		root := t.TempDir()
		for p, c := range tc.files {
			writeFileRel(t, root, p, c)
		}
		if got := projectUsesTailwind(root, filepath.Join(root, "dist")); got != tc.want {
			t.Errorf("%s: projectUsesTailwind = %v, want %v", name, got, tc.want)
		}
	}
}

func TestTailwindNotAutoEnabledWhenConfigSetsIt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFileRel(t, root, "src/a.css", `@import "tailwindcss";`)
	cfg := config.Default()
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Tailwind.EnabledSet = true
	if New(root, cfg).Cfg.Tailwind.Enabled {
		t.Error("an explicit tailwind.enabled: false was overridden")
	}
	cfg = config.Default()
	cfg.OutDir = filepath.Join(root, "dist")
	if !New(root, cfg).Cfg.Tailwind.Enabled {
		t.Error("the Tailwind import did not turn Tailwind on")
	}
}
