package build

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDevBuildGateHoldsRequestsUntilTheRebuildEnds(t *testing.T) {
	hub := NewDevHub()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "served") })
	h := devBuildGate(hub, next)

	hub.BeginBuild()
	hub.BeginBuild()
	var served atomic.Int32
	var wg sync.WaitGroup
	for _, path := range []string{"/", "/chunks/x.js"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
			if rec.Body.String() == "served" {
				served.Add(1)
			}
		}()
	}
	// The dev endpoints and the health check do not wait.
	for _, path := range []string{"/__krate/hotreload", "/healthz"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Body.String() != "served" {
			t.Errorf("%s waited for the rebuild", path)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if n := served.Load(); n != 0 {
		t.Fatalf("%d requests were answered while a rebuild ran", n)
	}
	hub.EndBuild()
	time.Sleep(50 * time.Millisecond)
	if n := served.Load(); n != 0 {
		t.Fatalf("a request was answered while one of two nested rebuilds still ran")
	}
	hub.EndBuild()
	wg.Wait()
	if n := served.Load(); n != 2 {
		t.Fatalf("%d of 2 requests were answered after the rebuild", n)
	}
}

// probe requests urls over and over until stopped and keeps every answer that
// is not a 200.
type probe struct {
	stop chan struct{}
	done sync.WaitGroup
	mu   sync.Mutex
	bad  []string
	n    int
}

func startProbe(base string, urls func() []string) *probe {
	p := &probe{stop: make(chan struct{})}
	p.done.Add(1)
	go func() {
		defer p.done.Done()
		for {
			select {
			case <-p.stop:
				return
			default:
			}
			for _, u := range urls() {
				resp, err := http.Get(base + u)
				p.mu.Lock()
				p.n++
				switch {
				case err != nil:
					p.bad = append(p.bad, u+": "+err.Error())
				case resp.StatusCode != 200:
					p.bad = append(p.bad, fmt.Sprintf("%s: %d", u, resp.StatusCode))
				}
				p.mu.Unlock()
				if err == nil {
					resp.Body.Close()
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()
	return p
}

func (p *probe) finish() (int, []string) {
	close(p.stop)
	p.done.Wait()
	return p.n, p.bad
}

// A full rebuild (here: a page is added) empties the output directory and
// writes it again. Requests that arrive meanwhile got 404 pages, and the chunks
// a page names were missing when its scripts loaded.
func TestDevRequestsDuringAFullRebuildAreNeverAnswered404(t *testing.T) {
	files := map[string]string{
		"src/pages/index.tsx":       "export default function Home() { return <h1>home</h1>; }\n",
		"src/components/Toggle.tsx": devToggleIsland,
		"src/pages/toggle.tsx": `import Toggle from "../components/Toggle";
export default async function Page() { return <main><h1>toggle page</h1><Toggle /></main>; }
`,
		"src/pages/dyn.tsx": devSSRPageSrc("dyn"),
	}
	d := startDevRun(t, files)
	body := d.waitBody(t, "/toggle/", "toggle page")
	assets := regexp.MustCompile(`/chunks/[A-Za-z0-9_.\-]+\.js`).FindAllString(body, -1)
	if len(assets) == 0 {
		t.Fatalf("the page names no chunk:\n%s", body)
	}
	d.waitBody(t, "/dyn/", "dyn")
	urls := append([]string{"/", "/toggle/", "/dyn/"}, assets...)
	p := startProbe(d.base, func() []string { return urls })

	for i := 0; i < 3; i++ {
		page := fmt.Sprintf("src/pages/extra%d.tsx", i)
		d.write(t, page, "export default function X() { return <h1>extra</h1>; }\n")
		d.waitBody(t, fmt.Sprintf("/extra%d/", i), "extra")
	}
	n, bad := p.finish()
	if len(bad) > 0 {
		t.Fatalf("%d of %d requests during the rebuilds failed, first: %v", len(bad), n, bad[:min(len(bad), 5)])
	}
	if n < 2*len(urls) {
		t.Fatalf("only %d requests were made", n)
	}
}

// The chunks of the build before stay served after a rebuild replaced them: a
// page served just before it, or still open, names them, and loading one
// failed with "Failed to fetch dynamically imported module".
func TestDevServesTheChunksTheLastRebuildReplaced(t *testing.T) {
	files := map[string]string{
		"src/pages/index.tsx":       "export default function Home() { return <h1>home</h1>; }\n",
		"src/lib/label.ts":          "export const label = \"one\";\n",
		"src/components/Toggle.tsx": strings.Replace(devToggleIsland, `import { useState } from "react";`, "import { useState } from \"react\";\nimport { label } from \"../lib/label\";", 1),
		"src/components/Other.tsx":  strings.ReplaceAll(strings.Replace(devToggleIsland, `import { useState } from "react";`, "import { useState } from \"react\";\nimport { label } from \"../lib/label\";", 1), "Toggle", "Other"),
		"src/pages/toggle.tsx": `import Toggle from "../components/Toggle";
import Other from "../components/Other";
export default async function Page() { return <main><h1>toggle page</h1><Toggle /><Other /></main>; }
`,
	}
	files["src/components/Other.tsx"] = strings.Replace(files["src/components/Other.tsx"], `{on ? "on" : "off"}`, `{label}{on ? "on" : "off"}`, 1)
	files["src/components/Toggle.tsx"] = strings.Replace(files["src/components/Toggle.tsx"], `{on ? "on" : "off"}`, `{label}{on ? "on" : "off"}`, 1)
	d := startDevRun(t, files)
	body := d.waitBody(t, "/toggle/", "toggle page")
	entries := devIslandChunkURL.FindAllString(body, -1)
	if len(entries) == 0 {
		t.Fatalf("no island chunk on the page:\n%s", body)
	}
	// The chunks the island entries import, as the browser loads them.
	imports := func() []string {
		var out []string
		for _, e := range entries {
			_, js := getText(t, d.base+e)
			for _, m := range regexp.MustCompile(`from\s*"\./([^"]+\.js)"`).FindAllStringSubmatch(js, -1) {
				out = append(out, "/chunks/"+m[1])
			}
		}
		return out
	}
	before := imports()
	if len(before) == 0 {
		t.Fatalf("the island imports no shared chunk")
	}

	// An edit of a module the island imports gives the shared chunk a new name.
	d.write(t, "src/lib/label.ts", "export const label = \"two\";\n")
	waitUntilTrue(t, 40*time.Second, "the island to change", func() bool {
		after := imports()
		return strings.Join(after, ",") != strings.Join(before, ",")
	})
	for _, u := range before {
		if code, _ := getText(t, d.base+u); code != 200 {
			t.Errorf("%s answers %d after the rebuild replaced it", u, code)
		}
	}

	// A full rebuild keeps them too.
	d.write(t, "src/pages/extra.tsx", "export default function X() { return <h1>extra</h1>; }\n")
	d.waitBody(t, "/extra/", "extra")
	for _, u := range before {
		if code, _ := getText(t, d.base+u); code != 200 {
			t.Errorf("%s answers %d after a full rebuild", u, code)
		}
	}
}
