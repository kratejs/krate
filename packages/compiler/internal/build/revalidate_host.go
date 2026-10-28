package build

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

// internalSecretHeader carries the per-process secret on host <-> sidecar
// internal requests.
const internalSecretHeader = "X-Krate-Internal"

// revalidateEndpoint is the host's internal on-demand revalidation endpoint.
const revalidateEndpoint = "/__krate/internal/revalidate"

// revalidatePathReq is one revalidatePath(path, type) call.
type revalidatePathReq struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

// revalidateRequest is the body of POST /__krate/internal/revalidate.
type revalidateRequest struct {
	Paths []revalidatePathReq `json:"paths"`
	Tags  []string            `json:"tags"`
}

// revalidatedTarget is one applied path or tag.
type revalidatedTarget struct {
	Target string   `json:"target"`
	Kind   string   `json:"kind"`
	Routes []string `json:"routes,omitempty"`
}

// skippedTarget is a path or tag that could not be revalidated, with why.
type skippedTarget struct {
	Target string `json:"target"`
	Reason string `json:"reason"`
}

// revalidateResult is the structured reply of the revalidation endpoint.
type revalidateResult struct {
	Revalidated []revalidatedTarget `json:"revalidated"`
	Skipped     []skippedTarget     `json:"skipped"`
}

// sidecarInvalidateTarget is one route (optionally one params variant) the
// SSR sidecar must drop from its ISR and region caches.
type sidecarInvalidateTarget struct {
	Route  string            `json:"route"`
	Params map[string]string `json:"params,omitempty"`
}

// revalidateHost owns on-demand revalidation for one `krate serve`/`krate dev`
// process. It listens on a loopback-only port for POST
// /__krate/internal/revalidate (authenticated by a random per-process
// secret), resolves paths against the build manifest and fans the
// invalidation out to every cache: the SSR sidecar (all workers), the Go
// region cache, the QuickJS data cache and the Node API sidecar's data cache.
type revalidateHost struct {
	secret string
	outDir string
	// Data backs unstable_cache / fetch caching in embedded QuickJS routes.
	data *jsruntime.DataCache

	listener net.Listener
	url      string
	client   *http.Client

	mu sync.Mutex
	// ssrBase is the SSR sidecar base URL; ssrRunning reports whether the
	// process is alive (a dead renderer holds no cache).
	ssrBase    string
	ssrRunning func() bool
	goAPI      *goAPISupervisor
	nodeAPI    string // base URL of the Node API/action sidecar, "" when not running

	manifestMu   sync.Mutex
	manifestTime time.Time
	pages        []PageMeta

	warnMu sync.Mutex
	warned map[string]bool
}

// newRevalidateHost creates the host with a fresh random secret.
func newRevalidateHost(outDir string) (*revalidateHost, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("generating internal secret: %w", err)
	}
	return &revalidateHost{
		secret: hex.EncodeToString(buf),
		outDir: outDir,
		data:   jsruntime.NewDataCache(0),
		client: &http.Client{Timeout: 30 * time.Second},
		warned: map[string]bool{},
	}, nil
}

// Start listens on 127.0.0.1 (random port) and serves the internal endpoint.
func (h *revalidateHost) Start() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("internal revalidation listener: %w", err)
	}
	h.listener = ln
	h.url = "http://" + ln.Addr().String()
	srv := &http.Server{Handler: h.handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return nil
}

// Close stops the internal listener.
func (h *revalidateHost) Close() {
	if h.listener != nil {
		_ = h.listener.Close()
	}
}

// Env returns the variables every server-side runtime needs to reach the host.
func (h *revalidateHost) Env() map[string]string {
	return map[string]string{
		"KRATE_HOST_URL":        h.url,
		"KRATE_INTERNAL_SECRET": h.secret,
	}
}

// EnvList is Env as KEY=VALUE pairs.
func (h *revalidateHost) EnvList() []string {
	return []string{"KRATE_HOST_URL=" + h.url, "KRATE_INTERNAL_SECRET=" + h.secret}
}

// Authorized reports whether r carries the internal secret (constant-time).
func (h *revalidateHost) Authorized(r *http.Request) bool {
	got := r.Header.Get(internalSecretHeader)
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(h.secret)) == 1
}

// SetSSR registers the running SSR sidecar.
func (h *revalidateHost) SetSSR(s *SSRServer, port int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ssrBase = fmt.Sprintf("http://127.0.0.1:%d", port)
	h.ssrRunning = s.IsRunning
}

// SetGoAPI registers the Go API sidecar (owner of the Go region cache).
func (h *revalidateHost) SetGoAPI(g *goAPISupervisor) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.goAPI = g
}

// SetNodeAPI registers the Node API/action sidecar base URL.
func (h *revalidateHost) SetNodeAPI(base string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nodeAPI = base
}

// CacheHooks returns the QuickJS bridge (synchronous revalidation + data cache).
func (h *revalidateHost) CacheHooks() *jsruntime.CacheHooks {
	return &jsruntime.CacheHooks{Revalidate: h.ApplyJSON, Data: h.data}
}

