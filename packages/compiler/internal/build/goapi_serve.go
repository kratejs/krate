package build

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/execx"
	"github.com/kratejs/krate/packages/compiler/internal/safe"
)

// goMarkerRe matches the placeholder an SSR page bundle emits for a Go component
// it could not inline (a runtime-tier component, or a server-tier component
// whose call site was not annotated): <!--krate-go:Name:revalidate:BASE64(props)-->.
var goMarkerRe = regexp.MustCompile(`<!--krate-go:([^:>]+):(\d+):([^>]*)-->`)

// resolveGoMarkers replaces every Go-component placeholder in html by rendering
// it through the Go host (renderGo). reqCtx is the JSON request context
// forwarded to components that take a krate.Request; each marker carries its own
// revalidate window. Non-matching html is returned unchanged.
func resolveGoMarkers(html, reqCtx string, renderGo func([]goRenderRegion) (map[string]string, error)) string {
	out, _ := resolveGoMarkersE(html, reqCtx, renderGo)
	return out
}

func resolveGoMarkersE(html, reqCtx string, renderGo func([]goRenderRegion) (map[string]string, error)) (string, error) {
	ms := goMarkerRe.FindAllStringSubmatchIndex(html, -1)
	if len(ms) == 0 {
		return html, nil
	}
	reqs := make([]goRenderRegion, 0, len(ms))
	for i, m := range ms {
		name := html[m[2]:m[3]]
		revalidate, _ := strconv.Atoi(html[m[4]:m[5]])
		b64 := html[m[6]:m[7]]
		props, err := base64.RawURLEncoding.DecodeString(b64)
		if err != nil {
			continue
		}
		reqs = append(reqs, goRenderRegion{
			ID:         fmt.Sprintf("gomarker-%d", i),
			Name:       name,
			Props:      string(props),
			Request:    reqCtx,
			Revalidate: revalidate,
		})
	}
	if len(reqs) == 0 {
		return html, nil
	}
	rendered, err := renderGo(reqs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %sGo component render failed:%s %v\n", cYellow, cReset, err)
		return "", err
	}
	var b strings.Builder
	last := 0
	for i, m := range ms {
		b.WriteString(html[last:m[0]])
		if h, ok := rendered[fmt.Sprintf("gomarker-%d", i)]; ok {
			b.WriteString(h)
		}
		last = m[1]
	}
	b.WriteString(html[last:])
	return b.String(), nil
}

// goRenderRegion is one runtime Go component to render at request time.
type goRenderRegion struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Props      string `json:"props"`
	Request    string `json:"request,omitempty"`
	Revalidate int    `json:"revalidate,omitempty"`
	// Route is the page route the region belongs to (cache index only).
	Route string `json:"-"`
}

// regionCacheEntry is a cached Go-region render with its expiry (PPR/ISR
// parity with Node regions).
type regionCacheEntry struct {
	html  string
	exp   time.Time
	route string
	// hits and seq drive eviction: an entry nobody asked for twice goes first,
	// oldest first, so a client minting variants cannot flush the hot ones.
	hits uint32
	seq  uint64
}

// goRegionCacheMax bounds the Go region cache so a long-lived server cannot
// grow it without limit across many dynamic variants.
const goRegionCacheMax = 2048

// regionRoute returns the page route a region render belongs to: its Route,
// else the "url" of its request context (streamRegionPage sets it to the
// canonical page route).
func regionRoute(r goRenderRegion) string {
	if r.Route != "" || r.Request == "" {
		return r.Route
	}
	var ctx struct {
		URL string `json:"url"`
	}
	if json.Unmarshal([]byte(r.Request), &ctx) == nil {
		return ctx.URL
	}
	return ""
}

// regionVary is what renders of one Go component have read of the request,
// learned from the "vary" of its frames (the Go host records the reads of a
// krate.Request, like the Vary header of an HTTP response). A render is cached
// per the values of exactly these names, so a cookie the component never reads
// cannot split its entries, and one it reads always does.
type regionVary struct {
	// full: the component reads the request in a way that cannot be recorded
	// (or the host predates recording): the entry is keyed on the whole request.
	full                            bool
	headers, cookies, query, params map[string]bool
}

