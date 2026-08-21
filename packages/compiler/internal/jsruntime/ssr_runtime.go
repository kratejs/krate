package jsruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"krate-compiler/internal/escape"
)

// SSRPageRequest is the input for rendering a page via QuickJS.
type SSRPageRequest struct {
	Route   string            `json:"route"`
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Params  map[string]string `json:"params"`
	Query   map[string]string `json:"query"`
}

// SSRPageResponse is the output from rendering a page via QuickJS.
type SSRPageResponse struct {
	HTML       string `json:"html"`
	HeadHTML   string `json:"headHTML,omitempty"`
	ScriptHTML string `json:"scriptHTML,omitempty"`
	Status     int    `json:"status"`
	Redirect   string `json:"redirect,omitempty"`
	NotFound   bool   `json:"notFound,omitempty"`
}

// ISRCacheEntry holds a cached ISR page render.
type ISRCacheEntry struct {
	HTML       string
	HeadHTML   string
	ScriptHTML string
	Timestamp  time.Time
}

// SSRPageManifest describes a page that can be rendered at serve time.
type SSRPageManifest struct {
	Route      string `json:"route"`
	BundlePath string `json:"bundlePath"` // IIFE bundle relative to outDir
	Mode       string `json:"mode"`       // ssr, isr, streaming
	Revalidate int    `json:"revalidate,omitempty"`
}

// SSRManifest is the on-disk manifest for embedded SSR.
type SSRManifest struct {
	Pages             []SSRPageManifest `json:"pages"`
	Stylesheet        string            `json:"stylesheet,omitempty"`
	RuntimeJS         string            `json:"runtimeJS,omitempty"`
	RuntimeComponents []SSRPageManifest `json:"runtimeComponents,omitempty"`
}

// SSRRuntime renders pages via embedded QuickJS, replacing the Node.js sidecar.
type SSRRuntime struct {
	root        string
	outDir      string
	manifest    *SSRManifest
	isrCache    map[string]*ISRCacheEntry
	isrMu       sync.RWMutex
	bundleCache map[string][]byte // bundlePath → code
	bundleMu    sync.Mutex
}

// NewSSRRuntime creates an embedded SSR runtime from the manifest.
func NewSSRRuntime(root, outDir string) *SSRRuntime {
	return &SSRRuntime{
		root:        root,
		outDir:      outDir,
		isrCache:    make(map[string]*ISRCacheEntry),
		bundleCache: make(map[string][]byte),
	}
}

// LoadManifest reads the SSR manifest from the output directory.
func (s *SSRRuntime) LoadManifest() error {
	manifestPath := filepath.Join(s.outDir, "ssr-manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading ssr-manifest.json: %w", err)
	}
	s.manifest = &SSRManifest{}
	return json.Unmarshal(data, s.manifest)
}

// IsSSRPage checks if a route requires server-side rendering.
func (s *SSRRuntime) IsSSRPage(route string) bool {
	if p := s.findPage(route); p != nil {
		return p.Mode == "ssr" || p.Mode == "streaming"
	}
	return false
}

// IsISRPage checks if a route uses ISR.
func (s *SSRRuntime) IsISRPage(route string) bool {
	if p := s.findPage(route); p != nil {
		return p.Mode == "isr"
	}
	return false
}

// IsStreamingPage checks if a route uses streaming SSR.
func (s *SSRRuntime) IsStreamingPage(route string) bool {
	if p := s.findPage(route); p != nil {
		return p.Mode == "streaming"
	}
	return false
}

// IsRunning returns true if the manifest is loaded.
func (s *SSRRuntime) IsRunning() bool {
	return s.manifest != nil
}

// GetStylesheet returns the global CSS filename.
func (s *SSRRuntime) GetStylesheet() string {
	if s.manifest == nil {
		return ""
	}
	return s.manifest.Stylesheet
}

// GetRuntimeJS returns the shared runtime chunk path.
func (s *SSRRuntime) GetRuntimeJS() string {
	if s.manifest == nil {
		return ""
	}
	return s.manifest.RuntimeJS
}

// ISRPageInfo describes an ISR page with its revalidation interval.
type ISRPageInfo struct {
	Route      string
	Revalidate int // seconds
}

// GetISRPages returns all ISR pages and their revalidation intervals.
func (s *SSRRuntime) GetISRPages() []ISRPageInfo {
	if s.manifest == nil {
		return nil
	}
	var pages []ISRPageInfo
	for _, p := range s.manifest.Pages {
		if p.Mode == "isr" {
			reval := p.Revalidate
			if reval <= 0 {
				reval = 60
			}
			pages = append(pages, ISRPageInfo{
				Route:      p.Route,
				Revalidate: reval,
			})
		}
	}
	return pages
}

