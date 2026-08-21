package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SSRServer manages the Node.js SSR renderer process.
type SSRServer struct {
	port     int
	root     string
	cmd      *exec.Cmd
	mu       sync.Mutex
	running  bool
	manifest *ServerManifest
}

// SSRResponse is the JSON response from the Node.js renderer.
type SSRResponse struct {
	HTML       string `json:"html"`
	Status     int    `json:"status"`
	HeadHTML   string `json:"headHTML,omitempty"`
	ScriptHTML string `json:"scriptHTML,omitempty"`
	Redirect   string `json:"redirect,omitempty"`
	NotFound   bool   `json:"notFound,omitempty"`
	Cached     bool   `json:"cached,omitempty"`
}

// ssrRenderTimeout bounds a single render request so a hung render cannot
// wedge the HTTP handler waiting for the renderer.
const ssrRenderTimeout = 30 * time.Second

// NewSSRServer creates a new SSR server manager.
func NewSSRServer(root string, port int) *SSRServer {
	return &SSRServer{
		port: port,
		root: root,
	}
}

// Start launches the Node.js renderer server process.
func (s *SSRServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	rendererPath := s.findRendererScript()
	if rendererPath == "" {
		return fmt.Errorf("krate SSR renderer not found — ensure @krate/runtime is installed")
	}

	manifestPath := filepath.Join(s.root, "dist", "server-manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return fmt.Errorf("server-manifest.json not found in dist/ — no SSR/ISR pages to render")
	}

	// Load manifest for route lookup
	data, _ := os.ReadFile(manifestPath)
	s.manifest = &ServerManifest{}
	json.Unmarshal(data, s.manifest)

	runtimeCmd := "npx"
	runtimeArgs := []string{"tsx", rendererPath}

	env := os.Environ()
	env = append(env,
		fmt.Sprintf("KRATE_SSR_PORT=%d", s.port),
		fmt.Sprintf("KRATE_MANIFEST=%s", manifestPath),
		fmt.Sprintf("KRATE_ROOT=%s", s.root),
	)

	s.cmd = exec.Command(runtimeCmd, runtimeArgs...)
	s.cmd.Env = env
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("starting SSR renderer: %w", err)
	}

	s.running = true

	// Wait for server to be ready
	go func() {
		s.cmd.Wait()
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	// Poll for readiness
	ready := false
	for i := 0; i < 50; i++ { // 5 seconds max
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/__krate/ssr/health", s.port))
		if err == nil && resp.StatusCode == 200 {
			ready = true
			resp.Body.Close()
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
	}

	if !ready {
		s.Stop()
		return fmt.Errorf("SSR renderer did not become ready within 5 seconds")
	}

	return nil
}

// Stop gracefully shuts down the Node.js renderer server.
func (s *SSRServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
		s.cmd.Wait()
	}
	s.running = false
}

