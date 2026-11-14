package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestLocalJSXConstantsAndJSXProps(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  []string
	}{
		"local JSX list in the component": {
			files: map[string]string{"src/pages/index.tsx": `
export default function Legal() {
  const sections = [{ id: 'a', title: 'A', body: (<p>Body A</p>) }, { id: 'b', title: 'B', body: (<><p>Body B</p></>) }];
  return <main>{sections.map(s => <section key={s.id} id={s.id}><h2>{s.title}</h2>{s.body}</section>)}</main>;
}`},
			want: []string{`<section id="a"><h2>A</h2><p>Body A</p></section>`, `<p>Body B</p>`},
		},
		"JSX prop props.x": {
			files: map[string]string{
				"src/hero.tsx": `export function Hero(props: { intro: any }) { return <div class="hero">{props.intro}</div>; }`,
				"src/pages/index.tsx": `import { Hero } from '../hero';
export default function Page() { return <Hero intro={<p>Welcome</p>} />; }`,
			},
			want: []string{`<div class="hero"><p>Welcome</p></div>`},
		},
		"JSX prop destructured": {
			files: map[string]string{
				"src/hero.tsx": `export function Hero({ intro }: { intro: any }) { return <div class="hero">{intro}</div>; }`,
				"src/pages/index.tsx": `import { Hero } from '../hero';
export default function Page() { return <Hero intro={<><p>Welcome</p><p>Two</p></>} />; }`,
			},
			want: []string{`<div class="hero"><p>Welcome</p><p>Two</p></div>`},
		},
		"JSX prop array and conditional": {
			files: map[string]string{
				"src/hero.tsx": `export function Hero({ a, b }: { a: any, b: any }) { return <div class="hero">{a}{b}</div>; }`,
				"src/pages/index.tsx": `import { Hero } from '../hero';
export default function Page() { return <Hero a={[<i key="1">x</i>, <i key="2">y</i>]} b={true ? <b>yes</b> : <b>no</b>} />; }`,
			},
			want: []string{`<i>x</i><i>y</i>`, `<b>yes</b>`},
		},
		"JSX prop array only": {
			files: map[string]string{
				"src/hero.tsx": `export function Hero({ a }: { a: any }) { return <div class="hero">{a}</div>; }`,
				"src/pages/index.tsx": `import { Hero } from '../hero';
export default function Page() { return <Hero a={[<i key="1">x</i>, <i key="2">y</i>]} />; }`,
			},
			want: []string{`<i>x</i><i>y</i>`},
		},
		"JSX prop cond only": {
			files: map[string]string{
				"src/hero.tsx": `export function Hero({ b }: { b: any }) { return <div class="hero">{b}</div>; }`,
				"src/pages/index.tsx": `import { Hero } from '../hero';
export default function Page() { return <Hero b={true ? <b>yes</b> : <b>no</b>} />; }`,
			},
			want: []string{`<b>yes</b>`},
		},
		"JSX prop with page signal": {
			files: map[string]string{
				"src/hero.tsx": `export function Hero({ intro }: { intro: any }) { return <div class="hero">{intro}</div>; }`,
				"src/pages/index.tsx": `import { createSignal } from '@krate/runtime';
import { Hero } from '../hero';
export default function Page() { const [n, setN] = createSignal(0); return <div><button onClick={() => setN(n()+1)}>{n()}</button><Hero intro={<p>Welcome</p>} /></div>; }`,
			},
			want: []string{`<div class="hero"><p>Welcome</p></div>`},
		},
		"head helpers on an interactive page": {
			files: map[string]string{
				"src/lib/seo.ts": "export const absoluteUrl = (p: string) => 'https://example.com' + p;\nexport function ogImage(s: string) { return `https://example.com/og/${s}.png`; }",
				"src/pages/index.tsx": `import { Head, createSignal } from '@krate/runtime';
import { absoluteUrl, ogImage } from '../lib/seo';
export default function Page() {
  const [n, setN] = createSignal(0);
  return <div><Head><link rel="canonical" href={absoluteUrl('/pricing')} /><meta property="og:image" content={ogImage('x')} /></Head><button onClick={() => setN(n()+1)}>{n()}</button></div>;
}`,
			},
			want: []string{`href="https://example.com/pricing"`, `content="https://example.com/og/x.png"`},
		},
		"JSX prop uses caller local static": {
			files: map[string]string{
				"src/hero.tsx": `export function Hero({ intro }: { intro: any }) { return <div class="hero">{intro}</div>; }`,
				"src/pages/index.tsx": `import { Hero } from '../hero';
export default function Page() { const title = 'T' + 1; return <Hero intro={<p>{title}</p>} />; }`,
			},
			want: []string{`<div class="hero"><p>T1</p></div>`},
		},
		"JSX prop uses caller local interactive": {
			files: map[string]string{
				"src/hero.tsx": `import { createSignal } from '@krate/runtime';
export function Hero({ intro }: { intro: any }) { const [n, setN] = createSignal(0); return <div class="hero"><button onClick={() => setN(n()+1)}>{n()}</button>{intro}</div>; }`,
				"src/pages/index.tsx": `import { Hero } from '../hero';
export default function Page() { const title = 'T' + 1; return <Hero intro={<p>{title}</p>} />; }`,
			},
			want: []string{`<p>T1</p>`},
		},
		"JSX prop keeps the caller's name when the callee has its own": {
			files: map[string]string{
				"src/hero.tsx": `export function Hero({ intro, title }: { intro: any, title: string }) { return <div class="hero"><h1>{title}</h1>{intro}</div>; }`,
				"src/pages/index.tsx": `import { Hero } from '../hero';
export default function Page() { const title = 'caller'; return <Hero title="callee" intro={<p>{title}</p>} />; }`,
			},
			want: []string{`<h1>callee</h1><p>caller</p>`},
		},
		"local JSX constant is not captured by a map parameter": {
			files: map[string]string{"src/pages/index.tsx": `
export default function Page() {
  const label = <b>outer</b>;
  const rows = [{ v: label }];
  return <main>{['inner'].map(label => <i>{label}</i>)}{rows.map(r => <p>{r.v}</p>)}</main>;
}`},
			want: []string{`<i>inner</i>`, `<p><b>outer</b></p>`},
		},
		"JSX prop interactive": {
			files: map[string]string{
				"src/hero.tsx": `import { createSignal } from '@krate/runtime';
export function Hero(props: { intro: any }) { const [n, setN] = createSignal(0); return <div class="hero"><button onClick={() => setN(n() + 1)}>{n()}</button>{props.intro}</div>; }`,
				"src/pages/index.tsx": `import { Hero } from '../hero';
export default function Page() { return <Hero intro={<p>Welcome</p>} />; }`,
			},
			want: []string{`<p>Welcome</p>`},
		},
		"head helpers with JSON-LD and title": {
			files: map[string]string{
				"src/lib/seo.ts": "export const absoluteUrl = (p: string) => 'https://example.com' + p;\nexport function ogImage(s: string) { return `https://example.com/og/${s}.png`; }\nexport const ld = (n: string) => JSON.stringify({ '@type': 'Thing', name: n });",
				"src/pages/index.tsx": `import { Head } from '@krate/runtime';
import { absoluteUrl, ogImage, ld } from '../lib/seo';
export default function Page() {
  const slug = 'pricing';
  return <div><Head><title>{'Price ' + slug}</title><link rel="canonical" href={absoluteUrl('/pricing')} /><meta property="og:image" content={ogImage(slug)} /><script type="application/ld+json">{ld('x')}</script></Head><p>hi</p></div>;
}`,
			},
			want: []string{`<link rel="canonical" href="https://example.com/pricing"`, `content="https://example.com/og/pricing.png"`, `<title>Price pricing</title>`, `"@type":"Thing"`},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			html, _, err := buildKrateProject(t, tc.files)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(html, w) {
					t.Errorf("html misses %s:\n%s", w, html)
				}
			}
		})
	}
}