// FindPageForRoute matches a URL path against route patterns and returns the page + params.
func (s *SSRRuntime) FindPageForRoute(urlPath string) (*SSRPageManifest, map[string]string) {
	if s.manifest == nil {
		return nil, nil
	}
	for i := range s.manifest.Pages {
		p := &s.manifest.Pages[i]
		if params, ok := matchSSRRoute(urlPath, p.Route); ok {
			return p, params
		}
	}
	return nil, nil
}

// Invalidate removes the ISR cache for a route.
func (s *SSRRuntime) Invalidate(route string) {
	s.isrMu.Lock()
	delete(s.isrCache, route)
	s.isrMu.Unlock()
}

// RenderPage renders a page via QuickJS and returns the response.
func (s *SSRRuntime) RenderPage(req SSRPageRequest) (*SSRPageResponse, error) {
	page := s.findPage(req.Route)
	if page == nil {
		return &SSRPageResponse{Status: 404, NotFound: true}, nil
	}

	// ISR: serve from cache if available and not stale
	if page.Mode == "isr" {
		s.isrMu.RLock()
		cached, ok := s.isrCache[page.Route]
		s.isrMu.RUnlock()
		if ok && time.Since(cached.Timestamp) < time.Duration(page.Revalidate)*time.Second {
			return &SSRPageResponse{
				HTML:       cached.HTML,
				HeadHTML:   cached.HeadHTML,
				ScriptHTML: cached.ScriptHTML,
				Status:     200,
			}, nil
		}
	}

	code, err := s.loadBundle(page.BundlePath)
	if err != nil {
		return nil, fmt.Errorf("loading bundle for %s: %w", page.Route, err)
	}

	// Create a fresh VM for this render
	rt, err := New()
	if err != nil {
		return nil, fmt.Errorf("creating JS runtime: %w", err)
	}
	defer rt.Close()

	// Execute the IIFE bundle — defines __krate_renderPage, streaming markers, etc.
	if _, err := rt.Execute(string(code)); err != nil {
		return nil, fmt.Errorf("executing bundle: %w", err)
	}

	// Streaming: two-phase render
	if page.Mode == "streaming" {
		return s.renderStreaming(rt, req)
	}

	// Standard SSR/ISR render
	result, err := s.renderComponent(rt, nil)
	if err != nil {
		return &SSRPageResponse{Status: 500}, err
	}

	headHTML := extractDelimitedHTML(result, "head-start", "head-end")
	scriptHTML := extractDelimitedHTML(result, "script-start", "script-end")

	resp := &SSRPageResponse{
		HTML:       result,
		HeadHTML:   headHTML,
		ScriptHTML: scriptHTML,
		Status:     200,
	}

	if page.Mode == "isr" {
		s.isrMu.Lock()
		s.isrCache[page.Route] = &ISRCacheEntry{
			HTML:       result,
			HeadHTML:   headHTML,
			ScriptHTML: scriptHTML,
			Timestamp:  time.Now(),
		}
		s.isrMu.Unlock()
	}

	return resp, nil
}

// renderComponent calls __krate_renderPage(propsJSON) and returns the HTML.
func (s *SSRRuntime) renderComponent(rt *Runtime, props map[string]any) (string, error) {
	propsJSON := "{}"
	if props != nil {
		data, err := json.Marshal(props)
		if err != nil {
			return "", fmt.Errorf("marshaling props: %w", err)
		}
		propsJSON = string(data)
	}

	escapedProps := escape.JSStringDQ(propsJSON)
	jsCode := fmt.Sprintf(`(function() {
		try {
			var html = __krate_renderPage(%s);
			return JSON.stringify({html: html || '', status: 200});
		} catch(e) {
			return JSON.stringify({html: '', status: 500, error: e.message || String(e)});
		}
	})()`, escapedProps)

	result, err := rt.Execute(jsCode)
	if err != nil {
		return "", fmt.Errorf("rendering component: %w", err)
	}

	var parsed struct {
		HTML   string `json:"html"`
		Status int    `json:"status"`
		Error  string `json:"error"`
	}
	if err := jsonUnmarshal([]byte(result.(string)), &parsed); err != nil {
		return "", fmt.Errorf("parsing render result: %w", err)
	}
	if parsed.Error != "" {
		return "", fmt.Errorf("render error: %s", parsed.Error)
	}
	return parsed.HTML, nil
}

