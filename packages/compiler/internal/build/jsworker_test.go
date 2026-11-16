package build

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/jsworker"
)

// The parity tests build one fixture once per build-time JavaScript engine and
// require the same pages and the same warnings from each.

// buildEngines returns quickjs and the worker runtimes installed here.
func buildEngines(t *testing.T) []string {
	t.Helper()
	engines := []string{"quickjs"}
	for _, n := range []string{"node", "bun"} {
		if _, err := exec.LookPath(n); err == nil {
			engines = append(engines, n)
		}
	}
	if len(engines) == 1 {
		requireE2E(t, "neither node nor bun is installed")
	}
	return engines
}

// engineBuild is what one build of a parity fixture produced.
type engineBuild struct {
	pages    map[string]string
	warnings string
	raw      string
	prof     map[string]int
	profTime map[string]time.Duration
}

// projectFixture writes a project into a new directory.
type projectFixture func(t *testing.T) (string, *config.Config)

func filesFixture(files map[string]string) projectFixture {
	return func(t *testing.T) (string, *config.Config) {
		b, root := hooksProject(t, files)
		return root, b.Cfg
	}
}

// volatileRe matches what differs between builds of one project made in
// different directories: chunk content hashes and the project directory.
var volatileRe = regexp.MustCompile(`\.[0-9a-z]{6,8}\.(js|css)|-[0-9a-z]{6,8}\.mjs|island_[0-9a-f]{12}|isl-[0-9a-z]+`)

func buildWithEngine(t *testing.T, fixture projectFixture, engine string, setup func(*Builder)) engineBuild {
	t.Helper()
	t.Setenv(jsworker.EnvVar, engine)
	root, cfg := fixture(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()
	metadataCache.Clear()
	realHintsCache.Clear()
	resetModuleWarnings()
	b := New(root, cfg)
	b.Prof = NewProfiler(true)
	if setup != nil {
		setup(b)
	}
	logs := captureStderrFile(t)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("%s build: %v\n%s", engine, err, logs())
	}
	normalize := func(s string) string {
		// Node prints file URLs, which escape "~" (Windows short names)
		// and spaces.
		s = strings.ReplaceAll(s, strings.NewReplacer("~", "%7E", " ", "%20").Replace(filepath.ToSlash(root)), "<root>")
		s = strings.ReplaceAll(s, filepath.ToSlash(root), "<root>")
		s = strings.ReplaceAll(s, root, "<root>")
		return volatileRe.ReplaceAllString(s, "#")
	}
	out := engineBuild{pages: map[string]string{}, prof: map[string]int{}, profTime: map[string]time.Duration{}}
	_ = filepath.WalkDir(cfg.OutDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".html" {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatal(rerr)
		}
		rel, _ := filepath.Rel(cfg.OutDir, p)
		out.pages[filepath.ToSlash(rel)] = normalize(string(data))
		return nil
	})
	out.raw = logs()
	out.warnings = warningBlocks(normalize(out.raw))
	b.Prof.mu.Lock()
	for k, n := range b.Prof.counts {
		if strings.HasPrefix(k, "js:") {
			out.prof[k] = n
			out.profTime[k] = b.Prof.totals[k]
		}
	}
	b.Prof.mu.Unlock()
	return out
}