// goVaryFrame is the "vary" member of a gorender frame.
type goVaryFrame struct {
	Full    bool     `json:"full"`
	Headers []string `json:"headers"`
	Cookies []string `json:"cookies"`
	Query   []string `json:"query"`
	Params  []string `json:"params"`
}

func (v *regionVary) merge(f *goVaryFrame) {
	if f == nil {
		v.full = true // no record: assume it read everything
		return
	}
	if f.Full {
		v.full = true
	}
	add := func(dst *map[string]bool, names []string) {
		for _, n := range names {
			if *dst == nil {
				*dst = map[string]bool{}
			}
			(*dst)[n] = true
		}
	}
	add(&v.headers, f.Headers)
	add(&v.cookies, f.Cookies)
	add(&v.query, f.Query)
	add(&v.params, f.Params)
}

// regionKey identifies a Go region render by everything the component could
// have read: name + props + a digest of the whole request context (method, url,
// params, query and every header). It is the key of a component whose reads are
// not recorded. Raw cookies and Authorization values never sit in the cache
// map, only their digest.
func regionKey(r goRenderRegion) string {
	return r.Name + "\x00" + r.Props + "\x00" + requestDigest(r.Request)
}

func requestDigest(request string) string {
	if request == "" {
		return ""
	}
	var ctx any
	if err := json.Unmarshal([]byte(request), &ctx); err != nil {
		sum := sha256.Sum256([]byte(request))
		return hex.EncodeToString(sum[:])
	}
	canon, _ := json.Marshal(ctx) // map keys are sorted: the form is canonical
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:])
}

// requestParts is the request context a region render is given.
type requestParts struct {
	Params  map[string]string `json:"params"`
	Query   map[string]string `json:"query"`
	Headers map[string]string `json:"headers"`
}

func (p requestParts) header(name string) (string, bool) {
	for k, v := range p.Headers {
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return "", false
}

func (p requestParts) cookie(name string) (string, bool) {
	raw, ok := p.header("Cookie")
	if !ok {
		return "", false
	}
	probe := http.Request{Header: http.Header{"Cookie": []string{raw}}}
	c, err := probe.Cookie(name)
	if err != nil {
		return "", false
	}
	return c.Value, true
}

// trackedRegionKey is the key of a component whose reads are recorded: name +
// props + a digest of the values of the names it read (absent is not the same
// as empty). Equal keys mean equal inputs, so the render is the same.
func trackedRegionKey(r goRenderRegion, v *regionVary) string {
	var parts requestParts
	if r.Request != "" {
		if err := json.Unmarshal([]byte(r.Request), &parts); err != nil {
			return regionKey(r) // unreadable context: key on all of it
		}
	}
	pick := func(names map[string]bool, get func(string) (string, bool)) map[string]*string {
		out := make(map[string]*string, len(names))
		for n := range names {
			if val, ok := get(n); ok {
				v := val
				out[n] = &v
			} else {
				out[n] = nil
			}
		}
		return out
	}
	lookup := func(m map[string]string) func(string) (string, bool) {
		return func(n string) (string, bool) { val, ok := m[n]; return val, ok }
	}
	canon, _ := json.Marshal(map[string]any{
		"h": pick(lowerNames(v.headers), parts.header),
		"c": pick(v.cookies, parts.cookie),
		"q": pick(v.query, lookup(parts.Query)),
		"p": pick(v.params, lookup(parts.Params)),
	})
	sum := sha256.Sum256(canon)
	return r.Name + "\x00" + r.Props + "\x00v\x00" + hex.EncodeToString(sum[:])
}

func lowerNames(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k := range in {
		out[strings.ToLower(k)] = true
	}
	return out
}

// regionCacheKeyLocked returns the cache key of a render, or false when no
// render of the component has shown what it reads yet (it must render).
func (g *goAPISupervisor) regionCacheKeyLocked(r goRenderRegion) (string, bool) {
	v := g.vary[r.Name]
	if v == nil {
		return "", false
	}
	if v.full {
		return regionKey(r), true
	}
	return trackedRegionKey(r, v), true
}

// goAPISupervisor manages the compiled Go API sidecar subprocess. It starts
// the binary, proxies matching /api requests to it, and restarts it whenever
// the route manifest changes (i.e. a rebuild produced new Go API routes).
type goAPISupervisor struct {
	binPath      string
	manifestPath string
	port         int
	// socket, when set, is an AF_UNIX socket the sidecar listens on; the proxy
	// dials it instead of the loopback TCP port.
	socket string
	env    []string
	// readyTimeout bounds how long Start waits for the host to listen.
	readyTimeout time.Duration

	proxy     *httputil.ReverseProxy
	stopCh    chan struct{}
	closeOnce sync.Once

	// client + renderURL reach the host's /__krate/gorender endpoint for runtime
	// Go-component regions.
	client    *http.Client
	renderURL string

	mu      sync.Mutex
	cmd     *exec.Cmd
	routes  []goAPIRouteEntry
	lastMod time.Time
	// regionCache memoizes Go region renders that declare a revalidate window.
	regionCache map[string]regionCacheEntry
	// vary is what each component has been seen to read of the request.
	vary map[string]*regionVary
	// seq orders cache uses for eviction.
	seq uint64
	// invalidations counts InvalidateRoutes calls (render/invalidate race guard).
	invalidations uint64
}

// newGoAPISupervisor creates a supervisor for the given sidecar binary and
// route manifest.
func newGoAPISupervisor(binPath, manifestPath string, port int) *goAPISupervisor {
	target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		fmt.Fprintf(os.Stderr, "  %sGo API proxy error%s %s %s: %v\n", cRed, cReset, r.Method, r.URL.Path, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":"Bad Gateway"}`)
	}
	return &goAPISupervisor{
		binPath:      binPath,
		manifestPath: manifestPath,
		port:         port,
		proxy:        proxy,
		stopCh:       make(chan struct{}),
		client:       &http.Client{Timeout: 30 * time.Second},
		renderURL:    fmt.Sprintf("http://127.0.0.1:%d/__krate/gorender", port),
		regionCache:  map[string]regionCacheEntry{},
		readyTimeout: 10 * time.Second,
	}
}

// SetSocket makes the supervisor talk to the sidecar over an AF_UNIX socket at
// path (passed to the child as KRATE_GOAPI_SOCKET) instead of loopback TCP.
func (g *goAPISupervisor) SetSocket(path string) {
	if path == "" {
		return
	}
	g.socket = path
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 128,
		IdleConnTimeout:     90 * time.Second,
	}
	g.proxy.Transport = transport
	g.client.Transport = transport
	g.renderURL = "http://krate/__krate/gorender"
}

