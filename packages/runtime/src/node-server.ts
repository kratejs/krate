// @krate/runtime/node-server - runs the portable Krate handler under plain Node.
//
// The `node` deploy target bundles this with the build data into
// dist/.krate/server/server.mjs. It needs no Go binary: it binds PORT/HOST,
// serves the built static files from dist with the same hardening as
// `krate start` (no dotfiles, no directory listings, Krate's server-only
// output hidden, hashed assets immutable, precompressed .br/.gz siblings,
// Range, Last-Modified), hands everything else to the handler, and drains
// in-flight requests on SIGTERM/SIGINT.

import fs from "node:fs";
import fsp from "node:fs/promises";
import http from "node:http";
import path from "node:path";
import zlib from "node:zlib";
import { Readable } from "node:stream";
import { fileURLToPath } from "node:url";
import type { KrateHandler, StaticLayer } from "./server-handler.js";
import { requestFlash } from "./handler-html.js";
import { extName, isHiddenPath, isInternalOutputPath } from "./handler-routing.js";

export interface NodeServerOptions {
  handler: KrateHandler;
  /** Directory holding the built site (dist). */
  distDir: string;
  trustProxy: boolean;
  /** Graceful-shutdown drain period in seconds (default 15). */
  shutdownTimeout?: number;
  /** Default port when PORT is unset. */
  port?: number;
}

const MIME: Record<string, string> = {
  ".html": "text/html; charset=utf-8",
  ".htm": "text/html; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".cjs": "text/javascript; charset=utf-8",
  ".json": "application/json",
  ".map": "application/json; charset=utf-8",
  ".webmanifest": "application/manifest+json",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".webp": "image/webp",
  ".avif": "image/avif",
  ".ico": "image/vnd.microsoft.icon",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
  ".otf": "font/otf",
  ".txt": "text/plain; charset=utf-8",
  ".xml": "text/xml; charset=utf-8",
  ".rss": "application/xml; charset=utf-8",
  ".atom": "application/xml; charset=utf-8",
  ".wasm": "application/wasm",
  ".pdf": "application/pdf",
  ".zip": "application/zip",
  ".mp4": "video/mp4",
  ".webm": "video/webm",
  ".mp3": "audio/mpeg",
  ".wav": "audio/wav",
  ".ogg": "audio/ogg",
  ".md": "text/markdown; charset=utf-8",
};

function safeDecode(p: string): string {
  try {
    return decodeURIComponent(p);
  } catch {
    return p;
  }
}

function acceptsEncoding(header: string | null, coding: string): boolean {
  if (!header) return false;
  let wildcard = -1;
  for (let part of header.split(",")) {
    part = part.trim();
    if (!part) continue;
    let token = part;
    let q = 1;
    const semi = part.indexOf(";");
    if (semi >= 0) {
      token = part.slice(0, semi).trim();
      for (const param of part.slice(semi + 1).split(";")) {
        const t = param.trim();
        if (t.startsWith("q=")) {
          const v = parseFloat(t.slice(2));
          if (!Number.isNaN(v)) q = v;
        }
      }
    }
    if (token.toLowerCase() === coding) return q > 0;
    if (token === "*") wildcard = q;
  }
  return wildcard > 0;
}

function compressible(ct: string): boolean {
  ct = ct.toLowerCase();
  return ct.startsWith("text/") || ct.includes("javascript") || ct.includes("json") || ct.includes("svg") || ct.includes("xml");
}

