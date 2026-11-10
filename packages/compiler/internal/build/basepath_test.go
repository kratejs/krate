package build

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

const basePathPageSrc = `import { createSignal } from "@krate/runtime";
import "./global.css";
import pic from "../lib/pic.png";
function Wrap(props) { return <Link {...props} class="wrap" />; }
function Named({ to }) { return <Link href={to}>named</Link>; }
const items = [{ url: "/one" }, { url: "/two" }];
export default function Page() {
	const [n, setN] = createSignal(0);
	const [open, setOpen] = createSignal(false);
	const load = async () => { const m = await import("../lib/lazy"); setN(m.v); setOpen(true); };
	const spawn = () => new Worker(new URL("../lib/w.ts", import.meta.url));
	return (
		<main>
			<Link id="lit" href="/about">about</Link>
			<Wrap id="spread" href="/spread">spread</Wrap>
			<Named to="/named" />
			{items.map((i) => <Link href={i.url}>{i.url}</Link>)}
			<Link id="ext" href="https://example.org/x">ext</Link>
			<Link id="proto" href="//example.org/x">proto</Link>
			<Link id="hash" href="#top">hash</Link>
			<Link id="rel" href="rel/path">rel</Link>
			<Link id="query" href="?q=1">query</Link>
			{open() ? <Link id="cond" href="/cond">cond</Link> : null}
			<img id="pic" src={pic} />
			<button id="load" onClick={load}>{n()}</button>
			<button id="spawn" onClick={spawn}>spawn</button>
		</main>
	);
}
`

// basePathProject writes a project that uses every kind of URL the compiler
// emits. configure sets the base path under test.
func basePathProject(t *testing.T, configure func(*config.Config)) (string, *config.Config) {
	t.Helper()
	root := codeThemeProject(t, map[string]string{
		"index.tsx": basePathPageSrc,
		"about.tsx": `export default function About() { return <main><Link id="home" href="/">home</Link></main>; }`,
		"global.css": `.a { background: url(../lib/bg.png); }
.b { background: url(/pub.png); }
.c { background: url(/not-ours.png); }
.d { background: url(https://example.org/x.png); }
`,
	})
	writeFileRel(t, root, "src/lib/pic.png", "pic")
	writeFileRel(t, root, "src/lib/bg.png", "bg")
	writeFileRel(t, root, "src/lib/lazy.ts", "export const v = 7;\n")
	writeFileRel(t, root, "src/lib/w.ts", "self.postMessage(1);\n")
	writeFileRel(t, root, "public/pub.png", "pub")
	writeFileRel(t, root, "public/favicon.ico", "ico")
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.PublicDir = filepath.Join(root, "public")
	cfg.Minify = false
	cfg.Head.Links = []config.HeadLink{{Rel: "icon", Href: "/favicon.ico"}}
	configure(cfg)
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return root, cfg
}

func readOutput(t *testing.T, cfg *config.Config, glob string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(cfg.OutDir, glob))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no output matches %s (%v)", glob, err)
	}
	var all bytes.Buffer
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(data)
	}
	return all.String()
}

func anchorHref(t *testing.T, html, id string) string {
	t.Helper()
	tag := regexp.MustCompile(`<a\b[^>]*\bid="` + id + `"[^>]*>`).FindString(html)
	m := regexp.MustCompile(`\bhref="([^"]*)"`).FindStringSubmatch(tag)
	if m == nil {
		t.Fatalf("no href on #%s in %q", id, tag)
	}
	return m[1]
}

func assertBasePathOutput(t *testing.T, cfg *config.Config, base string) {
	t.Helper()
	html := readOutput(t, cfg, "index.html")
	js := readOutput(t, cfg, "index.*.js")
	css := readOutput(t, cfg, "styles.*.css")

	for id, want := range map[string]string{
		"lit":    base + "/about",
		"spread": base + "/spread",
		"ext":    "https://example.org/x",
		"proto":  "//example.org/x",
		"hash":   "#top",
		"rel":    "rel/path",
		"query":  "?q=1",
	} {
		if got := anchorHref(t, html, id); got != want {
			t.Errorf("#%s href = %q, want %q", id, got, want)
		}
	}
	for _, want := range []string{
		`href="` + base + `/named"`, `href="` + base + `/one"`, `href="` + base + `/two"`, `href="` + base + `/cond"`,
		`src="` + base + `/assets/pic-`, `href="` + base + `/favicon.ico"`, `src="` + base + `/chunks/runtime.`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %s:\n%s", want, html)
		}
	}
	for _, want := range []string{base + `/chunks/lazy-`, base + `/workers/w-`} {
		if !regexp.MustCompile(`["'\x60]` + regexp.QuoteMeta(want)).MatchString(js) {
			t.Errorf("hydration js missing %s:\n%s", want, js)
		}
	}
	for _, want := range []string{"url(" + base + "/assets/bg-", "url(" + base + "/pub.png)", "url(/not-ours.png)", "url(https://example.org/x.png)"} {
		if !strings.Contains(css, want) {
			t.Errorf("css missing %s:\n%s", want, css)
		}
	}
	if !strings.Contains(html, `modulepreload" href="`+base+`/chunks/lazy-`) {
		t.Errorf("dynamic chunk preload lost:\n%s", html)
	}
	if doubled := base + base; base != "" && strings.Contains(html+js+css, doubled) {
		t.Errorf("base path applied twice:\n%s\n%s\n%s", html, js, css)
	}
}