// warningBlocks returns the warnings in a build's output, from each
// "Warning:" line through its hint, sorted.
func warningBlocks(out string) string {
	var blocks []string
	var cur []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Warning:") {
			if cur != nil {
				blocks = append(blocks, strings.Join(cur, "\n"))
			}
			cur = []string{}
		}
		if cur == nil {
			continue
		}
		cur = append(cur, strings.TrimSpace(l))
		if strings.Contains(l, "hint:") {
			blocks = append(blocks, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	if cur != nil {
		blocks = append(blocks, strings.Join(cur, "\n"))
	}
	// The failed-worker notice is checked on its own.
	kept := blocks[:0]
	for _, bl := range blocks {
		if !strings.Contains(bl, "worker failed") {
			kept = append(kept, bl)
		}
	}
	sort.Strings(kept)
	return strings.Join(kept, "\n\n")
}

// checkParity builds fixture with every engine, compares each build with the
// QuickJS one, and checks that the workers ran the labels in want.
func checkParity(t *testing.T, fixture projectFixture, want ...string) map[string]engineBuild {
	t.Helper()
	builds := map[string]engineBuild{}
	for _, e := range buildEngines(t) {
		t.Run(e, func(t *testing.T) { builds[e] = buildWithEngine(t, fixture, e, nil) })
	}
	base, ok := builds["quickjs"]
	if !ok || t.Failed() {
		return builds
	}
	if len(base.pages) == 0 {
		t.Fatal("the build wrote no pages")
	}
	for e, got := range builds {
		if e == "quickjs" {
			for k := range got.prof {
				if !strings.HasPrefix(k, "js:quickjs-") {
					t.Errorf("quickjs build ran %s", k)
				}
			}
			continue
		}
		comparePages(t, e, base.pages, got.pages)
		if got.warnings != base.warnings {
			t.Errorf("%s warnings differ\nquickjs:\n%s\n%s:\n%s", e, base.warnings, e, got.warnings)
		}
		for _, label := range want {
			if got.prof["js:"+e+"-"+label] == 0 {
				t.Errorf("%s build ran no %s evaluation on the worker (profile %v)", e, label, got.prof)
			}
		}
	}
	return builds
}

func comparePages(t *testing.T, engine string, want, got map[string]string) {
	t.Helper()
	var keys []string
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if got[k] != want[k] {
			t.Errorf("%s: %s differs from the quickjs build\nquickjs:\n%s\n%s:\n%s", engine, k, want[k], engine, got[k])
		}
	}
	if len(got) != len(want) {
		t.Errorf("%s wrote %d pages, quickjs %d", engine, len(got), len(want))
	}
}

var metadataParityFiles = map[string]string{
	"src/pages/index.tsx": `export default function Home() { return <p>home</p>; }`,
	"src/pages/_layout.tsx": `export const metadata = { title: { default: "Site", template: "%s | Site" }, description: "layout", metadataBase: "https://x.example" };
export default function L({ children }: any) { return <div>{children}</div>; }`,
	"src/lib/seo.ts": `export function pageMetadata(title: string, description: string) { return { title, description, openGraph: { title, images: ['/og.png'] }, keywords: [title, "x"] }; }
export function failing(): never { throw new Error("no title"); }`,
	"src/lib/data.ts": `const posts: Record<string, string> = { a: "Alpha", b: "Beta", c: "Gamma" };
let calls = 0;
export async function post(slug: string) { calls++; await Promise.resolve(); return { title: posts[slug], calls, env: process.env.KRATE_PARITY ?? "unset" }; }`,
	"src/pages/blog/[slug].tsx": `import { post } from '../../lib/data';
export function generateStaticParams() { return [{ slug: "a" }, { slug: "b" }, { slug: "c" }]; }
export async function generateMetadata({ params }: any, parent: Promise<any>) {
  const { slug } = await params;
  const above = await parent;
  const p = await post(slug);
  return { title: "Post " + p.title, description: above.description + " " + p.calls + " " + p.env, openGraph: { title: "OG " + slug, images: ["/og/" + slug + ".png"] }, alternates: { canonical: "/blog/" + slug } };
}
export default function Post({ params }: any) { return <h1>{params.slug}</h1>; }`,
	"src/pages/about.tsx": `import { pageMetadata } from '../lib/seo';
export const metadata = pageMetadata('About', 'About us');
const hero = <h1>Local Hero</h1>;
export default function About() { return <main>{hero}</main>; }`,
	"src/pages/404.tsx": `import { failing } from '../lib/seo';

export const metadata = failing();
export default function NotFound() { return <h1>Missing</h1>; }`,
	"src/pages/docs/_layout.tsx": `export async function generateMetadata() { throw new TypeError("layout boom"); }
export default function L({ children }: any) { return <div>{children}</div>; }`,
	"src/pages/docs/intro.tsx": `export default function Intro() { return <p>intro</p>; }`,
	"src/pages/timer.tsx": `export async function generateMetadata() {
  await new Promise((r) => setTimeout(r, 5));
  return { title: "Timer" };
}
export default function T() { return <p>t</p>; }`,
}

