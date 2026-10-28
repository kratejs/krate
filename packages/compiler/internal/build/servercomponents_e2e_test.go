package build

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scFrame is one NDJSON frame of /__krate/regions, with the fields server
// component renders use.
type scFrame struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Status   int    `json:"status"`
	HTML     string `json:"html"`
	Error    string `json:"error"`
	NotFound bool   `json:"notFound"`
	Redirect string `json:"redirect"`
	Fatal    bool   `json:"fatal"`
}

func postSCRegions(t *testing.T, base string, body map[string]any) []scFrame {
	t.Helper()
	data, _ := json.Marshal(body)
	resp, err := http.Post(base+"/__krate/regions", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("regions request: %v", err)
	}
	defer resp.Body.Close()
	var out []scFrame
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var f scFrame
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			t.Fatalf("bad frame %q: %v", line, err)
		}
		if f.Type != "end" {
			out = append(out, f)
		}
	}
	return out
}

// TestAsyncServerComponentSidecarE2E builds a project with an async page
// (params as a Promise, fetch, cookies(), notFound(), redirect(), a thrown
// error and a nested async child) plus a synchronous page with an async child
// under <Suspense>, boots the real SSR sidecar, and checks every frame.
func TestAsyncServerComponentSidecarE2E(t *testing.T) {
	ensureRuntimeDist(t)
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node not available: %v", err)
	}
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/post/"):
			fmt.Fprintf(w, `{"title":"Title of %s"}`, strings.TrimPrefix(r.URL.Path, "/post/"))
		case strings.HasPrefix(r.URL.Path, "/comments/"):
			fmt.Fprintf(w, `["first on %s","second"]`, strings.TrimPrefix(r.URL.Path, "/comments/"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiSrv.Close()

	b, root := scProject(t, map[string]string{
		"src/api.ts": `export const API = ` + fmt.Sprintf("%q", apiSrv.URL) + `;`,
		"src/Comments.tsx": `import { API } from './api';
export async function Comments({ slug }: any) {
  const list = await fetch(API + "/comments/" + slug).then((r: any) => r.json());
  return <ul>{list.map((c: any) => <li>{c}</li>)}</ul>;
}`,
		"src/pages/post/[slug].tsx": `import { cookies, notFound, redirect } from '@krate/runtime/server';
import { API } from '../../api';
import { Comments } from '../../Comments';
export default async function Post({ params, searchParams }: any) {
  const { slug } = await params;
  const { view } = await searchParams;
  if (slug === "missing") notFound();
  if (slug === "old") redirect("/post/new");
  if (slug === "boom") throw new Error("exploded on " + slug);
  const post = await fetch(API + "/post/" + slug).then((r: any) => r.json());
  const jar = await cookies();
  return <article>
    <h1>{post.title}</h1>
    <p class="sync">{params.slug}</p>
    <p class="view">{view}</p>
    <p class="sid">{jar.get("sid")?.value}</p>
    <Comments slug={slug} />
  </article>;
}`,
		"src/pages/feed.tsx": `import { Comments } from '../Comments';
export default function Feed() {
  return <main><h1>Feed</h1><Suspense fallback={<p>loading</p>}><Comments slug="feed" /></Suspense></main>;
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	outDir := b.Cfg.OutDir
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	base, stop := startSidecar(t, node, repoRoot, root, outDir)
	defer stop()

	page := func(slug string) scFrame {
		t.Helper()
		frames := postSCRegions(t, base, map[string]any{
			"route": "/post/[slug]", "url": "/post/" + slug, "method": "GET",
			"params":  map[string]string{"slug": slug},
			"query":   map[string]string{"view": "full"},
			"headers": map[string]string{"Cookie": "sid=abc123; other=1"},
			"regions": []map[string]string{{"id": "page", "kind": "page"}},
		})
		if len(frames) != 1 {
			t.Fatalf("slug %s: want 1 frame, got %+v", slug, frames)
		}
		return frames[0]
	}

	ok := page("hello")
	if ok.Status != 200 {
		t.Fatalf("async page status %d (%s)", ok.Status, ok.Error)
	}
	for _, want := range []string{
		"<h1>Title of hello</h1>",
		`<p class="sync">hello</p>`,
		`<p class="view">full</p>`,
		`<p class="sid">abc123</p>`,
		"<li>first on hello</li><li>second</li>",
	} {
		if !strings.Contains(ok.HTML, want) {
			t.Errorf("async page HTML missing %q:\n%s", want, ok.HTML)
		}
	}

	if nf := page("missing"); !nf.NotFound || nf.Status != 404 {
		t.Errorf("notFound(): got %+v", nf)
	}
	if rd := page("old"); rd.Redirect != "/post/new" {
		t.Errorf("redirect(): got %+v", rd)
	}
	boom := page("boom")
	if boom.Status != 500 || !boom.Fatal || !strings.Contains(boom.Error, "exploded on boom") || boom.HTML != "" {
		t.Errorf("thrown error must be a fatal 500 frame, got %+v", boom)
	}

	// The synchronous page's async child renders as its own region from the
	// page server bundle.
	m := readManifest(t, root)
	regs := m.Regions["/feed"]
	if len(regs) != 1 || regs[0].ServerExport == "" {
		t.Fatalf("feed regions: %+v", regs)
	}
	frames := postSCRegions(t, base, map[string]any{
		"route": "/feed", "url": "/feed", "method": "GET",
		"regions": []map[string]string{{"id": regs[0].ID, "kind": "component"}},
	})
	if len(frames) != 1 || frames[0].Type != "region" || !strings.Contains(frames[0].HTML, "<li>first on feed</li>") {
		t.Fatalf("feed region frames: %+v", frames)
	}
	if strings.Contains(frames[0].HTML, "loading") {
		t.Errorf("region must render the server component, not the fallback: %s", frames[0].HTML)
	}
}

// startSidecar stages and boots the SSR sidecar for a built test project and
// waits for its health endpoint.
func startSidecar(t *testing.T, node, repoRoot, root, outDir string) (string, func()) {
	t.Helper()
	staged := stageServerRenderer(repoRoot, outDir)
	if staged == "" {
		t.Fatal("stageServerRenderer returned empty path")
	}
	port := freePort(t)
	cmd := exec.Command(node, staged)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("KRATE_SSR_PORT=%d", port),
		fmt.Sprintf("KRATE_MANIFEST=%s", filepath.Join(outDir, "server-manifest.json")),
		fmt.Sprintf("KRATE_ROOT=%s", root),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting renderer: %v", err)
	}
	stop := func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	base := fmt.Sprintf("http://localhost:%d", port)
	if !waitHealth(t, base) {
		stop()
		t.Fatal("renderer did not become ready")
	}
	return base, stop
}
