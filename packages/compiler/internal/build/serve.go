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
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/execx"
	"github.com/kratejs/krate/packages/compiler/internal/gohost"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
	"github.com/kratejs/krate/packages/compiler/internal/safe"
)

const apiServerScriptContent = `import http from 'node:http';
import path from 'node:path';
import fs from 'node:fs';
import { pathToFileURL } from 'node:url';
import process from 'node:process';
import cluster from 'node:cluster';
import { AsyncLocalStorage } from 'node:async_hooks';
import { randomBytes, timingSafeEqual } from 'node:crypto';

const API_DIR = path.resolve(process.argv[2] || './src/api');
const PORT = parseInt(process.argv[3] || '3001', 10);
const MIDDLEWARE_PATH = path.resolve(process.argv[4] || '');
const ACTION_DIR = path.resolve(process.argv[5] || './.krate/actions');

// In dev, route modules are re-imported with a cache-busting query so edits
// take effect; in production we rely on Node's own module cache (imported once).
const HOT = process.env.KRATE_API_HOT === '1';
// Cluster across cores so the Node API sidecar uses more than one thread.
const WORKERS = Math.max(1, parseInt(process.env.KRATE_API_WORKERS || '1', 10) || 1);

// Per-process caches so a request does no filesystem probing and no module
// re-import: URL path -> resolved route file, and file -> imported namespace.
// The module cache is bypassed in dev (HOT) so edits are picked up.
const fileCache = new Map();
const moduleCache = new Map();
` + actionSidecarJS + actionSidecarNodeAdapter + apiSidecarClusterJS + `
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

    if (urlPath === '/__krate/internal/cache-invalidate') {
      const chunks = [];
      for await (const chunk of req) chunks.push(chunk);
      const out = req.method === 'POST'
        ? await internalCacheInvalidate(req.headers['x-krate-internal'], Buffer.concat(chunks).toString())
        : { status: 405, body: { error: 'method not allowed' } };
      res.statusCode = out.status;
      res.setHeader('Content-Type', 'application/json');
      return res.end(JSON.stringify(out.body));
    }

    if (urlPath.startsWith('/__krate/action/')) {
      if (req.method !== 'POST') return sendActionResponse(res, actionFail(405, 'Method Not Allowed'));
      try {
        return await runAction(req, res, urlObj);
      } catch (e) {
        console.error('[krate] server action error:', e);
        return sendActionResponse(res, actionFail(500, 'Server action failed'));
      }
    }

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
    
    const cleanKey = cleanRel.join('/');
    let targetFile = fileCache.get(cleanKey);
    if (targetFile === undefined) {
      targetFile = null;
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
      fileCache.set(cleanKey, targetFile);
    }

    if (!targetFile) {
      res.statusCode = 404;
      res.setHeader('Content-Type', 'application/json');
      return res.end(JSON.stringify({ error: "API Route Not Found: " + urlPath }));
    }

    let routeModule = moduleCache.get(targetFile);
    if (!routeModule || HOT) {
      routeModule = await import(pathToFileURL(targetFile).href + (HOT ? '?t=' + Date.now() : ''));
      if (!HOT) moduleCache.set(targetFile, routeModule);
    }
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
        
        const revalidateScope = newRevalidateScope('api');
        const response = await REVALIDATE_STORE.run(revalidateScope, () => methodHandler(request));
        // revalidatePath/revalidateTag calls are applied before responding.
        await flushRevalidate(revalidateScope);
        
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
        // Legacy pattern: handler(req, res). The response is held back until
        // queued revalidations are applied.
        const revalidateScope = newRevalidateScope('api');
        const end = res.end.bind(res);
        res.end = (...endArgs) => {
          flushRevalidate(revalidateScope).then(
            () => end(...endArgs),
            (e) => {
              console.error('[krate] API route revalidation failed:', e);
              if (!res.headersSent) res.statusCode = 500;
              end(...endArgs);
            },
          );
          return res;
        };
        await REVALIDATE_STORE.run(revalidateScope, () => methodHandler(req, res));
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

if (WORKERS > 1 && cluster.isPrimary) {
  for (let i = 0; i < WORKERS; i++) cluster.fork();
  // Restart a worker that dies, after a short delay, but give up when they
  // keep dying (a bad route module, a port that stays unbindable) instead of
  // forking forever. The delay also gives a briefly busy port time to free up.
  const exits = [];
  cluster.on('exit', () => {
    const now = Date.now();
    exits.push(now);
    while (exits.length && now - exits[0] > 60000) exits.shift();
    if (exits.length > 100) {
      console.error('[krate-api] workers keep exiting; giving up');
      process.exit(1);
    }
    setTimeout(() => cluster.fork(), 100);
  });
} else {
  // The port was free when Go chose it, but a bind can still fail with
  // EADDRINUSE: any socket holding that local port blocks the wildcard bind,
  // including a short-lived outgoing connection. A clustered worker exits
  // quietly so the primary respawns it after a delay (listening again after a
  // failed cluster listen trips a Node assertion on Windows); a standalone
  // server retries the bind for a few seconds.
  let bindRetries = 100;
  server.on('error', (err) => {
    if (err && err.code === 'EADDRINUSE') {
      if (cluster.isWorker) process.exit(1);
      if (bindRetries-- > 0) {
        setTimeout(() => {
          try { server.close(); } catch {}
          server.listen(PORT);
        }, 100);
        return;
      }
    }
    throw err;
  });
  server.listen(PORT, () => {
  });
}`

