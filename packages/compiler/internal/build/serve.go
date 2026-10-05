package build

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
)

const apiServerScriptContent = `import http from 'node:http';
import path from 'node:path';
import fs from 'node:fs';
import { pathToFileURL } from 'node:url';
import process from 'node:process';

const API_DIR = path.resolve(process.argv[2] || './src/api');
const PORT = parseInt(process.argv[3] || '3001', 10);
const MIDDLEWARE_PATH = path.resolve(process.argv[4] || '');

let middlewareModule = null;
if (MIDDLEWARE_PATH && fs.existsSync(MIDDLEWARE_PATH)) {
  try {
    middlewareModule = await import(pathToFileURL(MIDDLEWARE_PATH).href + '?t=' + Date.now());
  } catch (e) {
  }
}

const server = http.createServer(async (req, res) => {
  try {
    const urlObj = new URL(req.url, "http://" + req.headers.host);
    const urlPath = urlObj.pathname;

    // Middleware endpoint: execute middleware and return result
    if (urlPath === '/__krate/middleware') {
      if (!middlewareModule || !middlewareModule.middleware) {
        res.setHeader('Content-Type', 'application/json');
        return res.end(JSON.stringify({ action: 'continue' }));
      }

      const bodyChunks = [];
      for await (const chunk of req) { bodyChunks.push(chunk); }
      const bodyStr = Buffer.concat(bodyChunks).toString();
      let body = {};
      try { body = JSON.parse(bodyStr); } catch {}

      const headers = {};
      for (const [key, value] of Object.entries(req.headers)) {
        headers[key] = Array.isArray(value) ? value.join(', ') : value;
      }

      const request = new Request(body.url || ('http://localhost' + urlPath), {
        method: body.method || 'GET',
        headers: new Headers(headers),
      });

      try {
        const result = await middlewareModule.middleware(request);
        if (result && typeof result.status === 'number') {
          const respHeaders = {};
          for (const [k, v] of result.headers.entries()) { respHeaders[k] = v; }
          res.setHeader('Content-Type', 'application/json');
          return res.end(JSON.stringify({
            status: result.status,
            headers: respHeaders,
            body: result.status === 204 ? null : await result.text(),
          }));
        }
        res.setHeader('Content-Type', 'application/json');
        return res.end(JSON.stringify({ action: 'continue' }));
      } catch (e) {
        res.setHeader('Content-Type', 'application/json');
        return res.end(JSON.stringify({ action: 'continue', error: e.message }));
      }
    }
    
    const relativePath = urlPath.replace(/^\/api/, '');
    
    let targetFile = null;
    const extensions = ['.ts', '.js', '.tsx', '.jsx'];

    // Build a path relative to API_DIR, rejecting any attempt to escape it.
    // Without this, /api/../../etc/passwd would resolve outside API_DIR and be
    // handed to import(), giving unauthenticated arbitrary module execution.
    const cleanRel = (() => {
      let p = relativePath || '/';
      try { p = decodeURIComponent(p); } catch { return null; }
      if (p.includes('\0') || p.includes('\\')) return null;
      const segments = p.split('/');
      const clean = [];
      for (const seg of segments) {
        if (seg === '' || seg === '.') continue;
        if (seg === '..') return null;
        clean.push(seg);
      }
      return clean;
    })();
    if (cleanRel === null) {
      res.statusCode = 400;
      res.setHeader('Content-Type', 'application/json');
      return res.end(JSON.stringify({ error: "Invalid API route path: " + urlPath }));
    }
    
    for (const ext of extensions) {
      const fileCheck = path.join(API_DIR, ...cleanRel) + ext;
      if (fs.existsSync(fileCheck) && !fs.statSync(fileCheck).isDirectory()) {
        targetFile = fileCheck;
        break;
      }
      const indexCheck = path.join(API_DIR, ...cleanRel, 'index' + ext);
      if (fs.existsSync(indexCheck)) {
        targetFile = indexCheck;
        break;
      }
    }

    if (!targetFile) {
      res.statusCode = 404;
      res.setHeader('Content-Type', 'application/json');
      return res.end(JSON.stringify({ error: "API Route Not Found: " + urlPath }));
    }

    const routeModule = await import(pathToFileURL(targetFile).href + '?t=' + Date.now());
    const method = req.method.toUpperCase();

    // Check for named method exports (GET, POST, PUT, DELETE, PATCH, OPTIONS, HEAD)
    const methodHandler = routeModule[method] || routeModule.default;

    if (typeof methodHandler === 'function') {
      // Modern pattern: check if function expects Request (web standard API)
      const fnStr = methodHandler.toString();
      const argCount = methodHandler.length;
      
      if (argCount <= 1) {
        // Web standard API: handler(request) => Response
        const headers = {};
        for (const [key, value] of Object.entries(req.headers)) {
          headers[key] = Array.isArray(value) ? value.join(', ') : value;
        }
        
        let body = null;
        if (req.method !== 'GET' && req.method !== 'HEAD') {
          const chunks = [];
          for await (const chunk of req) {
            chunks.push(chunk);
          }
          body = Buffer.concat(chunks);
        }
        
        const request = new Request(urlObj.href, {
          method: req.method,
          headers: new Headers(headers),
          body: body && body.length > 0 ? body : undefined,
        });
        
        const response = await methodHandler(request);
        
        if (response instanceof Response || (response && typeof response.status === 'number' && typeof response.headers === 'object')) {
          res.statusCode = response.status;
          for (const [key, value] of response.headers.entries()) {
            res.setHeader(key, value);
          }
          const responseBody = await response.text();
          res.end(responseBody);
        } else if (response !== undefined && response !== null) {
          res.setHeader('Content-Type', 'application/json');
          res.end(JSON.stringify(response));
        } else {
          res.statusCode = 200;
          res.end();
        }
      } else {
        // Legacy pattern: handler(req, res)
        await methodHandler(req, res);
      }
    } else {
      res.statusCode = 500;
      res.setHeader('Content-Type', 'application/json');
      return res.end(JSON.stringify({ error: "API route must export a handler function (GET, POST, etc. or default)." }));
    }
  } catch (err) {
    res.statusCode = 500;
    res.setHeader('Content-Type', 'application/json');
    return res.end(JSON.stringify({ error: "Internal Server Error", details: err.message }));
  }
});

server.listen(PORT, () => {
});`