func TestBasePathReachesEveryEmittedURL(t *testing.T) {
	requireRuntimeDist(t)
	_, cfg := basePathProject(t, func(c *config.Config) { c.BasePath = "/repo" })
	assertBasePathOutput(t, cfg, "/repo")
}

func TestSiteURLPathActsAsBasePath(t *testing.T) {
	requireRuntimeDist(t)
	_, cfg := basePathProject(t, func(c *config.Config) { c.Site.URL = "https://example.com/example/a" })
	assertBasePathOutput(t, cfg, "/example/a")
	if html := readOutput(t, cfg, "index.html"); !strings.Contains(html, `"basePath":"/example/a"`) {
		t.Errorf("runtime config lost the base path:\n%s", html)
	}
}

func TestRootSiteOutputHasNoBasePath(t *testing.T) {
	requireRuntimeDist(t)
	_, cfg := basePathProject(t, func(c *config.Config) { c.Site.URL = "https://example.com" })
	assertBasePathOutput(t, cfg, "")
}

func TestExplicitRootBasePathOverridesSiteURLPath(t *testing.T) {
	requireRuntimeDist(t)
	_, cfg := basePathProject(t, func(c *config.Config) {
		c.BasePath = "/"
		c.Site.URL = "https://example.com/behind/proxy"
	})
	assertBasePathOutput(t, cfg, "")
}

// TestImportMetaURLIsAbsolute guards `new URL(rel, import.meta.url)`, which
// throws on a root-relative second argument.
func TestImportMetaURLIsAbsolute(t *testing.T) {
	got := substituteImportMetaURL(`new URL("x.png",import.meta.url)`, "", "index.abc.js", "/repo")
	if want := `new URL("x.png",(location.origin+"/repo/index.abc.js"))`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestDynamicChunkURLsAreRelativeToBase(t *testing.T) {
	js := `import("/repo/chunks/a-1.js");import("/chunks/b-2.js");import("/elsewhere/chunks/c-3.js");`
	got := dynamicChunkURLs(js, "/repo")
	if len(got) != 2 || got[0] != "/chunks/a-1.js" || got[1] != "/chunks/b-2.js" {
		t.Errorf("got %v", got)
	}
}

func TestBasePathWarnsAboutPlainTags(t *testing.T) {
	requireRuntimeDist(t)
	root := codeThemeProject(t, map[string]string{
		"index.tsx": `export default function Page() {
	const dyn = "/dyn";
	return <main>
		<a href="/about">plain</a>
		<img src="/pub.png" />
		<a href="/repo/about">prefixed</a>
		<a href="https://example.org/">external</a>
		<a href="#top">hash</a>
		<Link href="/about">link</Link>
	</main>;
}`,
	})
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.BasePath = "/repo"
	b := New(root, cfg)
	stderr := captureStderr(t, func() {
		if err := b.BuildAll(); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
	})
	if got := strings.Count(stderr, "KR7144"); got != 2 {
		t.Fatalf("got %d KR7144 warnings, want 2:\n%s", got, stderr)
	}
	for _, want := range []string{`<a href="/about">`, `<img src="/pub.png">`} {
		if !strings.Contains(stderr, want) {
			t.Errorf("warning missing %s:\n%s", want, stderr)
		}
	}
	// The tags themselves stay exactly as written.
	if html := readOutput(t, cfg, "index.html"); !strings.Contains(html, `href=/about>plain`) && !strings.Contains(html, `href="/about">plain`) {
		t.Errorf("plain anchor was rewritten:\n%s", html)
	}
}

func TestPrefixHeadURLsOnlyMovesSiteFiles(t *testing.T) {
	dir := t.TempDir()
	writeFileRel(t, dir, "site.css", "x")
	cfg := config.Default()
	cfg.PublicDir = dir
	cfg.BasePath = "/repo"
	b := New(dir, cfg)
	in := `<head><link rel="stylesheet" href="/site.css"><link rel="stylesheet" href="/other.css"><link rel="stylesheet" href="/repo/styles.a.css"><script src="/site.css"></script><link href="//cdn.example/a.css"></head><body><link href="/site.css"></body>`
	want := `<head><link rel="stylesheet" href="/repo/site.css"><link rel="stylesheet" href="/other.css"><link rel="stylesheet" href="/repo/styles.a.css"><script src="/repo/site.css"></script><link href="//cdn.example/a.css"></head><body><link href="/site.css"></body>`
	if got := b.prefixHeadURLs(in); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// TestBasePathBrowser serves dist only under the base path, with no redirects,
// so any URL that missed the prefix is a real 404.
func TestBasePathBrowser(t *testing.T) {
	browser := requireBrowserTest(t)
	requireRuntimeDist(t)
	_, cfg := basePathProject(t, func(c *config.Config) {
		c.Site.URL = "https://example.com/example/a"
		c.Minify = true
	})
	runBrowserScript(t, "base_path.mjs", cfg.OutDir, browser, "/example/a")
}
