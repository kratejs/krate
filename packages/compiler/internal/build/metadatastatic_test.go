package build

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// metadataFixture covers the chain shapes the faster paths handle and the
// ones they hand back to the full evaluation.
func metadataFixture() map[string]string {
	return map[string]string{
		"src/config/site.ts": `export const SITE_NAME = 'Acme';
export const SITE_TAGLINE = 'Things for people';
export const SITE_DESCRIPTION = ` + "`Everything ${SITE_NAME} makes.`" + `;
export const CARD_SIZE = { width: 1200, height: 630 } as const;
export const ROBOTS_OFF = { index: false, follow: true };
`,
		"src/server/site-url.ts": `export function siteOrigin(): string {
  return (process.env.APP_URL ?? 'http://localhost:3000').replace(/\/+$/, '');
}
`,
		"src/lib/seo.ts": `import { SITE_DESCRIPTION, SITE_NAME, CARD_SIZE } from '../config/site';
import { siteOrigin } from '../server/site-url';
export function pageMetadata({ title, description = SITE_DESCRIPTION, path, index = true }: any) {
  const url = siteOrigin() + path;
  const ogTitle = title ? title + ' | ' + SITE_NAME : SITE_NAME;
  const images = [{ url: new URL('/og', siteOrigin()).toString(), alt: ogTitle, ...CARD_SIZE }];
  return {
    title: title ? title : { absolute: ogTitle },
    description,
    alternates: { canonical: url },
    robots: index ? undefined : { index: false, follow: true },
    openGraph: { type: 'website', siteName: SITE_NAME, url, title: ogTitle, description, images },
    twitter: { card: 'summary_large_image', title: ogTitle, images: images.map((i) => i.url) },
  };
}
`,
		"src/pages/_layout.tsx": `import type { Metadata, Viewport } from '@krate/runtime/server';
import { SITE_DESCRIPTION, SITE_NAME, SITE_TAGLINE, CARD_SIZE } from '../config/site';
import { siteOrigin } from '../server/site-url';
export const metadata: Metadata = {
  metadataBase: new URL(siteOrigin()),
  title: { default: ` + "`${SITE_NAME}: ${SITE_TAGLINE}`" + `, template: ` + "`%s | ${SITE_NAME}`" + ` },
  description: SITE_DESCRIPTION,
  applicationName: SITE_NAME,
  openGraph: {
    type: 'website',
    siteName: SITE_NAME,
    title: ` + "`${SITE_NAME}: ${SITE_TAGLINE}`" + `,
    description: SITE_DESCRIPTION,
    images: [{ url: '/og', alt: SITE_NAME, ...CARD_SIZE }],
  },
  twitter: { card: 'summary_large_image' },
};
export const viewport: Viewport = { colorScheme: 'dark light', themeColor: '#070b0c' };
export default function Root({ children }: any) { return <div>{children}</div>; }
`,
		"src/pages/index.tsx": `export default function Home() { return <p>home</p>; }`,
		"src/pages/settings.tsx": `import type { Metadata } from '@krate/runtime/server';
export const metadata: Metadata = { title: 'Settings' };
export default function Settings() { return <p>settings</p>; }`,
		"src/pages/hidden.tsx": `export const metadata = { title: 'Hidden', robots: { index: false, follow: false }, keywords: ['a', 'b'], other: { '2': 'two', z: 'zed', '1': 'one' } };
export default function Hidden() { return <p>hidden</p>; }`,
		"src/pages/brand.tsx": `import { SITE_NAME, ROBOTS_OFF } from '../config/site';
const SECTION = 'Brand';
export const metadata = { title: ` + "`${SECTION} of ${SITE_NAME}`" + `, description: SECTION + ' page', robots: { ...ROBOTS_OFF }, alternates: { canonical: '/brand' } };
export default function Brand() { return <p>brand</p>; }`,
		"src/pages/helper.tsx": `import { pageMetadata } from '../lib/seo';
export const metadata = pageMetadata({ title: 'Helper', path: '/helper', index: false });
export default function Helper() { return <p>helper</p>; }`,
		"src/pages/home2.tsx": `import { pageMetadata } from '../lib/seo';
export const metadata = pageMetadata({ path: '/' });
export default function Home2() { return <p>home2</p>; }`,
		"src/pages/gm.tsx": `export async function generateMetadata(_: any, parent: Promise<any>) {
  const p = await parent;
  return { title: 'Generated', description: 'under ' + String(p.metadataBase) };
}
export default function Gm() { return <p>gm</p>; }`,
		"src/pages/blog/_layout.tsx": `export const metadata = {
  openGraph: { title: { template: '%s - Blog', default: 'Blog' }, type: 'article', images: ['/blog-og.png'] },
  alternates: { canonical: '/blog' },
};
export default function BlogLayout({ children }: any) { return <section>{children}</section>; }`,
		"src/pages/blog/index.tsx": `export const metadata = { title: 'Blog home' };
export default function BlogHome() { return <p>blog</p>; }`,
		"src/pages/blog/post.tsx": `export const metadata = { title: 'A post', openGraph: { title: 'A post', description: 'og' }, twitter: { title: 'tw' } };
export default function Post() { return <p>post</p>; }`,
		"src/pages/blog/[slug].tsx": `export function generateStaticParams() { return [{ slug: 'one' }, { slug: 'two' }]; }
export const metadata = { title: 'Entry', description: 'An entry' };
export default function Entry({ params }: any) { return <p>{params.slug}</p>; }`,
		"src/pages/blog/tag/[tag].tsx": `export function generateStaticParams() { return [{ tag: 'x' }, { tag: 'y' }]; }
export async function generateMetadata({ params }: any) {
  const { tag } = await params;
  return { title: 'Tag ' + tag, openGraph: { title: 'Tag ' + tag } };
}
export default function Tag({ params }: any) { return <p>{params.tag}</p>; }`,
		"src/pages/broken/_layout.tsx": `function boom(): string { throw new Error('layout boom'); }
export const metadata = { title: boom() };
export default function Broken({ children }: any) { return <div>{children}</div>; }`,
		"src/pages/broken/index.tsx": `export const metadata = { title: 'Under a broken layout' };
export default function UnderBroken() { return <p>broken</p>; }`,
		"src/pages/docs/_layout.tsx": `export async function generateMetadata() { return { description: 'docs layout' }; }
export default function Docs({ children }: any) { return <div>{children}</div>; }`,
		"src/pages/item/[id].tsx": `export const metadata = { title: 'Item', openGraph: { images: ['/item.png'] } };
export default function Item({ params }: any) { return <p>{params.id}</p>; }`,
		"src/pages/dated/_layout.tsx": `export const metadata = { other: { built: new Date(0) }, robots: Object.freeze({ index: true }) };
export default function Dated({ children }: any) { return <div>{children}</div>; }`,
		"src/pages/dated/index.tsx": `export const metadata = { title: 'Dated' };
export default function DatedPage() { return <p>dated</p>; }`,
		"src/pages/live.tsx": `export const metadata = { title: 'Live' };
export const dynamic = 'force-dynamic';
export default function Live() { return <p>live</p>; }`,
		"src/pages/livegm.tsx": `export async function generateMetadata() { return { title: 'Live gm' }; }
export const dynamic = 'force-dynamic';
export default function LiveGm() { return <p>live gm</p>; }`,
		"src/pages/docs/intro.tsx": `export const metadata = { title: 'Intro' };
export default function Intro() { return <p>intro</p>; }`,
	}
}