const (
	cReset  = "\033[0m"
	cRed    = "\033[31m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cBlue   = "\033[34m"
	cCyan   = "\033[36m"
	cGray   = "\033[90m"
	cBold   = "\033[1m"
)

// regionOpenRe matches a compiled dynamic region's opening splice marker:
// <!--suspense:ID--> (Suspense boundary) or <!--region:ID--> (standalone
// runtime component). The matching closing marker is <!--/suspense:ID--> or
// <!--/region:ID-->. Go's regexp (RE2) has no backreferences, so open/close
// are matched separately.
var regionOpenRe = regexp.MustCompile(`<!--(suspense|region):([^>]+?)-->`)

// regionFrame is one NDJSON frame from the sidecar's /__krate/regions stream.
type regionFrame struct {
	Type        string `json:"type"`
	ID          string `json:"id,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Status      int    `json:"status,omitempty"`
	HTML        string `json:"html,omitempty"`
	Error       string `json:"error,omitempty"`
	Count       int    `json:"count,omitempty"`
	CacheStatus string `json:"cacheStatus,omitempty"`
	NotFound    bool   `json:"notFound,omitempty"`
	Redirect    string `json:"redirect,omitempty"`
	Title       string `json:"title,omitempty"`
}

// regionPageResult carries sidecar outcome back to the caller so it can apply
// ISR cache headers, status, redirects, and title swaps that page-region
// renders report.
type regionPageResult struct {
	// served is true when the shell had region markers and was handled here.
	served bool
	// isISR reports whether the page is an ISR page (region render was cached).
	isISR bool
	// cacheStatus is the region frame's cacheStatus ("hit"/"stale"/"miss").
	cacheStatus string
	// titleOverride, when non-empty, replaces the baked <title>.
	titleOverride string
	// notFound signals the page-region render did not match.
	notFound bool
	// redirect is the Location for a page-region redirect response.
	redirect string
	// renderErr is non-empty when the page-region render reported a server
	// error (status >= 500). The caller serves the baked shell / error page.
	renderErr string
}

// streamRegionPage serves a page with the static-first architecture: the
// build-time static shell (suspense/region splice markers + baked content) is
// read from disk and streamed to the browser, and each dynamic region's HTML is
// fetched from the SSR sidecar (/__krate/regions) and spliced in place of its
// marker's inner content, one render per region with a flush after each. For
// SSR/ISR pages the shell carries a single coarse "page" region whose render
// replaces the whole baked body. Returns the page-region result (served=false
// when the shell had no region markers and the caller should use another path).
func streamRegionPage(w http.ResponseWriter, flusher http.Flusher, absOut, route string, ssrPort int, params, query, headers map[string]string) regionPageResult {
	res := regionPageResult{}
	relPath := strings.TrimPrefix(route, "/")
	if relPath == "" {
		relPath = "index.html"
	} else {
		relPath = relPath + "/index.html"
	}
	data, err := os.ReadFile(filepath.Join(absOut, relPath))
	if err != nil {
		return res
	}
	shell := string(data)

	// Only pages with region splice markers use this path. Marker kinds:
	//   <!--suspense:ID-->...<!--/suspense:ID--> — Suspense boundary or the
	//       coarse page region; the inner content is baked (fallback / stale
	//       body) and kept if the region render fails or is skipped.
	//   <!--region:ID--><!--/region:ID--> — standalone runtime component; empty
	//       slot filled by the region render.
	opens := regionOpenRe.FindAllStringSubmatchIndex(shell, -1)
	if len(opens) == 0 {
		return res
	}
	res.served = true

	type boundary struct {
		openEnd int // index just past the opening marker's "-->"
		id      string
		kind    string // "suspense" | "region"
		fbStart int    // start of inner content (for suspense: baked content)
		fbEnd   int    // end of inner content (start of closing marker)
	}
	var boundaries []boundary
	for _, m := range opens {
		openTag := shell[m[0]:m[1]]
		kind := "region"
		if strings.HasPrefix(openTag, "<!--suspense:") {
			kind = "suspense"
		}
		id := shell[m[4]:m[5]]
		openEnd := m[1]
		closeMarker := "<!--/" + kind + ":" + id + "-->"
		rel := strings.Index(shell[openEnd:], closeMarker)
		if rel < 0 {
			// Malformed marker pair — bail to the non-region path.
			res.served = false
			return res
		}
		boundaries = append(boundaries, boundary{
			openEnd: openEnd,
			id:      id,
			kind:    kind,
			fbStart: openEnd,
			fbEnd:   openEnd + rel,
		})
	}

	// The marker id "page" denotes a coarse whole-page region (SSR/ISR shell);
	// its frame carries the page's ISR cache status for the caller's headers.

	// Open a raw TCP connection to the sidecar and POST /__krate/regions. The
	// read deadline bounds how long we wait for region frames so a stalled
	// sidecar can't pin this request goroutine forever.
	conn, err := dialSidecar(ssrPort, 30*time.Second)
	if err != nil {
		res.served = false
		return res
	}
	defer conn.Close()

	// Send the explicit splice list so the sidecar knows each marker's kind
	// without reading the manifest for standalone runtime comps.
	regionList := make([]map[string]string, 0, len(boundaries))
	for _, b := range boundaries {
		kind := "component"
		if b.kind == "suspense" && b.id == "page" {
			kind = "page"
		}
		regionList = append(regionList, map[string]string{"id": b.id, "kind": kind})
	}
	reqBody, _ := json.Marshal(map[string]interface{}{
		"route":   route,
		"url":     route,
		"method":  "GET",
		"headers": headers,
		"params":  params,
		"query":   query,
		"regions": regionList,
	})
	httpReq := fmt.Sprintf("POST /__krate/regions HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", ssrPort, len(reqBody))
	if _, err := conn.Write([]byte(httpReq)); err != nil {
		res.served = false
		return res
	}
	if _, err := conn.Write(reqBody); err != nil {
		res.served = false
		return res
	}

	tcpBuf := bufio.NewReaderSize(conn, 256)

	// Read status line (e.g., "HTTP/1.1 200 OK\r\n")
	if _, err := tcpBuf.ReadString('\n'); err != nil {
		res.served = false
		return res
	}

	// Read headers until empty line.
	for {
		line, err := tcpBuf.ReadString('\n')
		if err != nil {
			res.served = false
			return res
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
	}

	// Read one NDJSON line per region, collecting region HTML + page metadata.
	regionHTML := make(map[string]string, len(boundaries))
	framesLeft := len(boundaries)
	for framesLeft > 0 {
		line, err := tcpBuf.ReadBytes('\n')
		if err != nil {
			break
		}
		var frame regionFrame
		if err := json.Unmarshal(line, &frame); err != nil {
			continue
		}
		switch frame.Type {
		case "region":
			if frame.ID == "page" {
				res.isISR = frame.CacheStatus != ""
				res.cacheStatus = frame.CacheStatus
				res.notFound = frame.NotFound
				res.redirect = frame.Redirect
				res.titleOverride = frame.Title
				// A page-region render that reported an error (or whose status is
				// a server error) must not replace the baked body with empty HTML.
				// Keep the baked content and surface the failure to the caller.
				if frame.Status >= 500 || frame.Error != "" {
					res.renderErr = frame.Error
					if res.renderErr == "" {
						res.renderErr = fmt.Sprintf("page-region render failed with status %d", frame.Status)
					}
					framesLeft--
					continue
				}
				if frame.NotFound {
					// Sidecar couldn't match — treat as no render (keep baked).
					framesLeft--
					continue
				}
			}
			regionHTML[frame.ID] = frame.HTML
			framesLeft--
		case "skip":
			// Sidecar has no renderer for this marker — keep baked content.
			framesLeft--
		case "error":
			if frame.ID != "" {
				framesLeft--
			} else {
				framesLeft = 0
			}
		case "end":
			framesLeft = 0
		}
	}

	// A page-region render that 404s (unknown dynamic variant) or redirects is
	// surfaced to the caller instead of streaming the shell.
	if res.notFound {
		res.served = true
		return res
	}

	// Apply title override from the page-region render to the baked head.
	if res.titleOverride != "" {
		shell = replaceTitle(shell, res.titleOverride)
	}

	// Status headers must be set BEFORE the first body write below: the caller's
	// post-return Set calls would otherwise be silently dropped once the body is
	// committed. All region frames have been read by now, so the page-region
	// cache/render status is known.
	if res.renderErr != "" {
		w.Header().Set("X-Krate-Error", "render")
	}
	if res.isISR && res.cacheStatus != "" {
		w.Header().Set("X-Krate-Cache", strings.ToUpper(res.cacheStatus))
	}

	// The caller owns the remaining response headers (Content-Type, cache
	// headers) and sets them before calling this function. The 200 status is
	// committed implicitly by the first body write below; this function only
	// writes the body, flushing after each splice so the page streams
	// progressively.
	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}

	// Write the shell, replacing each marker block's inner content with its
	// region HTML (or the baked content when the region render failed/skipped),
	// flushing after each splice so the page streams progressively.
	cursor := 0
	for _, b := range boundaries {
		_, _ = w.Write([]byte(shell[cursor:b.openEnd]))
		if html, ok := regionHTML[b.id]; ok {
			_, _ = w.Write([]byte(html))
		} else if b.kind == "suspense" {
			// Region render failed or no frame — keep the baked content.
			_, _ = w.Write([]byte(shell[b.fbStart:b.fbEnd]))
		}
		// Standalone runtime regions with no frame leave the empty slot empty.
		flush()
		cursor = b.fbEnd
	}
	_, _ = w.Write([]byte(shell[cursor:]))
	flush()

	return res
}

// replaceTitle swaps the baked <title> content in a shell for a freshly
// rendered one. Minified shells may have unquoted/empty titles; a simple
// scan/replace on the first <title>…</title> span is sufficient.
func replaceTitle(shell, title string) string {
	re := regexp.MustCompile(`(?i)<title[^>]*>[\s\S]*?</title>`)
	loc := re.FindStringIndex(shell)
	if loc == nil {
		return shell
	}
	return shell[:loc[0]] + "<title>" + title + "</title>" + shell[loc[1]:]
}

// ServeDev starts an HTTP server with live reload SSE + request logging.
func ServeDev(root string, cfg *config.Config, hub *DevHub, startTime time.Time) error {
	return serve(root, cfg, hub, startTime)
}

// Serve starts an HTTP server with request logging (production preview).
func Serve(root string, cfg *config.Config, startTime time.Time) error {
	return serve(root, cfg, nil, startTime)
}

func serve(root string, cfg *config.Config, hub *DevHub, startTime time.Time) error {
	port := cfg.ServerPort()
	apiPort := port + 1

	// Load embedded middleware runtime if configured (default: quickjs)
	var middlewareRT *jsruntime.MiddlewareRuntime
	middlewareRTOption := strings.ToLower(cfg.SSR.MiddlewareRuntime)
	if middlewareRTOption == "" || middlewareRTOption == "quickjs" {
		middlewareRT = jsruntime.LoadMiddlewareRuntime(root)
	}

	go func() {
		scriptPath := filepath.Join(root, ".krate", "api-server.js")
		_ = os.MkdirAll(filepath.Dir(scriptPath), 0755)
		_ = os.WriteFile(scriptPath, []byte(apiServerScriptContent), 0644)

		apiDir := filepath.Join(cfg.OutDir, "api")
		middlewarePath := filepath.Join(root, ".krate", "middleware.js")

		portStr := fmt.Sprintf("%d", apiPort)
		var cmd *exec.Cmd

		runtimeOpt := strings.ToLower(cfg.Runtime)
		switch runtimeOpt {
		case "bun":
			cmd = exec.Command("bun", "run", scriptPath, apiDir, portStr, middlewarePath)

		case "deno":
			cmd = exec.Command("deno", "run", "--allow-net", "--allow-read", "--allow-sys", "--allow-env", scriptPath, apiDir, portStr, middlewarePath)

		default:
			cmd = exec.Command("node", scriptPath, apiDir, portStr, middlewarePath)
		}

		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}()

	// Start the Go API sidecar if Go API routes were compiled during the build.
	// Go routes take precedence over JS routes for the same /api path.
	var goAPI *goAPISupervisor
	goAPIManifestPath := filepath.Join(root, ".krate", "goapi-routes.json")
	if _, err := os.Stat(goAPIManifestPath); err == nil {
		goAPIPort := port + 2
		goAPI = newGoAPISupervisor(goAPIServerBinPath(root), goAPIManifestPath, goAPIPort)
		goAPI.SetEnv(environ.KVList(environ.Current))
		if err := goAPI.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "  %s⚠ Go API sidecar not started:%s %v\n", cYellow, cReset, err)
		} else {
			fmt.Printf("  %s⚡%s Go API → %shttp://localhost:%d%s\n", cCyan, cReset, cCyan, goAPIPort, cReset)
		}
	}

	absOut, err := filepath.Abs(cfg.OutDir)
	if err != nil {
		return fmt.Errorf("resolving out dir: %w", err)
	}

	fileServer := http.FileServer(http.Dir(absOut))
	mux := http.NewServeMux()

	// Load the build manifest to learn which dynamic routes are closed
	// (static-only: unknown params must 404, not serve the [param] template).
	staticOnlyRoutes := loadStaticOnlyRoutes(absOut)

	// Load embedded API route runtime if configured (default: quickjs)
	var apiRT *jsruntime.APIRouteRuntime
	apiRTOption := strings.ToLower(cfg.SSR.APIRuntime)
	if apiRTOption == "" || apiRTOption == "quickjs" {
		apiDir := filepath.Join(cfg.OutDir, "api")
		if _, err := os.Stat(apiDir); err == nil {
			apiRT = jsruntime.NewAPIRouteRuntime(apiDir)
			apiRT.SetEnv(environ.Current)
		}
	}

	sidecarURL, _ := url.Parse(fmt.Sprintf("http://localhost:%d", apiPort))
	apiProxy := httputil.NewSingleHostReverseProxy(sidecarURL)
	apiProxy.Transport = newSidecarTransport()

	// Optional user-provided API sidecar. Requests it owns are forwarded first;
	// a 404 falls through to the built-in Go/TS/QuickJS routes below.
	customSidecar, err := newAPISidecar(root, cfg)
	if err != nil {
		return err
	}
	if customSidecar != nil {
		if err := customSidecar.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "  %s⚠ API sidecar not started:%s %v\n", cYellow, cReset, err)
		} else {
			target := cfg.API.Sidecar.Target
			if target == "" {
				target = fmt.Sprintf("http://127.0.0.1:%d", cfg.API.Sidecar.Port)
			}
			mode := "proxy"
			if cfg.API.Sidecar.Command != "" {
				mode = "supervised"
			}
			fmt.Printf("  %s⚡%s API sidecar (%s) %s → %s%s%s\n", cCyan, cReset, mode, customSidecar.Prefix(), cCyan, target, cReset)
		}
	}

	// Non-/api sidecar prefixes get their own subtree handler. A 404 from the
	// sidecar is surfaced as a real 404 (no built-in routes exist off /api).
	if customSidecar != nil && customSidecar.Prefix() != "/api" {
		p := customSidecar.Prefix()
		customSidecarHandler := func(w http.ResponseWriter, r *http.Request) {
			if customSidecar.Handle(w, r) {
				return
			}
			http.NotFound(w, r)
		}
		mux.HandleFunc(p+"/", customSidecarHandler)
		mux.HandleFunc(p, customSidecarHandler)
	}

	apiBodyLimit := cfg.MaxAPIBodyBytes()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, apiBodyLimit)
		}

		// Custom sidecar first (default /api prefix): owns the routes it
		// responds to; a 404 falls through to the built-ins.
		if customSidecar != nil && customSidecar.Prefix() == "/api" && customSidecar.Handle(w, r) {
			return
		}

		// Go API routes take precedence (max-performance compiled sidecar)
		if goAPI != nil && goAPI.Active() && goAPI.RouteMatches(r.Method, r.URL.Path) {
			goAPI.Proxy(w, r)
			return
		}

		// Use embedded quickjs runtime if available
		if apiRT != nil {
			headers := make(map[string]string)
			for k, v := range r.Header {
				headers[k] = strings.Join(v, ", ")
			}

			var body string
			if r.Method != "GET" && r.Method != "HEAD" {
				b, err := readBodyLimited(r)
				if err != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusRequestEntityTooLarge)
					_, _ = io.WriteString(w, `{"error":"Request Entity Too Large"}`)
					return
				}
				body = string(b)
			}

			result := apiRT.Execute(jsruntime.APIRequest{
				URL:     fmt.Sprintf("http://localhost:%d%s", port, r.URL.RequestURI()),
				Method:  r.Method,
				Path:    r.URL.Path,
				Headers: headers,
				Body:    body,
			})

			for k, v := range result.Headers {
				w.Header().Set(k, v)
			}
			if result.Status > 0 {
				w.WriteHeader(result.Status)
			}
			if result.Body != "" {
				_, _ = w.Write([]byte(result.Body))
			}
			return
		}

		// Fallback: proxy to sidecar
		apiProxy.ServeHTTP(w, r)
	})

	// Start SSR renderer server if there are SSR/ISR/streaming pages
	ssrPort := cfg.SSR.RendererPort
	if ssrPort == 0 {
		ssrPort = port + 10
	}
	ssr := NewSSRServer(root, ssrPort, cfg.SSR.SSRRuntime)
	ssr.SetEnv(environ.Current)
	ssr.SetTuning(cfg.SSR.Timeout, cfg.SSR.MaxCacheSize)
	ssr.SetPPR(cfg.PPR, cfg.PPRRevalidate)
	ssrStarted := false
	if err := ssr.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "  %s⚠ SSR renderer not started:%s %v\n", cYellow, cReset, err)
	} else {
		ssrStarted = true
		fmt.Printf("  %s⚡%s SSR renderer → %shttp://localhost:%d%s\n", cCyan, cReset, cCyan, ssrPort, cReset)
	}

	// SSR/ISR/Streaming route handler — intercepts before static file server
	if ssrStarted {
		mux.HandleFunc("/__krate/ssr/", func(w http.ResponseWriter, r *http.Request) {
			// Forward internal SSR endpoints to the renderer
			ssrURL := fmt.Sprintf("http://localhost:%d%s", ssrPort, r.URL.Path)
			proxyReq, _ := http.NewRequest(r.Method, ssrURL, r.Body)
			proxyReq.Header = r.Header
			resp, err := sidecarClient.Do(proxyReq)
			if err != nil {
				w.WriteHeader(502)
				_, _ = w.Write([]byte(`{"error":"SSR renderer unavailable"}`))
				return
			}
			defer resp.Body.Close()
			w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
		})
	}

	// Wrap file server to serve custom 404.html if it exists
	notif404File := filepath.Join(absOut, "404.html")
	custom404, _ := os.ReadFile(notif404File)

	// Scan for dynamic routes ([param] directories) in the output
	dynRoutes := findDynamicRoutes(absOut)
	if len(dynRoutes) > 0 {
		fmt.Printf("  %s⚡%s Dynamic routes: %d patterns\n", cCyan, cReset, len(dynRoutes))
	}

	handlerWith404 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hashed assets (styles.<hash>.css, index.<hash>.js, chunks/*.<hash>.js,
		// assets/<name>-<hash>.*) are content-addressed, so they can be cached
		// immutably. HTML must never be, since it changes per build.
		if isHashedAsset(r.URL.Path) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else if strings.HasSuffix(r.URL.Path, ".html") || strings.HasSuffix(r.URL.Path, "/") || r.URL.Path == "" {
			w.Header().Set("Cache-Control", "no-cache")
		}

		// A concrete static file beats any dynamic [param] pattern: /items/alpha
		// must resolve to the static page, not the /items/[id] template.
		if !staticRouteExists(absOut, r.URL.Path) {
			// Check dynamic routes — if a URL matches a [param] pattern, serve the template
			for _, dr := range dynRoutes {
				if staticOnlyRoutes[normalizeRoutePattern(dr.pattern)] {
					// Static-only route: valid params were baked at build time and
					// the fallback template was not emitted. Unknown params 404.
					continue
				}
				if params, ok := matchDynamicRoute(r.URL.Path, dr.pattern); ok {
					templatePath := filepath.Join(dr.dir, "index.html")
					templateHTML, err := os.ReadFile(templatePath)
					if err != nil {
						break
					}
					pageHTML := applyDynamicRouteParams(string(templateHTML), params)

					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.WriteHeader(200)
					_, _ = w.Write([]byte(pageHTML))
					return
				}
			}
		}

		if len(custom404) == 0 {
			fileServer.ServeHTTP(w, r)
			return
		}
		// Buffer the response so we can replace Go's default "404 page not found"
		buf := &captureResponse{status: http.StatusOK}
		fileServer.ServeHTTP(buf, r)
		if buf.status == http.StatusNotFound {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(custom404)
			return
		}
		// Non-404: forward the buffered response
		for k, v := range buf.header {
			w.Header()[k] = v
		}
		w.WriteHeader(buf.status)
		body := buf.body.Bytes()
		_, _ = w.Write(body)
	})

	// Dev-only live-reload SSE + overlay endpoints. The hub broadcasts every
	// event to all connected tabs and retains the latest build state.
	if hub != nil {
		mux.HandleFunc("/__krate/hotreload", func(w http.ResponseWriter, r *http.Request) {
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "streaming unsupported", http.StatusInternalServerError)
				return
			}
			// SSE is long-lived: extend the write deadline so the server's
			// WriteTimeout doesn't kill an idle stream.
			setStreamingDeadline(w, 24*time.Hour)
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
			flusher.Flush()

			ch, cancel := hub.Subscribe()
			defer cancel()

			// Catch a late subscriber up with the current build state, so a tab
			// opened after a failed initial build still shows the error.
			if cur, ok := hub.Current(); ok && len(cur.Diagnostics) > 0 {
				writeBuildError(w, cur)
				flusher.Flush()
			}

			// Heartbeat keeps intermediaries from idle-closing the stream.
			heartbeat := time.NewTicker(25 * time.Second)
			defer heartbeat.Stop()

			for {
				select {
				case ev, ok := <-ch:
					if !ok {
						return
					}
					routes := ev.Routes

					// Invalidate SSR renderer cache for changed pages
					if ssrStarted && len(routes) > 0 {
						for _, route := range routes {
							page := ssr.findPage(route)
							if page != nil && page.Source != "" {
								invBody, _ := json.Marshal(map[string]string{
									"route":      route,
									"source":     page.Source,
									"bundlePath": page.BundlePath,
								})
								resp, err := sidecarClient.Post(
									fmt.Sprintf("http://localhost:%d/__krate/ssr/invalidate", ssrPort),
									"application/json",
									bytes.NewReader(invBody),
								)
								if err == nil {
									resp.Body.Close()
								}
							}
						}
					}

					switch {
					case ev.Type == "client-error" && ev.ClientError != nil:
						data, _ := json.Marshal(ev)
						fmt.Fprintf(w, "event: client-error\ndata: %s\n\n", data)
					case len(ev.Diagnostics) > 0:
						// Build errors take priority: tell the client to show
						// them instead of reloading (a reload would show stale
						// output).
						writeBuildError(w, ev)
					case len(routes) > 0:
						// Partial reload: send affected page routes
						data := `{"pages":[`
						for i, r := range routes {
							if i > 0 {
								data += ","
							}
							data += `"` + r + `"`
						}
						data += `]}`
						fmt.Fprintf(w, "event: reload\ndata: %s\n\n", data)
					default:
						fmt.Fprintf(w, "event: reload\ndata: {}\n\n")
					}
					flusher.Flush()
				case <-heartbeat.C:
					fmt.Fprintf(w, ": ping\n\n")
					flusher.Flush()
				case <-r.Context().Done():
					return
				}
			}
		})

		// Runtime errors reported by the browser. Logged to the terminal and
		// rebroadcast to every tab so multi-tab sessions stay in sync.
		mux.HandleFunc("/__krate/client-error", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var ce ClientError
			if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&ce); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if ce.Message != "" {
				loc := ce.URL
				if ce.Line > 0 {
					loc = fmt.Sprintf("%s:%d:%d", ce.URL, ce.Line, ce.Col)
				}
				fmt.Fprintf(os.Stderr, "%s[browser]%s %s %s\n", cRed, cReset, ce.Message, loc)
			}
			hub.Publish(DevEvent{Type: "client-error", ClientError: &ce})
			w.WriteHeader(http.StatusNoContent)
		})

		// Status for the dev toolbar: fanned-out build state without SSE.
		mux.HandleFunc("/__krate/status", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if cur, ok := hub.Current(); ok {
				_ = json.NewEncoder(w).Encode(cur)
				return
			}
			_ = json.NewEncoder(w).Encode(DevEvent{Type: "status", BuildOK: true})
		})

		// Open a file in the developer's editor. The path is validated against
		// the project root so the endpoint cannot be used to launch arbitrary
		// files.
		mux.HandleFunc("/__krate/open", func(w http.ResponseWriter, r *http.Request) {
			file := r.URL.Query().Get("file")
			line := r.URL.Query().Get("line")
			rel, ok := safeProjectPath(root, file)
			if !ok {
				http.Error(w, "invalid path", http.StatusBadRequest)
				return
			}
			if err := openInEditor(cfg.DevServer.Editor, rel, line); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}

	// SSR/ISR/Streaming page handler — proxies dynamic pages to the Node.js renderer
	var ssrPageHandler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ssrStarted {
			handlerWith404.ServeHTTP(w, r)
			return
		}

		route := strings.TrimRight(r.URL.Path, "/")
		if route == "" {
			route = "/"
		}

		// Match the URL against route patterns to extract dynamic params (e.g.
		// [id]). For a dynamic-route page the shell and the region render live
		// under the CANONICAL pattern route (e.g. "video/[id]/index.html" +
		// manifest route "/video/[id]"), not the concrete URL — the sidecar
		// resolves the pattern and keys ISR cache variants by params. So the
		// canonical route is what everything downstream uses.
		page, params := ssr.FindPageForRoute(route)

		// Request-time pages (ssr/isr/streaming) MUST go through the sidecar
		// even when a concrete static file was baked at build time — a
		// pre-generated ISR variant (generateStaticParams) or a streaming shell
		// still needs request-time rendering/revalidation. Serving the static
		// file would freeze it at the build timestamp forever (no revalidate,
		// no fresh region content). Only pages that are NOT request-time (SSG /
		// plain static) give precedence to their concrete static file over a
		// sibling dynamic [param] template.
		if shouldServeStatic(absOut, r.URL.Path, page) {
			handlerWith404.ServeHTTP(w, r)
			return
		}

		if page == nil {
			// Not an SSR/ISR/streaming page (SSG or unknown).
			handlerWith404.ServeHTTP(w, r)
			return
		}
		isISR := page.Mode == RenderISR.String()
		if page.Route != "" {
			route = page.Route
		}
		if params == nil {
			params = make(map[string]string)
		}

		headers := make(map[string]string)
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}

		query := make(map[string]string)
		for k, v := range r.URL.Query() {
			if len(v) > 0 {
				query[k] = v[0]
			}
		}

		// All SSR/ISR/streaming pages now use the static-first assembly: the
		// shell is baked with splice markers (component regions for streaming
		// pages, one coarse "page" region for SSR/ISR pages) and the sidecar
		// renders each region. Set cache headers up front, then stream the
		// assembled shell+regions.
		flusher, _ := w.(http.Flusher)

		if isISR {
			// ISR responses are cacheable. The cache variant is encoded in the
			// URL (path params + query), so s-maxage + stale-while-revalidate
			// let browsers/CDNs hold HTML while the sidecar refreshes stale
			// entries in the background.
			reval := ssr.GetRevalidate(route)
			if reval <= 0 {
				reval = defaultISRRevalidate
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", fmt.Sprintf("public, s-maxage=%d, stale-while-revalidate=%d", reval, 2*reval))
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "no-store")
		}

		rr := streamRegionPage(w, flusher, absOut, route, ssrPort, params, query, headers)
		if rr.served {
			if rr.renderErr != "" {
				// The page-region render failed; the baked body was served as a
				// graceful fallback. Log it so the failure is visible.
				fmt.Fprintf(os.Stderr, "  %sPage region error (%s):%s %s\n", cYellow, route, cReset, rr.renderErr)
				w.Header().Set("X-Krate-Error", "render")
			}
			// Apply the page-region cache status header (ISR hit/stale/miss).
			if isISR && rr.cacheStatus != "" {
				switch rr.cacheStatus {
				case "stale":
					w.Header().Set("X-Krate-Cache", "STALE")
				case "hit":
					w.Header().Set("X-Krate-Cache", "HIT")
				default:
					w.Header().Set("X-Krate-Cache", "MISS")
				}
			}
			if rr.notFound {
				if len(custom404) > 0 {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.Header().Del("Cache-Control")
					w.WriteHeader(404)
					_, _ = w.Write(custom404)
				} else {
					http.NotFound(w, r)
				}
				return
			}
			if rr.redirect != "" {
				http.Redirect(w, r, rr.redirect, http.StatusFound)
				return
			}
			return
		}

		// No region markers in the shell: this page is fully static (its body
		// was baked with nothing request-time to render — e.g. a page the global
		// streaming override marked dynamic but that contains no dynamic
		// regions). Serve the baked shell directly; there is nothing to fetch
		// from the sidecar.
		relPath := strings.TrimPrefix(route, "/")
		if relPath == "" {
			relPath = "index.html"
		} else {
			relPath = relPath + "/index.html"
		}
		if data, err := os.ReadFile(filepath.Join(absOut, relPath)); err == nil {
			_, _ = w.Write(data)
			return
		}
		handlerWith404.ServeHTTP(w, r)
	})

	// Redirect/rewrite middleware — applies config-based URL transformations
	var redirectRewriteHandler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Check redirects first
		for _, rd := range cfg.Redirects {
			if matchRedirect(path, rd.Source) {
				dest := rewriteDestination(path, rd.Source, rd.Destination)
				status := http.StatusFound
				if rd.Permanent {
					status = http.StatusMovedPermanently
				}
				http.Redirect(w, r, dest, status)
				return
			}
		}

		// Check rewrites (internal path mapping, no redirect)
		for _, rw := range cfg.Rewrites {
			if matchRedirect(path, rw.Source) {
				rewritten := rewriteDestination(path, rw.Source, rw.Destination)
				r.URL.Path = rewritten
				r.URL.RawPath = ""
				break
			}
		}

		ssrPageHandler.ServeHTTP(w, r)
	})

	// User middleware handler — calls middleware.ts via embedded quickjs or sidecar
	middlewareFile := filepath.Join(root, ".krate", "middleware.js")
	var middlewareHandler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip __krate internal endpoints and API routes
		if strings.HasPrefix(r.URL.Path, "/__krate/") || strings.HasPrefix(r.URL.Path, "/api/") {
			redirectRewriteHandler.ServeHTTP(w, r)
			return
		}

		if _, err := os.Stat(middlewareFile); os.IsNotExist(err) {
			redirectRewriteHandler.ServeHTTP(w, r)
			return
		}

		// Use embedded quickjs runtime if available
		if middlewareRT != nil {
			reqHeaders := make(map[string]string)
			for k, v := range r.Header {
				reqHeaders[k] = strings.Join(v, ", ")
			}
			result := middlewareRT.Execute(jsruntime.MiddlewareRequest{
				URL:     fmt.Sprintf("http://localhost:%d%s", port, r.URL.RequestURI()),
				Method:  r.Method,
				Path:    r.URL.Path,
				Headers: reqHeaders,
			})

			if result.Error != "" {
				fmt.Fprintf(os.Stderr, "  %sMiddleware error:%s %s\n", cYellow, cReset, result.Error)
				redirectRewriteHandler.ServeHTTP(w, r)
				return
			}

			if result.Status > 0 && result.Status != 200 {
				for k, v := range result.Headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(result.Status)
				if result.Body != "" {
					_, _ = w.Write([]byte(result.Body))
				}
				return
			}

			if result.Headers != nil {
				for k, v := range result.Headers {
					w.Header().Set(k, v)
				}
			}

			redirectRewriteHandler.ServeHTTP(w, r)
			return
		}

		// Fallback: call middleware via sidecar
		fullURL := fmt.Sprintf("http://localhost:%d%s", port, r.URL.RequestURI())
		middlewareBody, _ := json.Marshal(map[string]interface{}{
			"url":    fullURL,
			"method": r.Method,
			"path":   r.URL.Path,
		})
		middlewareURL := fmt.Sprintf("http://localhost:%d/__krate/middleware", apiPort)
		resp, err := sidecarClient.Post(middlewareURL, "application/json", bytes.NewReader(middlewareBody))
		if err != nil {
			// Middleware unavailable, continue
			redirectRewriteHandler.ServeHTTP(w, r)
			return
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)
		var middlewareResult struct {
			Action  string            `json:"action"`
			Status  int               `json:"status"`
			Headers map[string]string `json:"headers"`
			Body    string            `json:"body"`
			Error   string            `json:"error"`
		}
		if err := json.Unmarshal(respBody, &middlewareResult); err != nil {
			redirectRewriteHandler.ServeHTTP(w, r)
			return
		}

		// If middleware returned an error, log and continue
		if middlewareResult.Error != "" {
			fmt.Fprintf(os.Stderr, "  %sMiddleware error:%s %s\n", cYellow, cReset, middlewareResult.Error)
			redirectRewriteHandler.ServeHTTP(w, r)
			return
		}

		// If middleware returned a response (redirect, rewrite, custom response)
		if middlewareResult.Status > 0 && middlewareResult.Status != 200 {
			for k, v := range middlewareResult.Headers {
				w.Header().Set(k, v)
			}
			w.WriteHeader(middlewareResult.Status)
			if middlewareResult.Body != "" {
				_, _ = w.Write([]byte(middlewareResult.Body))
			}
			return
		}

		// Apply middleware headers but continue to page handler
		if middlewareResult.Headers != nil {
			for k, v := range middlewareResult.Headers {
				w.Header().Set(k, v)
			}
		}

		redirectRewriteHandler.ServeHTTP(w, r)
	})

	// Final handler chain: userMiddleware -> redirectRewrite -> logging -> ssrPageHandler.
	// When community plugins with serve hooks are configured, wrap the chain in
	// the plugin serve handler (ServeRequest interceptor + ServeResponse buffer).
	var top http.Handler = middlewareHandler
	if len(cfg.Plugins) > 0 {
		top = wirePluginServeHandlers(root, cfg, top)
	}
	if hub == nil {
		// Preview/static serving: gzip compressible assets.
		top = gzipMiddleware(top)
	}
	mux.Handle("/", bodyLimitMiddleware(cfg.MaxBodyBytes(), loggingMiddleware(top)))

	// Health/readiness endpoints. /healthz is liveness (always 200 while the
	// process serves); /readyz additionally requires the SSR sidecar when one
	// was expected.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if ssrStarted && !ssr.IsRunning() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "ssr-unavailable\n")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ready\n")
	})

	// Wrap the mux so every route (including /api/, /__krate/, health) gets
	// request IDs, security headers, and panic recovery.
	var rootHandler http.Handler = requestIDMiddleware(basePathMiddleware(cfg.BaseURLPath(), securityHeadersMiddleware(cfg, corsMiddleware(cfg, panicRecoveryMiddleware(Logger, mux)))))

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("listen on :%d: %w", port, err)
	}

	addr := listener.Addr().(*net.TCPAddr)
	label := "dev"
	if hub == nil {
		label = "serve"
	}
	fmt.Printf("%s  %s server → %shttp://localhost:%d%s %s(started in %s)%s\n", cGreen, label, cCyan, addr.Port, cReset, cGray, time.Since(startTime).Round(time.Millisecond), cReset)

	// Start ISR background revalidation — one timer per page, each route
	// revalidating on its own cadence (previously every ISR page was revalidated
	// together on the shortest interval, stampeding every request at once). The
	// refresh is non-destructive: the sidecar re-renders each cached variant in
	// place so dynamic variants are never evicted. The sidecar also serves stale
	// HTML while a revalidation runs, so expiry never blocks a request even
	// before the timer fires.
	var isrWg sync.WaitGroup
	isrDone := make(chan struct{})
	if ssrStarted {
		isrPages := ssr.GetISRPages()
		if len(isrPages) > 0 {
			fmt.Printf("  %s⚡%s ISR revalidation: %d pages\n", cCyan, cReset, len(isrPages))
			for _, p := range isrPages {
				page := p
				isrWg.Add(1)
				go func() {
					defer isrWg.Done()
					interval := time.Duration(page.Revalidate) * time.Second
					if interval <= 0 {
						interval = defaultISRRevalidate * time.Second
					}
					ticker := time.NewTicker(interval)
					defer ticker.Stop()
					for {
						if !ssr.IsRunning() {
							return
						}
						if err := ssr.RefreshPage(page.Route); err != nil {
							fmt.Fprintf(os.Stderr, "  %sISR revalidation failed (%s):%s %v\n", cYellow, page.Route, cReset, err)
						}
						select {
						case <-ticker.C:
						case <-isrDone:
							return
						}
					}
				}()
			}
		}
	}

	// Graceful shutdown: handle SIGINT and SIGTERM (containers send SIGTERM),
	// stop sidecars, then drain with a bounded deadline so a stalled/streaming
	// connection can't hang shutdown forever.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	httpServer := newHTTPServer(rootHandler)
	isrCloseOnce := sync.Once{}

	go func() {
		<-sigCh
		fmt.Printf("\n%sShutting down...%s\n", cGray, cReset)
		// Stop ISR revalidation goroutine
		isrCloseOnce.Do(func() { close(isrDone) })
		if ssrStarted {
			ssr.Stop()
			fmt.Printf("  %s✓%s SSR renderer stopped\n", cGreen, cReset)
		}
		if goAPI != nil {
			goAPI.Close()
			fmt.Printf("  %s✓%s Go API sidecar stopped\n", cGreen, cReset)
		}
		if customSidecar != nil {
			customSidecar.Stop()
			fmt.Printf("  %s✓%s API sidecar stopped\n", cGreen, cReset)
		}
		ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "  %s⚠ graceful shutdown timed out, forcing close:%s %v\n", cYellow, cReset, err)
			_ = httpServer.Close()
		}
	}()

	if cfg.DevServer.Open {
		openBrowser(fmt.Sprintf("http://localhost:%d", addr.Port))
	}

	defer plugin.CloseGoPlugins()

	return httpServer.Serve(listener)
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Flush() {
	if f, ok := lrw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (lrw *loggingResponseWriter) Unwrap() http.ResponseWriter {
	return lrw.ResponseWriter
}

// captureResponse buffers the full response so we can inspect and replace it.
type captureResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (cr *captureResponse) Header() http.Header {
	if cr.header == nil {
		cr.header = make(http.Header)
	}
	return cr.header
}

func (cr *captureResponse) Write(b []byte) (int, error) {
	return cr.body.Write(b)
}

func (cr *captureResponse) WriteHeader(code int) {
	cr.status = code
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		if r.URL.Path == "/__krate/hotreload" {
			next.ServeHTTP(w, r)
			return
		}

		lrw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(lrw, r)

		// Request line rendered by prettyHandler. The request ID is attached by
		// the outer requestIDMiddleware so logs can be correlated with responses;
		// it is emitted as an attribute for the structured fallback path.
		Logger.Info("request",
			"id", requestIDFrom(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", lrw.statusCode,
			"duration", time.Since(start),
		)
	})
}

func copyDirToOut(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}

func openBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}

	proc, err := os.StartProcess(cmd, args, &os.ProcAttr{
		Files: []*os.File{nil, nil, nil},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sWarning: could not open browser: %v%s\n", cYellow, err, cReset)
	} else {
		_ = proc.Release()
	}
}

// matchRedirect checks if a request path matches a redirect/rewrite source pattern.
// Supports exact match and wildcard suffix (e.g. "/old/*").
func matchRedirect(path, source string) bool {
	if strings.HasSuffix(source, "/*") {
		prefix := strings.TrimSuffix(source, "/*")
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
	return path == source
}

// rewriteDestination replaces wildcards and path segments in the destination.
func rewriteDestination(path, source, destination string) string {
	if strings.HasSuffix(source, "/*") {
		prefix := strings.TrimSuffix(source, "/*")
		suffix := strings.TrimPrefix(path, prefix)
		if strings.Contains(destination, ":splat") {
			return strings.ReplaceAll(destination, ":splat", suffix)
		}
		return destination + suffix
	}
	return destination
}

// staticRouteExists reports whether the output dir holds a concrete static
// file for the URL path — in which case dynamic [param] routing must give way.
func staticRouteExists(absOut, urlPath string) bool {
	up, err := url.PathUnescape(urlPath)
	if err != nil {
		up = urlPath
	}
	rel := strings.Trim(strings.TrimPrefix(up, "/"), "/")
	if rel == "" {
		rel = "index.html"
	} else if strings.HasSuffix(rel, "/") {
		rel = rel + "index.html"
	}
	candidates := []string{
		filepath.Join(absOut, filepath.FromSlash(rel)),               // literal file
		filepath.Join(absOut, filepath.FromSlash(rel), "index.html"), // folder route
	}
	for _, cand := range candidates {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

// shouldServeStatic reports whether a request should be answered from the
// baked static build instead of the request-time (sidecar) pipeline.
//
// A concrete static file always wins for SSG pages (or unmatched routes) so it
// can shadow a sibling dynamic [param] template. But a page the manifest
// registers as ssr/isr/streaming MUST NOT be served statically even when the
// build baked a concrete file for it — a pre-generated ISR variant
// (generateStaticParams) or a streaming shell still needs request-time
// rendering/revalidation, and serving the baked bytes would freeze the page at
// its build timestamp (no revalidate, no fresh region content).
func shouldServeStatic(absOut, urlPath string, page *ManifestPage) bool {
	if page != nil && page.Mode != RenderSSG.String() {
		return false
	}
	return staticRouteExists(absOut, urlPath)
}

// dynamicRoute represents a URL pattern with [param] segments found in the output directory.
type dynamicRoute struct {
	pattern string // e.g. "video/[id]"
	dir     string // absolute path to the directory, e.g. dist/video/[id]
}

// loadStaticOnlyRoutes reads dist/manifest.json and returns the set of dynamic
// route patterns whose params are closed (static-only). Keys are normalized
// ("/blog/[slug]" and "blog/[slug]" are equivalent).
func loadStaticOnlyRoutes(absOut string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(absOut, "manifest.json"))
	if err != nil {
		return out
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return out
	}
	for _, r := range m.StaticOnlyRoutes {
		out[normalizeRoutePattern(r)] = true
	}
	return out
}

// normalizeRoutePattern canonicalizes a route/pattern for comparison: leading
// slash, no trailing slash.
func normalizeRoutePattern(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	return p
}

// findDynamicRoutes scans the output directory for directories containing [param]
// segments and returns them as dynamic route patterns. Only directories with
// index.html are included (leaf route templates).
func findDynamicRoutes(absOut string) []dynamicRoute {
	var routes []dynamicRoute
	_ = filepath.Walk(absOut, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		base := info.Name()
		if strings.Contains(base, "[") && strings.Contains(base, "]") {
			// Only include directories that have an index.html (actual page templates)
			indexFile := filepath.Join(path, "index.html")
			if _, statErr := os.Stat(indexFile); statErr == nil {
				rel, err := filepath.Rel(absOut, path)
				if err == nil {
					routes = append(routes, dynamicRoute{
						pattern: filepath.ToSlash(rel),
						dir:     path,
					})
				}
			}
		}
		return nil
	})
	return routes
}

// matchDynamicRoute checks if a URL path matches a dynamic route pattern and returns params.
// e.g. matchDynamicRoute("/video/abc123", "video/[id]") returns {id: "abc123"}, true
func matchDynamicRoute(urlPath, pattern string) (map[string]string, bool) {
	urlParts := strings.Split(strings.Trim(urlPath, "/"), "/")
	patParts := strings.Split(strings.Trim(pattern, "/"), "/")
	if len(urlParts) != len(patParts) {
		return nil, false
	}
	params := make(map[string]string)
	for i, pp := range patParts {
		if strings.HasPrefix(pp, "[") && strings.HasSuffix(pp, "]") {
			params[pp[1:len(pp)-1]] = urlParts[i]
		} else if pp != urlParts[i] {
			return nil, false
		}
	}
	return params, true
}

// applyDynamicRouteParams renders a built dynamic-route template for one
// request: it injects the matched params for client-side access and replaces
// every build-time sentinel (see dynamicParamSentinel) with the matched URL
// segment. Because the template is built with sentinels wherever the param is
// read — body text, <title>, and meta attributes — a single replacement pass
// covers the whole page. Values are HTML-escaped, which is correct in both
// text and double-quoted attribute contexts.
func applyDynamicRouteParams(templateHTML string, params map[string]string) string {
	paramsJSON, _ := json.Marshal(params)
	injectScript := "<script>window.__KRATE_PARAMS__=" + string(paramsJSON) + "</script>"
	out := strings.Replace(templateHTML, "</head>", injectScript+"</head>", 1)
	for name, paramValue := range params {
		out = strings.ReplaceAll(out, dynamicParamSentinel(name), html.EscapeString(paramValue))
	}
	return out
}
