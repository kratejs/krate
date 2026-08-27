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
	"time"

	"krate-compiler/internal/config"
	"krate-compiler/internal/jsruntime"
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

func colorStatus(code int) string {
	switch {
	case code >= 200 && code < 300:
		return fmt.Sprintf("%s%d%s", cGreen, code, cReset)
	case code >= 300 && code < 400:
		return fmt.Sprintf("%s%d%s", cCyan, code, cReset)
	case code >= 400 && code < 500:
		return fmt.Sprintf("%s%d%s", cYellow, code, cReset)
	default:
		return fmt.Sprintf("%s%d%s", cRed, code, cReset)
	}
}

// wrapInPageShell reads the static HTML shell from dist and injects the renderer's
// dynamic content (title + body) into it. This gives SSR/ISR/streaming pages the
// full page structure (layout, CSS, nav, footer, scripts) instead of raw fragments.
//
// The renderer returns: <title>...</title><div class="card">...</div>
// The static HTML has:  <head>...<title>...</title>...</head><body>...<main>...</main>...</body>
// We replace the title in <head> and the content inside <main>.
func wrapInPageShell(absOut, route, dynamicHTML string) string {
	prefix, suffix := splitPageShell(absOut, route, dynamicHTML, "", "")
	if prefix == "" {
		return dynamicHTML
	}
	return prefix + dynamicHTML + suffix
}

// serveStaticRuntimePage serves a streaming page from its build-time static
// HTML (server components frozen) and resolves the runtime component krate-id
// placeholders via the embedded QuickJS runtime. Returns true if the page was
// served this way; false if it has no static HTML or no runtime placeholders
// (in which case the caller falls back to the renderer proxy).
func serveStaticRuntimePage(w http.ResponseWriter, flusher http.Flusher, absOut, route string, runtimeCompRT *jsruntime.RuntimeComponentRuntime) bool {
	relPath := strings.TrimPrefix(route, "/")
	if relPath == "" {
		relPath = "index.html"
	} else {
		relPath = relPath + "/index.html"
	}
	data, err := os.ReadFile(filepath.Join(absOut, relPath))
	if err != nil {
		return false
	}
	html := string(data)

	// Runtime placeholders are `<div krate-id="N"></div>` backed by a
	// `<script type="application/krate-runtime">{id: {__krate_component, ...}}`
	propsRe := regexp.MustCompile(`<script type="application/krate-runtime">([\s\S]*?)</script>`)
	m := propsRe.FindStringSubmatch(html)
	if m == nil {
		return false
	}
	if runtimeCompRT == nil {
		// Runtime components can't be resolved without the QuickJS bundles.
		return false
	}
	var propsByID map[string]map[string]any
	if err := json.Unmarshal([]byte(m[1]), &propsByID); err != nil {
		return false
	}
	if len(propsByID) == 0 {
		return false
	}

	// HTML is minified, so the placeholder attribute may be quoted or not:
	// `<div krate-id="0"></div>` or `<div krate-id=0></div>`.
	idRe := regexp.MustCompile(`<div[^>]*krate-id="?(\d+)"?[^>]*></div>`)
	html = idRe.ReplaceAllStringFunc(html, func(placeholder string) string {
		sub := idRe.FindStringSubmatch(placeholder)
		if len(sub) < 2 {
			return placeholder
		}
		id := sub[1]
		entry, ok := propsByID[id]
		if !ok {
			return placeholder
		}
		name, _ := entry["__krate_component"].(string)
		if name == "" {
			return placeholder
		}
		props := make(map[string]any, len(entry))
		for k, v := range entry {
			if k != "__krate_component" {
				props[k] = v
			}
		}
		propsJSON, _ := json.Marshal(props)
		res := runtimeCompRT.RenderComponent(name, string(propsJSON))
		if res.Error != "" {
			fmt.Fprintf(os.Stderr, "  %sRuntime component error (%s):%s %s\n", cYellow, name, cReset, res.Error)
			return placeholder
		}
		return `<div krate-id="` + id + `">` + res.HTML + `</div>`
	})

	w.Write([]byte(html))
	if flusher != nil {
		flusher.Flush()
	}
	return true
}

