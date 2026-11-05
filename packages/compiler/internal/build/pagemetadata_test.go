package build

import (
	"os"
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

// A metadata value the literal reader cannot see (a module constant, a call)
// is evaluated at build time instead of dropped.
func TestPageMetadataWithComputedValuesIsEvaluated(t *testing.T) {
	b, root := hooksProject(t, map[string]string{
		"src/pages/index.tsx": `const name = "computed";
export const metadata = { title: name.toUpperCase(), description: ["a", "b"].join("+") };
export default function Home() { return <main><h1>Home</h1></main>; }`,
	})
	got := captureStderr(t, func() {
		if err := b.BuildAll(); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
	})
	if strings.Contains(got, "KR7122") {
		t.Errorf("a computable metadata value must not warn:\n%s", got)
	}
	head := headOf(readMetaDist(t, root, "index.html"))
	if !strings.Contains(head, "<title>COMPUTED</title>") || !strings.Contains(head, "content=a+b") {
		t.Errorf("computed metadata belongs in the head:\n%s", head)
	}
}

// What truly cannot be evaluated still warns (KR7122) and the page builds.
func TestPageMetadataThatCannotBeEvaluatedWarns(t *testing.T) {
	b, root := hooksProject(t, map[string]string{
		"src/pages/index.tsx": `export const metadata = { title: window.document.title };
export default function Home() { return <main><h1>Home</h1></main>; }`,
	})
	got := captureStderr(t, func() {
		if err := b.BuildAll(); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
	})
	if !strings.Contains(got, "KR7122") || !strings.Contains(got, "force-dynamic") {
		t.Errorf("expected a KR7122 warning with a hint, got:\n%s", got)
	}
	if html := readMetaDist(t, root, "index.html"); !strings.Contains(html, "<h1>Home</h1>") {
		t.Errorf("the page still builds:\n%s", html)
	}
}

// A page whose own default export is a runtime component is a region: the
// shell holds its marker, so the manifest must name a bundle that renders it.
func TestRuntimePageIsItsOwnRegion(t *testing.T) {
	for name, src := range map[string]string{
		"directive": "// @runtime\nexport default function Home() { return <main><h1>runtime page {String(1 + 1)}</h1></main>; }",
		"decorator": "@runtime\nexport default function Home() { return <main><h1>runtime page</h1></main>; }",
	} {
		t.Run(name, func(t *testing.T) {
			b, root := scProject(t, map[string]string{"src/pages/index.tsx": src})
			if err := b.BuildAll(); err != nil {
				t.Fatalf("BuildAll: %v", err)
			}
			html := readDist(t, filepath.Join(root, "dist"), "index.html")
			marker := "<!--region:region-Home_c0-->"
			if !strings.Contains(html, marker) {
				t.Fatalf("shell has no region marker:\n%s", html)
			}
			m := readManifest(t, root)
			regs := m.Regions["/"]
			if len(regs) != 1 || regs[0].ID != "region-Home_c0" || regs[0].BundlePath == "" || !strings.HasSuffix(regs[0].SourcePath, "index.tsx") {
				t.Fatalf("regions[/] = %+v, want one region with a runtime bundle", regs)
			}
			if _, err := os.Stat(filepath.Join(root, "dist", filepath.FromSlash(regs[0].BundlePath))); err != nil {
				t.Errorf("bundle %s missing: %v", regs[0].BundlePath, err)
			}
		})
	}
}
