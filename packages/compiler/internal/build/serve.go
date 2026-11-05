package build

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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

var apiServerScriptContent = `import http from 'node:http';
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
const LOOPBACK = '127.0.0.1';
// Cluster across cores so the Node API sidecar uses more than one thread.
const WORKERS = Math.max(1, parseInt(process.env.KRATE_API_WORKERS || '1', 10) || 1);

// Exit when the Krate host dies: a SIGKILL or OOM kill gives it no chance to stop its sidecars.
const PARENT_PID = parseInt(process.env.KRATE_PARENT_PID || '', 10);
if (PARENT_PID > 0) {
  const parentWatch = setInterval(() => {
    try {
      process.kill(PARENT_PID, 0);
    } catch (e) {
      if (!e || e.code !== 'EPERM') process.exit(0);
    }
  }, 2000);
  if (parentWatch.unref) parentWatch.unref();
}

// Per-process caches so a request does no filesystem probing and no module
// re-import: URL path -> resolved route file, and file -> imported namespace.
// The module cache is bypassed in dev (HOT) so edits are picked up.
const fileCache = new Map();
const moduleCache = new Map();
` + actionSidecarJS + actionSidecarNodeAdapter + apiSidecarClusterJS + apiSidecarSharedJS + `
let middlewareModule = null;
await krateLoadMiddleware();

function routeParams(file, segments) {
  const rel = path.relative(API_DIR, file).replace(/\.[^.]+$/, '');
  let fileSegs = rel.split(path.sep);
  if (fileSegs[fileSegs.length - 1] === 'index') fileSegs = fileSegs.slice(0, -1);
  const params = {};
  for (let i = 0; i < fileSegs.length; i++) {
    const seg = fileSegs[i];
    if (seg.startsWith('[[...') && seg.endsWith(']]')) {
      if (i < segments.length) params[seg.slice(5, -2)] = segments.slice(i).join('/');
      return params;
    }
    if (seg.startsWith('[...') && seg.endsWith(']')) {
      params[seg.slice(4, -1)] = segments.slice(i).join('/');
      return params;
    }
    if (seg.startsWith('[') && seg.endsWith(']')) {
      if (i < segments.length) params[seg.slice(1, -1)] = segments[i];
    }
  }
  return params;
}

function isDynamicSegment(name) {
  return name.startsWith('[') && name.endsWith(']') && !name.startsWith('[...') && !name.startsWith('[[...');
}
function isCatchAllSegment(name) {
  return name.startsWith('[...') || name.startsWith('[[...');
}
function resolveDynamicFile(dir, segments, exts) {
  let entries;
  try { entries = fs.readdirSync(dir, { withFileTypes: true }); } catch { return null; }
  if (segments.length === 0) {
    for (const ext of exts) {
      const index = path.join(dir, 'index' + ext);
      if (fs.existsSync(index)) return index;
    }
    return null;
  }
  if (segments.length > 0) {
    const next = path.join(dir, segments[0]);
    try {
      if (fs.statSync(next).isDirectory()) {
        const hit = resolveDynamicFile(next, segments.slice(1), exts);
        if (hit) return hit;
      }
    } catch {}
  }
  for (const entry of entries) {
    const name = entry.name;
    const full = path.join(dir, name);
    if (entry.isDirectory()) {
      if (isCatchAllSegment(name) && segments.length > 0) {
        for (const ext of exts) {
          const index = path.join(full, 'index' + ext);
          if (fs.existsSync(index)) return index;
        }
      }
      if (isDynamicSegment(name) && segments.length > 0) {
        const hit = resolveDynamicFile(full, segments.slice(1), exts);
        if (hit) return hit;
      }
      continue;
    }
    const base = name.replace(/\.[^.]+$/, '');
    if (isCatchAllSegment(base) && (segments.length > 0 || base.startsWith('[[...'))) return full;
    if (segments.length === 1 && isDynamicSegment(base)) return full;
  }
  return null;
}

const server = http.createServer(async (req, res) => {
  try {
    // Only the Krate host (which adds the per-process secret after running
    // middleware and the action checks) may call the sidecar.
    if (!sidecarAuthorized(req.headers['x-krate-internal'])) {
      res.statusCode = 403;
      res.setHeader('Content-Type', 'text/plain; charset=utf-8');
      return res.end('forbidden');
    }
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
      if (HOT) await krateLoadMiddleware();
      if (!middlewareModule || !middlewareModule.middleware) {
        res.setHeader('Content-Type', 'application/json');
        return res.end(JSON.stringify({ action: 'continue' }));
      }

      const bodyChunks = [];
      for await (const chunk of req) { bodyChunks.push(chunk); }
      let body = {};
      try { body = JSON.parse(Buffer.concat(bodyChunks).toString()); } catch {}

      const request = krateAttach(new Request(body.url || ('http://localhost' + urlPath), {
        method: body.method || 'GET',
        headers: krateRequestHeaders(body.headers),
      }));

      res.setHeader('Content-Type', 'application/json');
      try {
        const hasMatcher = !!krateMatchersFor(middlewareModule);
        if (!krateMiddlewareMatches(middlewareModule, body.path || urlObj.pathname)) {
          return res.end(JSON.stringify({ action: 'continue', headers: [], matcher: hasMatcher }));
        }
        const result = await middlewareModule.middleware(request);
        return res.end(JSON.stringify({ ...(await krateMiddlewareJSON(result)), matcher: hasMatcher }));
      } catch (e) {
        console.error('[krate] middleware error: ' + ((e && e.stack) || e));
        return res.end(JSON.stringify({ action: 'error', error: (e && e.message) || String(e) }));
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
      if (!targetFile) targetFile = resolveDynamicFile(API_DIR, cleanRel, extensions);
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
    const picked = krateHandler(routeModule, method);
    const routeParamsValue = routeParams(targetFile, cleanRel);

    if (!picked.fn) {
      res.statusCode = picked.status;
      if (picked.allow) res.setHeader('Allow', picked.allow.join(', '));
      if (picked.status === 204) return res.end();
      res.setHeader('Content-Type', 'application/json');
      return res.end(JSON.stringify(picked.status === 405 ? { error: 'Method Not Allowed' } : { error: picked.error }));
    }
    const methodHandler = picked.fn;
    const namedHandler = picked.named;

    {
      // Web standard API: handler(request, { params }) => Response, unless the
      // default export takes (req, res) (legacy Node style).
      const argCount = methodHandler.length;
      if (namedHandler || argCount <= 1) {
        const headers = new Headers();
        for (const [key, value] of Object.entries(req.headers)) {
          if (Array.isArray(value)) { for (const v of value) headers.append(key, v); }
          else if (value !== undefined) headers.append(key, value);
        }

        let body = null;
        if (req.method !== 'GET' && req.method !== 'HEAD') {
          const chunks = [];
          for await (const chunk of req) {
            chunks.push(chunk);
          }
          body = Buffer.concat(chunks);
        }

        const ac = new AbortController();
        res.on('close', () => { if (!res.writableEnded) ac.abort(); });
        const request = krateAttach(new Request(urlObj.href, {
          method: req.method,
          headers,
          body: body && body.length > 0 ? body : undefined,
          signal: ac.signal,
        }));

        const revalidateScope = newRevalidateScope('api');
        const response = await REVALIDATE_STORE.run(revalidateScope, () => methodHandler(request, { params: routeParamsValue }));
        // revalidatePath/revalidateTag calls are applied before responding.
        await flushRevalidate(revalidateScope);

        if (response instanceof Response || (response && typeof response.status === 'number' && typeof response.headers === 'object')) {
          res.statusCode = response.status;
          for (const [key, value] of krateHeaderPairs(response.headers)) {
            if (key === 'set-cookie') {
              const prev = res.getHeader('set-cookie');
              res.setHeader('set-cookie', prev ? [].concat(prev, value) : [value]);
            } else {
              res.setHeader(key, value);
            }
          }
          if (picked.headOnly || !response.body || response.status === 204 || response.status === 304) {
            if (typeof response.text === 'function' && response.body) { try { await response.body.cancel(); } catch {} }
            return res.end();
          }
          // Pipe the body so SSE / chunked responses reach the client as they
          // are produced instead of after the handler finishes.
          const reader = response.body.getReader();
          res.on('close', () => { reader.cancel().catch(() => {}); });
          for (;;) {
            const r = await reader.read();
            if (r.done) break;
            const chunk = typeof r.value === 'string' ? r.value : Buffer.from(r.value);
            if (!res.write(chunk)) await new Promise((ok) => res.once('drain', ok));
          }
          res.end();
        } else if (response !== undefined && response !== null) {
          res.setHeader('Content-Type', 'application/json');
          res.end(JSON.stringify(response));
        } else {
          res.statusCode = 204;
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
    }
  } catch (err) {
    if (res.headersSent) { res.destroy(err); return; }
    res.statusCode = 500;
    res.setHeader('Content-Type', 'application/json');
    return res.end(JSON.stringify(krateErrorBody(err, req.method + ' ' + req.url)));
  }
});

if (WORKERS > 1 && cluster.isPrimary) {
  // Node leaves scheduling to the OS on Windows (SCHED_NONE), where one
  // worker tends to accept every connection. Round-robin keeps clustered
  // caches - and therefore revalidation fan-out - reachable on every platform.
  cluster.schedulingPolicy = cluster.SCHED_RR;
  for (let i = 0; i < WORKERS; i++) cluster.fork();
  // Restart a worker that dies, after a short delay, but give up when they
  // keep dying (a bad route module, a port that stays unbindable) instead of
  // forking forever. The delay also gives a briefly busy port time to free up.
  const exits = [];
  let exitLogs = 0;
  cluster.on('exit', (worker, code, signal) => {
    const now = Date.now();
    exits.push(now);
    while (exits.length && now - exits[0] > 60000) exits.shift();
    if (exitLogs++ < 3) {
      console.error('[krate-api] worker ' + (worker && worker.process ? worker.process.pid : '?') + ' exited (code=' + code + ', signal=' + signal + ')');
    }
    // A worker can exit while a transient local-port holder clears. Keep
    // retrying long enough for that to happen; only a sustained failure is
    // fatal.
    if (exits.length > 500) {
      console.error('[krate-api] workers keep exiting; giving up');
      process.exit(1);
    }
    setTimeout(() => cluster.fork(), 100);
  });
} else {
  // A clustered worker that loses its primary is shutting down; do not try to
  // send handles or log an EPIPE stack.
  process.on('disconnect', () => process.exit(0));
  process.on('uncaughtException', (err) => {
    if (err && err.code === 'EPIPE') process.exit(0);
    console.error(err);
    process.exit(1);
  });
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
          server.listen(PORT, LOOPBACK);
        }, 100);
        return;
      }
    }
    throw err;
  });
  server.listen(PORT, LOOPBACK, () => {
  });
}`

