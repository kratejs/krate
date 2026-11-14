package build

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const segLayout = `export default function L({ children }: any) { return <div id="root-layout"><nav>root-nav</nav>{children}</div>; }`

func segProject() map[string]string {
	return map[string]string{
		"src/pages/_layout.tsx": segLayout,
		"src/pages/index.tsx":   `export default function Home() { return <h1>home</h1>; }`,
		"src/pages/404.tsx":     `export default function NF() { return <h1 id="p-root404">ROOT-404</h1>; }`,
		"src/pages/500.tsx":     `export default function E() { return <h1 id="p-root500">ROOT-500</h1>; }`,
		// A request-time page at the root: the root error page answers it.
		"src/pages/root-throw.tsx": `export const config = { ssr: true };
export default function T() { if (Math.random() >= 0) throw new Error("root secret"); return <p>never</p>; }`,

		"src/pages/blog/_layout.tsx":   `export default function L({ children }: any) { return <section id="blog-layout">{children}</section>; }`,
		"src/pages/blog/index.tsx":     `export default function B() { return <p id="blog-index">blog index</p>; }`,
		"src/pages/blog/not-found.tsx": `export default function NF() { return <p id="p-blog404">BLOG-404</p>; }`,
		"src/pages/blog/error.tsx":     `export default function E({ error }: any) { return <div id="p-blogerr"><p id="m">{error.message}</p><p id="d">{error.digest}</p></div>; }`,
		// A closed dynamic route: unknown params 404 with the blog page.
		"src/pages/blog/post/[slug].tsx": `export const dynamicParams = false;
export function generateStaticParams() { return [{ slug: "a" }, { slug: "b" }]; }
export default function P({ params }: any) { return <p id="post">post {params.slug}</p>; }`,
		"src/pages/blog/ssr-ok.tsx": `export const config = { ssr: true };
export default function P() { return <p id="ssr-ok">ssr ok</p>; }`,
		"src/pages/blog/ssr-nf.tsx": `import { notFound } from "@krate/runtime/server";
export const config = { ssr: true };
export default function P() { notFound(); return <p>never</p>; }`,
		"src/pages/blog/ssr-throw.tsx": `export const config = { ssr: true };
export default function P() { if (Math.random() >= 0) throw new Error("secret boom"); return <p>never</p>; }`,
		// ISR: the first render throws, later ones succeed. A cached 500 would
		// keep answering 500.
		"src/pages/blog/isr-flaky.tsx": `export const config = { isr: true, revalidate: 60 };
let n = 0;
export default function P() { n++; if (n === 1) throw new Error("isr first render"); return <p id="isr-ok">isr ok {n}</p>; }`,
		// ISR notFound() per param: a missing variant is a 404, never cached as a page.
		"src/pages/blog/isr/[id].tsx": `import { notFound } from "@krate/runtime/server";
export const config = { isr: true, revalidate: 60 };
export default function P({ params }: any) { if (params.id === "nope") notFound(); return <p id="isr-id">isr {params.id}</p>; }`,

		// A route group: its pages answer only routes inside the group.
		"src/pages/(shop)/_layout.tsx": `export default function L({ children }: any) { return <div id="shop-layout">{children}</div>; }`,
		"src/pages/(shop)/cart/index.tsx": `import { notFound } from "@krate/runtime/server";
export const config = { ssr: true };
export default function C() { notFound(); return <p>never</p>; }`,
		"src/pages/(shop)/cart/not-found.tsx": `export default function NF() { return <p id="p-cart404">CART-404</p>; }`,
	}
}

func captureStderrFile(t *testing.T) func() string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = f
	t.Cleanup(func() { os.Stderr = old })
	return func() string {
		data, _ := os.ReadFile(f.Name())
		return string(data)
	}
}

