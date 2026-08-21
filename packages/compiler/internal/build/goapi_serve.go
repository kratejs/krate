package build

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// goAPISupervisor manages the compiled Go API sidecar subprocess. It starts
// the binary, proxies matching /api requests to it, and restarts it whenever
// the route manifest changes (i.e. a rebuild produced new Go API routes).
type goAPISupervisor struct {
	binPath      string
	manifestPath string
	port         int

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
	cmd.Env = append(os.Environ(), fmt.Sprintf("KRATE_GOAPI_PORT=%d", g.port))
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
		_ = g.cmd.Process.Kill()
		_, _ = g.cmd.Process.Wait()
		g.cmd = nil
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
			routes, modTime, err := loadGoAPIManifest(g.manifestPath)
			if err != nil {
				// Manifest gone → no Go routes anymore → stop the sidecar.
				if os.IsNotExist(err) {
					g.mu.Lock()
					g.routes = nil
					g.mu.Unlock()
					g.stop()
				}
				continue
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
					continue
				}
				fmt.Printf("  %s⚡%s Go API sidecar restarted (%d routes)\n", cCyan, cReset, len(routes))
			}
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
