package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A Link prefetches on intent by default; prefetch="viewport" marks it for
// viewport prefetch and prefetch={false} drops the attribute.
func TestLinkPrefetchModes(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() {
  return (
    <main>
      <Link href="/a">plain</Link>
      <Link href="/b" prefetch="viewport">vp</Link>
      <Link href="/c" prefetch={false}>off</Link>
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
		`<a href=/a data-krate-link data-prefetch>plain`,
		`<a href=/b data-krate-link data-prefetch=viewport>vp`,
		`<a href=/c data-krate-link>off`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
}