/** Static files from the built dist directory, with the hardening of `krate start`. */
export function createNodeStaticLayer(distDir: string): StaticLayer {
  const root = path.resolve(distDir);
  let realRoot: string | null = null;
  const existsCache = new Map<string, boolean>();

  async function rootReal(): Promise<string> {
    if (!realRoot) realRoot = await fsp.realpath(root);
    return realRoot;
  }

  /** Maps a URL path to a file path under root; null for traversal or hidden/internal paths. */
  async function resolveFile(urlPath: string): Promise<string | null> {
    if (isInternalOutputPath(urlPath) || isHiddenPath(urlPath)) return null;
    if (urlPath.includes("\0")) return null;
    const rel = path.posix.normalize("/" + urlPath.replace(/\\/g, "/")).replace(/^\/+/, "");
    const full = path.join(root, ...rel.split("/").filter(Boolean));
    if (full !== root && !full.startsWith(root + path.sep)) return null;
    try {
      const real = await fsp.realpath(full);
      const rr = await rootReal();
      if (real !== rr && !real.startsWith(rr + path.sep)) return null;
    } catch {
      return full;
    }
    return full;
  }

  async function statFile(p: string): Promise<fs.Stats | null> {
    try {
      return await fsp.stat(p);
    } catch {
      return null;
    }
  }

  function memo(key: string, v: boolean): boolean {
    if (existsCache.size > 20000) existsCache.clear();
    existsCache.set(key, v);
    return v;
  }

  return {
    async exists(urlPath) {
      const cached = existsCache.get("e:" + urlPath);
      if (cached !== undefined) return cached;
      const rel = urlPath.replace(/^\/+|\/+$/g, "");
      const candidates = rel === "" ? ["index.html"] : [rel, rel + "/index.html"];
      for (const c of candidates) {
        const f = await resolveFile("/" + c);
        if (!f) continue;
        const st = await statFile(f);
        if (st && !st.isDirectory()) return memo("e:" + urlPath, true);
      }
      return memo("e:" + urlPath, false);
    },
    async isAsset(urlPath) {
      const cached = existsCache.get("a:" + urlPath);
      if (cached !== undefined) return cached;
      const ext = extName(safeDecode(urlPath)).toLowerCase();
      if (ext === "" || ext === ".html") return memo("a:" + urlPath, false);
      const f = await resolveFile(urlPath);
      if (!f) return memo("a:" + urlPath, false);
      const st = await statFile(f);
      return memo("a:" + urlPath, !!st && !st.isDirectory());
    },
    async serve(request, urlPath) {
      const method = request.method.toUpperCase();
      if (method !== "GET" && method !== "HEAD") return null;
      const file = await resolveFile(urlPath);
      if (!file) return null;
      let target = file;
      let st = await statFile(target);
      if (!st) return null;
      const url = new URL(request.url);
      if (st.isDirectory()) {
        // /dir without a slash is the canonical redirect to /dir/.
        if (!urlPath.endsWith("/")) {
          const base = path.posix.basename(urlPath) + "/";
          return new Response(null, { status: 301, headers: { Location: base + url.search } });
        }
        target = path.join(target, "index.html");
        st = await statFile(target);
        if (!st || st.isDirectory()) return null;
      } else if (urlPath.endsWith("/index.html")) {
        return new Response(null, { status: 301, headers: { Location: "./" + url.search } });
      } else if (urlPath.endsWith("/")) {
        return null;
      }

      const ext = path.extname(target).toLowerCase();
      let contentType = MIME[ext] || "application/octet-stream";
      const headers = new Headers();
      const ae = request.headers.get("accept-encoding");
      let encoding = "";
      let source = target;
      let size = st.size;
      let mtime = st.mtime;
      // Build-time precompressed siblings, never for a page carrying flash state.
      if (!request.headers.get("range") && !requestFlash(request.headers.get("cookie"))) {
        for (const [enc, suffix] of [["br", ".br"], ["gzip", ".gz"]] as const) {
          if (!acceptsEncoding(ae, enc === "gzip" ? "gzip" : "br")) continue;
          const sib = await statFile(target + suffix);
          if (sib && !sib.isDirectory()) {
            encoding = enc;
            source = target + suffix;
            size = sib.size;
            mtime = sib.mtime;
            break;
          }
        }
      }
      if (compressible(contentType) || encoding) headers.append("Vary", "Accept-Encoding");
      if (encoding) headers.set("Content-Encoding", encoding);
      headers.set("Content-Type", contentType);
      headers.set("Last-Modified", mtime.toUTCString());
      headers.set("Accept-Ranges", "bytes");
      const ims = request.headers.get("if-modified-since");
      if (ims && Math.floor(mtime.getTime() / 1000) <= Math.floor(new Date(ims).getTime() / 1000)) {
        return new Response(null, { status: 304, headers });
      }
      let status = 200;
      let start = 0;
      let end = size - 1;
      const range = request.headers.get("range");
      if (range && /^bytes=/.test(range) && !range.includes(",")) {
        const m = /^bytes=(\d*)-(\d*)$/.exec(range.trim());
        if (m && (m[1] !== "" || m[2] !== "")) {
          if (m[1] === "") {
            const n = parseInt(m[2], 10);
            start = Math.max(0, size - n);
          } else {
            start = parseInt(m[1], 10);
            if (m[2] !== "") end = Math.min(end, parseInt(m[2], 10));
          }
          if (start > end || start >= size) {
            headers.set("Content-Range", `bytes */${size}`);
            return new Response(null, { status: 416, headers });
          }
          status = 206;
          headers.set("Content-Range", `bytes ${start}-${end}/${size}`);
        }
      }
      headers.set("Content-Length", String(end - start + 1));
      if (method === "HEAD") return new Response(null, { status, headers });
      const stream = Readable.toWeb(fs.createReadStream(source, { start, end })) as unknown as ReadableStream<Uint8Array>;
      return new Response(stream, { status, headers });
    },
  };
}