func (h *revalidateHost) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(revalidateEndpoint, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = io.WriteString(w, `{"error":"method not allowed"}`)
			return
		}
		if !h.Authorized(r) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":"forbidden"}`)
			return
		}
		var req revalidateRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid revalidation body: " + err.Error()})
			return
		}
		res, err := h.Apply(req)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "revalidated": res.Revalidated, "skipped": res.Skipped})
			return
		}
		_ = json.NewEncoder(w).Encode(res)
	})
	return mux
}

// ApplyJSON is Apply over JSON, for the QuickJS bridge.
func (h *revalidateHost) ApplyJSON(payload string) string {
	var req revalidateRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		b, _ := json.Marshal(map[string]string{"error": "invalid revalidation payload: " + err.Error()})
		return string(b)
	}
	res, err := h.Apply(req)
	if err != nil {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(b)
	}
	b, _ := json.Marshal(res)
	return string(b)
}

// Apply resolves and applies a revalidation. Every cache is invalidated before
// it returns, so a caller that waits for it gets read-your-writes. Paths that
// name static (SSG) pages or no page are reported under Skipped (with a
// one-time warning per route); a failure to reach a cache is an error.
func (h *revalidateHost) Apply(req revalidateRequest) (revalidateResult, error) {
	res := revalidateResult{Revalidated: []revalidatedTarget{}, Skipped: []skippedTarget{}}
	pages := h.manifestPages()

	var targets []sidecarInvalidateTarget
	var routes []string
	for _, p := range req.Paths {
		ts, rs, skipped, err := resolveRevalidatePath(pages, p)
		if err != nil {
			res.Skipped = append(res.Skipped, skippedTarget{Target: p.Path, Reason: err.Error()})
			continue
		}
		for _, sk := range skipped {
			h.warnSkipped(sk)
			res.Skipped = append(res.Skipped, sk)
		}
		if len(ts) > 0 {
			targets = append(targets, ts...)
			routes = append(routes, rs...)
			res.Revalidated = append(res.Revalidated, revalidatedTarget{Target: p.Path, Kind: "path", Routes: rs})
		}
	}
	var tags []string
	for _, t := range req.Tags {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
			res.Revalidated = append(res.Revalidated, revalidatedTarget{Target: t, Kind: "tag"})
		}
	}
	if len(targets) == 0 && len(tags) == 0 {
		return res, nil
	}

	h.mu.Lock()
	ssrBase, ssrRunning, goAPI, nodeAPI := h.ssrBase, h.ssrRunning, h.goAPI, h.nodeAPI
	h.mu.Unlock()

	if len(tags) > 0 {
		h.data.InvalidateTags(tags)
	}
	if goAPI != nil {
		goAPI.InvalidateRoutes(routes)
	}
	var errs []error
	if ssrBase != "" && (ssrRunning == nil || ssrRunning()) {
		body, _ := json.Marshal(map[string]any{"targets": targets, "tags": tags})
		if err := h.post(ssrBase+"/__krate/ssr/cache-invalidate", body); err != nil {
			errs = append(errs, fmt.Errorf("SSR renderer: %w", err))
		}
	}
	if nodeAPI != "" && len(tags) > 0 {
		body, _ := json.Marshal(map[string]any{"tags": tags})
		if err := h.post(nodeAPI+"/__krate/internal/cache-invalidate", body); err != nil && !isConnRefused(err) {
			// A sidecar that is not listening holds no cache; anything else
			// means its data cache may still serve the old entries.
			errs = append(errs, fmt.Errorf("Node API sidecar: %w", err))
		}
	}
	return res, errors.Join(errs...)
}