func TestJSXHeldInConstPassedAsProp(t *testing.T) {
	html, _, err := buildKrateProject(t, map[string]string{
		"src/components/LegalDoc.tsx": `export default function LegalDoc({ title, sections }: { title: string; sections: { id: string; heading: string; body: any }[] }) {
  return (
    <article>
      <h1>{title}</h1>
      {sections.map((s) => (
        <section id={s.id}><h2>{s.heading}</h2>{s.body}</section>
      ))}
    </article>
  );
}`,
		"src/components/Card.tsx": `export default function Card({ body, content }: { body?: any; content?: any }) {
  return <div class="card">{body}{content && content.a}</div>;
}`,
		"src/pages/index.tsx": `import LegalDoc from '../components/LegalDoc';
import Card from '../components/Card';
const sections = [
  { id: 'use', heading: 'Use', body: <>You may use <strong>this</strong> site.</> },
  { id: 'privacy', heading: 'Privacy', body: <p>We keep little data.</p> },
];
const intro = <p>Hello</p>;
export default function Terms() {
  const parts = { a: <b>A</b> };
  return <main><LegalDoc title="Terms" sections={sections} /><Card body={intro} content={parts} /></main>;
}`,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, w := range []string{
		`<section id="use"><h2>Use</h2>You may use <strong>this</strong> site.</section>`,
		`<section id="privacy"><h2>Privacy</h2><p>We keep little data.</p></section>`,
		`<div class="card"><p>Hello</p><b>A</b></div>`,
	} {
		if !strings.Contains(html, w) {
			t.Errorf("html misses %s:\n%s", w, html)
		}
	}
}

// A component that holds state, passed as a JSX prop, is a real component
// instance: it renders its initial state and hydrates (KRATE_BROWSER checks
// that it flips to the mounted value).
func TestStatefulComponentPassedAsJSXProp(t *testing.T) {
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	for rel, content := range map[string]string{
		"src/components/Probe.tsx": `import { createSignal, onMount } from '@krate/runtime';
export default function Probe() {
  const [v, setV] = createSignal('ssr');
  onMount(() => setV('hydrated'));
  return <span id="probe" class={v() === 'hydrated' ? 'probe is-on' : 'probe'} showIf={v() !== 'gone'}>{v()}</span>;
}`,
		"src/components/Shell.tsx": `export default function Shell({ slot }: { slot?: any }) {
  return <div class="slot">{slot ?? null}</div>;
}`,
		"src/pages/_layout.tsx": `import Shell from '../components/Shell';
import Probe from '../components/Probe';
export default function L({ children }: { children: any }) {
  return <><Shell slot={<Probe />} /><main>{children}</main></>;
}`,
		"src/pages/index.tsx": `export default function P() { return <h1>prop</h1>; }`,
	} {
		writeProjectFile(t, root, rel, content)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("build: %v", err)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	if !strings.Contains(html, `class="probe"`) && !strings.Contains(html, `class=probe`) {
		t.Errorf("the slot does not render Probe:\n%s", html)
	}
	if !strings.Contains(html, ">ssr<") && !strings.Contains(html, ">ssr") {
		t.Errorf("the slot does not render Probe's initial state:\n%s", html)
	}

	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		return
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "jsx_prop_slot_browser.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", script, cfg.OutDir, browser)
	cmd.Env = append(os.Environ(), "NODE_PATH="+filepath.Join(repoRoot, "node_modules"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser check failed: %v\n%s", err, out)
	}
}
