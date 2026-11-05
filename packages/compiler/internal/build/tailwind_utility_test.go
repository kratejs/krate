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
		".tabs-github{tab-size:8}", ".tabs-4{tab-size:4}",
		".lh-lg\\/tight{font-size:", "line-height:1.25", "line-height:33px",
		".card{", "&:hover{color:", "@media print{display:none}", "@media (prefers-color-scheme: dark){color:",
		".hover\\:card:hover{",
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("missing %q in generated CSS:\n%s", want, sheet)
		}
	}
}