func resetMetadataRuns(b *Builder) {
	b.metaRuns.Layout.Store(0)
	b.metaRuns.Page.Store(0)
	b.metaRuns.Data.Store(0)
	b.metaRuns.Batch.Store(0)
	b.metaRuns.Chain.Store(0)
}

// distHeads maps every built HTML file to its <head>.
func distHeads(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	dist := filepath.Join(root, "dist")
	_ = filepath.Walk(dist, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".html") {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		rel, _ := filepath.Rel(dist, p)
		out[filepath.ToSlash(rel)] = headOf(string(data))
		return nil
	})
	return out
}

// The per-page results through the faster paths equal those of the full
// chain, results and errors alike, page by page.
func TestMetadataFastPathMatchesFullChain(t *testing.T) {
	t.Parallel()
	b, root := hooksProject(t, metadataFixture())
	b.Env = map[string]string{"APP_URL": "https://acme.example/"}
	if err := b.BuildAll(); err != nil {
		t.Fatal(err)
	}
	pages, err := findPages(b.Cfg.PagesDir)
	if err != nil {
		t.Fatal(err)
	}
	bnd := b.newBundler()
	paramSets := map[string][]map[string]string{
		"blog/[slug].tsx":    {{"slug": "one"}, {"slug": "two"}},
		"blog/tag/[tag].tsx": {{"tag": "x"}, {"tag": "y"}},
	}
	wantPath := map[string]string{
		"index.tsx":          "page",
		"settings.tsx":       "batch",
		"hidden.tsx":         "batch",
		"brand.tsx":          "batch",
		"helper.tsx":         "page",
		"home2.tsx":          "page",
		"gm.tsx":             "page",
		"blog/index.tsx":     "batch",
		"blog/post.tsx":      "batch",
		"blog/[slug].tsx":    "data",
		"blog/tag/[tag].tsx": "page",
		"broken/index.tsx":   "chain",
		"live.tsx":           "batch",
		"dated/index.tsx":    "chain",
		"item/[id].tsx":      "batch",
		"docs/intro.tsx":     "chain",
	}
	checked := 0
	for _, page := range pages {
		rel, _ := filepath.Rel(b.Cfg.PagesDir, page)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(filepath.Base(rel), "_") {
			continue
		}
		segs, pageFiles := b.metadataSegments(page)
		sets := paramSets[rel]
		if sets == nil && isDynamicRoute(page, b.Cfg.PagesDir) {
			sets = []map[string]string{dynamicRouteSentinels(extractParamNames(page, b.Cfg.PagesDir))}
		}
		if sets == nil {
			sets = []map[string]string{nil}
		}
		want, werr := b.evalMetadata(page, segs, pageFiles, sets, bnd)
		b.resetMetadataStatic()
		resetMetadataRuns(b)
		got, gerr := b.resolveMetadataChain(page, segs, pageFiles, sets, bnd)
		path := "chain"
		switch {
		case b.metaRuns.Chain.Load() > 0:
		case b.metaRuns.Batch.Load() > 0:
			path = "batch"
		case b.metaRuns.Data.Load() > 0:
			path = "data"
		case b.metaRuns.Page.Load() > 0:
			path = "page"
		}
		if w, ok := wantPath[rel]; ok && w != path {
			t.Errorf("%s took the %s path, want %s", rel, path, w)
		}
		if fmt.Sprint(werr) != fmt.Sprint(gerr) || !reflect.DeepEqual(want, got) {
			t.Errorf("%s: fast path\n%#v (%v)\nfull chain\n%#v (%v)", rel, got, gerr, want, werr)
		}
		checked++
		if rel == "broken/index.tsx" && !strings.Contains(fmt.Sprint(werr, want), "layout boom") {
			t.Errorf("the broken layout must report its error: %#v (%v)", want, werr)
		}
	}
	if checked < 12 {
		t.Fatalf("checked only %d pages", checked)
	}
	_ = root
}