// Render renders runtime Go-component regions via the host's /__krate/gorender
// endpoint, returning marker id -> HTML.
func (g *goAPISupervisor) Render(regions []goRenderRegion) (map[string]string, error) {
	return g.RenderContext(context.Background(), regions)
}

// RenderContext is Render bound to ctx: when the visitor's request goes away
// the call to the Go host is cancelled instead of running to the client
// timeout.
func (g *goAPISupervisor) RenderContext(ctx context.Context, regions []goRenderRegion) (map[string]string, error) {
	if len(regions) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(regions))
	// Serve fresh cached regions; render the rest.
	var miss []goRenderRegion
	now := time.Now()
	g.mu.Lock()
	startGen := g.invalidations
	for _, r := range regions {
		if r.Revalidate > 0 {
			if key, ok := g.regionCacheKeyLocked(r); ok {
				if e, ok := g.regionCache[key]; ok && now.Before(e.exp) {
					g.seq++
					e.hits++
					e.seq = g.seq
					g.regionCache[key] = e
					out[r.ID] = e.html
					continue
				}
			}
		}
		miss = append(miss, r)
	}
	g.mu.Unlock()
	if len(miss) == 0 {
		return out, nil
	}
	body, err := json.Marshal(map[string]any{"regions": miss})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", g.renderURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	firstErr := ""
	nameOf := make(map[string]string, len(miss))
	for _, r := range miss {
		nameOf[r.ID] = r.Name
	}
	varyOf := map[string]*goVaryFrame{}
	for {
		var frame struct {
			Type  string       `json:"type"`
			ID    string       `json:"id"`
			HTML  string       `json:"html"`
			Error string       `json:"error"`
			Vary  *goVaryFrame `json:"vary"`
		}
		if err := dec.Decode(&frame); err != nil {
			break
		}
		if frame.ID != "" && frame.Error == "" {
			varyOf[frame.ID] = frame.Vary
		}
		if frame.Error != "" && firstErr == "" {
			firstErr = frame.Error
		}
		if frame.ID != "" && frame.Error == "" {
			out[frame.ID] = frame.HTML
		}
	}
	if firstErr != "" {
		return out, fmt.Errorf("go region render failed: %s", firstErr)
	}
	// Cache renders that declare a revalidate window, unless a revalidation
	// ran while they rendered (their HTML may predate it).
	g.mu.Lock()
	// What the renders read of the request is learned first: the entries below
	// are keyed on it.
	if g.vary == nil {
		g.vary = map[string]*regionVary{}
	}
	for id, vf := range varyOf {
		name := nameOf[id]
		if name == "" {
			continue
		}
		v := g.vary[name]
		if v == nil {
			v = &regionVary{}
			g.vary[name] = v
		}
		v.merge(vf)
	}
	if g.invalidations == startGen {
		for _, r := range miss {
			if r.Revalidate <= 0 {
				continue
			}
			if html, ok := out[r.ID]; ok {
				key, known := g.regionCacheKeyLocked(r)
				if !known {
					continue
				}
				g.regionCacheSetLocked(key, regionCacheEntry{
					html:  html,
					exp:   time.Now().Add(time.Duration(r.Revalidate) * time.Second),
					route: regionRoute(r),
				})
			}
		}
	}
	g.mu.Unlock()
	return out, nil
}