export function requestFromNode(req: http.IncomingMessage, trustProxy: boolean): Request {
  const encrypted = !!(req.socket as { encrypted?: boolean }).encrypted;
  const host = req.headers.host || "localhost";
  const headers = new Headers();
  for (const [k, v] of Object.entries(req.headers)) {
    if (v === undefined) continue;
    if (Array.isArray(v)) for (const x of v) headers.append(k, x);
    else headers.set(k, v);
  }
  if (!trustProxy) {
    // Client-supplied forwarding headers are replaced by what the server observed.
    headers.delete("forwarded");
    headers.delete("x-real-ip");
    headers.set("x-forwarded-for", req.socket.remoteAddress || "");
    headers.set("x-forwarded-host", host);
    headers.set("x-forwarded-proto", encrypted ? "https" : "http");
  }
  const method = (req.method || "GET").toUpperCase();
  const init: RequestInit & { duplex?: string } = { method, headers };
  if (method !== "GET" && method !== "HEAD") {
    init.body = Readable.toWeb(req) as unknown as ReadableStream;
    init.duplex = "half";
  }
  return new Request((encrypted ? "https://" : "http://") + host + (req.url || "/"), init);
}

export async function writeResponse(req: http.IncomingMessage, res: http.ServerResponse, response: Response): Promise<void> {
  res.statusCode = response.status;
  const cookies: string[] = typeof (response.headers as any).getSetCookie === "function" ? (response.headers as any).getSetCookie() : [];
  response.headers.forEach((value, key) => {
    if (key.toLowerCase() !== "set-cookie") res.setHeader(key, value);
  });
  if (cookies.length) res.setHeader("set-cookie", cookies);

  const ct = String(res.getHeader("content-type") || "");
  const wantsGzip =
    !req.headers.range && acceptsEncoding(String(req.headers["accept-encoding"] || ""), "gzip");
  let gzip: zlib.Gzip | null = null;
  if (compressible(ct)) {
    res.setHeader("Vary", appendVary(res.getHeader("vary"), "Accept-Encoding"));
    if (wantsGzip && !res.getHeader("content-encoding") && response.status !== 206 && !res.getHeader("content-range") && response.body) {
      res.removeHeader("content-length");
      res.setHeader("Content-Encoding", "gzip");
      gzip = zlib.createGzip();
      gzip.pipe(res);
    }
  }
  const socket = res.socket;
  if (socket && typeof socket.setNoDelay === "function") socket.setNoDelay(true);

  if (req.method === "HEAD" || !response.body) {
    res.end();
    return;
  }
  const out: { write(chunk: Uint8Array): boolean; end(): void; once(ev: string, cb: () => void): unknown } = gzip || res;
  const reader = response.body.getReader();
  res.on("close", () => {
    reader.cancel().catch(() => {});
  });
  try {
    for (;;) {
      const r = await reader.read();
      if (r.done) break;
      if (!out.write(r.value)) await new Promise<void>((ok) => out.once("drain", ok));
      // Streaming pages flush each chunk as soon as it exists.
      if (gzip) gzip.flush();
    }
  } catch (err) {
    if (!res.headersSent) throw err;
    res.destroy(err as Error);
    return;
  }
  out.end();
}