// renderStreaming does two-phase streaming SSR.
func (s *SSRRuntime) renderStreaming(rt *Runtime, req SSRPageRequest) (*SSRPageResponse, error) {
	// Phase 1: Render with empty props (Suspense fallback)
	if _, err := rt.Execute("if(typeof __krate_resetBoundaryCounter==='function')__krate_resetBoundaryCounter();if(typeof __krate_setStreamingResolved==='function')__krate_setStreamingResolved(false);"); err != nil {
		return nil, fmt.Errorf("streaming reset: %w", err)
	}

	fallbackHTML, err := s.renderComponent(rt, nil)
	if err != nil {
		return nil, fmt.Errorf("streaming fallback: %w", err)
	}

	// Phase 2: Render with resolved props
	if _, err := rt.Execute("if(typeof __krate_resetBoundaryCounter==='function')__krate_resetBoundaryCounter();if(typeof __krate_setStreamingResolved==='function')__krate_setStreamingResolved(true);"); err != nil {
		return nil, fmt.Errorf("streaming resolve: %w", err)
	}

	resolvedHTML, err := s.renderComponent(rt, nil)
	if err != nil {
		return nil, fmt.Errorf("streaming resolved: %w", err)
	}

	if _, err := rt.Execute("if(typeof __krate_setStreamingResolved==='function')__krate_setStreamingResolved(false);"); err != nil {
		// non-fatal
	}

	resolvedMap := extractSuspenseMarkers(resolvedHTML)
	script := buildSuspenseScript(resolvedMap)

	return &SSRPageResponse{
		HTML:       fallbackHTML + script,
		Status:     200,
		HeadHTML:   extractDelimitedHTML(fallbackHTML, "head-start", "head-end"),
		ScriptHTML: extractDelimitedHTML(fallbackHTML, "script-start", "script-end"),
	}, nil
}

// loadBundle reads the IIFE bundle code, using cache if available.
func (s *SSRRuntime) loadBundle(bundlePath string) ([]byte, error) {
	s.bundleMu.Lock()
	defer s.bundleMu.Unlock()

	if cached, ok := s.bundleCache[bundlePath]; ok {
		return cached, nil
	}

	absPath := filepath.Join(s.outDir, bundlePath)
	// Normalize path separators
	absPath = filepath.FromSlash(absPath)

	code, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", absPath, err)
	}

	s.bundleCache[bundlePath] = code
	return code, nil
}

// ClearBundleCache removes all cached bundles. Call during dev mode hot reload.
func (s *SSRRuntime) ClearBundleCache() {
	s.bundleMu.Lock()
	s.bundleCache = make(map[string][]byte)
	s.bundleMu.Unlock()
}

// findPage returns the SSRPageManifest for a given route.
func (s *SSRRuntime) findPage(route string) *SSRPageManifest {
	if s.manifest == nil {
		return nil
	}
	for i := range s.manifest.Pages {
		if s.manifest.Pages[i].Route == route {
			return &s.manifest.Pages[i]
		}
	}
	// Try with/without trailing slash
	if route != "/" {
		for i := range s.manifest.Pages {
			p := &s.manifest.Pages[i]
			if p.Route == route+"/" || p.Route == strings.TrimSuffix(route, "/") {
				return p
			}
		}
	}
	return nil
}

// matchSSRRoute checks if a URL path matches a route pattern and extracts params.
func matchSSRRoute(urlPath, pattern string) (map[string]string, bool) {
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

// extractDelimitedHTML extracts content between <!--marker-start--> and <!--marker-end--> comments.
func extractDelimitedHTML(html, startMarker, endMarker string) string {
	start := "<!--" + startMarker + "-->"
	end := "<!--" + endMarker + "-->"
	i := strings.Index(html, start)
	if i < 0 {
		return ""
	}
	j := strings.Index(html[i+len(start):], end)
	if j < 0 {
		return ""
	}
	return html[i+len(start) : i+len(start)+j]
}

// extractSuspenseMarkers extracts resolved suspense boundary content.
func extractSuspenseMarkers(html string) map[string]string {
	resolved := make(map[string]string)
	// Parse <!--suspense-resolved:N-->...<!--/suspense-resolved:N--> markers
	for {
		startTag := "<!--suspense-resolved:"
		idx := strings.Index(html, startTag)
		if idx < 0 {
			break
		}
		numStart := idx + len(startTag)
		numEnd := strings.Index(html[numStart:], "-->")
		if numEnd < 0 {
			break
		}
		id := html[numStart : numStart+numEnd]
		contentStart := numStart + numEnd + 3 // skip -->
		endTag := "<!--/suspense-resolved:" + id + "-->"
		contentEnd := strings.Index(html[contentStart:], endTag)
		if contentEnd < 0 {
			break
		}
		resolved[id] = html[contentStart : contentStart+contentEnd]
		html = html[contentStart+contentEnd+len(endTag):]
	}
	return resolved
}

// buildSuspenseScript generates a <script> that replaces fallback spans with resolved content.
func buildSuspenseScript(resolvedMap map[string]string) string {
	if len(resolvedMap) == 0 {
		return ""
	}
	mapJSON, _ := json.Marshal(resolvedMap)
	return fmt.Sprintf(
		`<script>(function(){var m=%s;Object.keys(m).forEach(function(id){var s=document.querySelector('span[data-suspense="'+id+'"]');if(s)s.outerHTML=m[id];var t=document.querySelectorAll('template[data-suspense]');t.forEach(function(el){el.remove()})})})();</script>`,
		string(mapJSON),
	)
}
