package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const narrowHead = `import { getCollection } from "krate/content";
import { createSignal } from "@krate/runtime";
export function generateStaticParams() { return getCollection("blog").map((p) => ({ slug: p.slug })); }
`

// Every shape a blog post page is naturally written in. Each has one hydrating
// control, and must ship its own post at most, never the other posts.
var narrowShapes = map[string]string{
	"inline find": narrowHead + `export default function Post(props: { params?: { slug: string } }) {
  const post = getCollection("blog").find((p) => p.slug === props.params?.slug);
  const [n, setN] = createSignal(0);
  if (!post) return <h1>Not found</h1>;
  return <article><h1>{post.data.title}</h1><div dangerouslySetInnerHTML={{ __html: post.html }} /><button onClick={() => setN(n() + 1)}>{n()}</button></article>;
}`,
	"destructured params": narrowHead + `export default function Post({ params }: { params: { slug: string } }) {
  const post = getCollection("blog").find((p) => p.slug === params.slug);
  const [n, setN] = createSignal(0);
  if (!post) return <h1>Not found</h1>;
  return <article><h1>{post.data.title}</h1><div dangerouslySetInnerHTML={{ __html: post.html }} /><button onClick={() => setN(n() + 1)}>{n()}</button></article>;
}`,
	"nested params pattern": narrowHead + `export default function Post({ params: { slug } }: { params: { slug: string } }) {
  const post = getCollection("blog").find((p) => p.slug === slug);
  const [n, setN] = createSignal(0);
  if (!post) return <h1>Not found</h1>;
  return <article><h1>{post.data.title}</h1><div dangerouslySetInnerHTML={{ __html: post.html }} /><button onClick={() => setN(n() + 1)}>{n()}</button></article>;
}`,
	"slug destructured from params": narrowHead + `export default function Post(props: { params: { slug: string } }) {
  const { slug } = props.params;
  const post = getCollection("blog").find((p) => p.slug === slug);
  const [n, setN] = createSignal(0);
  if (!post) return <h1>Not found</h1>;
  return <article><h1>{post.data.title}</h1><div dangerouslySetInnerHTML={{ __html: post.html }} /><button onClick={() => setN(n() + 1)}>{n()}</button></article>;
}`,
	"collection in a const": narrowHead + `export default function Post({ params }: { params: { slug: string } }) {
  const posts = getCollection("blog");
  const post = posts.find((p) => p.slug === params.slug);
  const [n, setN] = createSignal(0);
  if (!post) return <h1>Not found</h1>;
  return <article><h1>{post.data.title}</h1><div dangerouslySetInnerHTML={{ __html: post.html }} /><button onClick={() => setN(n() + 1)}>{n()}</button></article>;
}`,
	"module-level collection const": narrowHead + `const posts = getCollection("blog");
export default function Post({ params }: { params: { slug: string } }) {
  const post = posts.find((p) => p.slug === params.slug);
  const [n, setN] = createSignal(0);
  if (!post) return <h1>Not found</h1>;
  return <article><h1>{post.data.title}</h1><div dangerouslySetInnerHTML={{ __html: post.html }} /><button onClick={() => setN(n() + 1)}>{n()}</button></article>;
}`,
	"helper function": narrowHead + `function getPost(slug: string) { return getCollection("blog").find((p) => p.slug === slug); }
export default function Post({ params }: { params: { slug: string } }) {
  const post = getPost(params.slug);
  const [n, setN] = createSignal(0);
  if (!post) return <h1>Not found</h1>;
  return <article><h1>{post.data.title}</h1><div dangerouslySetInnerHTML={{ __html: post.html }} /><button onClick={() => setN(n() + 1)}>{n()}</button></article>;
}`,
	"arrow helper": narrowHead + `const getPost = (slug: string) => getCollection("blog").find((p) => p.slug === slug);
export default function Post({ params }: { params: { slug: string } }) {
  const post = getPost(params.slug);
  const [n, setN] = createSignal(0);
  if (!post) return <h1>Not found</h1>;
  return <article><h1>{post.data.title}</h1><div dangerouslySetInnerHTML={{ __html: post.html }} /><button onClick={() => setN(n() + 1)}>{n()}</button></article>;
}`,
	"arrow component": narrowHead + `const Post = ({ params }: { params: { slug: string } }) => {
  const post = getCollection("blog").find((p) => p.slug === params.slug);
  const [n, setN] = createSignal(0);
  if (!post) return <h1>Not found</h1>;
  return <article><h1>{post.data.title}</h1><div dangerouslySetInnerHTML={{ __html: post.html }} /><button onClick={() => setN(n() + 1)}>{n()}</button></article>;
};
export default Post;`,
	"other posts listed": narrowHead + `export default function Post({ params }: { params: { slug: string } }) {
  const post = getCollection("blog").find((p) => p.slug === params.slug);
  const others = getCollection("blog").filter((p) => p.slug !== params.slug);
  const [n, setN] = createSignal(0);
  if (!post) return <h1>Not found</h1>;
  return <article><h1>{post.data.title}</h1><div dangerouslySetInnerHTML={{ __html: post.html }} /><ul>{others.map((o) => <li>{o.data.title}</li>)}</ul><button onClick={() => setN(n() + 1)}>{n()}</button></article>;
}`,
	"client component receives the post": narrowHead + `import Meta from "../../components/Meta";
export default function Post({ params }: { params: { slug: string } }) {
  const post = getCollection("blog").find((p) => p.slug === params.slug);
  if (!post) return <h1>Not found</h1>;
  return <article><h1>{post.data.title}</h1><div dangerouslySetInnerHTML={{ __html: post.html }} /><Meta post={post} /></article>;
}`,
}

