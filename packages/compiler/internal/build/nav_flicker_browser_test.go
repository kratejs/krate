package build

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// navFlickerFiles is a small app for measuring what a soft navigation looks
// like frame by frame: a layout with links, a loading.tsx, a stylesheet per
// page, a compiled page with a counter (page script) and a page with an island.
var navFlickerFiles = map[string]string{
	"src/components/Counter.tsx": `"use client";
import { useState } from "react";
export default function Counter() {
  const [n, setN] = useState(0);
  return <button id="count-b" onClick={() => setN(n + 1)}>{n}</button>;
}`,
	"src/pages/_layout.tsx": `export default function Layout({ children }: { children: any }) {
  return (
    <div class="shell">
      <nav>
        <Link id="to-home" href="/">Home</Link>
        <Link id="to-a" href="/a/">A</Link>
        <Link id="to-b" href="/b/">B</Link>
      </nav>
      {children}
    </div>
  );
}`,
	"src/pages/loading.tsx": `export default function Loading() { return <div id="loading-ui">Loading...</div>; }`,
	"src/pages/home.css":    `.home-title { color: rgb(1, 2, 3); }`,
	"src/pages/a.css":       `.a-title { color: rgb(10, 20, 30); }`,
	"src/pages/b.css":       `.b-title { color: rgb(40, 50, 60); }`,
	"src/pages/index.tsx": `import "./home.css";
import { useState } from "react";
export default function Home() {
  const [n, setN] = useState(0);
  return <main id="page-home"><h1 class="home-title">Home</h1><button id="count-home" onClick={() => setN(n + 1)}>{n}</button></main>;
}`,
	"src/pages/a.tsx": `import "./a.css";
import { useState } from "react";
export default function A() {
  const [n, setN] = useState(0);
  return <main id="page-a"><h1 class="a-title">A</h1><button id="count-a" onClick={() => setN(n + 1)}>{n}</button></main>;
}`,
	"src/pages/b.tsx": `import "./b.css";
import Counter from "../components/Counter";
export default function B() { return <main id="page-b"><h1 class="b-title">B</h1><Counter /></main>; }`,
}