func (h *revalidateHost) post(url string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(internalSecretHeader, h.secret)
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func isConnRefused(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

// warnSkipped logs a skipped revalidation once per target route.
func (h *revalidateHost) warnSkipped(s skippedTarget) {
	h.warnMu.Lock()
	seen := h.warned[s.Target]
	h.warned[s.Target] = true
	h.warnMu.Unlock()
	if !seen {
		fmt.Fprintf(os.Stderr, "  %s⚠ revalidatePath(%s) skipped:%s %s\n", cYellow, s.Target, cReset, s.Reason)
	}
}

// manifestPages returns every page of the build (dist/manifest.json, all
// modes), reloaded when a dev rebuild rewrites the manifest.
func (h *revalidateHost) manifestPages() []PageMeta {
	file := filepath.Join(h.outDir, "manifest.json")
	fi, err := os.Stat(file)
	h.manifestMu.Lock()
	defer h.manifestMu.Unlock()
	if err != nil {
		return h.pages
	}
	if fi.ModTime().Equal(h.manifestTime) && h.pages != nil {
		return h.pages
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return h.pages
	}
	var m Manifest
	if json.Unmarshal(data, &m) != nil {
		return h.pages
	}
	h.pages = m.Pages
	h.manifestTime = fi.ModTime()
	return h.pages
}

// normalizeRevalidatePath strips query/hash and trailing slashes.
func normalizeRevalidatePath(p string) (string, error) {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("path must start with \"/\"")
	}
	clean := path.Clean(p)
	if clean != "/" {
		clean = strings.TrimRight(clean, "/")
	}
	return clean, nil
}

func isPatternRoute(route string) bool { return strings.Contains(route, "[") }

func requestTimeMode(m RenderMode) bool {
	return m == RenderSSR || m == RenderISR || m == RenderStreaming
}

// staticSkip is the skipped entry for an SSG route.
func staticSkip(route string) skippedTarget {
	return skippedTarget{Target: route, Reason: "route " + route + " is static (SSG): its HTML was baked at build time and cannot be revalidated at runtime; make the page ISR (export const revalidate = N) or SSR (export const dynamic = \"force-dynamic\"), or rebuild"}
}

// resolveRevalidatePath maps one revalidatePath call to sidecar targets.
//
//	type "page": an exact route (static route or a pattern such as
//	  /blog/[slug], which targets every cached variant) or a concrete URL
//	  matched against the dynamic patterns (targets that params variant).
//	type "layout": every page whose route is at or below the path.
//
// It returns the targets, the page routes they cover, and skipped entries for
// static (SSG) routes. An error means nothing matched.
func resolveRevalidatePath(pages []PageMeta, req revalidatePathReq) ([]sidecarInvalidateTarget, []string, []skippedTarget, error) {
	p, err := normalizeRevalidatePath(req.Path)
	if err != nil {
		return nil, nil, nil, err
	}
	typ := req.Type
	if typ == "" {
		typ = "page"
	}
	if typ != "page" && typ != "layout" {
		return nil, nil, nil, fmt.Errorf("unknown revalidatePath type %q (want \"page\" or \"layout\")", req.Type)
	}

	var targets []sidecarInvalidateTarget
	var routes []string
	var skipped []skippedTarget
	add := func(pm PageMeta, params map[string]string) {
		if !requestTimeMode(pm.Mode) {
			skipped = append(skipped, staticSkip(pm.Route))
			return
		}
		targets = append(targets, sidecarInvalidateTarget{Route: pm.Route, Params: params})
		routes = append(routes, pm.Route)
	}

	if typ == "layout" {
		sorted := append([]PageMeta(nil), pages...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Route < sorted[j].Route })
		for _, pm := range sorted {
			if routeUnder(pm.Route, p) {
				add(pm, nil)
			}
		}
		if len(targets) == 0 && len(skipped) == 0 {
			return nil, nil, nil, fmt.Errorf("no page at or below %s", p)
		}
		return targets, routes, skipped, nil
	}

	// Exact route first (a static route, or the pattern itself).
	for _, pm := range pages {
		if pm.Route == p {
			add(pm, nil)
			return targets, routes, skipped, nil
		}
	}
	// A concrete URL of a dynamic route: static routes never reach here, so
	// the first matching pattern (fewest dynamic segments first) wins.
	var dyn []PageMeta
	for _, pm := range pages {
		if isPatternRoute(pm.Route) {
			dyn = append(dyn, pm)
		}
	}
	sort.SliceStable(dyn, func(i, j int) bool {
		return strings.Count(dyn[i].Route, "[") < strings.Count(dyn[j].Route, "[")
	})
	for _, pm := range dyn {
		if params, ok := matchRoute(p, pm.Route); ok {
			if !requestTimeMode(pm.Mode) {
				skipped = append(skipped, skippedTarget{Target: p, Reason: staticSkip(pm.Route).Reason})
				return nil, nil, skipped, nil
			}
			add(pm, params)
			return targets, routes, skipped, nil
		}
	}
	return nil, nil, nil, fmt.Errorf("no page matches %s", p)
}

// routeUnder reports whether route is prefix itself or below it, segment by
// segment; a concrete prefix segment matches a dynamic route segment.
func routeUnder(route, prefix string) bool {
	if prefix == "/" {
		return true
	}
	rs := strings.Split(strings.Trim(route, "/"), "/")
	ps := strings.Split(strings.Trim(prefix, "/"), "/")
	if len(rs) < len(ps) {
		return false
	}
	for i, seg := range ps {
		if rs[i] == seg {
			continue
		}
		if strings.HasPrefix(rs[i], "[") && strings.HasSuffix(rs[i], "]") {
			continue
		}
		return false
	}
	return true
}

// ssrInternalProxy forwards /__krate/ssr/* from the public listener to the
// SSR sidecar. Only the health check is public: the mutating endpoints
// (revalidate, refresh, invalidate, cache-invalidate) purge or re-render
// caches, so anything else requires the internal secret.
func ssrInternalProxy(host *revalidateHost, ssrPort int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/__krate/ssr/health" && !host.Authorized(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":"forbidden: internal SSR endpoint"}`)
			return
		}
		ssrURL := fmt.Sprintf("http://localhost:%d%s", ssrPort, r.URL.Path)
		proxyReq, _ := http.NewRequest(r.Method, ssrURL, r.Body)
		proxyReq.Header = r.Header
		resp, err := sidecarClient.Do(proxyReq)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"error":"SSR renderer unavailable"}`)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}
}
