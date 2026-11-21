package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Links to pages another app serves on the same domain (the marketing site at
// "/" while Krate runs under "/app") opt out of the base path and the router.
func TestBasePathOutsideLinks(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() {
  return (
    <main>
      <Link href="/in-site">inside</Link>
      <Link href="/pricing" basePath={false}>outside-link</Link>
      <a href="/blog" data-krate-no-base>outside-anchor</a>
      <a href="/oops">plain</a>
    </main>
  );
}`,
	})
	b.Cfg.BasePath = "/app"

	logs, buildErr := captureBuild(b)
	if buildErr != nil {
		t.Fatalf("BuildAll: %v", buildErr)
	}

	data, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		`<a data-krate-link data-prefetch href=/app/in-site>`,
		`<a href=/pricing data-krate-external>outside-link`,
		`<a href=/blog>outside-anchor`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	if strings.Contains(html, "data-krate-no-base") {
		t.Errorf("the build-time marker must not be emitted:\n%s", html)
	}
	if strings.Contains(html, "basePath") && strings.Contains(html, "basepath=") {
		t.Errorf("basePath prop leaked onto the anchor:\n%s", html)
	}
	out := string(logs)
	if !strings.Contains(out, `/oops`) || strings.Contains(out, `"/blog"`) || strings.Contains(out, `"/pricing"`) {
		t.Errorf("KR7144 should name only the plain /oops anchor:\n%s", out)
	}
	if !strings.Contains(out, "data-krate-no-base") {
		t.Errorf("KR7144 hint should point at data-krate-no-base:\n%s", out)
	}
}

// An @import of a missing local stylesheet fails the build with its position;
// a remote @import still passes through.
func TestMissingCSSImportFailsBuild(t *testing.T) {
	t.Parallel()
	b, _ := scProject(t, map[string]string{
		"src/pages/index.tsx": `import "../styles.css";
export default function Home() { return <main>x</main>; }`,
		"src/styles.css": "@import url(https://fonts.example.com/a.css);\nbody { margin: 0 }\n@import './missing.css';\n",
	})
	err := b.BuildAll()
	if err == nil {
		t.Fatal("build should fail on the unresolvable @import")
	}
	for _, want := range []string{"styles.css:3:", "missing.css", "KR7145"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}
