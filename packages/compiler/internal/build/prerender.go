package build

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

const emptyPageRegionMarker = "<!--suspense:page--><!--/suspense:page-->"

// hasPrerenderPages reports whether any built page result is queued for
// build-time prerendering through the staged SSR driver.
func hasPrerenderPages(results []*PageResult) bool {
	for _, r := range results {
		if r != nil && r.Prerender {
			return true
		}
	}
	return false
}

// prerenderFrame is the NDJSON frame returned by /__krate/regions for a
// build-time prerender request.
type prerenderFrame struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	Status       int    `json:"status"`
	HTML         string `json:"html"`
	Title        string `json:"title"`
	Head         string `json:"head"`
	Error        string `json:"error"`
	NotFound     bool   `json:"notFound"`
	Denied       int    `json:"denied"`
	Redirect     string `json:"redirect"`
	NoStore      bool   `json:"noStore"`
	DynamicUsage string `json:"dynamicUsage"`
	ErrorCaught  bool   `json:"errorCaught"`
}

// prerenderAsyncPages renders every PageResult marked Prerender through one
// temporary instance of the staged SSR driver, baking the rendered HTML and
// head tags (from generateMetadata / metadata / <Head>) into the PageResult.
// Pages that succeed without reading request-only APIs become RenderSSG (or
// keep RenderISR with their initial HTML seeded when PrerenderSeedISR is set).
// Pages that read request-only APIs (headers(), cookies(), searchParams,
// no-store, forbidden/unauthorized) fall back to request-time rendering with a
// note, or fail with KR7103 when PrerenderRequired is set (output: "static").
func (b *Builder) prerenderAsyncPages(results []*PageResult, serverBundles map[string]string) []error {
	var targets []*PageResult
	for _, r := range results {
		if r != nil && r.Prerender {
			targets = append(targets, r)
		}
	}
	if len(targets) == 0 {
		return nil
	}

	var errs []error
	var ready []*PageResult
	manifestPages := make([]ManifestPage, 0, len(targets))
	seenRoutes := make(map[string]bool, len(targets))
	for _, r := range targets {
		bundlePath := serverBundles[r.SourcePath]
		if bundlePath == "" {
			errs = append(errs, fmt.Errorf("prerender %s (%s): missing server bundle", routeFromOutName(r.OutName), r.SourcePath))
			continue
		}
		ready = append(ready, r)
		route := routeFromOutName(r.OutName)
		if !seenRoutes[route] {
			seenRoutes[route] = true
			manifestPages = append(manifestPages, ManifestPage{
				Route:            route,
				Source:           r.SourcePath,
				Mode:             r.Mode.String(),
				Revalidate:       r.Revalidate,
				BundlePath:       filepath.ToSlash(bundlePath),
				ServerComponents: true,
			})
		}
	}
	if len(ready) == 0 {
		return errs
	}

	staged := stageServerRenderer(b.stderr(), b.Root, b.Cfg.OutDir)
	if staged == "" {
		hasRequired := false
		for _, r := range ready {
			if r.PrerenderRequired {
				hasRequired = true
				break
			}
		}
		if hasRequired {
			return append(errs, fmt.Errorf("cannot prerender async pages with output: \"static\": SSR renderer driver not found under %s (ensure @krate/runtime is installed)", b.Root))
		}
		fmt.Fprintf(b.stderr(), "  %sWarning: skipping build-time prerender of %d async page(s) - SSR renderer source not found under %s (pages will render per request)%s\n", cYellow, len(ready), b.Root, cReset)
		return errs
	}

	// Write a temporary manifest outside dist/ so static builds never leave a
	// server-manifest.json behind in the output directory.
	tmpDir := filepath.Join(b.Root, ".krate")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return append(errs, fmt.Errorf("prerender manifest dir: %w", err))
	}
	tmpManifestPath := filepath.Join(tmpDir, "prerender-manifest-"+randomHex(6)+".json")
	smData, err := json.Marshal(&ServerManifest{Pages: manifestPages})
	if err != nil {
		return append(errs, fmt.Errorf("marshaling prerender manifest: %w", err))
	}
	if err := os.WriteFile(tmpManifestPath, smData, 0644); err != nil {
		return append(errs, fmt.Errorf("writing prerender manifest: %w", err))
	}
	defer func() { _ = os.Remove(tmpManifestPath) }()

	port, err := pickFreeLocalPort()
	if err != nil {
		return append(errs, fmt.Errorf("allocating prerender port: %w", err))
	}

	secret := randomHex(16)
	srv := NewSSRServer(b.Root, port, b.gspRuntime())
	srv.SetOutDir(b.Cfg.OutDir)
	srv.SetManifestPath(tmpManifestPath)
	srv.SetInternalSecret(secret)
	timeoutMs := b.Cfg.SSR.Timeout
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}
	srv.SetTuning(timeoutMs, 0)
	env := make(map[string]string, len(b.Env)+1)
	for k, v := range b.Env {
		env[k] = v
	}
	env["KRATE_PRERENDER"] = "1"
	srv.SetEnv(env)

	if err := srv.Start(); err != nil {
		return append(errs, fmt.Errorf("starting prerender renderer: %w", err))
	}
	defer srv.Shutdown()

	workers := buildWorkerLimit()
	if workers < 4 {
		workers = 4
	}
	if workers > 16 {
		workers = 16
	}
	if len(ready) < workers {
		workers = len(ready)
	}

	httpTimeout := time.Duration(timeoutMs)*time.Millisecond + 10*time.Second
	client := &http.Client{
		Timeout: httpTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        workers,
			MaxIdleConnsPerHost: workers,
			IdleConnTimeout:     30 * time.Second,
		},
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/__krate/regions", port)

	var (
		mu             sync.Mutex
		wg             sync.WaitGroup
		prerenderedSSG atomic.Int32
		seededISR      atomic.Int32
	)
	pool := newWorkerPool(workers)

	for _, r := range ready {
		r := r
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.acquire()
			defer pool.release()

			route := routeFromOutName(r.OutName)
			frame, err := fetchPrerenderFrame(client, endpoint, secret, route, r.Params)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("prerender %s (%s): %w", route, r.SourcePath, err))
				mu.Unlock()
				return
			}

			if reason := prerenderDynamicReason(frame); reason != "" {
				if r.PrerenderRequired {
					d := diag.New(r.Page, 1, 1,
						fmt.Sprintf("async page %s uses %s, which needs request-time rendering, but output is \"static\"", r.SourcePath, reason),
						fmt.Sprintf("remove `output: \"static\"` from the config, or stop calling %s during rendering", reason),
						"").WithCode(diag.CodeServerComponentStaticOutput)
					mu.Lock()
					errs = append(errs, d)
					mu.Unlock()
					return
				}
				printf("  %s*%s %s uses %s during prerender -> %s\n", cCyan, cReset, route, reason, r.Mode.String())
				return
			}

			if frame.Status >= 500 || frame.Error != "" || frame.ErrorCaught {
				msg := frame.Error
				if msg == "" {
					msg = fmt.Sprintf("render failed with status %d", frame.Status)
				}
				mu.Lock()
				errs = append(errs, fmt.Errorf("prerender %s (%s): %s", route, r.SourcePath, msg))
				mu.Unlock()
				return
			}

			if r.PrerenderSeedISR {
				r.HTML = strings.Replace(r.HTML, emptyPageRegionMarker, "<!--suspense:page-->"+frame.HTML+"<!--/suspense:page-->", 1)
				seededISR.Add(1)
			} else {
				r.HTML = strings.Replace(r.HTML, emptyPageRegionMarker, frame.HTML, 1)
				r.Mode = RenderSSG
				r.ServerPage = false
				prerenderedSSG.Add(1)
			}

			if head := prerenderHeadHTML(frame); head != "" {
				r.HeadHTML = dedupeHeadLast(head) + r.HeadHTML
			}
		}()
	}
	wg.Wait()

	if n := prerenderedSSG.Load(); n > 0 {
		printf("  %s*%s Prerendered %d async page(s) -> ssg\n", cCyan, cReset, n)
	}
	if n := seededISR.Load(); n > 0 {
		printf("  %s*%s Prerendered initial HTML for %d ISR page(s)\n", cCyan, cReset, n)
	}
	return errs
}

