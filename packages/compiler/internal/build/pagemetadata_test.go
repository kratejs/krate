package build

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPageMetadataExportBecomesHeadTags(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx": `export const metadata = {
  title: "Home & <Stuff>",
  description: "The \"home\" page",
  alternates: { canonical: "https://example.com/" },
  openGraph: { title: "OG Home", images: ["/og.png"] },
  twitter: { card: "summary" },
};
export default function Home() { return <main><h1>Home</h1></main>; }`,
		"src/pages/head.tsx": `import { Head } from "@krate/runtime";
export const metadata = { title: "From metadata", description: "meta desc" };
export default function P() { return <main><Head><title>From Head</title></Head><h1>x</h1></main>; }`,
		"src/pages/plain.tsx": `export default function P() { return <main><h1>plain</h1></main>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	home := readDist(t, filepath.Join(root, "dist"), "index.html")
	for _, want := range []string{
		"<title>Home &amp; &lt;Stuff&gt;</title>",
		"name=description",
		"canonical",
		"og:title",
		"og:image",
		"twitter:card",
	} {
		if !strings.Contains(home, want) {
			t.Errorf("index.html missing %q:\n%s", want, home)
		}
	}
	if strings.Contains(home, "<title>Krate App</title>") {
		t.Errorf("default title must not survive a metadata title:\n%s", home)
	}

	// An explicit <Head> overrides the metadata for the same key; other
	// metadata tags still apply.
	head := readDist(t, filepath.Join(root, "dist"), "head/index.html")
	if !strings.Contains(head, "<title>From Head</title>") || strings.Contains(head, "From metadata") {
		t.Errorf("an explicit <Head> title must win:\n%s", head)
	}
	if !strings.Contains(head, "meta desc") {
		t.Errorf("the rest of the metadata should still apply:\n%s", head)
	}

	// No metadata export: nothing is added.
	plain := readDist(t, filepath.Join(root, "dist"), "plain/index.html")
	if !strings.Contains(plain, "<title>Krate App</title>") {
		t.Errorf("a page without metadata keeps the default title:\n%s", plain)
	}
}

func TestPageMetadataWithoutStaticValuesWarns(t *testing.T) {
	b, _ := scProject(t, map[string]string{
		"src/pages/index.tsx": `const name = "computed";
export const metadata = { title: name };
export default function Home() { return <main><h1>Home</h1></main>; }`,
	})
	// A warning, not an error: the page still builds.
	got := captureStderr(t, func() {
		if err := b.BuildAll(); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
	})
	if !strings.Contains(got, "KR7122") || !strings.Contains(got, "<Head>") {
		t.Errorf("expected a KR7122 warning with a hint, got:\n%s", got)
	}
}