// TestBuildJSParityMetadata covers metadata, generateMetadata per static
// param, generateStaticParams, thrown errors and their warnings.
func TestBuildJSParityMetadata(t *testing.T) {
	builds := checkParity(t, filesFixture(metadataParityFiles), "metadata", "static-params")
	if b, ok := builds["quickjs"]; ok {
		head := b.pages["blog/b/index.html"]
		if !strings.Contains(head, "Post Beta | Site") {
			t.Errorf("blog/b head:\n%s", head)
		}
		if !strings.Contains(b.warnings, "layout boom") || !strings.Contains(b.warnings, "no title") {
			t.Errorf("warnings lack the thrown errors:\n%s", b.warnings)
		}
	}
	if b, ok := builds["node"]; ok && !strings.Contains(b.pages["timer/index.html"], "<title>Timer | Site</title>") {
		t.Errorf("timer head:\n%s", b.pages["timer/index.html"])
	}
}

// TestBuildJSParityHints covers resource-hint renders, merged and alone,
// with a render whose module throws.
func TestBuildJSParityHints(t *testing.T) {
	fixture := func(t *testing.T) (string, *config.Config) { return hintsFixture(t, 6, 2) }
	builds := checkParity(t, fixture, "real-hints")
	if b, ok := builds["quickjs"]; ok && !strings.Contains(b.pages["p3/index.html"], "/models/m3.glb") {
		t.Errorf("p3 lacks its hints:\n%s", b.pages["p3/index.html"])
	}
}

// TestBuildJSParityIslands covers island server renders.
func TestBuildJSParityIslands(t *testing.T) {
	fixture := func(t *testing.T) (string, *config.Config) {
		root := t.TempDir()
		cfg := setupIslandProject(t, root, true)
		providerSite(t, root)
		return root, cfg
	}
	builds := checkParity(t, fixture, "islands-ssr")
	if b, ok := builds["quickjs"]; ok && strings.Count(b.pages["app/index.html"], "data-krate-island-ssr") != 4 {
		t.Errorf("app/index.html:\n%s", b.pages["app/index.html"])
	}
}

// TestBuildJSWorkerFailureFallsBack kills the worker after its first
// evaluation, and gives another build a runtime that cannot start: both
// builds finish in QuickJS with the pages a QuickJS build makes.
func TestBuildJSWorkerFailureFallsBack(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	fixture := filesFixture(metadataParityFiles)
	base := buildWithEngine(t, fixture, "quickjs", nil)
	var killed sync.Once
	crash := buildWithEngine(t, fixture, "node", func(b *Builder) {
		p := b.jsWorker()
		if p == nil {
			t.Fatal("no worker pool")
		}
		p.Observe = func(string, time.Duration) {
			killed.Do(func() { killWorkers(p) })
		}
	})
	comparePages(t, "crashed node", base.pages, crash.pages)
	if crash.warnings != base.warnings {
		t.Errorf("warnings differ\nquickjs:\n%s\ncrashed:\n%s", base.warnings, crash.warnings)
	}
	if !strings.Contains(crash.raw, "worker failed") {
		t.Errorf("the build did not report the failed worker:\n%s", crash.raw)
	}
	broken := buildWithEngine(t, fixture, "node", func(b *Builder) {
		b.js.once = true
		b.js.pool = jsworker.New("node", filepath.Join(t.TempDir(), "missing-node"), 1)
	})
	comparePages(t, "unstartable node", base.pages, broken.pages)
}

