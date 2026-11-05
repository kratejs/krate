package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildTailwindOffProject builds a one-page project and returns what the build
// wrote to stderr.
func buildTailwindOffProject(t *testing.T, enabled bool, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx",
		"export default function Page() { return <p class=\"p-4 flex\">hi</p>; }\n")
	for rel, content := range files {
		writeFileRel(t, root, rel, content)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Tailwind.Enabled = enabled
	var err error
	out := captureStderr(t, func() { err = New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return out
}

func TestWarnsWhenTailwindConfigExistsButTailwindIsOff(t *testing.T) {
	out := buildTailwindOffProject(t, false, map[string]string{
		"tailwind.config.js": "module.exports = { content: ['./src/**/*.tsx'] };\n",
	})
	if !strings.Contains(out, "KR7140") || !strings.Contains(out, "tailwind.config.js") || !strings.Contains(out, "enabled: true") {
		t.Fatalf("no Tailwind-off warning with a hint:\n%s", out)
	}
}

func TestWarnsWhenTailwindDirectivesAreUsedButTailwindIsOff(t *testing.T) {
	for name, css := range map[string]string{
		"@tailwind":        "@tailwind base;\n@tailwind utilities;\n",
		"@import tailwind": "@import \"tailwindcss\";\n",
	} {
		t.Run(name, func(t *testing.T) {
			out := buildTailwindOffProject(t, false, map[string]string{
				"src/pages/index.css": css,
				"src/pages/index.tsx": "import './index.css';\nexport default function Page() { return <p class=\"p-4\">hi</p>; }\n",
			})
			if !strings.Contains(out, "KR7140") || !strings.Contains(out, "enabled: true") {
				t.Fatalf("no Tailwind-off warning:\n%s", out)
			}
		})
	}
}

func TestTailwindOffWarningIsPrintedOnce(t *testing.T) {
	out := buildTailwindOffProject(t, false, map[string]string{
		"tailwind.config.js":  "module.exports = {};\n",
		"src/pages/index.css": "@tailwind base;\n@tailwind utilities;\n",
		"src/pages/index.tsx": "import './index.css';\nexport default function Page() { return <p>hi</p>; }\n",
		"src/pages/about.tsx": "import './index.css';\nexport default function Page() { return <p>about</p>; }\n",
	})
	if n := strings.Count(out, "KR7140"); n != 1 {
		t.Fatalf("warning printed %d times, want 1:\n%s", n, out)
	}
}

func TestNoTailwindWarningWhenEnabledOrUnused(t *testing.T) {
	withConfig := map[string]string{"tailwind.config.js": "module.exports = {};\n"}
	if out := buildTailwindOffProject(t, true, withConfig); strings.Contains(out, "KR7140") {
		t.Fatalf("warned although Tailwind is enabled:\n%s", out)
	}
	if out := buildTailwindOffProject(t, false, nil); strings.Contains(out, "KR7140") {
		t.Fatalf("warned although there is no Tailwind input:\n%s", out)
	}
}