// A blog's [slug] page, written the obvious way, must not put the other posts
// in any client asset: neither in a generated page's script nor in the
// fallback template's.
func TestBlogPostPageShipsOnlyItsOwnPost(t *testing.T) {
	for name, page := range narrowShapes {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			ensureRuntimeDist(t)
			if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
				t.Fatal(err)
			}
			writeFileRel(t, root, "src/pages/index.tsx", "export default function P() { return <h1>hi</h1>; }\n")
			writeFileRel(t, root, "src/pages/blog/[slug].tsx", page)
			writeFileRel(t, root, "src/components/Meta.tsx", `import { createSignal } from "@krate/runtime";
export default function Meta(props: { post: { data: { title: string } } }) {
  const [n, setN] = createSignal(0);
  return <button onClick={() => setN(n() + 1)}>{props.post.data.title} {n()}</button>;
}`)
			for _, n := range []string{"one", "two", "three"} {
				writeFileRel(t, root, "content/blog/"+n+".md", "---\ntitle: Post "+n+"\n---\nBODYMARK"+strings.ToUpper(n)+" "+strings.Repeat("filler ", 30)+"\n")
			}
			cfg := contentConfigFor(root, map[string]any{"title": "string"})
			if _, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
				t.Fatalf("BuildAll: %v", err)
			}
			sawOwn := map[string]bool{}
			_ = filepath.WalkDir(cfg.OutDir, func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				rel, _ := filepath.Rel(cfg.OutDir, p)
				rel = filepath.ToSlash(rel)
				data, _ := os.ReadFile(p)
				s := string(data)
				owner := ""
				if parts := strings.Split(rel, "/"); len(parts) >= 3 && parts[0] == "blog" {
					owner = parts[1]
				}
				for _, n := range []string{"one", "two", "three"} {
					mark := "BODYMARK" + strings.ToUpper(n)
					if !strings.Contains(s, mark) {
						continue
					}
					if strings.HasSuffix(rel, "/index.html") && owner == n {
						sawOwn[n] = true
						continue
					}
					if owner != n {
						t.Errorf("%s (post %q) carries the body of post %q", rel, owner, n)
					}
				}
				return nil
			})
			for _, n := range []string{"one", "two", "three"} {
				if !sawOwn[n] {
					t.Errorf("blog/%s/index.html does not render its own body", n)
				}
			}
		})
	}
}

// A lookup that cannot be folded still ships the data it needs, and says how
// much, so the author can decide.
func TestUnfoldableLookupWarnsWithSize(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", `import { getCollection } from "krate/content";
import { createSignal } from "@krate/runtime";
export default function Home() {
  const [q, setQ] = createSignal("");
  const hits = () => getCollection("blog").filter((p) => p.html.includes(q()));
  return <main><input onInput={(e) => setQ(e.target.value)} /><ul>{hits().map((p) => <li>{p.data.title}</li>)}</ul></main>;
}
`)
	for _, n := range []string{"one", "two"} {
		writeFileRel(t, root, "content/blog/"+n+".md", "---\ntitle: Post "+n+"\n---\n"+strings.Repeat("filler ", 200)+"\n")
	}
	cfg := contentConfigFor(root, map[string]any{"title": "string"})
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if !strings.Contains(out, "KR8810") || !strings.Contains(out, "2 entries (about 2.") || !strings.Contains(out, "KB)") {
		t.Errorf("want a KR8810 warning naming the entry count and size:\n%s", out)
	}
}
