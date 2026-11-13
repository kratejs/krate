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