// IsRunning returns whether the SSR server process is alive.
func (s *SSRServer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// RenderPage sends a render request to the Node.js renderer and returns the response.
func (s *SSRServer) RenderPage(route, url, method string, headers map[string]string, params, query map[string]string) (*SSRResponse, error) {
	if !s.IsRunning() {
		return nil, fmt.Errorf("SSR renderer not running")
	}

	reqBody := map[string]interface{}{
		"route":   route,
		"url":     url,
		"method":  method,
		"headers": headers,
	}
	if params != nil {
		reqBody["params"] = params
	}
	if query != nil {
		reqBody["query"] = query
	}

	body, _ := json.Marshal(reqBody)
	client := &http.Client{Timeout: ssrRenderTimeout}
	resp, err := client.Post(
		fmt.Sprintf("http://localhost:%d/__krate/render", s.port),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("SSR render request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	result := &SSRResponse{}
	if err := json.Unmarshal(respBody, result); err != nil {
		return nil, fmt.Errorf("parsing SSR response: %w", err)
	}

	return result, nil
}

// RevalidatePage triggers ISR revalidation for a specific route.
func (s *SSRServer) RevalidatePage(route string) error {
	if !s.IsRunning() {
		return fmt.Errorf("SSR renderer not running")
	}

	body, _ := json.Marshal(map[string]string{"route": route})
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(
		fmt.Sprintf("http://localhost:%d/__krate/ssr/revalidate", s.port),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// IsSSRPage checks if a route needs server-side rendering based on the manifest.
func (s *SSRServer) IsSSRPage(route string) bool {
	if page, _ := s.FindPageForRoute(route); page != nil {
		return page.Mode == "ssr" || page.Mode == "streaming"
	}
	return false
}

// IsISRPage checks if a route uses ISR.
func (s *SSRServer) IsISRPage(route string) bool {
	if page, _ := s.FindPageForRoute(route); page != nil {
		return page.Mode == "isr"
	}
	return false
}

// IsStreamingPage checks if a route uses streaming SSR.
func (s *SSRServer) IsStreamingPage(route string) bool {
	if page, _ := s.FindPageForRoute(route); page != nil {
		return page.Mode == "streaming"
	}
	return false
}

// SSRPort returns the port the SSR server is listening on.
func (s *SSRServer) SSRPort() int {
	return s.port
}

// GetStylesheet returns the global CSS filename from the manifest, or "" if none.
func (s *SSRServer) GetStylesheet() string {
	if s.manifest == nil {
		return ""
	}
	return s.manifest.Stylesheet
}

// GetRuntimeJS returns the shared runtime chunk path from the manifest, or "" if none.
func (s *SSRServer) GetRuntimeJS() string {
	if s.manifest == nil {
		return ""
	}
	return s.manifest.RuntimeJS
}

// ISRPage represents an ISR page with its revalidation interval.
type ISRPage struct {
	Route      string
	Revalidate int // seconds
}

// GetISRPages returns all ISR pages and their revalidation intervals.
func (s *SSRServer) GetISRPages() []ISRPage {
	if s.manifest == nil {
		return nil
	}
	var pages []ISRPage
	for _, p := range s.manifest.Pages {
		if p.Mode == "isr" {
			reval := p.Revalidate
			if reval <= 0 {
				reval = 60
			}
			pages = append(pages, ISRPage{
				Route:      p.Route,
				Revalidate: reval,
			})
		}
	}
	return pages
}

// findPage returns the ManifestPage for a given route, or nil if not found.
func (s *SSRServer) findPage(route string) *ManifestPage {
	if s.manifest == nil {
		return nil
	}
	for i := range s.manifest.Pages {
		if s.manifest.Pages[i].Route == route {
			return &s.manifest.Pages[i]
		}
	}
	return nil
}

// FindPageForRoute matches a URL path against route patterns (including [param] segments)
// and returns the matching page with extracted params. Returns nil if no match.
func (s *SSRServer) FindPageForRoute(urlPath string) (*ManifestPage, map[string]string) {
	if s.manifest == nil {
		return nil, nil
	}
	for i := range s.manifest.Pages {
		p := &s.manifest.Pages[i]
		if params, ok := matchRoute(urlPath, p.Route); ok {
			return p, params
		}
	}
	return nil, nil
}

func (s *SSRServer) findRendererScript() string {
	// Search for the server-renderer file in the runtime package
	candidates := []string{
		// Monorepo: packages/runtime/src/server-renderer.ts
		filepath.Join(s.root, "packages", "runtime", "src", "server-renderer.ts"),
		// Monorepo from compiler dir
		filepath.Join(s.root, "..", "runtime", "src", "server-renderer.ts"),
		// npm installed
		filepath.Join(s.root, "node_modules", "@krate", "runtime", "src", "server-renderer.ts"),
		// Go binary relative
		filepath.Join(filepath.Dir(os.Args[0]), "..", "runtime", "src", "server-renderer.ts"),
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}

	// Walk up looking for packages/runtime
	dir := s.root
	for i := 0; i < 5; i++ {
		candidate := filepath.Join(dir, "packages", "runtime", "src", "server-renderer.ts")
		if _, err := os.Stat(candidate); err == nil {
			abs, _ := filepath.Abs(candidate)
			return abs
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return ""
}

func matchRoute(urlPath, pattern string) (map[string]string, bool) {
	urlParts := strings.Split(strings.Trim(urlPath, "/"), "/")
	patParts := strings.Split(strings.Trim(pattern, "/"), "/")

	if len(urlParts) != len(patParts) {
		return nil, false
	}

	params := make(map[string]string)
	for i, pp := range patParts {
		if strings.HasPrefix(pp, "[") && strings.HasSuffix(pp, "]") {
			paramName := pp[1 : len(pp)-1]
			params[paramName] = urlParts[i]
		} else if pp != urlParts[i] {
			return nil, false
		}
	}

	return params, true
}