// A whole build gives the same heads with and without the faster paths.
func TestMetadataFastPathBuildMatches(t *testing.T) {
	t.Parallel()
	heads := map[bool]map[string]string{}
	for _, on := range []bool{false, true} {
		b, root := hooksProject(t, metadataFixture())
		b.noMetadataFastPath = !on
		b.Env = map[string]string{"APP_URL": "https://acme.example/"}
		if err := b.BuildAll(); err != nil {
			t.Fatal(err)
		}
		heads[on] = distHeads(t, root)
	}
	if len(heads[true]) < 12 {
		t.Fatalf("only %d pages built", len(heads[true]))
	}
	var names []string
	for k := range heads[false] {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if heads[false][k] != heads[true][k] {
			t.Errorf("%s head differs\nfull chain:\n%s\nfast path:\n%s", k, heads[false][k], heads[true][k])
		}
	}
	if len(heads[false]) != len(heads[true]) {
		t.Errorf("built %d pages with the fast path, %d without", len(heads[true]), len(heads[false]))
	}
	for _, want := range []string{"<title>Settings | Acme</title>", "https://acme.example/og", "content=dark light"} {
		if !strings.Contains(heads[true]["settings/index.html"], want) {
			t.Errorf("settings head lacks %q:\n%s", want, heads[true]["settings/index.html"])
		}
	}
}