// regionCacheSetLocked stores an entry. When the cache is full, expired
// entries go first, then the oldest entry nobody asked for twice (so a client
// minting request variants cannot flush the entries that are being hit), then
// the oldest. The caller holds g.mu.
func (g *goAPISupervisor) regionCacheSetLocked(key string, e regionCacheEntry) {
	g.seq++
	e.seq = g.seq
	if old, ok := g.regionCache[key]; ok {
		e.hits = old.hits
	} else if len(g.regionCache) >= goRegionCacheMax {
		now := time.Now()
		for k, v := range g.regionCache {
			if !now.Before(v.exp) {
				delete(g.regionCache, k)
			}
		}
		for len(g.regionCache) >= goRegionCacheMax {
			victim, once := "", false
			var oldest uint64
			for k, v := range g.regionCache {
				cold := v.hits == 0
				switch {
				case victim == "":
				case cold && !once:
				case cold == once && v.seq < oldest:
				default:
					continue
				}
				victim, once, oldest = k, cold, v.seq
			}
			delete(g.regionCache, victim)
		}
	}
	g.regionCache[key] = e
}

// InvalidateRoutes drops cached Go region renders of the given page routes
// and returns how many were removed. An empty list only bumps the
// invalidation generation (in-flight renders are not cached).
func (g *goAPISupervisor) InvalidateRoutes(routes []string) int {
	want := make(map[string]bool, len(routes))
	for _, r := range routes {
		want[r] = true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.invalidations++
	n := 0
	for k, e := range g.regionCache {
		if want[e.route] {
			delete(g.regionCache, k)
			n++
		}
	}
	return n
}

// SetEnv provides environment variables for the sidecar process.
func (g *goAPISupervisor) SetEnv(env []string) {
	g.env = env
}

// Start loads the route manifest, spawns the sidecar, and begins watching the
// manifest for changes (dev rebuilds restart the process).
func (g *goAPISupervisor) Start() error {
	routes, modTime, err := loadGoAPIManifest(g.manifestPath)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.routes = routes
	g.lastMod = modTime
	g.mu.Unlock()

	if err := g.spawn(); err != nil {
		return err
	}
	go g.watch()
	return nil
}

// Active reports whether the sidecar process is currently running.
func (g *goAPISupervisor) Active() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cmd != nil && g.cmd.Process != nil
}

// RouteMatches reports whether the Go sidecar handles the given method+path.
func (g *goAPISupervisor) RouteMatches(method, path string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return goAPIRouteMatches(method, path, g.routes)
}

// AllowedMethods reports the methods the Go routes define for a path, and
// whether any Go route matches the path at all.
func (g *goAPISupervisor) AllowedMethods(path string) ([]string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return goAPIAllowedMethods(path, g.routes)
}