// splitPageShell splits a static page into prefix (up to <main>) and suffix (after </main>).
// Returns ("", "") if the static file can't be read.
// If stylesheet is provided, generates a minimal shell when the static file is missing.
func splitPageShell(absOut, route, dynamicHTML string, stylesheet string, runtimeJSFile string) (prefix, suffix string) {
	relPath := strings.TrimPrefix(route, "/")
	if relPath == "" {
		relPath = "index.html"
	} else {
		relPath = relPath + "/index.html"
	}
	staticPath := filepath.Join(absOut, relPath)
	staticBytes, err := os.ReadFile(staticPath)
	if err != nil {
		// Generate minimal shell from stylesheet when no pre-built HTML exists
		if stylesheet != "" {
			prefix = "<!DOCTYPE html><html lang=en><head><meta charset=UTF-8><meta name=viewport content=\"width=device-width, initial-scale=1.0\"><title>Krate App</title><link rel=stylesheet href=/" + stylesheet + "></head><body><div id=root><main>"
			suffix = "</main></div>"
			if runtimeJSFile != "" {
				suffix += "<script src=\"/" + runtimeJSFile + "\"></script>"
			}
			suffix += "</body></html>"
			return prefix, suffix
		}
		return "", ""
	}
	shell := string(staticBytes)

	titleRe := regexp.MustCompile(`<title>([^<]*)</title>`)
	titleMatch := titleRe.FindStringSubmatch(dynamicHTML)
	if len(titleMatch) > 0 {
		shell = titleRe.ReplaceAllString(shell, titleMatch[0])
	}

	mainRe := regexp.MustCompile(`(?s)<main>(.*?)</main>`)
	loc := mainRe.FindStringIndex(shell)
	if loc == nil {
		return "", ""
	}
	prefix = shell[:loc[0]] + "<main>"
	suffix = "</main>" + shell[loc[1]:]
	return prefix, suffix
}

// ServeDev starts an HTTP server with live reload SSE + request logging.
func ServeDev(root string, cfg *config.Config, reload <-chan []string, startTime time.Time) error {
	return serve(root, cfg, reload, startTime)
}

// Serve starts an HTTP server with request logging (production preview).
func Serve(root string, cfg *config.Config, startTime time.Time) error {
	return serve(root, cfg, nil, startTime)
}