// TestSegmentPagesServe drives the real server: every place a request can fail
// answers with the page nearest to it, with the right status, headers and
// layouts, and no failure is cached as a success.
func TestSegmentPagesServe(t *testing.T) {
	e2eEnv(t)
	b, root := scProject(t, segProject())
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	logs := captureStderrFile(t)
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(path string) (*http.Response, string) {
		t.Helper()
		req, _ := http.NewRequest("GET", base+path, nil)
		req.Header.Set("Accept", "text/html")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp, string(data)
	}
	expect := func(t *testing.T, path string, status int, has []string, hasNot []string) (*http.Response, string) {
		t.Helper()
		resp, body := get(path)
		if resp.StatusCode != status {
			t.Errorf("GET %s: status %d, want %d\n%.400s", path, resp.StatusCode, status, body)
		}
		for _, w := range has {
			if !strings.Contains(body, w) {
				t.Errorf("GET %s: body missing %q", path, w)
			}
		}
		for _, w := range hasNot {
			if strings.Contains(body, w) {
				t.Errorf("GET %s: body must not contain %q", path, w)
			}
		}
		return resp, body
	}

	t.Run("static", func(t *testing.T) {
		resp, _ := expect(t, "/nope/", 404, []string{"p-root404", "root-nav"}, []string{"p-blog404"})
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("content-type %q", ct)
		}
		expect(t, "/blog/zzz/", 404, []string{"p-blog404", "blog-layout", "root-nav"}, []string{"p-root404"})
		expect(t, "/blog/zzz/deeper/still/", 404, []string{"p-blog404"}, nil)
		expect(t, "/blog/", 200, []string{"blog-index"}, nil)
		expect(t, "/blog/nope.html", 404, []string{"p-blog404"}, nil)
	})

	t.Run("closed dynamic route", func(t *testing.T) {
		expect(t, "/blog/post/a/", 200, []string{"post a"}, nil)
		expect(t, "/blog/post/zzz/", 404, []string{"p-blog404", "blog-layout"}, nil)
	})

	t.Run("ssr notFound", func(t *testing.T) {
		expect(t, "/blog/ssr-ok/", 200, []string{"ssr ok"}, nil)
		resp, _ := expect(t, "/blog/ssr-nf/", 404, []string{"p-blog404", "blog-layout"}, []string{"p-root404"})
		if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
			t.Errorf("a request-time 404 must not be cacheable: %q", cc)
		}
	})

	t.Run("route groups scope their pages", func(t *testing.T) {
		expect(t, "/cart/", 404, []string{"p-cart404", "shop-layout"}, []string{"p-root404"})
		// No route matches here, so no group page applies.
		expect(t, "/cart/zzz/", 404, []string{"p-root404"}, []string{"p-cart404"})
	})

	digestRe := regexp.MustCompile(`"digest":"([0-9a-f]{10})"`)
	t.Run("ssr error", func(t *testing.T) {
		resp, body := expect(t, "/blog/ssr-throw/", 500, []string{"blog-layout", "root-nav", "__krate_error", "Internal Server Error"}, []string{"secret boom", "p-root500"})
		if resp.Header.Get("X-Krate-Error") != "render" {
			t.Errorf("X-Krate-Error = %q", resp.Header.Get("X-Krate-Error"))
		}
		if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
			t.Errorf("a 500 must not be cacheable: %q", cc)
		}
		m := digestRe.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no digest in the page:\n%s", body)
		}
		// The page text is filled in from the same data (static error pages).
		if !strings.Contains(body, `id="m">Internal Server Error`) && !strings.Contains(body, "Internal Server Error</p>") {
			t.Errorf("sanitized message missing from the HTML text")
		}
		log := logs()
		if !strings.Contains(log, "secret boom") || !strings.Contains(log, m[1]) {
			t.Errorf("the log must carry the real error and the page's digest %s:\n%.1500s", m[1], log)
		}
	})

	t.Run("root error page", func(t *testing.T) {
		expect(t, "/root-throw/", 500, []string{"p-root500", "root-nav"}, []string{"root secret"})
	})

	t.Run("isr", func(t *testing.T) {
		// The first render throws: 500, and the next request renders afresh.
		expect(t, "/blog/isr-flaky/", 500, []string{"blog-layout"}, []string{"isr first render"})
		resp, _ := expect(t, "/blog/isr-flaky/", 200, []string{"isr-ok"}, nil)
		if resp.Header.Get("X-Krate-Cache") == "HIT" {
			t.Errorf("a 500 must not be served from the ISR cache")
		}
		expect(t, "/blog/isr/yes/", 200, []string{"isr yes"}, nil)
		for i := 0; i < 2; i++ {
			resp, _ := expect(t, "/blog/isr/nope/", 404, []string{"p-blog404", "blog-layout"}, []string{"isr nope"})
			if resp.Header.Get("X-Krate-Cache") != "" || !strings.Contains(resp.Header.Get("Cache-Control"), "no-store") {
				t.Errorf("a not-found render must not be cached as a success: cache=%q cc=%q", resp.Header.Get("X-Krate-Cache"), resp.Header.Get("Cache-Control"))
			}
		}
	})

	t.Run("segment pages are not routes", func(t *testing.T) {
		// The built files exist, but no route serves them as pages.
		for _, rel := range []string{"blog/404/index.html", "404/index.html", "blog/500/index.html"} {
			if _, err := os.Stat(filepath.Join(b.Cfg.OutDir, filepath.FromSlash(rel))); err == nil {
				t.Errorf("unexpected route directory %s", rel)
			}
		}
	})
}

func TestServerErrorSanitizing(t *testing.T) {
	prod := newServerError("secret", "stack here", "", false)
	if prod.Message != "Internal Server Error" || prod.Stack != "" || prod.Digest == "" {
		t.Errorf("production error = %+v", prod)
	}
	dev := newServerError("boom", "at x", "", true)
	if dev.Message != "boom" || dev.Stack != "at x" || dev.Digest != errorDigest("boom", "at x") {
		t.Errorf("dev error = %+v", dev)
	}
	if got := newServerError("m", "s", "abc123", false).Digest; got != "abc123" {
		t.Errorf("a digest from the renderer is kept: %q", got)
	}
	// The renderer (server-renderer.ts) hashes sha256(message + "\n" + stack).
	if got := errorDigest("msg", "stack"); got != "68e97d1118" {
		t.Errorf("errorDigest drifted from the renderer's hash: %q", got)
	}
}

func TestSegmentIndexRefusesPathsOutsideTheOutput(t *testing.T) {
	out := t.TempDir()
	secret := filepath.Join(filepath.Dir(out), "secret.html")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := newSegmentIndex(out)
	for _, f := range []string{"../secret.html", "/etc/passwd", "a/../../secret.html"} {
		if _, ok := idx.readPage(SegmentMatch{File: f}); ok {
			t.Errorf("readPage(%q) must be refused", f)
		}
	}
}