// apiServerBunScriptContent is the Bun-native API sidecar: it uses Bun.serve
// instead of node:http, so it skips the Node http stack (and its cluster shim)
// for higher throughput. It serves the same dist/api-node ESM routes and the
// /__krate/middleware endpoint. Selected by ssr.apiRuntime: "bun" (or the
// top-level runtime: "bun").
const apiServerBunScriptContent = `import path from 'node:path';
import fs from 'node:fs';
import { pathToFileURL } from 'node:url';
import process from 'node:process';
import { AsyncLocalStorage } from 'node:async_hooks';
import { randomBytes, timingSafeEqual } from 'node:crypto';

const API_DIR = path.resolve(process.argv[2] || './src/api');
const PORT = parseInt(process.argv[3] || '3001', 10);
const MIDDLEWARE_PATH = process.argv[4] ? path.resolve(process.argv[4]) : '';
const ACTION_DIR = process.argv[5] ? path.resolve(process.argv[5]) : path.resolve('./.krate/actions');
const HOT = process.env.KRATE_API_HOT === '1';

const fileCache = new Map();
const moduleCache = new Map();

let middlewareModule = null;
if (MIDDLEWARE_PATH) {
  try { middlewareModule = await import(pathToFileURL(MIDDLEWARE_PATH).href); } catch {}
}

function json(body, status) { return Response.json(body, { status: status || 200 }); }


` + actionSidecarJS + `
function sanitize(p) {
  let rel = p || '/';
  try { rel = decodeURIComponent(rel); } catch { return null; }
  if (rel.includes('\0') || rel.includes('\\')) return null;
  const clean = [];
  for (const seg of rel.split('/')) {
    if (seg === '' || seg === '.') continue;
    if (seg === '..') return null;
    clean.push(seg);
  }
  return clean;
}

function resolveFile(clean) {
  const key = clean.join('/');
  if (fileCache.has(key)) return fileCache.get(key);
  let target = null;
  const exts = ['.ts', '.js', '.tsx', '.jsx'];
  for (const ext of exts) {
    const fileCheck = path.join(API_DIR, ...clean) + ext;
    if (fs.existsSync(fileCheck) && !fs.statSync(fileCheck).isDirectory()) { target = fileCheck; break; }
    const indexCheck = path.join(API_DIR, ...clean, 'index' + ext);
    if (fs.existsSync(indexCheck)) { target = indexCheck; break; }
  }
  fileCache.set(key, target);
  return target;
}

async function loadModule(file) {
  if (!HOT) { const cached = moduleCache.get(file); if (cached) return cached; }
  const mod = await import(pathToFileURL(file).href + (HOT ? '?t=' + Date.now() : ''));
  if (!HOT) moduleCache.set(file, mod);
  return mod;
}

async function handleMiddleware(req) {
  if (!middlewareModule || !middlewareModule.middleware) return json({ action: 'continue' });
  try {
    const result = await middlewareModule.middleware(req);
    if (result && typeof result.status === 'number') {
      const headers = {};
      for (const [k, v] of result.headers.entries()) headers[k] = v;
      const body = result.status === 204 ? null : await result.text();
      return json({ status: result.status, headers, body });
    }
    return json({ action: 'continue' });
  } catch (e) { return json({ action: 'continue', error: e.message }); }
}

async function toResponse(out) {
  if (out instanceof Response) return out;
  if (out && typeof out.status === 'number') {
    const headers = {};
    if (out.headers && typeof out.headers.entries === 'function') {
      for (const [k, v] of out.headers.entries()) headers[k] = v;
    } else if (out.headers) {
      for (const k in out.headers) if (k[0] !== '_' && typeof out.headers[k] === 'string') headers[k] = out.headers[k];
    }
    let body = null;
    if (out.status !== 204 && typeof out.text === 'function') body = await out.text();
    return new Response(body, { status: out.status, headers });
  }
  if (out !== undefined && out !== null) return Response.json(out);
  return new Response(null, { status: 204 });
}

async function legacyInvoke(fn, req, url) {
  const headers = {};
  for (const [k, v] of req.headers.entries()) headers[k] = v;
  let bodyStr = '';
  if (req.method !== 'GET' && req.method !== 'HEAD') { try { bodyStr = await req.text(); } catch {} }
  return await new Promise((resolve) => {
    let finished = false;
    const outHeaders = {};
    let outBody = '';
    const nodeReq = { url: url.href, method: req.method, headers, body: bodyStr, socket: { remoteAddress: '127.0.0.1' } };
    const res = {
      statusCode: 200,
      setHeader(k, v) { outHeaders[k] = String(v); },
      getHeader(k) { return outHeaders[k]; },
      removeHeader(k) { delete outHeaders[k]; },
      write(c) { outBody += c; },
      end(c) { if (c) outBody += c; finished = true; resolve(new Response(outBody, { status: res.statusCode || 200, headers: outHeaders })); },
      on() {},
    };
    try {
      const out = fn(nodeReq, res);
      if (out && typeof out.then === 'function') {
        out.then(() => { if (!finished) resolve(new Response(outBody, { status: res.statusCode || 200, headers: outHeaders })); });
      } else if (!finished) {
        resolve(new Response(outBody, { status: res.statusCode || 200, headers: outHeaders }));
      }
    } catch (e) { resolve(json({ error: e.message }, 500)); }
  });
}

Bun.serve({
  port: PORT,
  async fetch(req) {
    const url = new URL(req.url);
    const urlPath = url.pathname;
    if (urlPath === '/__krate/internal/cache-invalidate') {
      const out = req.method === 'POST'
        ? await internalCacheInvalidate(req.headers.get('x-krate-internal'), await req.text())
        : { status: 405, body: { error: 'method not allowed' } };
      return json(out.body, out.status);
    }
    if (urlPath.startsWith('/__krate/action/')) {
      if (req.method !== 'POST') return actionFail(405, 'Method Not Allowed');
      try {
        return await krateRunAction(req, urlPath, url.search);
      } catch (e) {
        console.error('[krate] server action error:', e);
        return actionFail(500, 'Server action failed');
      }
    }
    if (urlPath === '/__krate/middleware') return handleMiddleware(req);
    const clean = sanitize(urlPath.replace(/^\/api/, ''));
    if (clean === null) return json({ error: 'Invalid API route path: ' + urlPath }, 400);
    const file = resolveFile(clean);
    if (!file) return json({ error: 'API Route Not Found: ' + urlPath }, 404);
    let mod;
    try { mod = await loadModule(file); } catch (e) { return json({ error: 'Internal Server Error', details: e.message }, 500); }
    const fn = mod[req.method.toUpperCase()] || mod.default;
    if (typeof fn !== 'function') return json({ error: 'API route must export a handler function (GET, POST, etc. or default).' }, 500);
    try {
      const revalidateScope = newRevalidateScope('api');
      const out = await REVALIDATE_STORE.run(revalidateScope, async () => (fn.length >= 2 ? legacyInvoke(fn, req, url) : toResponse(await fn(req))));
      // revalidatePath/revalidateTag calls are applied before responding.
      await flushRevalidate(revalidateScope);
      return out;
    } catch (e) { return json({ error: 'Internal Server Error', details: e.message }, 500); }
  },
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
	// Fatal marks a failed render that has no usable baked content (an async
	// server component): the response must be an error, not the shell.
	Fatal bool `json:"fatal,omitempty"`
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
	// fatalErr is non-empty when a server component render failed. Nothing
	// was written; the caller answers with a 500.
	fatalErr string
}

// regionBoundary is one splice marker and its nested markers.
type regionBoundary struct {
	openStart int
	openEnd   int
	fbEnd     int // start of the closing marker (end of baked inner content)
	id        string
	kind      string // "suspense" | "region"
	children  []*regionBoundary
}

// regionShell is a parsed baked shell. ok is false when the file had no markers
// or a mismatched marker pair (caller falls back to the static path).
type regionShell struct {
	modTime time.Time
	size    int64
	shell   string
	roots   []*regionBoundary
	all     []*regionBoundary
	ok      bool
}

// regionShellCache memoizes parsed shells by path so the hot path skips the disk
// read and marker parse. It reloads when the file's size/mtime changes (dev
// rebuilds rewrite the shell).
type regionShellCache struct {
	mu sync.Mutex
	m  map[string]*regionShell
}

func newRegionShellCache() *regionShellCache {
	return &regionShellCache{m: map[string]*regionShell{}}
}

func (c *regionShellCache) load(absPath string) (*regionShell, error) {
	fi, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if e, ok := c.m[absPath]; ok && e.size == fi.Size() && e.modTime.Equal(fi.ModTime()) {
		c.mu.Unlock()
		return e, nil
	}
	c.mu.Unlock()

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	e := parseRegionShell(string(data))
	e.size = fi.Size()
	e.modTime = fi.ModTime()

	c.mu.Lock()
	c.m[absPath] = e
	c.mu.Unlock()
	return e, nil
}

// parseRegionShell extracts the nested marker tree. Marker kinds:
//
//	<!--suspense:ID-->...<!--/suspense:ID-->  Suspense boundary / coarse page region
//	<!--region:ID--><!--/region:ID-->          standalone runtime component
func parseRegionShell(shell string) *regionShell {
	e := &regionShell{shell: shell}
	opens := regionOpenRe.FindAllStringSubmatchIndex(shell, -1)
	if len(opens) == 0 {
		return e
	}
	e.ok = true
	var stack []*regionBoundary
	for _, m := range opens {
		kind := shell[m[2]:m[3]]
		id := shell[m[4]:m[5]]
		openEnd := m[1]
		closeMarker := "<!--/" + kind + ":" + id + "-->"
		rel := strings.Index(shell[openEnd:], closeMarker)
		if rel < 0 {
			// Malformed marker pair - bail to the non-region path.
			e.ok = false
			return e
		}
		b := &regionBoundary{openStart: m[0], openEnd: openEnd, fbEnd: openEnd + rel, id: id, kind: kind}
		// Markers nest (a runtime component inside an SSR/ISR page region), so
		// build a tree: a boundary is a child of the nearest still-open one.
		for len(stack) > 0 && stack[len(stack)-1].fbEnd <= b.openStart {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, b)
		} else {
			e.roots = append(e.roots, b)
		}
		stack = append(stack, b)
		e.all = append(e.all, b)
	}
	return e
}

// streamRegionPage serves a page with the static-first architecture: the
// build-time static shell (suspense/region splice markers + baked content) is
// read from disk and streamed to the browser, and each dynamic region's HTML is
// fetched from the SSR sidecar (/__krate/regions) and spliced in place of its
// marker's inner content, one render per region with a flush after each. For
// SSR/ISR pages the shell carries a single coarse "page" region whose render
// replaces the whole baked body. Returns the page-region result (served=false
// when the shell had no region markers and the caller should use another path).
func streamRegionPage(w http.ResponseWriter, flusher http.Flusher, shells *regionShellCache, absOut, route string, ssrPort int, params, query, headers map[string]string, goRenders map[string]goRenderRegion, renderGo func([]goRenderRegion) (map[string]string, error)) regionPageResult {
	res := regionPageResult{}
	relPath := strings.TrimPrefix(route, "/")
	if relPath == "" {
		relPath = "index.html"
	} else {
		relPath = relPath + "/index.html"
	}
	// The baked shell + its parsed marker tree are cached per route; only a
	// missing/malformed shell falls through to the non-region path.
	e, err := shells.load(filepath.Join(absOut, relPath))
	if err != nil || e == nil || !e.ok {
		return res
	}
	shell := e.shell
	roots := e.roots
	all := e.all
	res.served = true

	// The marker id "page" denotes a coarse whole-page region (SSR/ISR shell);
	// its frame carries the page's ISR cache status for the caller's headers.

	// Partition splice markers into Go-host regions and Node-sidecar regions.
	// Region HTML is merged by id regardless of which engine rendered it. The
	// marker id "page" (SSR/ISR shell) also carries the page ISR cache status.
	regionHTML := make(map[string]string, len(all))
	regionList := make([]map[string]string, 0, len(all))
	var goReq []goRenderRegion
	reqCtx, _ := json.Marshal(map[string]any{
		"method": "GET", "url": route, "params": params, "query": query, "headers": headers,
	})
	for _, b := range all {
		if gr, ok := goRenders[b.id]; ok {
			gr.Request = string(reqCtx)
			gr.Route = route
			goReq = append(goReq, gr)
			continue
		}
		kind := "component"
		if b.kind == "suspense" && b.id == "page" {
			kind = "page"
		}
		regionList = append(regionList, map[string]string{"id": b.id, "kind": kind})
	}

	// Only involve the Node SSR sidecar when it has something to render. A page
	// whose regions are all Go (or static) is served by the Go server + Go host
	// alone - no node/bun process, no extra hop.
	if len(regionList) > 0 {
		if err := fetchNodeRegions(ssrPort, route, params, query, headers, regionList, regionHTML, &res); err != nil {
			res.served = false
			return res
		}
	}

	// Render runtime Go-component regions via the Go host and merge their HTML.
	if len(goReq) > 0 && renderGo != nil {
		if goHTML, gerr := renderGo(goReq); gerr == nil {
			for id, h := range goHTML {
				regionHTML[id] = h
			}
		}
	}

	// Resolve Go-component placeholders inside region HTML (an SSR page bundle
	// emits these for runtime-tier Go components and any server-tier component
	// it could not inline) by rendering them through the Go host, forwarding the
	// request context. Each marker carries its own revalidate window.
	if renderGo != nil {
		for id, h := range regionHTML {
			if strings.Contains(h, "krate-go:") {
				regionHTML[id] = resolveGoMarkers(h, string(reqCtx), renderGo)
			}
		}
	}

	// A page-region render that 404s (unknown dynamic variant) or redirects,
	// or a server component render that failed, is surfaced to the caller
	// instead of streaming the shell.
	if res.notFound || res.redirect != "" || res.fatalErr != "" {
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
	// flushing after each splice so the page streams progressively. Nested
	// markers are spliced recursively: when a parent region is replaced its
	// baked body (and any markers inside it) is discarded, but when a suspense
	// region keeps its baked fallback, markers nested inside are still spliced.
	var writeSpan func(lo, hi int, children []*regionBoundary)
	writeSpan = func(lo, hi int, children []*regionBoundary) {
		cursor := lo
		for _, b := range children {
			_, _ = w.Write([]byte(shell[cursor:b.openEnd]))
			if html, ok := regionHTML[b.id]; ok {
				_, _ = w.Write([]byte(html))
			} else if b.kind == "suspense" {
				// Keep the baked body but splice any markers nested inside it.
				writeSpan(b.openEnd, b.fbEnd, b.children)
			}
			// Standalone runtime regions with no frame leave the empty slot empty.
			cursor = b.fbEnd
			flush()
		}
		_, _ = w.Write([]byte(shell[cursor:hi]))
	}
	writeSpan(0, len(shell), roots)
	flush()

	return res
}

// fetchNodeRegions POSTs the region list to the Node SSR sidecar's
// /__krate/regions endpoint, reads the NDJSON frames, and merges the returned
// HTML into regionHTML (setting page-region metadata on res). Only called when
// the page has at least one Node-rendered region, so a Go-only page never pays
// for a Node hop. It reuses the pooled keep-alive sidecar client (no per-request
// TCP handshake) and bounds the whole exchange so a stalled sidecar can't pin the
// request goroutine forever.
func fetchNodeRegions(ssrPort int, route string, params, query, headers map[string]string, regionList []map[string]string, regionHTML map[string]string, res *regionPageResult) error {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"route":   route,
		"url":     route,
		"method":  "GET",
		"headers": headers,
		"params":  params,
		"query":   query,
		"regions": regionList,
	})
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/__krate/regions", ssrPort), bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := regionClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ssr regions: unexpected status %d", resp.StatusCode)
	}

	tcpBuf := bufio.NewReaderSize(resp.Body, 256)

	// Read one NDJSON line per region, collecting region HTML + page metadata.
	framesLeft := len(regionList)
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
			// notFound()/redirect() thrown by a server component region apply
			// to the whole response, like the page region's.
			if frame.ID != "page" && (frame.NotFound || frame.Redirect != "") {
				res.notFound = res.notFound || frame.NotFound
				if res.redirect == "" {
					res.redirect = frame.Redirect
				}
				framesLeft--
				continue
			}
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
					if frame.Fatal {
						res.fatalErr = res.renderErr
					}
					framesLeft--
					continue
				}
				if frame.NotFound {
					// Sidecar couldn't match - treat as no render (keep baked).
					framesLeft--
					continue
				}
			}
			regionHTML[frame.ID] = frame.HTML
			framesLeft--
		case "skip":
			// Sidecar has no renderer for this marker - keep baked content.
			framesLeft--
		case "error":
			msg := frame.Error
			if msg == "" {
				msg = fmt.Sprintf("region %s render failed with status %d", frame.ID, frame.Status)
			}
			if frame.Fatal && res.fatalErr == "" {
				res.fatalErr = msg
			}
			if res.renderErr == "" {
				res.renderErr = msg
			}
			if frame.ID != "" {
				framesLeft--
			} else {
				framesLeft = 0
			}
		case "end":
			framesLeft = 0
		}
	}
	return nil
}

// replaceTitle swaps the baked <title> content in a shell for a freshly
// rendered one. Minified shells may have unquoted/empty titles; a simple
// scan/replace on the first <title>...</title> span is sufficient.
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
	apiPort := internalPort(port + 1)

	// On-demand revalidation (revalidatePath/revalidateTag) reaches this
	// process through a loopback-only internal endpoint authenticated by a
	// per-process secret; every server-side runtime gets its URL + secret.
	revalOut, err := filepath.Abs(cfg.OutDir)
	if err != nil {
		return fmt.Errorf("resolving out dir: %w", err)
	}
	revalHost, err := newRevalidateHost(revalOut)
	if err != nil {
		return err
	}
	if err := revalHost.Start(); err != nil {
		return err
	}
	defer revalHost.Close()
	serverEnv := withEnv(environ.Current, revalHost.Env())

	// Load embedded middleware runtime if configured (default: quickjs)
	var middlewareRT *jsruntime.MiddlewareRuntime
	middlewareRTOption := strings.ToLower(cfg.SSR.MiddlewareRuntime)
	if middlewareRTOption == "" || middlewareRTOption == "quickjs" {
		middlewareRT = jsruntime.LoadMiddlewareRuntime(root)
	}

	// The embedded QuickJS runtime serves JS/TS API routes in-process by
	// default, so the Node API sidecar only needs to start when it is actually
	// selected (apiRuntime / middlewareRuntime is node/bun/deno) or when a route
	// uses external imports the embedded runtime cannot run.
	apiDir := filepath.Join(cfg.OutDir, "api")
	nodeAPIDir := filepath.Join(cfg.OutDir, "api-node")
	actionDir := filepath.Join(root, ".krate", "actions")
	hasActions := hasActionManifest(root)
	middlewarePath := filepath.Join(root, ".krate", "middleware.js")
	apiRTOption := strings.ToLower(cfg.SSR.APIRuntime)
	nodeOnlyRoutes := loadNodeOnlyAPIRoutes(root)
	nodeWorkers := cfg.SSR.NodeWorkers
	if nodeWorkers <= 0 {
		nodeWorkers = jsruntime.DefaultAPIWorkers()
	}
	needNodeSidecar := sidecarNeedsNode(apiRTOption, middlewareRTOption, middlewareRT, middlewarePath, nodeOnlyRoutes) || hasActions

	var nodeAPICmdMu sync.Mutex
	var nodeAPICmd *exec.Cmd
	if needNodeSidecar {
		// An explicit node-family apiRuntime selects the sidecar command;
		// otherwise fall back to the project's top-level runtime.
		sidecarRuntime := strings.ToLower(cfg.Runtime)
		if isNodeFamilyRuntime(apiRTOption) {
			sidecarRuntime = apiRTOption
		}
		printf("  %s⚡%s API sidecar (%s) → %shttp://localhost:%d%s\n", cCyan, cReset, sidecarRuntime, cCyan, apiPort, cReset)
		revalHost.SetNodeAPI(fmt.Sprintf("http://127.0.0.1:%d", apiPort))

		go func() {
			defer func() {
				if r := recover(); r != nil {
					_ = safe.Recover(r)
				}
			}()

			portStr := fmt.Sprintf("%d", apiPort)
			var cmd *exec.Cmd

			switch sidecarRuntime {
			case "bun":
				// Bun-native server (Bun.serve): skips the Node http stack.
				bunPath := filepath.Join(root, ".krate", "api-server.bun.mjs")
				_ = os.MkdirAll(filepath.Dir(bunPath), 0755)
				_ = os.WriteFile(bunPath, []byte(apiServerBunScriptContent), 0644)
				cmd = exec.Command("bun", "run", bunPath, nodeAPIDir, portStr, middlewarePath, actionDir)

			case "deno":
				scriptPath := filepath.Join(root, ".krate", "api-server.mjs")
				_ = os.MkdirAll(filepath.Dir(scriptPath), 0755)
				_ = os.WriteFile(scriptPath, []byte(apiServerScriptContent), 0644)
				cmd = exec.Command("deno", "run", "--allow-net", "--allow-read", "--allow-sys", "--allow-env", scriptPath, nodeAPIDir, portStr, middlewarePath, actionDir)

			default:
				scriptPath := filepath.Join(root, ".krate", "api-server.mjs")
				_ = os.MkdirAll(filepath.Dir(scriptPath), 0755)
				_ = os.WriteFile(scriptPath, []byte(apiServerScriptContent), 0644)
				cmd = exec.Command("node", scriptPath, nodeAPIDir, portStr, middlewarePath, actionDir)
			}

			execx.ConfigureTree(cmd)
			cmd.Env = append(os.Environ(),
				fmt.Sprintf("KRATE_API_WORKERS=%d", nodeWorkers),
				fmt.Sprintf("KRATE_API_HOT=%d", boolToInt(hub != nil)),
			)
			cmd.Env = append(cmd.Env, revalHost.EnvList()...)
			if cacheRuntime := stageCacheRuntime(root, cfg.OutDir); cacheRuntime != "" {
				cmd.Env = append(cmd.Env, "KRATE_CACHE_RUNTIME="+cacheRuntime)
			}
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			nodeAPICmdMu.Lock()
			nodeAPICmd = cmd
			nodeAPICmdMu.Unlock()
			_ = cmd.Run()
		}()
	}

	// Start the Go API sidecar if Go API routes were compiled during the build.
	// Go routes take precedence over JS routes for the same /api path.
	var goAPI *goAPISupervisor
	goAPIManifestPath := gohost.RoutesManifestPath(root)
	if _, err := os.Stat(goAPIManifestPath); err == nil {
		goAPIPort := port + 2
		goAPI = newGoAPISupervisor(goAPIServerBinPath(root), goAPIManifestPath, goAPIPort)
		// Prefer an AF_UNIX socket so the sidecar needs no port and the hop
		// skips the loopback TCP stack. Fall back to the TCP port only if the
		// socket directory can't be created.
		goAPISocket := filepath.Join(root, ".krate", "run", "goapi.sock")
		if err := os.MkdirAll(filepath.Dir(goAPISocket), 0o700); err == nil {
			_ = os.Remove(goAPISocket)
			goAPI.SetSocket(goAPISocket)
		}
		goAPI.SetEnv(environ.KVList(serverEnv))
		revalHost.SetGoAPI(goAPI)
		if err := goAPI.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "  %s⚠ Go API sidecar not started:%s %v\n", cYellow, cReset, err)
		} else if goAPI.socket != "" {
			printf("  %s⚡%s Go API → %s%s%s\n", cCyan, cReset, cCyan, goAPI.socket, cReset)
		} else {
			printf("  %s⚡%s Go API → %shttp://localhost:%d%s\n", cCyan, cReset, cCyan, goAPIPort, cReset)
		}
	}

	absOut := revalOut

	fileServer := http.FileServer(http.Dir(absOut))
	mux := http.NewServeMux()

	// Load the build manifest to learn which dynamic routes are closed
	// (static-only: unknown params must 404, not serve the [param] template).
	staticOnlyRoutes := loadStaticOnlyRoutes(absOut)
	goRegionsByRoute := loadGoRegions(absOut)
	// Parsed baked shells (per route) so the region hot path skips the disk read
	// and marker parse. Reloads automatically when a dev rebuild rewrites a shell.
	regionShells := newRegionShellCache()

	// Load embedded API route runtime if configured (default: quickjs)
	var apiRT *jsruntime.APIRouteRuntime
	if apiRTOption == "" || apiRTOption == "quickjs" {
		if _, err := os.Stat(apiDir); err == nil {
			apiRT = jsruntime.NewAPIRouteRuntime(apiDir)
			apiRT.SetEnv(serverEnv)
			apiRT.SetCacheHooks(revalHost.CacheHooks())
			// Dev picks up route edits by running each request in a fresh VM;
			// production reuses stateful workers for throughput.
			isolation := cfg.SSR.APIIsolation || hub != nil
			apiRT.SetPool(cfg.SSR.APIWorkers, isolation)
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
			printf("  %s⚡%s API sidecar (%s) %s → %s%s%s\n", cCyan, cReset, mode, customSidecar.Prefix(), cCyan, target, cReset)
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
	if hasActions {
		mux.HandleFunc("/__krate/action/", actionHandler(apiProxy, apiBodyLimit))
	}

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

		// Routes that need Node (external imports the embedded runtime cannot
		// run) are forwarded to the Node sidecar even when QuickJS is default.
		if apiRT != nil && nodeOnlyRoutes[apiRT.ResolveFile(r.URL.Path)] {
			apiProxy.ServeHTTP(w, r)
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

	// Load the SSR manifest for route lookup (server-manifest.json). The Node
	// renderer process only starts when a page/region actually needs it - a site
	// whose request-time regions are all Go is served by the Go server + Go host
	// alone (no node/bun process).
	ssrPort := cfg.SSR.RendererPort
	if ssrPort == 0 {
		ssrPort = internalPort(port + 10)
	}
	ssr := NewSSRServer(root, ssrPort, cfg.SSR.SSRRuntime)
	ssr.SetEnv(serverEnv)
	ssr.SetInternalSecret(revalHost.secret)
	ssr.SetTuning(cfg.SSR.Timeout, cfg.SSR.MaxCacheSize)
	ssr.SetPPR(cfg.PPR, cfg.PPRRevalidate)
	ssr.SetRendererWorkers(cfg.SSR.RendererWorkers)
	ssrStarted := false
	if err := ssr.LoadManifest(); err != nil {
		// No request-time pages: nothing routes through the renderer.
	} else if !ssr.NeedsNodeRenderer() {
		printf("  %s⚡%s SSR renderer skipped (all request-time regions render on the Go host)\n", cCyan, cReset)
	} else if err := ssr.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "  %s⚠ SSR renderer not started:%s %v\n", cYellow, cReset, err)
	} else {
		ssrStarted = true
		revalHost.SetSSR(ssr, ssrPort)
		printf("  %s⚡%s SSR renderer → %shttp://localhost:%d%s\n", cCyan, cReset, cCyan, ssrPort, cReset)
	}

	// SSR/ISR/Streaming route handler - intercepts before static file server
	if ssrStarted {
		mux.HandleFunc("/__krate/ssr/", ssrInternalProxy(revalHost, ssrPort))
	}

	// Wrap file server to serve custom 404.html if it exists
	notif404File := filepath.Join(absOut, "404.html")
	custom404, _ := os.ReadFile(notif404File)

	// Scan for dynamic routes ([param] directories) in the output
	dynRoutes := findDynamicRoutes(absOut)
	if len(dynRoutes) > 0 {
		printf("  %s⚡%s Dynamic routes: %d patterns\n", cCyan, cReset, len(dynRoutes))
	}

	// Opt-in User-Agent document selection (compat.mode hybrid + userAgent).
	uaOn := uaOptIn(cfg.Compat)

	handlerWith404 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Source maps are JSON; set the type explicitly since `.map` has no
		// standard mime entry on every platform (FileServer only sets a type
		// when one isn't already present).
		if strings.HasSuffix(r.URL.Path, ".map") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		}
		// Hashed assets (styles.<hash>.css, index.<hash>.js, chunks/*.<hash>.js,
		// assets/<name>-<hash>.*) are content-addressed, so they can be cached
		// immutably. HTML must never be, since it changes per build.
		w.Header().Set("Cache-Control", staticCacheControl(r.URL.Path, hub != nil))

		// Opt-in UA variant: serve the modern or legacy sibling document to
		// classified visitors (the default document stays the full hybrid).
		// Vary/Accept-CH apply only to HTML documents so cached hashed assets
		// are not fragmented per visitor.
		if uaOn && isHTMLDocRequest(r.URL.Path) {
			w.Header().Add("Vary", "User-Agent")
			w.Header().Add("Vary", "Sec-CH-UA")
			w.Header().Set("Accept-CH", "Sec-CH-UA")
			kind := "modern"
			if uaPrefersLegacyDoc(r.UserAgent(), r.Header.Get("Sec-CH-UA")) {
				kind = "legacy"
			}
			if sp := siblingDocPath(absOut, r.URL.Path, kind); sp != "" {
				if data, err := os.ReadFile(sp); err == nil {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.Header().Set("Cache-Control", "no-cache")
					_, _ = w.Write(data)
					return
				}
			}
		}

		// A concrete static file beats any dynamic [param] pattern: /items/alpha
		// must resolve to the static page, not the /items/[id] template.
		if !staticRouteExists(absOut, r.URL.Path) {
			// Check dynamic routes - if a URL matches a [param] pattern, serve the template
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
								if err := ssr.InvalidateDev(route, page.Source, page.BundlePath); err != nil {
									fmt.Fprintf(os.Stderr, "  %sSSR invalidation failed (%s):%s %v\n", cYellow, route, cReset, err)
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

	// SSR/ISR/Streaming page handler - proxies dynamic pages to the Node.js renderer
	var ssrPageHandler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Gate on the manifest (route lookup), not the Node process: a page whose
		// regions are all Go renders without any Node renderer running.
		if !ssr.HasManifest() {
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
		// manifest route "/video/[id]"), not the concrete URL - the sidecar
		// resolves the pattern and keys ISR cache variants by params. So the
		// canonical route is what everything downstream uses.
		page, params := ssr.FindPageForRoute(route)

		// Request-time pages (ssr/isr/streaming) MUST go through the sidecar
		// even when a concrete static file was baked at build time - a
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

		var routeGo map[string]goRenderRegion
		if goRegionsByRoute != nil {
			routeGo = goRegionsByRoute[route]
		}
		var renderGo func([]goRenderRegion) (map[string]string, error)
		if goAPI != nil && goAPI.Active() {
			renderGo = goAPI.Render
		}
		// 0 when no Node renderer runs: streamRegionPage only dials the sidecar
		// if the page has a region it must render.
		regionPort := 0
		if ssrStarted {
			regionPort = ssrPort
		}
		rr := streamRegionPage(w, flusher, regionShells, absOut, route, regionPort, params, query, headers, routeGo, renderGo)
		if rr.served && rr.fatalErr != "" {
			// A server component failed: there is no meaningful baked body to
			// fall back to, so answer 500 (the custom 500 page when present).
			fmt.Fprintf(os.Stderr, "  %sServer component error (%s):%s %s\n", cRed, route, cReset, rr.fatalErr)
			w.Header().Set("X-Krate-Error", "render")
			w.Header().Del("X-Krate-Cache")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusInternalServerError)
			if data, err := os.ReadFile(filepath.Join(absOut, "500.html")); err == nil {
				_, _ = w.Write(data)
			} else {
				_, _ = w.Write([]byte("<!doctype html><title>500</title><h1>Internal Server Error</h1>"))
			}
			return
		}
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
		// was baked with nothing request-time to render - e.g. a page the global
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

	// Redirect/rewrite middleware - applies config-based URL transformations
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

	// User middleware handler - calls middleware.ts via embedded quickjs or sidecar
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
		// Preview/static serving: gzip compressible assets, and serve
		// build-time precompressed (.br/.gz) siblings when present.
		top = gzipMiddleware(top)
		top = precompressedMiddleware(absOut, top)
	} else {
		// Dev: proxy configured prefixes (e.g. /api) to a backend.
		top = devProxyMiddleware(cfg.DevServer.Proxy, top)
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
	var rootHandler http.Handler = requestIDMiddleware(basePathMiddleware(cfg.BaseURLPath(), securityHeadersMiddleware(cfg, customHeadersMiddleware(cfg.Headers, corsMiddleware(cfg, panicRecoveryMiddleware(Logger, mux))))))

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("listen on :%d: %w", port, err)
	}

	addr := listener.Addr().(*net.TCPAddr)
	label := "dev"
	if hub == nil {
		label = "serve"
	}
	printf("%s  %s server → %shttp://localhost:%d%s %s(started in %s)%s\n", cGreen, label, cCyan, addr.Port, cReset, cGray, time.Since(startTime).Round(time.Millisecond), cReset)

	// Start ISR background revalidation - one timer per page, each route
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
			printf("  %s⚡%s ISR revalidation: %d pages\n", cCyan, cReset, len(isrPages))
			for _, p := range isrPages {
				page := p
				isrWg.Add(1)
				go func() {
					defer isrWg.Done()
					defer func() {
						if r := recover(); r != nil {
							_ = safe.Recover(r)
						}
					}()
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
		printf("\n%sShutting down...%s\n", cGray, cReset)
		defer func() {
			if r := recover(); r != nil {
				_ = safe.Recover(r)
			}
		}()
		// Stop ISR revalidation goroutine
		isrCloseOnce.Do(func() { close(isrDone) })
		if ssrStarted {
			ssr.Stop()
			printf("  %s✓%s SSR renderer stopped\n", cGreen, cReset)
		}
		if goAPI != nil {
			goAPI.Close()
			printf("  %s✓%s Go API sidecar stopped\n", cGreen, cReset)
		}
		if customSidecar != nil {
			customSidecar.Stop()
			printf("  %s✓%s API sidecar stopped\n", cGreen, cReset)
		}
		nodeAPICmdMu.Lock()
		nc := nodeAPICmd
		nodeAPICmdMu.Unlock()
		if nc != nil && nc.Process != nil {
			execx.KillTree(nc)
			printf("  %s✓%s Node API sidecar stopped\n", cGreen, cReset)
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

		// Emitted off the request path: the entry is queued and written by a
		// background goroutine so high-throughput serving doesn't block on stdout.
		emitAccessLog(accessRecord{
			id:       requestIDFrom(r.Context()),
			method:   r.Method,
			path:     r.URL.Path,
			status:   lrw.statusCode,
			duration: time.Since(start),
		})
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
// file for the URL path - in which case dynamic [param] routing must give way.
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
// A concrete static file always wins for SSG pages (or unmatched routes) so it
// can shadow a sibling dynamic [param] template. But a page the manifest
// registers as ssr/isr/streaming MUST NOT be served statically even when the
// build baked a concrete file for it - a pre-generated ISR variant
// (generateStaticParams) or a streaming shell still needs request-time
// rendering/revalidation, and serving the baked bytes would freeze the page at
// its build timestamp (no revalidate, no fresh region content).
func shouldServeStatic(absOut, urlPath string, page *ManifestPage) bool {
	if page != nil && page.Mode != RenderSSG.String() {
		// A baked asset (the page's hydration script, a chunk, an image) lives
		// next to the page's index.html, so a dynamic ISR/SSR route such as
		// posts/[slug] also matches posts/index.abc.js. Real files with an
		// extension are never pages.
		return isBakedAsset(absOut, urlPath)
	}
	return staticRouteExists(absOut, urlPath)
}

// isBakedAsset reports whether urlPath names an existing non-HTML file in the
// build output.
func isBakedAsset(absOut, urlPath string) bool {
	up, err := url.PathUnescape(urlPath)
	if err != nil {
		up = urlPath
	}
	ext := strings.ToLower(path.Ext(up))
	if ext == "" || ext == ".html" {
		return false
	}
	rel := strings.TrimPrefix(path.Clean("/"+up), "/")
	fi, err := os.Stat(filepath.Join(absOut, filepath.FromSlash(rel)))
	return err == nil && !fi.IsDir()
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

// loadGoRegions reads manifest.json and returns, per route, the runtime
// Go-component regions (region id -> render request) so the server can route
// them to the Go host instead of the Node SSR sidecar.
func loadGoRegions(absOut string) map[string]map[string]goRenderRegion {
	data, err := os.ReadFile(filepath.Join(absOut, "manifest.json"))
	if err != nil {
		return nil
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	out := map[string]map[string]goRenderRegion{}
	for route, regs := range m.Regions {
		for _, r := range regs {
			if r.Runtime != "go" {
				continue
			}
			propsJSON, _ := json.Marshal(r.Props)
			if out[route] == nil {
				out[route] = map[string]goRenderRegion{}
			}
			out[route][r.ID] = goRenderRegion{ID: r.ID, Name: r.Component, Props: string(propsJSON), Revalidate: r.Revalidate}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// loadNodeOnlyAPIRoutes reads .krate/api-routes.json and returns the set of
// out-dir-relative route files that require the Node runtime (they import
// external modules the embedded QuickJS runtime cannot run).
func loadNodeOnlyAPIRoutes(root string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(root, ".krate", "api-routes.json"))
	if err != nil {
		return out
	}
	var list []struct {
		File     string `json:"file"`
		NodeOnly bool   `json:"nodeOnly"`
	}
	if json.Unmarshal(data, &list) != nil {
		return out
	}
	for _, m := range list {
		if m.NodeOnly {
			out[m.File] = true
		}
	}
	return out
}

// sidecarNeedsNode reports whether the Node API sidecar must be started: the
// user selected a Node-family runtime for API routes or middleware, at least one
// route needs Node, or middleware exists that the embedded runtime did not load.
func sidecarNeedsNode(apiRTOption, middlewareRTOption string, middlewareRT *jsruntime.MiddlewareRuntime, middlewarePath string, nodeOnly map[string]bool) bool {
	if isNodeFamilyRuntime(apiRTOption) || isNodeFamilyRuntime(middlewareRTOption) {
		return true
	}
	if len(nodeOnly) > 0 {
		return true
	}
	if middlewareRT == nil {
		if _, err := os.Stat(middlewarePath); err == nil {
			return true
		}
	}
	return false
}

// isNodeFamilyRuntime reports whether a configured runtime name is a Node-family
// sidecar (node/bun/deno) rather than the embedded QuickJS runtime.
func isNodeFamilyRuntime(rt string) bool {
	switch strings.ToLower(rt) {
	case "node", "bun", "deno":
		return true
	default:
		return false
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
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
		rel, rerr := filepath.Rel(absOut, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		// Any directory under a [param] segment is a template, including
		// static leaves below one (blog/[slug]/comments).
		if isDynamicPattern(rel) {
			// Only include directories that have an index.html (actual page templates)
			if _, statErr := os.Stat(filepath.Join(path, "index.html")); statErr == nil {
				routes = append(routes, dynamicRoute{pattern: rel, dir: path})
			}
		}
		return nil
	})
	// Deterministic, most specific first (see compareRouteSpecificity).
	sort.SliceStable(routes, func(i, j int) bool { return compareRouteSpecificity(routes[i].pattern, routes[j].pattern) < 0 })
	return routes
}

// matchDynamicRoute checks if a URL path matches a dynamic route pattern and returns params.
// e.g. matchDynamicRoute("/video/abc123", "video/[id]") returns {id: "abc123"}, true
func matchDynamicRoute(urlPath, pattern string) (map[string]string, bool) {
	return matchRoutePattern(urlPath, pattern)
}

// applyDynamicRouteParams renders a built dynamic-route template for one
// request: it injects the matched params for client-side access and replaces
// every build-time sentinel (see dynamicParamSentinel) with the matched URL
// segment. Because the template is built with sentinels wherever the param is
// read - body text, <title>, and meta attributes - a single replacement pass
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

// sameOriginActionRequest is the CSRF guard for server-action POSTs. Browsers
// send Origin/Sec-Fetch-Site on cross-site form submissions; reject those while
// still allowing non-browser clients and same-origin form posts.
func sameOriginActionRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// sanitizeActionReturn accepts only a same-origin path for the post-action
// redirect. Absolute URLs and scheme-relative paths are dropped so an action
// form cannot be turned into an open redirect.
func sanitizeActionReturn(value string, r *http.Request) string {
	value = strings.TrimSpace(value)
	if value == "" || r == nil {
		return ""
	}
	if strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") && !strings.ContainsAny(value, "\r\n") {
		return value
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	if !strings.EqualFold(u.Host, r.Host) || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.RequestURI()
}

// handedOutPorts remembers the sidecar ports internalPort returned in this
// process, so servers starting at the same time never share one. Entries
// expire, so a server that restarts keeps getting its preferred port.
var (
	handedOutPortsMu sync.Mutex
	handedOutPorts   = map[int]time.Time{}
)

const handedOutPortTTL = time.Minute

// portHandedOut reports whether port was returned by internalPort recently.
// handedOutPortsMu must be held.
func portHandedOut(port int) bool {
	at, ok := handedOutPorts[port]
	if ok && time.Since(at) > handedOutPortTTL {
		delete(handedOutPorts, port)
		return false
	}
	return ok
}

// portFree reports whether port can be bound on both the loopback address and
// the wildcard address. The Node sidecars bind the wildcard address, but on
// macOS and Windows that bind succeeds next to a loopback-only listener, so
// checking the wildcard alone would hand out a port another server holds.
func portFree(port int) bool {
	for _, addr := range []string{"127.0.0.1", ""} {
		ln, err := net.Listen("tcp", net.JoinHostPort(addr, strconv.Itoa(port)))
		if err != nil {
			return false
		}
		_ = ln.Close()
	}
	return true
}

// internalPort returns preferred when it can be bound and has not been handed
// out already, and otherwise a free port chosen by the OS. Sidecars default to
// fixed offsets from the server port, which collide when several servers run
// at once.
func internalPort(preferred int) int {
	handedOutPortsMu.Lock()
	defer handedOutPortsMu.Unlock()
	if !portHandedOut(preferred) && portFree(preferred) {
		handedOutPorts[preferred] = time.Now()
		return preferred
	}
	for attempt := 0; attempt < 20; attempt++ {
		ln, err := net.Listen("tcp", ":0")
		if err != nil {
			break
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		if !portHandedOut(port) {
			handedOutPorts[port] = time.Now()
			return port
		}
	}
	return preferred
}

// withEnv returns base with extra layered on top (neither is modified).
func withEnv(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