// Twenty literal pages under a static layout: the layout is evaluated once
// and the pages need no run of their own.
func TestMetadataStaticLayoutRunsOnce(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"src/config/site.ts": `export const SITE_NAME = 'Acme';`,
		"src/pages/_layout.tsx": `import { SITE_NAME } from '../config/site';
export const metadata = { metadataBase: new URL(process.env.APP_URL || 'http://localhost'), title: { default: SITE_NAME, template: '%s | ' + SITE_NAME }, openGraph: { siteName: SITE_NAME, images: ['/og.png'] } };
export default function L({ children }: any) { return <div>{children}</div>; }`,
	}
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("p%02d", i)
		if i == 0 {
			name = "index"
		}
		files["src/pages/"+name+".tsx"] = fmt.Sprintf(`import { SITE_NAME } from '../config/site';
export const metadata = { title: 'Page %d', description: `+"`About ${SITE_NAME}`"+` };
export default function P() { return <p>%d</p>; }`, i, i)
	}
	heads := map[bool]map[string]string{}
	for _, on := range []bool{false, true} {
		func() {
			b, root := hooksProject(t, files)
			b.noMetadataFastPath = !on
			b.Env = map[string]string{"APP_URL": "https://acme.example"}
			if err := b.BuildAll(); err != nil {
				t.Fatal(err)
			}
			heads[on] = distHeads(t, root)
			l, p, d, bt, c := b.metaRuns.Layout.Load(), b.metaRuns.Page.Load(), b.metaRuns.Data.Load(), b.metaRuns.Batch.Load(), b.metaRuns.Chain.Load()
			t.Logf("fast=%v: layout=%d page=%d data=%d batch=%d chain=%d", on, l, p, d, bt, c)
			if on && (l != 1 || p != 0 || d != 0 || bt != 1 || c != 0) {
				t.Errorf("fast path: layout=%d page=%d data=%d batch=%d chain=%d, want 1 0 0 1 0", l, p, d, bt, c)
			}
			if !on && c < 20 {
				t.Errorf("full chain ran %d times for 20 pages", c)
			}
		}()
	}
	if len(heads[true]) != 20 {
		t.Fatalf("built %d pages", len(heads[true]))
	}
	for k, v := range heads[false] {
		if heads[true][k] != v {
			t.Errorf("%s head differs\nfull chain:\n%s\nfast path:\n%s", k, v, heads[true][k])
		}
	}
	if h := heads[true]["p05/index.html"]; !strings.Contains(h, "<title>Page 5 | Acme</title>") || !strings.Contains(h, "https://acme.example/og.png") {
		t.Errorf("p05 head:\n%s", h)
	}
}

func TestLiteralPageModuleForms(t *testing.T) {
	t.Parallel()
	b, root := hooksProject(t, map[string]string{
		"src/config/site.ts":   "export const NAME = 'Acme';\nexport const ALIAS = NAME;\nexport function f() { return 1; }\n",
		"src/config/impure.ts": "export const NAME = 'Acme';\nconsole.log('loaded');\n",
		"src/config/let.ts":    "export let NAME = 'Acme';\n",
		"src/pages/a.tsx":      "import { ALIAS } from '../config/site';\nconst X = 'x';\nexport const metadata = { title: `${ALIAS}-${X}`, n: -1.5, o: { '1': true, a: null, u: undefined }, l: [1, 'two'] as const };\nexport default function A() { return null; }",
		"src/pages/b.tsx":      "import { NAME } from '../config/impure';\nexport const metadata = { title: NAME };\nexport default function B() { return null; }",
		"src/pages/c.tsx":      "import { NAME } from '../config/let';\nexport const metadata = { title: NAME };\nexport default function C() { return null; }",
		"src/pages/d.tsx":      "export const metadata = { title: `a\\nb` };\nexport default function D() { return null; }",
		"src/pages/e.tsx":      "export const metadata = { title: 'x', n: 0x10 };\nexport default function E() { return null; }",
		"src/pages/f.tsx":      "const t = String(1);\nexport const metadata = { title: 'x' };\nexport const viewport = { themeColor: 'red' };\nexport default function F() { return t; }",
		"src/pages/g.tsx":      "export const metadata = { title: 'x' };\nexport function generateMetadata() { return {}; }\nexport default function G() { return null; }",
	})
	bnd := b.newBundler()
	want := map[string]string{
		"a.tsx": `{"o":[["metadata",{"o":[["title","Acme-x"],["n",-1.5],["o",{"o":[["1",true],["a",null],["u",{"u":1}]]}],["l",{"a":[1,"two"]}]]}]]}`,
		"b.tsx": "",
		"c.tsx": "",
		"d.tsx": "",
		"e.tsx": "",
		"f.tsx": `{"o":[["metadata",{"o":[["title","x"]]}],["viewport",{"o":[["themeColor","red"]]}]]}`,
		"g.tsx": "",
	}
	for name, w := range want {
		got, ok := b.literalPageModule(filepath.Join(root, "src", "pages", name), bnd)
		if got != w || ok != (w != "") {
			t.Errorf("%s: got %q %v, want %q", name, got, ok, w)
		}
	}
}
