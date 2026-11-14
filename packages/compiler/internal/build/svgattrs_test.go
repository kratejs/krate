package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A pasted Figma/Illustrator SVG carries namespace declarations and
// camelCase React-style names; both spellings must render as XML names.
func TestPastedSVGNamespacedAttributes(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() {
  return (
    <main>
      <svg xmlns="http://www.w3.org/2000/svg" xmlnsXlink="http://www.w3.org/1999/xlink" xmlSpace="preserve" version="1.1" viewBox="0 0 24 24">
        <defs><linearGradient id="g" gradientUnits="userSpaceOnUse"><stop offset="0" stopColor="#000" /></linearGradient></defs>
        <use xlinkHref="#g" />
        <use xlink:href="#g" xml:space="preserve" />
        <path fillRule="evenodd" clipRule="evenodd" d="M0 0h24v24H0z" />
      </svg>
    </main>
  );
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		`xmlns=http://www.w3.org/2000/svg`,
		`xmlns:xlink=http://www.w3.org/1999/xlink`,
		`xml:space=preserve`,
		`version=1.1`,
		`<use xlink:href=#g`,
		`stop-color=#000`,
		`fill-rule=evenodd`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	for _, bad := range []string{"xmlnsXlink", "xmlSpace", "xlinkHref"} {
		if strings.Contains(html, bad) {
			t.Errorf("React-style name %q leaked into HTML", bad)
		}
	}
}