// killWorkers kills every worker process of p, as a crash would.
func killWorkers(p *jsworker.Pool) {
	for _, pid := range p.PIDs() {
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Kill()
		}
	}
}

// TestBuildJSQuickJSOverride checks that KRATE_BUILD_JS=quickjs starts no
// worker.
func TestBuildJSQuickJSOverride(t *testing.T) {
	t.Setenv(jsworker.EnvVar, "quickjs")
	b := New(t.TempDir(), config.Default())
	if p := b.jsWorker(); p != nil {
		t.Fatalf("KRATE_BUILD_JS=quickjs selected %s", p.Name)
	}
}

// metadataPerfFiles is a site of pages pages, each with generateMetadata
// under a layout with metadata, sharing a library module that builds a
// lookup table when it loads, as content libraries do.
func metadataPerfFiles(pages int) map[string]string {
	files := map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <p>home</p>; }`,
		"src/pages/_layout.tsx": `export const metadata = { title: { default: "Site", template: "%s | Site" }, description: "layout", metadataBase: "https://x.example" };
export default function L({ children }: any) { return <div>{children}</div>; }`,
		"src/lib/seo.ts": `const rows = Array.from({ length: 20000 }, (_, i) => ({ id: i, slug: "s" + ((i * 7919) % 20000), words: ["a" + i, "b" + (i % 13), "c" + (i % 7)] }));
rows.sort((a, b) => (a.slug < b.slug ? -1 : a.slug > b.slug ? 1 : 0));
const bySlug = new Map(rows.map((r) => [r.slug, r]));
export function seo(n: number) {
  const r = bySlug.get("s" + n)!;
  return { title: "Page " + n, description: r.words.join(" ") + " " + rows.length, openGraph: { title: "OG " + n, images: ["/og/" + n + ".png"] }, alternates: { canonical: "/p" + n } };
}`,
	}
	for i := 0; i < pages; i++ {
		files[fmt.Sprintf("src/pages/p%d.tsx", i)] = fmt.Sprintf(`import { seo } from '../lib/seo';
export async function generateMetadata() { return seo(%d); }
export default function P() { return <main><h1>p%d</h1></main>; }`, i, i)
	}
	return files
}

// TestBuildJSMetadataPerf builds a 40-page metadata site with each engine and
// reports, for the best of three builds, the build time, the CPU time of the
// build and its workers, and the time spent in evaluations.
func TestBuildJSMetadataPerf(t *testing.T) {
	if os.Getenv("KRATE_BUILD_JS_PERF") == "" {
		t.Skip("set KRATE_BUILD_JS_PERF=1 to build a 40-page fixture three times per engine")
	}
	const pages = 40
	fixture := filesFixture(metadataPerfFiles(pages))
	var base map[string]string
	for _, e := range buildEngines(t) {
		var wall, cpu, workers, eval time.Duration
		for round := 0; round < 3; round++ {
			c0, ch0 := processCPU(), childCPU()
			start := time.Now()
			got := buildWithEngine(t, fixture, e, nil)
			w := time.Since(start)
			c, ch := processCPU()-c0+childCPU()-ch0, childCPU()-ch0
			if round == 0 || w < wall {
				wall = w
			}
			if round == 0 || c < cpu {
				cpu, workers = c, ch
			}
			var ev time.Duration
			for k, d := range got.profTime {
				if strings.HasSuffix(k, "-metadata") {
					ev += d
				}
			}
			if round == 0 || ev < eval {
				eval = ev
			}
			if base == nil {
				base = got.pages
			} else {
				comparePages(t, e, base, got.pages)
			}
		}
		t.Logf("%d metadata pages on %-7s: build %v, CPU %v (workers %v), evaluations %v", pages, e,
			wall.Round(time.Millisecond), cpu.Round(time.Millisecond), workers.Round(time.Millisecond), eval.Round(time.Millisecond))
	}
}
