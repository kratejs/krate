package build

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/execx"
	"github.com/kratejs/krate/packages/compiler/internal/safe"
)

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

	mu      sync.Mutex
	cmd     *exec.Cmd
	routes  []goAPIRouteEntry
	lastMod time.Time
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
	}
}

// SetSocket makes the supervisor talk to the sidecar over an AF_UNIX socket at
// path (passed to the child as KRATE_GOAPI_SOCKET) instead of loopback TCP.
func (g *goAPISupervisor) SetSocket(path string) {
	if path == "" {
		return
	}
	g.socket = path
	g.proxy.Transport = &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 128,
		IdleConnTimeout:     90 * time.Second,
	}
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
	return nil
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
					fmt.Printf("  %s⚡%s Go API sidecar restarted (%d routes)\n", cCyan, cReset, len(routes))
				}
			}()
		}
	}
}

// loadGoAPIManifest reads the route manifest and its modification time.
func loadGoAPIManifest(path string) ([]goAPIRouteEntry, time.Time, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	var m goAPIManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, time.Time{}, err
	}
	return m.Routes, fi.ModTime(), nil
}

// goAPIRouteMatches checks whether any route entry matches the method and path.
// Paths may contain {param} wildcards that match a single path segment.
func goAPIRouteMatches(method, path string, routes []goAPIRouteEntry) bool {
	for _, r := range routes {
		if r.Method != "" && !strings.EqualFold(r.Method, method) {
			continue
		}
		pathSegs := strings.Split(strings.Trim(path, "/"), "/")
		patSegs := strings.Split(strings.Trim(r.Path, "/"), "/")
		if len(pathSegs) != len(patSegs) {
			continue
		}
		ok := true
		for i, ps := range patSegs {
			if strings.HasPrefix(ps, "{") && strings.HasSuffix(ps, "}") {
				continue
			}
			if ps != pathSegs[i] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