func prerenderDynamicReason(f prerenderFrame) string {
	switch {
	case f.DynamicUsage != "":
		return f.DynamicUsage
	case f.Denied == 403:
		return "forbidden()"
	case f.Denied == 401:
		return "unauthorized()"
	case f.NotFound:
		return "notFound()"
	case f.Redirect != "":
		return "redirect()"
	case f.NoStore:
		return "uncached (no-store) data"
	default:
		return ""
	}
}

func prerenderHeadHTML(f prerenderFrame) string {
	var sb strings.Builder
	if f.Title != "" {
		sb.WriteString("<title>")
		sb.WriteString(f.Title)
		sb.WriteString("</title>\n")
	}
	if f.Head != "" {
		sb.WriteString(f.Head)
		if !strings.HasSuffix(f.Head, "\n") {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func fetchPrerenderFrame(client *http.Client, endpoint, secret, route string, params map[string]string) (prerenderFrame, error) {
	payload := map[string]any{
		"route":     route,
		"url":       route,
		"method":    "GET",
		"headers":   map[string]string{},
		"params":    params,
		"regions":   []map[string]string{{"id": "page", "kind": "page"}},
		"prerender": true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return prerenderFrame{}, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return prerenderFrame{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set(internalSecretHeader, secret)
	}
	resp, err := client.Do(req)
	if err != nil {
		return prerenderFrame{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return prerenderFrame{}, fmt.Errorf("renderer answered HTTP %d", resp.StatusCode)
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var out prerenderFrame
	var gotFrame bool
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var f prerenderFrame
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			return prerenderFrame{}, fmt.Errorf("invalid renderer frame: %w", err)
		}
		if f.Type == "end" {
			break
		}
		if f.Type == "region" || f.Type == "error" {
			out = f
			gotFrame = true
		}
	}
	if err := sc.Err(); err != nil {
		return prerenderFrame{}, fmt.Errorf("reading renderer response: %w", err)
	}
	if !gotFrame {
		return prerenderFrame{}, fmt.Errorf("renderer returned no page frame")
	}
	return out, nil
}

// pruneUnusedServerArtifacts drops server bundles that were only compiled for
// build-time prerendering and removes the staged SSR renderer from outDir when
// no remaining page requires Node SSR at runtime.
func (b *Builder) pruneUnusedServerArtifacts(results []*PageResult, serverBundles map[string]string) map[string]string {
	neededSources := make(map[string]bool)
	anyNodeRuntime := false
	for _, r := range results {
		if r == nil {
			continue
		}
		if r.Mode != RenderSSG && r.SourcePath != "" && pageNeedsNodeServerBundle(r) {
			neededSources[r.SourcePath] = true
			anyNodeRuntime = true
		}
	}
	if !anyNodeRuntime {
		krateDir := filepath.Join(b.Cfg.OutDir, ".krate")
		_ = os.RemoveAll(filepath.Join(krateDir, "server-bundles"))
		_ = os.Remove(filepath.Join(krateDir, "server-renderer.mjs"))
		_ = os.Remove(filepath.Join(krateDir, "highlight.json"))
		_ = os.Remove(filepath.Join(krateDir, "server-manifest.json"))
		_ = os.Remove(filepath.Join(krateDir, "isr-cache.json"))
		return nil
	}

	active := make(map[string]string, len(serverBundles))
	for src, rel := range serverBundles {
		if neededSources[src] {
			active[src] = rel
			continue
		}
		abs := filepath.Join(b.Cfg.OutDir, filepath.FromSlash(rel))
		_ = os.Remove(abs)
		_ = os.Remove(abs + ".map")
	}
	return active
}

func pickFreeLocalPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