// Proxy forwards a request to the Go sidecar.
func (g *goAPISupervisor) Proxy(w http.ResponseWriter, r *http.Request) {
	g.proxy.ServeHTTP(w, r)
}

// Close stops the watcher goroutine and kills the sidecar process.
func (g *goAPISupervisor) Close() {
	g.closeOnce.Do(func() { close(g.stopCh) })
	g.stop()
}

func (g *goAPISupervisor) spawn() error {
	g.stop()

	cmd := exec.Command(g.binPath)
	execx.ConfigureTree(cmd)
	cmd.Env = append(os.Environ(), g.env...)
	if g.socket != "" {
		cmd.Env = append(cmd.Env, "KRATE_GOAPI_SOCKET="+g.socket)
	}
	cmd.Env = append(cmd.Env, fmt.Sprintf("KRATE_GOAPI_PORT=%d", g.port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting Go API sidecar: %w", err)
	}
	g.mu.Lock()
	g.cmd = cmd
	g.mu.Unlock()
	go func(c *exec.Cmd) {
		err := c.Wait()
		g.mu.Lock()
		current := g.cmd == c
		if current {
			g.cmd = nil
			if err != nil {
				// Stop routing to a dead sidecar. The built-in handlers can
				// answer instead, and the manifest watcher restores Go routes
				// after a rebuild.
				g.routes = nil
			}
		}
		g.mu.Unlock()
		if current && err != nil {
			fmt.Fprintf(os.Stderr, "  %sGo API sidecar exited unexpectedly:%s %v\n", cRed, cReset, err)
			reportServerCrash("Go API sidecar", fmt.Sprintf("exited unexpectedly: %v", err), "")
		}
	}(cmd)
	// Wait for the host to accept connections so the first proxied request does
	// not race its listen(). A host that never comes up is a failed start, not
	// a silent 10s stall followed by 502s on every Go route.
	if !g.waitReady(g.readyTimeout) {
		g.stop()
		g.mu.Lock()
		g.routes = nil
		g.mu.Unlock()
		where := fmt.Sprintf("127.0.0.1:%d", g.port)
		if g.socket != "" {
			where = g.socket
		}
		return fmt.Errorf("Go API host did not accept connections on %s within %s (check its output above)", where, g.readyTimeout)
	}
	clearServerCrash("Go API sidecar")
	return nil
}

// waitReady polls the sidecar socket/port until it accepts a connection. It
// returns false when the timeout elapses or the process exits first.
func (g *goAPISupervisor) waitReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		g.mu.Lock()
		running := g.cmd != nil
		g.mu.Unlock()
		if !running {
			return false
		}
		var conn net.Conn
		var err error
		if g.socket != "" {
			if _, serr := os.Stat(g.socket); serr != nil {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			var d net.Dialer
			conn, err = d.Dial("unix", g.socket)
		} else {
			conn, err = net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", g.port), 200*time.Millisecond)
		}
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func (g *goAPISupervisor) stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cmd != nil && g.cmd.Process != nil {
		execx.KillTree(g.cmd)
		g.cmd = nil
	}
	// The killed child cannot unlink its own socket; remove it here so the next
	// spawn can bind cleanly.
	if g.socket != "" {
		_ = os.Remove(g.socket)
	}
}

// watch polls the route manifest and restarts the sidecar when it changes.
func (g *goAPISupervisor) watch() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-g.stopCh:
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						_ = safe.Recover(r) // print + keep watching
					}
				}()
				routes, modTime, err := loadGoAPIManifest(g.manifestPath)
				if err != nil {
					// Manifest gone -> no Go routes anymore -> stop the sidecar.
					if os.IsNotExist(err) {
						g.mu.Lock()
						g.routes = nil
						g.mu.Unlock()
						g.stop()
					}
					return
				}
				g.mu.Lock()
				changed := !modTime.Equal(g.lastMod)
				if changed {
					g.lastMod = modTime
					g.routes = routes
				}
				g.mu.Unlock()
				if changed {
					if err := g.spawn(); err != nil {
						fmt.Fprintf(os.Stderr, "  %s! Go API sidecar restart failed:%s %v\n", cYellow, cReset, err)
						return
					}
					printf("  %s*%s Go API sidecar restarted (%d routes)\n", cCyan, cReset, len(routes))
				}
			}()
		}
	}
}