// delayProxy fronts a Krate server and holds back documents, stylesheets and
// page scripts, so a navigation has a fetch phase long enough to sample. The
// delays are set with GET /__delay?doc=&css=&js= (milliseconds).
func delayProxy(t *testing.T, target string) string {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	var doc, css, js atomic.Int64
	proxy := httputil.NewSingleHostReverseProxy(u)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__delay" {
			q := r.URL.Query()
			set := func(v *atomic.Int64, k string) {
				if s := q.Get(k); s != "" {
					n, _ := strconv.Atoi(s)
					v.Store(int64(n))
				}
			}
			set(&doc, "doc")
			set(&css, "css")
			set(&js, "js")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		p := r.URL.Path
		var d int64
		switch {
		case strings.HasSuffix(p, ".css"):
			d = css.Load()
		case strings.HasSuffix(p, ".js"):
			if !strings.Contains(p, "/chunks/runtime.") {
				d = js.Load()
			}
		case !strings.Contains(filepath.Base(p), "."):
			d = doc.Load()
		}
		if d > 0 {
			time.Sleep(time.Duration(d) * time.Millisecond)
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// navFlickerResult is what testdata/nav_flicker.mjs measures per View
// Transitions mode.
type navFlickerResult struct {
	Mode string
	// Uncached click navigation from / to /a (slow document).
	Uncached struct {
		ContentMs, InteractiveMs, MinOpacity float64
		FadeFrames, BlankFrames              int
		LoadingFrames, NavigatingFrames      int
	}
	// Fast document: the loading UI must not flash.
	Fast struct {
		ContentMs     float64
		LoadingFrames int
	}
	// Click while the hover prefetch is still in flight.
	Inflight struct {
		ContentMs                 float64
		FadeFrames, LoadingFrames int
	}
	// history.back() to an entry the router rendered before.
	Back struct {
		ContentMs float64
		Fetches   int
	}
	// history.back() to the hard-loaded first entry.
	BackInitial struct {
		ContentMs float64
		Fetches   int
	}
	// Island page: time from content to a working click.
	Island struct {
		ContentMs, InteractiveMs float64
	}
	// View Transitions started by replace(?q=), refresh() and a page change.
	VT     struct{ Query, Refresh, Page int }
	Errors []string
}

// TestNavFlickerBrowser drives soft navigations in a real browser and samples
// every animation frame: the old page stays fully visible (no fade to blank)
// while an uncached document loads, the loading UI only appears for slow
// navigations, the new page is interactive as it appears, and back/forward to
// a visited entry renders without a network request.
func TestNavFlickerBrowser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the navigation flicker browser test")
	}
	if !canStopInProcessServer {
		t.Skip("in-process server needs a unix host")
	}
	ensureRuntimeDist(t)
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	b, root := scProject(t, navFlickerFiles)
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	// The build finds the runtime bundle under <root>/packages/runtime/dist.
	if err := linkRuntimePackage(t, filepath.Join(root, "packages")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	front := delayProxy(t, base)
	script, err := filepath.Abs(filepath.Join("testdata", "nav_flicker.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, front, browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser script failed: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var results []navFlickerResult
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &results); err != nil {
		t.Fatalf("parsing browser output: %v\n%s", err, out)
	}
	t.Logf("navigation frames (%s):\n%s", browser, lines[len(lines)-1])
	if len(results) != 2 {
		t.Fatalf("want results for VT off and on, got %d", len(results))
	}
	for _, r := range results {
		if len(r.Errors) > 0 {
			t.Errorf("[%s] page errors: %v", r.Mode, r.Errors)
		}
		u := r.Uncached
		if u.MinOpacity < 0.99 || u.FadeFrames > 0 || u.BlankFrames > 0 {
			t.Errorf("[%s] uncached navigation blanked the page: min opacity %.2f, %d faded and %d blank frames", r.Mode, u.MinOpacity, u.FadeFrames, u.BlankFrames)
		}
		if u.NavigatingFrames == 0 {
			t.Errorf("[%s] html[data-krate-navigating] was never set while the document loaded", r.Mode)
		}
		if u.LoadingFrames == 0 {
			t.Errorf("[%s] a slow navigation never showed the loading UI", r.Mode)
		}
		// The page script is preloaded with the document: the counter works
		// within a few frames of the page appearing.
		if u.InteractiveMs-u.ContentMs > 120 {
			t.Errorf("[%s] /a appeared at %.0fms but its counter only worked at %.0fms", r.Mode, u.ContentMs, u.InteractiveMs)
		}
		if r.Island.InteractiveMs-r.Island.ContentMs > 120 {
			t.Errorf("[%s] /b appeared at %.0fms but its island only worked at %.0fms", r.Mode, r.Island.ContentMs, r.Island.InteractiveMs)
		}
		if r.Fast.LoadingFrames > 0 {
			t.Errorf("[%s] a fast navigation flashed the loading UI for %d frames", r.Mode, r.Fast.LoadingFrames)
		}
		if r.Inflight.LoadingFrames > 0 || r.Inflight.FadeFrames > 0 {
			t.Errorf("[%s] a click during an in-flight prefetch showed the loading UI (%d frames) or faded (%d frames)", r.Mode, r.Inflight.LoadingFrames, r.Inflight.FadeFrames)
		}
		if r.Back.Fetches != 0 {
			t.Errorf("[%s] back to a visited entry fetched the document %d times", r.Mode, r.Back.Fetches)
		}
		if r.BackInitial.Fetches != 0 {
			t.Errorf("[%s] back to the first entry fetched the document %d times", r.Mode, r.BackInitial.Fetches)
		}
		if r.Mode == "vt-on" && (r.VT.Query != 0 || r.VT.Refresh != 0 || r.VT.Page != 1) {
			t.Errorf("[%s] view transitions: query %d, refresh %d, page %d (want 0, 0, 1)", r.Mode, r.VT.Query, r.VT.Refresh, r.VT.Page)
		}
	}
}