// apiServerBunScriptContent is the Bun-native API sidecar: it uses Bun.serve
// instead of node:http, so it skips the Node http stack (and its cluster shim)
// for higher throughput. It serves the same dist/api-node ESM routes and the
// /__krate/middleware endpoint. Selected by ssr.apiRuntime: "bun" (or the
// top-level runtime: "bun").
var apiServerBunScriptContent = `import path from 'node:path';
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

// Exit when the Krate host dies: a SIGKILL or OOM kill gives it no chance to stop its sidecars.
const PARENT_PID = parseInt(process.env.KRATE_PARENT_PID || '', 10);
if (PARENT_PID > 0) {
  const parentWatch = setInterval(() => {
    try {
      process.kill(PARENT_PID, 0);
    } catch (e) {
      if (!e || e.code !== 'EPERM') process.exit(0);
    }
  }, 2000);
  if (parentWatch.unref) parentWatch.unref();
}

const fileCache = new Map();
const moduleCache = new Map();

let middlewareModule = null;

function json(body, status) { return Response.json(body, { status: status || 200 }); }


` + actionSidecarJS + apiSidecarSharedJS + `
await krateLoadMiddleware();
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
  if (!target) target = resolveDynamicFile(API_DIR, clean, exts);
  fileCache.set(key, target);
  return target;
}

async function loadModule(file) {
  if (!HOT) { const cached = moduleCache.get(file); if (cached) return cached; }
  const mod = HOT ? await krateImportFresh(file, String(Date.now())) : await import(pathToFileURL(file).href);
  if (!HOT) moduleCache.set(file, mod);
  return mod;
}

async function handleMiddleware(req) {
  if (HOT) await krateLoadMiddleware();
  if (!middlewareModule || !middlewareModule.middleware) return json({ action: 'continue', headers: [] });
  let body = {};
  try { body = await req.json(); } catch {}
  try {
    const hasMatcher = !!krateMatchersFor(middlewareModule);
    if (!krateMiddlewareMatches(middlewareModule, body.path || new URL(req.url).pathname)) return json({ action: 'continue', headers: [], matcher: hasMatcher });
    const request = krateAttach(new Request(body.url || req.url, { method: body.method || 'GET', headers: krateRequestHeaders(body.headers) }));
    const result = await middlewareModule.middleware(request);
    return json({ ...(await krateMiddlewareJSON(result)), matcher: hasMatcher });
  } catch (e) {
    console.error('[krate] middleware error: ' + ((e && e.stack) || e));
    return json({ action: 'error', error: (e && e.message) || String(e) });
  }
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
      setHeader(k, v) { outHeaders[k] = Array.isArray(v) ? v.map(String).join(', ') : String(v); },
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

function routeParams(file, segments) {
  const rel = path.relative(API_DIR, file).replace(/\.[^.]+$/, '');
  let fileSegs = rel.split(path.sep);
  if (fileSegs[fileSegs.length - 1] === 'index') fileSegs = fileSegs.slice(0, -1);
  const params = {};
  for (let i = 0; i < fileSegs.length; i++) {
    const seg = fileSegs[i];
    if (seg.startsWith('[[...') && seg.endsWith(']]')) {
      if (i < segments.length) params[seg.slice(5, -2)] = segments.slice(i).join('/');
      return params;
    }
    if (seg.startsWith('[...') && seg.endsWith(']')) {
      params[seg.slice(4, -1)] = segments.slice(i).join('/');
      return params;
    }
    if (seg.startsWith('[') && seg.endsWith(']')) {
      if (i < segments.length) params[seg.slice(1, -1)] = segments[i];
    }
  }
  return params;
}

function isDynamicSegment(name) {
  return name.startsWith('[') && name.endsWith(']') && !name.startsWith('[...') && !name.startsWith('[[...');
}
function isCatchAllSegment(name) {
  return name.startsWith('[...') || name.startsWith('[[...');
}
function resolveDynamicFile(dir, segments, exts) {
  let entries;
  try { entries = fs.readdirSync(dir, { withFileTypes: true }); } catch { return null; }
  if (segments.length === 0) {
    for (const ext of exts) {
      const index = path.join(dir, 'index' + ext);
      if (fs.existsSync(index)) return index;
    }
    return null;
  }
  if (segments.length > 0) {
    const next = path.join(dir, segments[0]);
    try {
      if (fs.statSync(next).isDirectory()) {
        const hit = resolveDynamicFile(next, segments.slice(1), exts);
        if (hit) return hit;
      }
    } catch {}
  }
  for (const entry of entries) {
    const name = entry.name;
    const full = path.join(dir, name);
    if (entry.isDirectory()) {
      if (isCatchAllSegment(name) && segments.length > 0) {
        for (const ext of exts) {
          const index = path.join(full, 'index' + ext);
          if (fs.existsSync(index)) return index;
        }
      }
      if (isDynamicSegment(name) && segments.length > 0) {
        const hit = resolveDynamicFile(full, segments.slice(1), exts);
        if (hit) return hit;
      }
      continue;
    }
    const base = name.replace(/\.[^.]+$/, '');
    if (isCatchAllSegment(base) && (segments.length > 0 || base.startsWith('[[...'))) return full;
    if (segments.length === 1 && isDynamicSegment(base)) return full;
  }
  return null;
}

Bun.serve({
  port: PORT,
  hostname: '127.0.0.1',
  async fetch(req) {
    if (!sidecarAuthorized(req.headers.get('x-krate-internal'))) {
      return new Response('forbidden', { status: 403, headers: { 'content-type': 'text/plain; charset=utf-8' } });
    }
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
    try { mod = await loadModule(file); } catch (e) { return json(krateErrorBody(e, 'load ' + urlPath), 500); }
    const picked = krateHandler(mod, req.method.toUpperCase());
    if (!picked.fn) {
      if (picked.status === 204) return new Response(null, { status: 204, headers: { Allow: picked.allow.join(', ') } });
      const r = json(picked.status === 405 ? { error: 'Method Not Allowed' } : { error: picked.error }, picked.status);
      if (picked.allow) r.headers.set('Allow', picked.allow.join(', '));
      return r;
    }
    const fn = picked.fn;
    const namedHandler = picked.named;
    const routeParamsValue = routeParams(file, clean);
    try {
      const revalidateScope = newRevalidateScope('api');
      const request = krateAttach(req);
      const out = await REVALIDATE_STORE.run(revalidateScope, async () => ((fn.length >= 2 && !namedHandler) ? legacyInvoke(fn, req, url) : toResponse(await fn(request, { params: routeParamsValue }))));
      // revalidatePath/revalidateTag calls are applied before responding.
      await flushRevalidate(revalidateScope);
      if (picked.headOnly) return new Response(null, { status: out.status, headers: out.headers });
      return out;
    } catch (e) { return json(krateErrorBody(e, req.method + ' ' + urlPath), 500); }
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
	// NoStore is set when the render read request state (headers/cookies) or
	// otherwise opted out of caching. The page response must not be public.
	NoStore bool `json:"noStore,omitempty"`
	// Fatal marks a failed render that has no usable baked content (an async
	// server component): the response must be an error, not the shell.
	Fatal bool `json:"fatal,omitempty"`
	// Digest and Stack describe a failed render: the renderer logs the full
	// error with the digest, and the server shows visitors only the digest.
	Digest string `json:"digest,omitempty"`
	Stack  string `json:"stack,omitempty"`
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
	// noStore is true when any region read request state or otherwise opted
	// out of caching. The page response must not be publicly cacheable.
	noStore bool
	// errStack and errDigest describe the failed render behind renderErr.
	errStack  string
	errDigest string
	// unavailable is non-empty when the renderer could not be reached at all.
	// Nothing was written; the caller answers 503 rather than serving the baked
	// shell as if the page had rendered.
	unavailable string
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
	hash    string
	shell   string
	roots   []*regionBoundary
	all     []*regionBoundary
	ok      bool
}

// Small shells are verified by content hash on every lookup so a same-size
// rewrite with a preserved/coarse mtime cannot serve a stale marker tree.
const regionShellVerifyLimit = 1 << 20

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
	e, ok := c.m[absPath]
	c.mu.Unlock()
	if ok && e.size == fi.Size() && e.modTime.Equal(fi.ModTime()) && e.hash != "" {
		if fi.Size() > regionShellVerifyLimit {
			return e, nil
		}
		data, err := os.ReadFile(absPath)
		if err == nil && shellHash(data) == e.hash {
			return e, nil
		}
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	e = parseRegionShell(string(data))
	e.size = fi.Size()
	e.modTime = fi.ModTime()
	e.hash = shellHash(data)

	c.mu.Lock()
	c.m[absPath] = e
	c.mu.Unlock()
	return e, nil
}

func shellHash(data []byte) string {
	sum := sha256.Sum256(data)
	return string(sum[:])
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
func streamRegionPage(w http.ResponseWriter, flusher http.Flusher, shells *regionShellCache, absOut, route string, ssrPort int, params, query, headers map[string]string, goRenders map[string]goRenderRegion, renderGo func([]goRenderRegion) (map[string]string, error), isISR bool, revalidate int, internalSecret string) regionPageResult {
	return streamRegionPageMode(w, flusher, shells, absOut, route, ssrPort, params, query, headers, goRenders, renderGo, isISR, revalidate, internalSecret, false)
}

func streamRegionPageMode(w http.ResponseWriter, flusher http.Flusher, shells *regionShellCache, absOut, route string, ssrPort int, params, query, headers map[string]string, goRenders map[string]goRenderRegion, renderGo func([]goRenderRegion) (map[string]string, error), isISR bool, revalidate int, internalSecret string, outOfOrder bool) regionPageResult {
	return streamRegionPageModeSC(w, flusher, shells, absOut, route, ssrPort, params, query, headers, goRenders, renderGo, isISR, revalidate, internalSecret, outOfOrder, false, false)
}

// streamRegionPageModeSC is streamRegionPageMode with serverComponents set for
// pages that render async server components. Those stream out of order only
// when every dynamic region is a Suspense boundary: a failed boundary then
// keeps its fallback (React semantics), while a bare async component outside
// Suspense stays on the ordered path so its failure can still become a 500.
// failOnRenderErr turns a failed page render into a fatal result (nothing is
// written) so the caller can answer with the route's error page instead of
// the baked body.
func streamRegionPageModeSC(w http.ResponseWriter, flusher http.Flusher, shells *regionShellCache, absOut, route string, ssrPort int, params, query, headers map[string]string, goRenders map[string]goRenderRegion, renderGo func([]goRenderRegion) (map[string]string, error), isISR bool, revalidate int, internalSecret string, outOfOrder, serverComponents, failOnRenderErr bool) regionPageResult {
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

	// Sibling Suspense regions can stream as they finish. The shell is written
	// first and each region chunk inserts itself at its marker, so a slow region
	// never holds up the rest of the page. Page/ISR and nested regions keep the
	// ordered assembly path because they carry status or contain child markers.
	if outOfOrder && !isISR && canStreamOutOfOrder(all) && (!serverComponents || allSuspense(all)) {
		return streamSiblingRegions(w, flusher, shell, regionList, goReq, renderGo, string(reqCtx), ssrPort, route, params, query, headers, internalSecret)
	}

	// Only involve the Node SSR sidecar when it has something to render. A page
	// whose regions are all Go (or static) is served by the Go server + Go host
	// alone - no node/bun process, no extra hop.
	if len(regionList) > 0 {
		if err := fetchNodeRegions(ssrPort, route, params, query, headers, regionList, regionHTML, &res, internalSecret); err != nil {
			res.unavailable = err.Error()
			return res
		}
	}

	// Render runtime Go-component regions via the Go host and merge their HTML.
	if len(goReq) > 0 && renderGo != nil {
		goHTML, gerr := renderGo(goReq)
		if gerr != nil {
			res.renderErr = gerr.Error()
		} else {
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
				resolved, gerr := resolveGoMarkersE(h, string(reqCtx), renderGo)
				if gerr != nil {
					res.renderErr = gerr.Error()
					continue
				}
				regionHTML[id] = resolved
			}
		}
	}

	if failOnRenderErr && res.renderErr != "" && res.fatalErr == "" && !res.notFound && res.redirect == "" {
		res.fatalErr = res.renderErr
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

	// ISR cache headers are decided here, after every region frame is known:
	// a render that read request state (headers/cookies) or was invalidated
	// while running must not be public. Non-ISR pages are already no-store.
	if isISR {
		if res.noStore || res.renderErr != "" {
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Del("X-Krate-Cache")
		} else {
			if revalidate <= 0 {
				revalidate = defaultISRRevalidate
			}
			w.Header().Set("Cache-Control", fmt.Sprintf("public, s-maxage=%d, stale-while-revalidate=%d", revalidate, 2*revalidate))
		}
	}

	// The caller owns Content-Type and the no-store header for non-ISR pages.
	// The 200 status is committed implicitly by the first body write below;
	// this function only writes the body, flushing after each splice so the
	// page streams progressively.
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
func fetchNodeRegions(ssrPort int, route string, params, query, headers map[string]string, regionList []map[string]string, regionHTML map[string]string, res *regionPageResult, internalSecret string) error {
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
	if internalSecret != "" {
		req.Header.Set(internalSecretHeader, internalSecret)
	}
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
			// The renderer died or the stream was cut mid-way: the regions that
			// never arrived keep their baked content, which must not pass as a
			// successful (and cacheable) render.
			if res.renderErr == "" {
				res.renderErr = fmt.Sprintf("ssr regions: stream ended with %d of %d regions missing", framesLeft, len(regionList))
			}
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
			if frame.NoStore {
				res.noStore = true
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
					res.errStack, res.errDigest = frame.Stack, frame.Digest
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
				res.errStack, res.errDigest = frame.Stack, frame.Digest
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
	if hub != nil {
		port = cfg.DevPort()
	}

	// Bind the listener before any sidecar starts: a taken port must fail (or,
	// in dev, move to the next free one) without leaving an orphaned renderer.
	bindHost := strings.TrimSpace(cfg.Server.Host)
	if hub != nil {
		// Dev binds loopback unless devServer.host/server.host says otherwise.
		bindHost = devBindHost(cfg)
	}
	listener, err := bindServerListener(bindHost, port, hub != nil)
	if err != nil {
		return err
	}
	listenerOwned := false
	defer func() {
		if !listenerOwned {
			_ = listener.Close()
		}
	}()
	port = listener.Addr().(*net.TCPAddr).Port
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
		if hub != nil {
			// Dev reloads the module whenever .krate/middleware.js changes (and
			// picks it up if it is added after startup).
			middlewareRT = jsruntime.NewLazyMiddlewareRuntime(filepath.Join(root, ".krate", "middleware.js"))
		} else {
			middlewareRT = jsruntime.LoadMiddlewareRuntime(root)
		}
		if middlewareRT != nil {
			middlewareRT.SetEnv(serverEnv)
		}
	}

	// The embedded QuickJS runtime serves JS/TS API routes in-process by
	// default, so the Node API sidecar only needs to start when it is actually
	// selected (apiRuntime / middlewareRuntime is node/bun/deno) or when a route
	// uses external imports the embedded runtime cannot run.
	apiDir := apiBundleDir(cfg.OutDir)
	nodeAPIDir := apiNodeBundleDir(cfg.OutDir)
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
	apiBodyLimit := cfg.MaxAPIBodyBytes()

	var nodeAPICmdMu sync.Mutex
	var nodeAPICmd *exec.Cmd
	var nodeAPIStopOnce sync.Once
	nodeAPIStop := make(chan struct{})
	if needNodeSidecar {
		// An explicit node-family apiRuntime selects the sidecar command;
		// otherwise fall back to the project's top-level runtime.
		sidecarRuntime := strings.ToLower(cfg.Runtime)
		if isNodeFamilyRuntime(apiRTOption) {
			sidecarRuntime = apiRTOption
		}
		if sidecarRuntime == "" {
			sidecarRuntime = "node"
		}
		printf("  %s*%s API sidecar (%s) -> %shttp://localhost:%d%s\n", cCyan, cReset, sidecarRuntime, cCyan, apiPort, cReset)
		revalHost.SetNodeAPI(fmt.Sprintf("http://127.0.0.1:%d", apiPort))

		cacheRuntime := stageCacheRuntime(root, cfg.OutDir)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					_ = safe.Recover(r)
				}
			}()

			portStr := fmt.Sprintf("%d", apiPort)
			makeCmd := func() *exec.Cmd {
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
					fmt.Sprintf("KRATE_API_DEV=%d", boolToInt(hub != nil)),
					fmt.Sprintf("KRATE_ACTION_MAX_BODY=%d", apiBodyLimit),
					fmt.Sprintf("KRATE_PARENT_PID=%d", os.Getpid()),
				)
				cmd.Env = append(cmd.Env, revalHost.EnvList()...)
				if cacheRuntime != "" {
					cmd.Env = append(cmd.Env, "KRATE_CACHE_RUNTIME="+cacheRuntime)
				}
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				return cmd
			}

			delay := 100 * time.Millisecond
			for {
				select {
				case <-nodeAPIStop:
					return
				default:
				}
				cmd := makeCmd()
				nodeAPICmdMu.Lock()
				nodeAPICmd = cmd
				nodeAPICmdMu.Unlock()
				started := time.Now()
				err := cmd.Run()
				select {
				case <-nodeAPIStop:
					return
				default:
				}
				if time.Since(started) > 10*time.Second {
					delay = 100 * time.Millisecond
				}
				fmt.Fprintf(os.Stderr, "  %s! Node API sidecar exited (%v); restarting in %s%s\n", cYellow, err, delay, cReset)
				select {
				case <-nodeAPIStop:
					return
				case <-time.After(delay):
				}
				if delay < 5*time.Second {
					delay *= 2
				}
			}
		}()
	}

	// Start the Go API sidecar if Go API routes were compiled during the build.
	// Go routes take precedence over JS routes for the same /api path.
	var goAPI *goAPISupervisor
	goAPIManifestPath := gohost.RoutesManifestPath(root)
	if _, err := os.Stat(goAPIManifestPath); err == nil {
		goAPIPort := internalPort(port + 2)
		goAPI = newGoAPISupervisor(goAPIServerBinPath(root), goAPIManifestPath, goAPIPort)
		// Prefer an AF_UNIX socket so the sidecar needs no port and the hop
		// skips the loopback TCP stack. The path is chosen here and handed to
		// the child verbatim, so both sides always agree; when no usable socket
		// path exists the child listens on the TCP port instead.
		if goAPISocket := goAPISocketPath(root); goAPISocket != "" {
			_ = os.Remove(goAPISocket)
			goAPI.SetSocket(goAPISocket)
		}
		goAPI.SetEnv(environ.KVList(serverEnv))
		revalHost.SetGoAPI(goAPI)
		if err := goAPI.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "  %s! Go API sidecar not started:%s %v\n", cYellow, cReset, err)
			goAPI.Close()
			goAPI = nil
		} else if goAPI.socket != "" {
			printf("  %s*%s Go API -> %s%s%s\n", cCyan, cReset, cCyan, goAPI.socket, cReset)
		} else {
			printf("  %s*%s Go API -> %shttp://localhost:%d%s\n", cCyan, cReset, cCyan, goAPIPort, cReset)
		}
	}

	absOut := revalOut

	fileServer := http.FileServer(hardenedDir{http.Dir(absOut)})
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
	// Every request the host forwards carries the per-process secret (replacing
	// anything the client sent), so the sidecar can refuse direct connections
	// that bypass the middleware, CSRF and body-limit checks done here.
	baseDirector := apiProxy.Director
	apiProxy.Director = func(r *http.Request) {
		baseDirector(r)
		r.Header.Set(internalSecretHeader, revalHost.secret)
	}
	apiProxy.Transport = newSidecarTransport()

	// Optional user-provided API sidecar. Requests it owns are forwarded first;
	// a 404 falls through to the built-in Go/TS/QuickJS routes below.
	customSidecar, err := newAPISidecar(root, cfg)
	if err != nil {
		return err
	}
	if customSidecar != nil {
		if err := customSidecar.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "  %s! API sidecar not started:%s %v\n", cYellow, cReset, err)
		} else {
			target := cfg.API.Sidecar.Target
			if target == "" {
				target = fmt.Sprintf("http://127.0.0.1:%d", cfg.API.Sidecar.Port)
			}
			mode := "proxy"
			if cfg.API.Sidecar.Command != "" {
				mode = "supervised"
			}
			printf("  %s*%s API sidecar (%s) %s -> %s%s%s\n", cCyan, cReset, mode, customSidecar.Prefix(), cCyan, target, cReset)
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

	// Middleware (matcher-scoped) runs in front of pages, API routes and
	// server actions alike.
	gate := &middlewareGate{
		rt:         middlewareRT,
		dev:        hub != nil,
		sidecarURL: fmt.Sprintf("http://localhost:%d/__krate/middleware", apiPort),
		file:       middlewarePath,
		secret:     revalHost.secret,
		staticExists: func(urlPath string) bool {
			f, err := hardenedDir{http.Dir(absOut)}.Open(urlPath)
			if err != nil {
				return false
			}
			defer f.Close()
			fi, err := f.Stat()
			return err == nil && !fi.IsDir()
		},
	}

	if hasActions {
		mux.Handle("/__krate/action/", loggingMiddleware(gate.wrap(http.HandlerFunc(actionHandler(apiProxy, apiBodyLimit)))))
	}

	apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		// A path owned only by Go routes answers methods it does not define
		// the way the JS runtimes do: 405 + Allow, and automatic OPTIONS.
		if goAPI != nil && goAPI.Active() && apiRT != nil && apiRT.ResolveFile(r.URL.Path) == "" {
			if allowed, ok := goAPI.AllowedMethods(r.URL.Path); ok && writeGoMethodResponse(w, r, allowed) {
				return
			}
		}

		// Routes that need Node (external imports the embedded runtime cannot
		// run) are forwarded to the Node sidecar even when QuickJS is default.
		if apiRT != nil && nodeOnlyRoutes[apiRT.ResolveFile(r.URL.Path)] {
			apiProxy.ServeHTTP(newDeadlineWriter(w), r)
			return
		}

		// Use embedded quickjs runtime if available
		if apiRT != nil {
			serveQuickJSAPI(w, r, apiRT, hub != nil)
			return
		}

		// Fallback: proxy to sidecar
		apiProxy.ServeHTTP(newDeadlineWriter(w), r)
	})
	mux.Handle("/api/", loggingMiddleware(gate.wrap(apiHandler)))

	// Load the SSR manifest for route lookup (server-manifest.json). The Node
	// renderer process only starts when a page/region actually needs it - a site
	// whose request-time regions are all Go is served by the Go server + Go host
	// alone (no node/bun process).
	ssrPort := cfg.SSR.RendererPort
	if ssrPort == 0 {
		ssrPort = internalPort(port + 10)
	}
	ssr := NewSSRServer(root, ssrPort, cfg.SSR.SSRRuntime)
	ssr.SetOutDir(absOut)
	ssr.SetEnv(serverEnv)
	ssr.SetInternalSecret(revalHost.secret)
	ssr.SetTuning(cfg.SSR.Timeout, cfg.SSR.MaxCacheSize)
	ssr.SetPPR(cfg.PPR, cfg.PPRRevalidate)
	ssr.SetRendererWorkers(cfg.SSR.RendererWorkers)
	ssrStarted := false
	ssrStop := make(chan struct{})
	var ssrStopOnce sync.Once
	if err := ssr.LoadManifest(); err != nil {
		// No request-time pages: nothing routes through the renderer.
	} else if !ssr.NeedsNodeRenderer() {
		printf("  %s*%s SSR renderer skipped (all request-time regions render on the Go host)\n", cCyan, cReset)
	} else if err := ssr.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "  %s! SSR renderer not started:%s %v\n", cYellow, cReset, err)
	} else {
		ssrStarted = true
		revalHost.SetSSR(ssr, ssrPort)
		go ssr.Supervise(ssrStop)
		printf("  %s*%s SSR renderer -> %shttp://localhost:%d%s\n", cCyan, cReset, cCyan, ssrPort, cReset)
	}

	// SSR/ISR/Streaming route handler - intercepts before static file server
	if ssrStarted {
		mux.HandleFunc("/__krate/ssr/", ssrInternalProxy(revalHost, ssrPort))
		// Dev: invalidate the renderer's caches once per rebuild, not once per
		// connected tab.
		if hub != nil {
			hub.SetReloadHook(func(ev DevEvent) { ssr.InvalidateDevRoutes(ev.Routes) })
		}
	}

	// Segment pages: the nearest 404/500 page for a request (see segments.go).
	segs := newSegmentIndex(absOut)
	devMode := hub != nil

	// Scan for dynamic routes ([param] directories) in the output
	dynRoutes := findDynamicRoutes(absOut)
	if len(dynRoutes) > 0 {
		printf("  %s*%s Dynamic routes: %d patterns\n", cCyan, cReset, len(dynRoutes))
	}

	// Opt-in User-Agent document selection (compat.mode hybrid + userAgent).
	uaOn := uaOptIn(cfg.Compat)

	// Files the build content-hashed; only these are cached as immutable.
	hashedAssets := loadHashedAssets(absOut)

	// Static file layer. In serve mode, build-time precompressed siblings are
	// used here, after middleware, redirects and the request-time page
	// decision, and never for the HTML shell of an ssr/isr/streaming page.
	var staticServer http.Handler = fileServer
	if hub == nil {
		staticServer = precompressedMiddlewareIf(absOut, func(r *http.Request) bool {
			if !isHTMLDocRequest(r.URL.Path) && path.Ext(r.URL.Path) != "" {
				return false
			}
			// A page request carrying flash state is rendered with it, not
			// answered from the build-time snapshot.
			if hasFlashCookie(r) {
				return true
			}
			page, _ := ssr.FindPageForRoute(requestRoute(r.URL.Path))
			return page != nil && page.Mode != RenderSSG.String()
		}, fileServer)
	}

	handlerWith404 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server-only build artifacts live under dist/.krate. Never serve them
		// from the static handler; they contain bundles, the renderer, and the
		// persisted ISR cache.
		if isInternalOutputPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		// Source maps are JSON; set the type explicitly since `.map` has no
		// standard mime entry on every platform (FileServer only sets a type
		// when one isn't already present).
		if strings.HasSuffix(r.URL.Path, ".map") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		}
		// Hashed assets (styles.<hash>.css, index.<hash>.js, chunks/*.<hash>.js,
		// assets/<name>-<hash>.*) are content-addressed, so they can be cached
		// immutably. HTML must never be, since it changes per build.
		w.Header().Set("Cache-Control", staticCacheControl(r.URL.Path, hub != nil, hashedAssets))

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
		closedPattern := ""
		if !staticRouteExists(absOut, r.URL.Path) {
			// Check dynamic routes - if a URL matches a [param] pattern, serve the template
			for _, dr := range dynRoutes {
				if staticOnlyRoutes[normalizeRoutePattern(dr.pattern)] {
					// Static-only route: valid params were baked at build time and
					// the fallback template was not emitted. Unknown params 404, with
					// the not-found page nearest to that route.
					if closedPattern == "" {
						if _, ok := matchDynamicRoute(r.URL.Path, dr.pattern); ok {
							closedPattern = dr.pattern
						}
					}
					continue
				}
				if params, ok := matchDynamicRoute(r.URL.Path, dr.pattern); ok {
					if redirectToCanonicalSlash(w, r) {
						return
					}
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

		// Only buffer document requests. Buffering every asset just to detect a
		// missing one would retain whole images/videos/fonts in memory, and a
		// site without segment pages has nothing to substitute.
		if entries, _ := segs.load(); len(entries) == 0 || (!isHTMLDocRequest(r.URL.Path) && !strings.Contains(r.Header.Get("Accept"), "text/html")) {
			staticServer.ServeHTTP(w, r)
			return
		}
		// Buffer the response so we can replace Go's default "404 page not found"
		buf := &captureResponse{status: http.StatusOK}
		staticServer.ServeHTTP(buf, r)
		if buf.status == http.StatusNotFound {
			if m, ok := segs.forURL(SegmentNotFound, r.URL.Path, closedPattern); ok && respondSegment(w, segs, m, http.StatusNotFound, nil) {
				return
			}
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
		devHost := devBindHost(cfg)
		guard := func(pattern string, h http.HandlerFunc) {
			mux.Handle(pattern, devEndpointGuard(devHost, h))
		}
		guard("/__krate/hotreload", func(w http.ResponseWriter, r *http.Request) {
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
						data, _ := json.Marshal(struct {
							Pages   []string `json:"pages"`
							CSSOnly bool     `json:"cssOnly,omitempty"`
						}{routes, ev.CSSOnly})
						fmt.Fprintf(w, "event: reload\ndata: %s\n\n", data)
					default:
						// A reload with no diagnostics also clears any error
						// the tab was showing; the client does that itself.
						_, _ = fmt.Fprintf(w, "event: reload\ndata: {\"cssOnly\":%t}\n\n", ev.CSSOnly)
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
		guard("/__krate/client-error", func(w http.ResponseWriter, r *http.Request) {
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
		guard("/__krate/status", func(w http.ResponseWriter, r *http.Request) {
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
		guard("/__krate/open", func(w http.ResponseWriter, r *http.Request) {
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
		// Pages are read-only resources. Mutations go to /api or server
		// actions, which are routed before this handler.
		if !pageMethodAllowed(w, r) {
			return
		}
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
		// SSG pages answer /x with a 301 to /x/; request-time pages use the
		// same single canonical form.
		if redirectToCanonicalSlash(w, r) {
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
		// renders each region. ISR cache headers are decided by
		// streamRegionPage once the region renders report whether they read
		// request state; non-ISR pages are always no-store.
		flusher, _ := w.(http.Flusher)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !isISR {
			w.Header().Set("Cache-Control", "no-store")
		}
		reval := 0
		if isISR {
			reval = ssr.GetRevalidate(route)
			if reval <= 0 {
				reval = defaultISRRevalidate
			}
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
		outOfOrder := page != nil && page.Mode == RenderStreaming.String()
		errMatch, hasErrPage := segs.forRoute(SegmentError, r.URL.Path, page.SegDir)
		rr := streamRegionPageModeSC(w, flusher, regionShells, absOut, route, regionPort, params, query, headers, routeGo, renderGo, isISR, reval, ssr.InternalSecret(), outOfOrder, page.ServerComponents, hasErrPage)
		if rr.served && rr.unavailable != "" {
			// The renderer is down or unreachable. Serving the baked shell with a
			// 200 would hide the outage (and let a CDN cache the empty page).
			fmt.Fprintf(os.Stderr, "  %sSSR renderer unavailable (%s):%s %s\n", cRed, route, cReset, rr.unavailable)
			h := w.Header()
			h.Set("X-Krate-Error", "renderer-unavailable")
			h.Set("Cache-Control", "no-store")
			h.Set("Retry-After", "1")
			h.Del("X-Krate-Cache")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("<!doctype html><title>503</title><h1>Service Unavailable</h1>"))
			return
		}
		if rr.served && rr.fatalErr != "" {
			// The render failed: answer 500 with the route's nearest error page
			// (the full error stays in the log, the page gets a sanitized one).
			srvErr := newServerError(rr.fatalErr, rr.errStack, rr.errDigest, devMode)
			fmt.Fprintf(os.Stderr, "  %sRender error (%s) [digest %s]:%s %s\n", cRed, route, srvErr.Digest, cReset, rr.fatalErr)
			w.Header().Set("X-Krate-Error", "render")
			w.Header().Del("X-Krate-Cache")
			if hasErrPage && respondSegment(w, segs, errMatch, http.StatusInternalServerError, &srvErr) {
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("<!doctype html><title>500</title><h1>Internal Server Error</h1>"))
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
				// notFound() or an unmatched variant: the page nearest to the route,
				// never cached as a success.
				w.Header().Del("X-Krate-Cache")
				if m, ok := segs.forRoute(SegmentNotFound, r.URL.Path, page.SegDir); ok && respondSegment(w, segs, m, http.StatusNotFound, nil) {
					return
				}
				w.Header().Set("Cache-Control", "no-store")
				http.NotFound(w, r)
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
				// rewriteDestination percent-encodes substituted values; the
				// request path holds the decoded form.
				if dec, err := url.PathUnescape(rewritten); err == nil {
					rewritten = dec
				}
				r.URL.Path = rewritten
				r.URL.RawPath = ""
				break
			}
		}

		ssrPageHandler.ServeHTTP(w, r)
	})

	// User middleware handler for pages (also wraps /api and server actions
	// above).
	var middlewareHandler http.Handler = gate.wrap(redirectRewriteHandler)

	// Final handler chain: userMiddleware -> redirectRewrite -> logging -> ssrPageHandler.
	// When community plugins with serve hooks are configured, wrap the chain in
	// the plugin serve handler (ServeRequest interceptor + ServeResponse buffer).
	var top http.Handler = middlewareHandler
	if len(cfg.Plugins) > 0 {
		top = wirePluginServeHandlers(root, cfg, top)
	}
	// No-JS form feedback: render a pending flash state into the page.
	top = flashNoticeMiddleware(top)
	if hub == nil {
		// Preview/static serving: gzip compressible assets, and serve
		// build-time precompressed (.br/.gz) siblings when present.
		top = gzipMiddleware(top)
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
	var devMux http.Handler = mux
	if hub != nil {
		devMux = devErrorShell(hub, root, absOut, mux)
	}
	proxyTrust.Store(cfg.Server.TrustProxy)
	proxyHops.Store(int32(cfg.Server.TrustedProxies))
	var rootHandler http.Handler = requestIDMiddleware(forwardedHeadersMiddleware(cfg.Server.TrustProxy, basePathMiddleware(cfg.BaseURLPath(), securityHeadersMiddleware(cfg, customHeadersMiddleware(cfg.Headers, corsMiddleware(cfg, panicRecoveryMiddleware(Logger, devMux)))))))

	listenerOwned = true
	addr := listener.Addr().(*net.TCPAddr)
	label := "dev"
	if hub == nil {
		label = "serve"
	}
	displayHost := bindHost
	if displayHost == "" || displayHost == "0.0.0.0" || displayHost == "::" || displayHost == "127.0.0.1" {
		displayHost = "localhost"
	}
	printf("%s  %s server -> %shttp://%s:%d%s %s(started in %s)%s\n", cGreen, label, cCyan, displayHost, addr.Port, cReset, cGray, time.Since(startTime).Round(time.Millisecond), cReset)

	// Start ISR background revalidation - one timer per page, each route
	// revalidating on its own cadence (previously every ISR page was revalidated
	// together on the shortest interval, stampeding every request at once). The
	// refresh is non-destructive: the sidecar re-renders each cached variant in
	// place so dynamic variants are never evicted. The sidecar also serves stale
	// HTML while a revalidation runs, so expiry never blocks a request even
	// before the timer fires.
	var isrWg sync.WaitGroup
	isrDone := make(chan struct{})
	var isrDoneOnce sync.Once
	if ssrStarted {
		isrPages := ssr.GetISRPages()
		if len(isrPages) > 0 {
			printf("  %s*%s ISR revalidation: %d pages\n", cCyan, cReset, len(isrPages))
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
					// The first refresh waits a full interval: the entries a
					// request just rendered are fresh, and refreshing at t=0
					// would re-render them (and change their body) right away.
					// A tick that lands while the renderer is restarting is
					// skipped; the supervisor brings it back.
					for {
						select {
						case <-ticker.C:
						case <-isrDone:
							return
						}
						if !ssr.IsRunning() {
							continue
						}
						if err := ssr.RefreshPage(page.Route); err != nil {
							fmt.Fprintf(os.Stderr, "  %sISR revalidation failed (%s):%s %v\n", cYellow, page.Route, cReset, err)
						}
					}
				}()
			}
		}
	}

	// Graceful shutdown: SIGINT/SIGTERM (containers send SIGTERM) stop the
	// listener, let in-flight requests finish within the grace period, and only
	// then stop the sidecars they may still be using. A clean shutdown returns
	// nil (exit 0); a second signal forces an immediate exit.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	httpServer := newHTTPServer(rootHandler)
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(listener) }()

	if cfg.DevServer.Open {
		openBrowser(fmt.Sprintf("http://localhost:%d", addr.Port))
	}

	defer plugin.CloseGoPlugins()

	stopSidecars := func() {
		defer func() {
			if r := recover(); r != nil {
				_ = safe.Recover(r)
			}
		}()
		isrDoneOnce.Do(func() { close(isrDone) })
		nodeAPIStopOnce.Do(func() { close(nodeAPIStop) })
		if ssrStarted {
			ssrStopOnce.Do(func() { close(ssrStop) })
			ssr.Shutdown()
			printf("  %s+%s SSR renderer stopped\n", cGreen, cReset)
		}
		if goAPI != nil {
			goAPI.Close()
			printf("  %s+%s Go API sidecar stopped\n", cGreen, cReset)
		}
		if customSidecar != nil {
			customSidecar.Stop()
			printf("  %s+%s API sidecar stopped\n", cGreen, cReset)
		}
		nodeAPICmdMu.Lock()
		nc := nodeAPICmd
		nodeAPICmdMu.Unlock()
		if nc != nil && nc.Process != nil {
			execx.KillTree(nc)
			printf("  %s+%s Node API sidecar stopped\n", cGreen, cReset)
		}
	}

	var restartCh <-chan struct{}
	if hub != nil {
		restartCh = hub.RestartRequested()
	}
	select {
	case err := <-serveErr:
		// The listener failed on its own (not a shutdown).
		stopSidecars()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-restartCh:
		// A config change restarts the dev server in place: close this server,
		// stop its sidecars, and let the caller start a new one on the same
		// ports.
		ctx, cancel := context.WithTimeout(context.Background(), serverShutdownGrace(cfg, true))
		if err := httpServer.Shutdown(ctx); err != nil {
			_ = httpServer.Close()
		}
		cancel()
		<-serveErr
		stopSidecars()
		return ErrDevRestart
	case <-sigCh:
	}

	printf("\n%sShutting down...%s\n", cGray, cReset)
	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "  %s! second signal, exiting immediately%s\n", cYellow, cReset)
		os.Exit(130)
	}()
	grace := serverShutdownGrace(cfg, hub != nil)
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "  %s! requests still running after %s, forcing close:%s %v\n", cYellow, grace, cReset, err)
		_ = httpServer.Close()
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		stopSidecars()
		return err
	}
	stopSidecars()
	return nil
}

// serverShutdownGrace is how long in-flight requests get to finish after a
// shutdown signal: server.shutdownTimeout seconds, default 15s. Dev keeps it
// short because its live-reload streams never finish on their own.
func serverShutdownGrace(cfg *config.Config, dev bool) time.Duration {
	if cfg.Server.ShutdownTimeout > 0 {
		return time.Duration(cfg.Server.ShutdownTimeout) * time.Second
	}
	if dev {
		return time.Second
	}
	return shutdownGrace
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

func copyDirToOut(src, dst string) ([]string, error) {
	var copied []string
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel != "." && isHiddenName(info.Name()) {
			// Dotfiles (.env, .git, .DS_Store) are never served, so copying them
			// would only ship secrets inside a static deploy.
			fmt.Fprintf(os.Stderr, "  %sWarning: not copying %s (dotfiles are not published; only .well-known is)%s\n", cYellow, filepath.ToSlash(path), cReset)
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			return err
		}
		copied = append(copied, filepath.ToSlash(rel))
		return nil
	})
	return copied, err
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

// matchRedirect checks if a request path matches a redirect/rewrite source
// pattern. Supports exact match, a trailing wildcard ("/old/*"), and named
// segment parameters ("/users/:id", "/files/:path*").
func matchRedirect(path, source string) bool {
	_, ok := matchRedirectParams(path, source)
	return ok
}

func matchRedirectParams(path, source string) (map[string]string, bool) {
	params, _, ok := matchRedirectParamsMods(path, source)
	return params, ok
}

// matchRedirectParamsMods is matchRedirectParams that also returns the modifier
// ('*', '+' or '?') each named parameter was declared with in the source.
func matchRedirectParamsMods(path, source string) (map[string]string, map[string]byte, bool) {
	params := map[string]string{}
	mods := map[string]byte{}
	// Literal sources match exactly; splitURLPath below intentionally ignores
	// trailing slashes for route matching, which must not make /old match /old/.
	if source == path {
		return params, mods, true
	}
	if !strings.ContainsAny(source, "*:") {
		return nil, nil, false
	}
	if strings.HasSuffix(source, "/*") {
		prefix := strings.TrimSuffix(source, "/*")
		if path == prefix {
			params["splat"] = ""
			return params, mods, true
		}
		if !strings.HasPrefix(path, prefix+"/") {
			return nil, nil, false
		}
		params["splat"] = strings.TrimPrefix(strings.TrimPrefix(path, prefix), "/")
		return params, mods, true
	}

	sourceParts := splitURLPath(source)
	pathParts := splitURLPath(path)
	pi := 0
	for _, part := range sourceParts {
		if strings.HasPrefix(part, ":") {
			name, mod := splitParamModifier(part[1:])
			mods[name] = mod
			switch mod {
			case '*':
				params[name] = strings.Join(pathParts[pi:], "/")
				return params, mods, true
			case '+':
				if pi >= len(pathParts) {
					return nil, nil, false
				}
				params[name] = strings.Join(pathParts[pi:], "/")
				return params, mods, true
			case '?':
				if pi < len(pathParts) {
					params[name] = pathParts[pi]
					pi++
				} else {
					params[name] = ""
				}
				continue
			}
			if pi >= len(pathParts) {
				return nil, nil, false
			}
			params[name] = pathParts[pi]
			pi++
			continue
		}
		if pi >= len(pathParts) || part != pathParts[pi] {
			return nil, nil, false
		}
		pi++
	}
	if pi != len(pathParts) {
		return nil, nil, false
	}
	return params, mods, true
}

// splitParamModifier splits a trailing '*', '+' or '?' off a parameter name.
func splitParamModifier(name string) (string, byte) {
	if n := len(name); n > 0 {
		switch name[n-1] {
		case '*', '+', '?':
			return name[:n-1], name[n-1]
		}
	}
	return name, 0
}

func splitURLPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

var destParamRe = regexp.MustCompile(`:([A-Za-z_][A-Za-z0-9_]*)`)

// escapeRedirectValue percent-encodes a matched (already decoded) path value
// for use inside a destination: each segment is escaped on its own so a "\"
// or an encoded "/" can never turn into a scheme-relative URL, while "/"
// between segments of a multi-segment value is kept.
func escapeRedirectValue(v string) string {
	if v == "" {
		return ""
	}
	segs := strings.Split(v, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return strings.Join(segs, "/")
}

// rewriteDestination fills ":splat" and ":name" (also ":name*", ":name+" and
// ":name?") placeholders in destination from the matched request path. Matched
// values are percent-encoded, and a path destination never starts with "//" or
// "/\" (an open redirect), whatever the request contained.
func rewriteDestination(path, source, destination string) string {
	params, mods, ok := matchRedirectParamsMods(path, source)
	if !ok {
		return destination
	}
	var out string
	if splat, ok := params["splat"]; ok {
		switch {
		case strings.Contains(destination, ":splat"):
			out = strings.ReplaceAll(destination, ":splat", escapeRedirectValue(splat))
		case splat != "":
			out = strings.TrimRight(destination, "/") + "/" + escapeRedirectValue(splat)
		default:
			out = destination
		}
	} else {
		out = replaceDestParams(destination, params, mods)
	}
	return sanitizeRedirectTarget(destination, out)
}

// replaceDestParams substitutes ":name" placeholders. A '*', '+' or '?' right
// after the name is part of the placeholder only when the source declared the
// parameter with that modifier, so "/x/:id?ref=1" keeps its query string.
func replaceDestParams(destination string, params map[string]string, mods map[string]byte) string {
	var sb strings.Builder
	last := 0
	for _, m := range destParamRe.FindAllStringSubmatchIndex(destination, -1) {
		name := destination[m[2]:m[3]]
		val, ok := params[name]
		if !ok {
			continue
		}
		end := m[1]
		if mod := mods[name]; mod != 0 && end < len(destination) && destination[end] == mod {
			end++
		}
		sb.WriteString(destination[last:m[0]])
		sb.WriteString(escapeRedirectValue(val))
		last = end
	}
	sb.WriteString(destination[last:])
	return sb.String()
}

// sanitizeRedirectTarget keeps a substituted path destination on-site. When the
// configured destination is a plain path (one leading slash), any run of
// leading slashes or backslashes produced by substitution collapses to a
// single slash and control characters are percent-encoded. Absolute URLs and
// an explicitly configured "//host" destination are left as configured.
func sanitizeRedirectTarget(configured, out string) string {
	if !strings.HasPrefix(configured, "/") || strings.HasPrefix(configured, "//") || strings.HasPrefix(configured, "/\\") {
		return out
	}
	var sb strings.Builder
	for _, r := range out {
		if r < 0x20 || r == 0x7f {
			fmt.Fprintf(&sb, "%%%02X", r)
			continue
		}
		sb.WriteRune(r)
	}
	out = sb.String()
	trimmed := strings.TrimLeft(out, "/\\")
	if trimmed != out {
		return "/" + trimmed
	}
	return out
}

// staticRouteExists reports whether the output dir holds a concrete static
// file for the URL path - in which case dynamic [param] routing must give way.
// isInternalOutputPath reports whether a request targets Krate's server-only
// output directory. It normalizes slash/backslash and repeated separators so
// encoded or Windows-shaped paths cannot slip past.
func isInternalOutputPath(urlPath string) bool {
	if decoded, err := url.PathUnescape(urlPath); err == nil {
		urlPath = decoded
	}
	p := strings.ReplaceAll(urlPath, "\\", "/")
	p = path.Clean("/" + p)
	if p == "/.krate" || strings.HasPrefix(p, "/.krate/") {
		return true
	}
	// Defense in depth: where older builds wrote server-only files into the
	// public root, they stay unreachable.
	return p == "/server-manifest.json" || p == "/api-node" || strings.HasPrefix(p, "/api-node/")
}

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
	data, err := os.ReadFile(ResolveManifestPath(absOut, "manifest.json"))
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
	data, err := os.ReadFile(ResolveManifestPath(absOut, "manifest.json"))
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
	return strings.EqualFold(u.Host, requestHost(r))
}

// sanitizeActionReturn accepts only a same-origin path for the post-action
// redirect. Absolute URLs and scheme-relative paths are dropped so an action
// form cannot be turned into an open redirect.
func sanitizeActionReturn(value string, r *http.Request) string {
	value = strings.TrimSpace(value)
	if value == "" || r == nil {
		return ""
	}
	if strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") &&
		!strings.ContainsAny(value, "\\\r\n") && !hasControlChar(value) {
		return value
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	if !strings.EqualFold(u.Host, requestHost(r)) || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.RequestURI()
}

func hasControlChar(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
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