func serve(root string, cfg *config.Config, reload <-chan []string, startTime time.Time) error {
	port := cfg.DevServer.Port
	if port == 0 {
		port = 3000
	}
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

	// Load embedded API route runtime if configured (default: quickjs)
	var apiRT *jsruntime.APIRouteRuntime
	apiRTOption := strings.ToLower(cfg.SSR.APIRuntime)
	if apiRTOption == "" || apiRTOption == "quickjs" {
		apiDir := filepath.Join(cfg.OutDir, "api")
		if _, err := os.Stat(apiDir); err == nil {
			apiRT = jsruntime.NewAPIRouteRuntime(apiDir)
		}
	}

	// Runtime component renderer (fills krate-id placeholders on SSG pages that
	// contain *.runtime.tsx components). Server components are baked at build
	// time; only these placeholders are resolved at request time.
	var runtimeCompRT *jsruntime.RuntimeComponentRuntime
	compDir := filepath.Join(cfg.OutDir, "server-components")
	if _, err := os.Stat(compDir); err == nil {
		runtimeCompRT = jsruntime.NewRuntimeComponentRuntime(compDir)
	}

	sidecarURL, _ := url.Parse(fmt.Sprintf("http://localhost:%d", apiPort))
	apiProxy := httputil.NewSingleHostReverseProxy(sidecarURL)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
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
				b, _ := io.ReadAll(r.Body)
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
				w.Write([]byte(result.Body))
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
	ssr := NewSSRServer(root, ssrPort)
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
			resp, err := http.DefaultClient.Do(proxyReq)
			if err != nil {
				w.WriteHeader(502)
				w.Write([]byte(`{"error":"SSR renderer unavailable"}`))
				return
			}
			defer resp.Body.Close()
			w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
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
		// Check dynamic routes first — if a URL matches a [param] pattern, serve the template
		for _, dr := range dynRoutes {
			if params, ok := matchDynamicRoute(r.URL.Path, dr.pattern); ok {
				templatePath := filepath.Join(dr.dir, "index.html")
				templateHTML, err := os.ReadFile(templatePath)
				if err != nil {
					break
				}
				pageHTML := string(templateHTML)

				// Inject params as a script tag before </head> for client-side access
				paramsJSON, _ := json.Marshal(params)
				injectScript := "<script>window.__KRATE_PARAMS__=" + string(paramsJSON) + "</script>"
				pageHTML = strings.Replace(pageHTML, "</head>", injectScript+"</head>", 1)

				// Replace signal-bound text nodes with actual param values.
				// The build renders [param] pages with placeholder values (e.g. "unknown").
				// We replace those with the actual matched param values so the page
				// displays correctly even before hydration runs.
				for _, paramValue := range params {
					pageHTML = strings.ReplaceAll(pageHTML, ">unknown<", ">"+html.EscapeString(paramValue)+"<")
				}

				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(200)
				w.Write([]byte(pageHTML))
				return
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
			w.Write(custom404)
			return
		}
		// Non-404: forward the buffered response
		for k, v := range buf.header {
			w.Header()[k] = v
		}
		w.WriteHeader(buf.status)
		body := buf.body.Bytes()
		w.Write(body)
	})

	// SSE endpoint for live reload (dev mode only)
	if reload != nil {
		mux.HandleFunc("/__krate/hotreload", func(w http.ResponseWriter, r *http.Request) {
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "streaming unsupported", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
			flusher.Flush()

			for {
				select {
				case routes, ok := <-reload:
					if !ok {
						return
					}

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
								http.Post(
									fmt.Sprintf("http://localhost:%d/__krate/ssr/invalidate", ssrPort),
									"application/json",
									bytes.NewReader(invBody),
								)
							}
						}
					}

					if len(routes) > 0 {
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
					} else {
						fmt.Fprintf(w, "event: reload\ndata: {}\n\n")
					}
					flusher.Flush()
				case <-r.Context().Done():
					return
				}
			}
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
		isSSR := ssr.IsSSRPage(route)
		isISR := ssr.IsISRPage(route)
		isStreaming := ssr.IsStreamingPage(route)

		if !isSSR && !isISR && !isStreaming {
			handlerWith404.ServeHTTP(w, r)
			return
		}

		headers := make(map[string]string)
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}

		// Match URL against route patterns to extract dynamic params (e.g., [id])
		_, params := ssr.FindPageForRoute(route)
		if params == nil {
			params = make(map[string]string)
		}

		query := make(map[string]string)
		for k, v := range r.URL.Query() {
			if len(v) > 0 {
				query[k] = v[0]
			}
		}

		// Streaming SSR: pipe chunked response from renderer via raw TCP
		// Manually parse HTTP response and chunked encoding to avoid any buffering.
		if isStreaming {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(200)
			flusher, ok := w.(http.Flusher)

			// Pages that mix server components (baked at build time) with runtime
			// components are served from their static HTML, and only the runtime
			// component placeholders are resolved at request time. This keeps
			// server components frozen instead of re-rendering the whole page.
			if served := serveStaticRuntimePage(w, flusher, absOut, route, runtimeCompRT); served {
				return
			}

			// Pre-split the page shell so we can stream chunks between prefix/suffix
			prefix, suffix := splitPageShell(absOut, route, "", ssr.GetStylesheet(), ssr.GetRuntimeJS())

			// Send shell prefix (HTML up to <main>) immediately
			if prefix != "" {
				w.Write([]byte(prefix))
				if ok {
					flusher.Flush()
				}
			}

			// Open raw TCP connection to Node.js renderer
			conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", ssrPort), 5*time.Second)
			if dialErr != nil {
				w.Write([]byte("<!-- SSR unavailable -->"))
				if suffix != "" {
					w.Write([]byte(suffix))
				}
				return
			}
			defer conn.Close()

			// Send HTTP POST request manually over raw TCP
			reqBody, _ := json.Marshal(map[string]interface{}{
				"route":   route,
				"url":     r.URL.String(),
				"method":  r.Method,
				"headers": headers,
				"params":  params,
				"query":   query,
			})
			httpReq := fmt.Sprintf("POST /__krate/render HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", ssrPort, len(reqBody))
			conn.Write([]byte(httpReq))
			conn.Write(reqBody)

			// Read HTTP response status line + headers manually (no http.ReadResponse)
			tcpBuf := bufio.NewReaderSize(conn, 256)

			// Read status line (e.g., "HTTP/1.1 200 OK\r\n")
			statusLine, err := tcpBuf.ReadString('\n')
			if err != nil {
				w.Write([]byte("<!-- SSR read error -->"))
				return
			}
			_ = statusLine // We trust the renderer returns 200

			// Read headers until empty line
			isChunked := false
			for {
				line, err := tcpBuf.ReadString('\n')
				if err != nil {
					break
				}
				line = strings.TrimRight(line, "\r\n")
				if line == "" {
					break // End of headers
				}
				if strings.HasPrefix(strings.ToLower(line), "transfer-encoding:") && strings.Contains(strings.ToLower(line), "chunked") {
					isChunked = true
				}
			}

			// Buffer the full body for runtime component replacement
			var bodyBuf bytes.Buffer

			if isChunked {
				// Read chunked transfer encoding manually, one chunk at a time
				for {
					// Read chunk size line (hex digits + \r\n)
					sizeLine, err := tcpBuf.ReadString('\n')
					if err != nil {
						break
					}
					sizeLine = strings.TrimRight(sizeLine, "\r\n")

					// Parse hex chunk size
					chunkSize := 0
					fmt.Sscanf(sizeLine, "%x", &chunkSize)
					if chunkSize == 0 {
						break // Terminal chunk
					}

					// Read exactly chunkSize bytes
					chunkData := make([]byte, chunkSize)
					_, err = io.ReadFull(tcpBuf, chunkData)
					if err != nil {
						break
					}

					// Consume trailing \r\n after chunk data
					tcpBuf.ReadString('\n')

					bodyBuf.Write(chunkData)
				}
			} else {
				// Non-chunked: just read until connection close
				buf := make([]byte, 4096)
				for {
					n, readErr := tcpBuf.Read(buf)
					if n > 0 {
						bodyBuf.Write(buf[:n])
					}
					if readErr != nil {
						break
					}
				}
			}

			// Write the collected body
			body := bodyBuf.Bytes()
			w.Write(body)
			if ok {
				flusher.Flush()
			}

			// Send shell suffix (</main> + rest)
			if suffix != "" {
				w.Write([]byte(suffix))
				if ok {
					flusher.Flush()
				}
			}
			return
		}

		// SSR or ISR: proxy to renderer (renderer handles ISR cache internally)
		result, err := ssr.RenderPage(route, r.URL.String(), r.Method, headers, params, query)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %sSSR error (%s):%s %v\n", cRed, route, cReset, err)
			// Fallback: try serving static file for ISR pages
			if isISR {
				handlerWith404.ServeHTTP(w, r)
				return
			}
			handlerWith404.ServeHTTP(w, r)
			return
		}

		if result.NotFound {
			if len(custom404) > 0 {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(404)
				w.Write(custom404)
			} else {
				http.NotFound(w, r)
			}
			return
		}

		if result.Redirect != "" {
			http.Redirect(w, r, result.Redirect, http.StatusFound)
			return
		}

		if result.Status == 500 {
			errPage, _ := os.ReadFile(filepath.Join(absOut, "500.html"))
			if len(errPage) > 0 {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(500)
				w.Write(errPage)
			} else {
				http.Error(w, "Internal Server Error", 500)
			}
			return
		}

		if result.Cached {
			w.Header().Set("X-Krate-Cache", "HIT")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(result.Status)
		w.Write([]byte(wrapInPageShell(absOut, route, result.HTML)))
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
					w.Write([]byte(result.Body))
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
		resp, err := http.Post(middlewareURL, "application/json", bytes.NewReader(middlewareBody))
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
				w.Write([]byte(middlewareResult.Body))
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

	// Final handler chain: userMiddleware -> redirectRewrite -> logging -> ssrPageHandler
	mux.Handle("/", loggingMiddleware(middlewareHandler))

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("listen on :%d: %w", port, err)
	}

	addr := listener.Addr().(*net.TCPAddr)
	label := "dev"
	if reload == nil {
		label = "serve"
	}
	fmt.Printf("%s  %s server → %shttp://localhost:%d%s %s(started in %s)%s\n", cGreen, label, cCyan, addr.Port, cReset, cGray, time.Since(startTime).Round(time.Millisecond), cReset)

	// Start ISR background revalidation goroutine
	var isrWg sync.WaitGroup
	isrDone := make(chan struct{})
	if ssrStarted {
		isrPages := ssr.GetISRPages()
		if len(isrPages) > 0 {
			isrWg.Add(1)
			go func() {
				defer isrWg.Done()
				defer close(isrDone)
				fmt.Printf("  %s⚡%s ISR revalidation: %d pages\n", cCyan, cReset, len(isrPages))
				for {
					// Find the shortest revalidation interval
					minInterval := isrPages[0].Revalidate
					for _, p := range isrPages {
						if p.Revalidate < minInterval {
							minInterval = p.Revalidate
						}
					}

					// Sleep with cancellation check
					timer := time.NewTimer(time.Duration(minInterval) * time.Second)
					select {
					case <-timer.C:
						// Timer expired, revalidate
					case <-isrDone:
						timer.Stop()
						return
					}

					if !ssr.IsRunning() {
						return
					}

					// Revalidate all ISR pages
					for _, p := range isrPages {
						if err := ssr.RevalidatePage(p.Route); err != nil {
							fmt.Fprintf(os.Stderr, "  %sISR revalidation failed (%s):%s %v\n", cYellow, p.Route, cReset, err)
						}
					}
				}
			}()
		}
	}

	// Graceful shutdown: handle SIGINT/SIGTERM, stop SSR server
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	httpServer := &http.Server{Handler: mux}
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
		httpServer.Shutdown(context.Background())
	}()

	if cfg.DevServer.Open {
		openBrowser(fmt.Sprintf("http://localhost:%d", addr.Port))
	}

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

		duration := time.Since(start)
		durStr := fmt.Sprintf("%dms", duration.Milliseconds())
		if duration.Milliseconds() == 0 {
			durStr = fmt.Sprintf("%dμs", duration.Microseconds())
		}
		fmt.Printf("  %s %s %s %s %s%s%s\n",
			cGray+time.Now().Format("15:04:05")+cReset,
			r.Method,
			r.URL.Path,
			colorStatus(lrw.statusCode),
			cGray, durStr, cReset)
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
		proc.Release()
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

// dynamicRoute represents a URL pattern with [param] segments found in the output directory.
type dynamicRoute struct {
	pattern string // e.g. "video/[id]"
	dir     string // absolute path to the directory, e.g. dist/video/[id]
}

// findDynamicRoutes scans the output directory for directories containing [param]
// segments and returns them as dynamic route patterns. Only directories with
// index.html are included (leaf route templates).
func findDynamicRoutes(absOut string) []dynamicRoute {
	var routes []dynamicRoute
	filepath.Walk(absOut, func(path string, info os.FileInfo, err error) error {
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