function appendVary(cur: unknown, value: string): string {
  const parts = String(cur || "").split(",").map((s) => s.trim()).filter(Boolean);
  if (!parts.some((p) => p.toLowerCase() === value.toLowerCase())) parts.push(value);
  return parts.join(", ");
}

/** Starts the HTTP server; resolves with the server once it listens. */
export async function startNodeServer(opts: NodeServerOptions): Promise<http.Server> {
  const port = parseInt(process.env.PORT || "", 10) || opts.port || 3000;
  const host = process.env.HOST || process.env.HOSTNAME || "0.0.0.0";
  const server = http.createServer(async (req, res) => {
    try {
      const request = requestFromNode(req, opts.trustProxy);
      const response = await opts.handler(request);
      await writeResponse(req, res, response);
    } catch (err) {
      console.error("[krate] request failed:", err);
      if (!res.headersSent) {
        res.statusCode = 500;
        res.setHeader("Content-Type", "text/plain; charset=utf-8");
        res.end("Internal Server Error\n");
      } else {
        res.destroy();
      }
    }
  });
  server.headersTimeout = 10000;
  server.requestTimeout = 30000;
  server.keepAliveTimeout = 120000;

  let shuttingDown = false;
  const shutdown = (signal: string) => {
    if (shuttingDown) {
      console.error(`[krate] second ${signal}, exiting immediately`);
      process.exit(130);
    }
    shuttingDown = true;
    console.log(`[krate] ${signal}: draining in-flight requests`);
    const grace = (opts.shutdownTimeout && opts.shutdownTimeout > 0 ? opts.shutdownTimeout : 15) * 1000;
    const timer = setTimeout(() => {
      console.error(`[krate] requests still running after ${grace / 1000}s, forcing close`);
      server.closeAllConnections();
      process.exit(0);
    }, grace);
    timer.unref();
    server.close(() => process.exit(0));
    server.closeIdleConnections();
  };
  process.on("SIGTERM", () => shutdown("SIGTERM"));
  process.on("SIGINT", () => shutdown("SIGINT"));

  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, host, () => {
      server.off("error", reject);
      resolve();
    });
  });
  const addr = server.address();
  const shown = addr && typeof addr === "object" ? addr.port : port;
  console.log(`[krate] node server listening on http://${host === "0.0.0.0" || host === "::" ? "localhost" : host}:${shown}`);
  return server;
}

/** Directory of the built site, relative to the bundle (dist/.krate/server/server.mjs). */
export function defaultDistDir(metaUrl: string): string {
  if (process.env.KRATE_DIST) return path.resolve(process.env.KRATE_DIST);
  return path.resolve(path.dirname(fileURLToPath(metaUrl)), "..", "..");
}

/** A node:http style (req, res) function that serves the handler; used by the Vercel function. */
export function nodeRequestListener(handler: KrateHandler, trustProxy: boolean): (req: http.IncomingMessage, res: http.ServerResponse) => Promise<void> {
  return async (req, res) => {
    try {
      const response = await handler(requestFromNode(req, trustProxy));
      await writeResponse(req, res, response);
    } catch (err) {
      console.error("[krate] request failed:", err);
      if (!res.headersSent) {
        res.statusCode = 500;
        res.setHeader("Content-Type", "text/plain; charset=utf-8");
        res.end("Internal Server Error\n");
      } else {
        res.destroy();
      }
    }
  };
}
