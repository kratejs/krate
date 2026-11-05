package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A project stylesheet's @utility blocks reach the generated CSS through the
// real build: functional utilities (name-*), a bare static name, --modifier()
// and nested rules.
func TestTailwindCustomUtilitiesInBuild(t *testing.T) {
	_, out, logged := buildFiles(t, map[string]string{
		"src/pages/index.tsx": `import "./site.css";
export default function P() {
  return <main class="card tabs-github tabs-4 lh-lg/tight hover:card"><p class="lh-lg/[33px]">x</p></main>;
}`,
		"src/pages/site.css": `@import "tailwindcss";
@theme { --tab-size-github: 8; }
@utility tabs-* { tab-size: --value(--tab-size-*, integer, [integer]); }
@utility lh-* { font-size: --value(--text-*, [length]); line-height: --modifier(--leading-*, [length], [*]); }
@utility card { color: red; &:hover { color: blue; } @media print { display: none; } @variant dark { color: white; } }`,
	}, nil)
	if strings.Contains(logged, "@utility") {
		t.Errorf("unexpected @utility warning:\n%s", logged)
	}
	var sheet string
	files, _ := filepath.Glob(filepath.Join(out, "*.css"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		sheet += string(b)
	}
	for _, want := range []string{
		".tabs-github{tab-size:var(--tab-size-github)}", ".tabs-4{tab-size:4}",
		".lh-lg\\/tight{font-size:", "line-height:1.25", "line-height:33px",
		".card{", "&:hover{color:", "@media print{display:none}", "@media (prefers-color-scheme: dark){color:",
		".hover\\:card:hover{",
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("missing %q in generated CSS:\n%s", want, sheet)
		}
	}
}

// @apply reads theme variables through var() like a utility class does, also
// when the @theme block lives in another stylesheet.
func TestTailwindApplyReadsLiveThemeVars(t *testing.T) {
	_, out, _ := buildFiles(t, map[string]string{
		"src/pages/index.tsx": `import "./theme.css";
import "./site.css";
export default function P() { return <main class="btn p-4">x</main>; }`,
		"src/pages/theme.css": `@import "tailwindcss";
@theme { --color-primary: #123456; }
.dark { --color-primary: #fedcba; --spacing: 2px; }`,
		"src/pages/site.css": `.btn { @apply bg-primary p-2; }`,
	}, nil)
	var sheet string
	files, _ := filepath.Glob(filepath.Join(out, "*.css"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		sheet += string(b)
	}
	for _, want := range []string{
		"background-color:var(--color-primary)", "padding:calc(var(--spacing)*2)",
		"--color-primary:#123456", "--spacing:0.25rem",
	} {
		if !strings.Contains(strings.ReplaceAll(sheet, " ", ""), strings.ReplaceAll(want, " ", "")) {
			t.Errorf("missing %q in generated CSS:\n%s", want, sheet)
		}
	}
}
