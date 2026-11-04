package build

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func contentConfigFor(root string, schema map[string]any) *config.Config {
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Content = map[string]any{"blog": map[string]any{"dir": "content/blog", "schema": schema}}
	return cfg
}

func TestContentCollectionHTMLKeepsHighlightedCode(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "content/blog/a.md", "---\ntitle: A\n---\nText\n\n```ts title=\"x.ts\"\nconst a: number = 1\n```\n\n- [x] done\n")
	writeFileRel(t, root, "content/blog/b.mdx", "---\ntitle: B\n---\nimport { Badge } from './x'\n\nexport const v = 42\n\n# {frontmatter.title} {v}\n\nSee <Badge>**new**</Badge>.\n\n```go\npackage main\n```\n")
	b := New(root, contentConfigFor(root, map[string]any{"title": "string"}))
	res := b.prepareContent()
	if len(res.Validation) != 0 {
		t.Fatalf("validation: %v", res.Validation)
	}
	byslug := map[string]string{}
	for _, e := range b.contentCollections["blog"] {
		byslug[e.Slug] = e.HTML
	}
	for _, want := range []string{`<pre class="chroma"`, `language-ts`, `data-title="x.ts"`, `checked=""`} {
		if !strings.Contains(byslug["a"], want) {
			t.Errorf("a.md html missing %q:\n%s", want, byslug["a"])
		}
	}
	mdx := byslug["b"]
	for _, want := range []string{`<pre class="chroma"`, `id="b-42"`, `B 42`, "<strong>new</strong>"} {
		if !strings.Contains(mdx, want) {
			t.Errorf("b.mdx html missing %q:\n%s", want, mdx)
		}
	}
	for _, bad := range []string{"import ", "export const", "{v}", "&lt;Badge", "<Badge"} {
		if strings.Contains(mdx, bad) {
			t.Errorf("b.mdx html has %q:\n%s", bad, mdx)
		}
	}
	// The dropped component is reported with its position.
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w.Error(), "b.mdx:10:") && strings.Contains(w.Error(), "Badge") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a positioned warning for <Badge>, got %v", res.Warnings)
	}
}

func TestContentSchemaErrorFailsBuild(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", "export default function P() { return <h1>hi</h1>; }\n")
	writeFileRel(t, root, "content/blog/bad.md", "---\ntitle: 5\n---\nbody\n")
	cfg := contentConfigFor(root, map[string]any{"title": "string", "order": "number"})
	err := New(root, cfg).BuildAll()
	var ve *ContentValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("BuildAll error = %v, want ContentValidationError", err)
	}
	if !strings.Contains(err.Error(), "bad.md") || !strings.Contains(err.Error(), "hint:") {
		t.Errorf("error should name the file and carry a hint: %v", err)
	}
}

func TestStaticContentPageDoesNotShipCollection(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", "export default function P() { return <h1>hi</h1>; }\n")
	writeFileRel(t, root, "src/pages/blog/[slug].tsx", `import { getCollection } from "krate/content";

export function generateStaticParams() {
  return getCollection("blog").filter((p) => !p.data.draft).map((p) => ({ slug: p.slug }));
}

export default function BlogPost(props: { params?: { slug: string } }) {
  const post = getCollection("blog").find((p) => p.slug === props.params?.slug);
  const [n, setN] = createSignal(0);
  if (!post) return <h1>Not found</h1>;
  return (
    <article>
      <h1>{post.data.title}</h1>
      <button onClick={() => setN(n() + 1)}>{n()}</button>
      <div dangerouslySetInnerHTML={{ __html: post.html }} />
    </article>
  );
}
`)
	writeFileRel(t, root, "content/blog/hello.md", "---\ntitle: Hello\n---\nPUBLICBODYMARK\n")
	writeFileRel(t, root, "content/blog/secret.md", "---\ntitle: Secret\ndraft: true\n---\nDRAFTSECRETMARK\n")
	cfg := contentConfigFor(root, map[string]any{"title": "string", "draft": "boolean?"})
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	var sawPublicHTML bool
	_ = filepath.WalkDir(cfg.OutDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, _ := os.ReadFile(p)
		s := string(data)
		if strings.Contains(s, "DRAFTSECRETMARK") || strings.Contains(s, "Secret") && strings.HasSuffix(p, ".js") {
			t.Errorf("draft entry leaked into %s", p)
		}
		if strings.HasSuffix(p, ".js") && strings.Contains(s, "PUBLICBODYMARK") {
			t.Errorf("entry body shipped to the browser in %s (only the rendered HTML needs to exist)", p)
		}
		if strings.HasSuffix(p, "hello/index.html") && strings.Contains(s, "PUBLICBODYMARK") {
			sawPublicHTML = true
		}
		return nil
	})
	if !sawPublicHTML {
		t.Error("hello page should render the entry html")
	}
}

