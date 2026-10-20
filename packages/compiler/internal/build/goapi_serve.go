package build

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	ms := goMarkerRe.FindAllStringSubmatchIndex(html, -1)
	if len(ms) == 0 {
		return html
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
		return html
	}
	rendered, err := renderGo(reqs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %sGo component render failed:%s %v\n", cYellow, cReset, err)
		return ""
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
	return b.String()
}

// goRenderRegion is one runtime Go component to render at request time.
type goRenderRegion struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Props      string `json:"props"`
	Request    string `json:"request,omitempty"`
	Revalidate int    `json:"revalidate,omitempty"`
}

// regionCacheEntry is a cached Go-region render with its expiry (PPR/ISR
// parity with Node regions).
type regionCacheEntry struct {
	html string
	exp  time.Time
}

// regionKey uniquely identifies a Go region render (name + props + request
// context, so per-URL request reads are not cached across URLs).
func regionKey(r goRenderRegion) string {
	return r.Name + "\x00" + r.Props + "\x00" + r.Request
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
}

// newGoAPISupervisor creates a supervisor for the given sidecar binary and
// route manifest.
func newGoAPISupervisor(binPath, manifestPath string, port int) *goAPISupervisor {
	target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	return &goAPISupervisor{
		binPath:      binPath,
		manifestPath: manifestPath,
		port:         port,
		proxy:        httputil.NewSingleHostReverseProxy(target),
		stopCh:       make(chan struct{}),
		client:       &http.Client{Timeout: 30 * time.Second},
		renderURL:    fmt.Sprintf("http://127.0.0.1:%d/__krate/gorender", port),
		regionCache:  map[string]regionCacheEntry{},
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
	if len(regions) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(regions))
	// Serve fresh cached regions; render the rest.
	var miss []goRenderRegion
	now := time.Now()
	g.mu.Lock()
	for _, r := range regions {
		if r.Revalidate > 0 {
			if e, ok := g.regionCache[regionKey(r)]; ok && now.Before(e.exp) {
				out[r.ID] = e.html
				continue
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
	req, err := http.NewRequest("POST", g.renderURL, bytes.NewReader(body))
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
	for {
		var frame struct {
			Type  string `json:"type"`
			ID    string `json:"id"`
			HTML  string `json:"html"`
			Error string `json:"error"`
		}
		if err := dec.Decode(&frame); err != nil {
			break
		}
		if frame.ID != "" && frame.Error == "" {
			out[frame.ID] = frame.HTML
		}
	}
	// Cache renders that declare a revalidate window.
	g.mu.Lock()
	for _, r := range miss {
		if r.Revalidate <= 0 {
			continue
		}
		if html, ok := out[r.ID]; ok {
			g.regionCache[regionKey(r)] = regionCacheEntry{
				html: html,
				exp:  time.Now().Add(time.Duration(r.Revalidate) * time.Second),
			}
		}
	}
	g.mu.Unlock()
	return out, nil
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
	// Wait for the host to accept connections so the first proxied request does
	// not race its listen().
	g.waitReady(10 * time.Second)
	return nil
}

// waitReady polls the sidecar socket/port until it accepts a connection or the
// timeout elapses.
func (g *goAPISupervisor) waitReady(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
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
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (g *goAPISupervisor) stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cmd != nil && g.cmd.Process != nil {
		execx.KillTree(g.cmd)
		_, _ = g.cmd.Process.Wait()
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
						fmt.Fprintf(os.Stderr, "  %s⚠ Go API sidecar restart failed:%s %v\n", cYellow, cReset, err)
						return
					}
					printf("  %s⚡%s Go API sidecar restarted (%d routes)\n", cCyan, cReset, len(routes))
				}
			}()
		}
	}
}