func TestUnfoldableCollectionChainDoesNotShipDrafts(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", `import { getCollection } from "krate/content";
import { createSignal } from "@krate/runtime";

export default function Home() {
  const [tag, setTag] = createSignal("a");
  const posts = getCollection("blog").filter((p) => p.data.tag === tag());
  return (
    <main>
      <button onClick={() => setTag("b")}>b</button>
      <ul>{posts.map((p) => <li>{p.data.title}</li>)}</ul>
    </main>
  );
}
`)
	writeFileRel(t, root, "content/blog/hello.md", "---\ntitle: Hello\ntag: a\n---\nPUBLICBODYMARK\n")
	writeFileRel(t, root, "content/blog/secret.md", "---\ntitle: Secret\ntag: b\ndraft: true\n---\nDRAFTSECRETMARK\n")
	cfg := contentConfigFor(root, map[string]any{"title": "string", "tag": "string?", "draft": "boolean?"})
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	var js string
	_ = filepath.WalkDir(cfg.OutDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".js") {
			data, _ := os.ReadFile(p)
			js += string(data)
		}
		return nil
	})
	if !strings.Contains(out, "src/pages/index.tsx:6:") || !strings.Contains(out, "KR8810") || !strings.Contains(out, "draft entries included: 1") {
		t.Errorf("expected a located KR8810 warning:\n%s", out)
	}
	for _, bad := range []string{"DRAFTSECRETMARK", "PUBLICBODYMARK"} {
		if strings.Contains(js, bad) {
			t.Errorf("%s shipped to the browser", bad)
		}
	}
}

// A page that mentions html does get it for the runtime chain; nothing else.
func TestUnfoldableCollectionChainShipsOnlyMentionedFields(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", `import { getCollection } from "krate/content";
import { createSignal } from "@krate/runtime";

export default function Home() {
  const [tag, setTag] = createSignal("a");
  const posts = getCollection("blog").filter((p) => p.data.tag === tag());
  return (
    <main>
      <button onClick={() => setTag("b")}>b</button>
      <div onClick={() => console.log(posts.map((p) => p.html))}>x</div>
    </main>
  );
}
`)
	writeFileRel(t, root, "content/blog/hello.md", "---\ntitle: Hello\ntag: a\nsecretfield: SECRETFIELDMARK\n---\nPUBLICBODYMARK\n")
	cfg := contentConfigFor(root, map[string]any{"title": "string", "tag": "string?", "secretfield": "string?"})
	if _, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	var js string
	_ = filepath.WalkDir(cfg.OutDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".js") {
			data, _ := os.ReadFile(p)
			js += string(data)
		}
		return nil
	})
	if !strings.Contains(js, "<p>PUBLICBODYMARK</p>") {
		t.Errorf("mentioned html must be shipped:\n%s", js)
	}
	for _, bad := range []string{"SECRETFIELDMARK", "body:", "title"} {
		if strings.Contains(js, bad) {
			t.Errorf("%q is not mentioned by the page and must not be shipped:\n%s", bad, js)
		}
	}
}
