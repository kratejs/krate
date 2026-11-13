// @vitest-environment node
import fs from "node:fs";
import http from "node:http";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import zlib from "node:zlib";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { createNodeStaticLayer, nodeRequestListener } from "./node-server.js";
import type { KrateHandler } from "./server-handler.js";

let tmp: string;
let dist: string;

function write(rel: string, data: string | Buffer) {
  const f = path.join(dist, rel);
  fs.mkdirSync(path.dirname(f), { recursive: true });
  fs.writeFileSync(f, data);
}

beforeAll(() => {
  tmp = fs.mkdtempSync(path.join(os.tmpdir(), "krate-node-server-"));
  dist = path.join(tmp, "dist");
  write("index.html", "<h1>home</h1>");
  write("about/index.html", "<h1>about</h1>");
  write("empty/readme.txt", "no index here");
  write("app.abc123.js", "console.log('0123456789');");
  write("app.abc123.js.gz", zlib.gzipSync("gz-body"));
  write("app.abc123.js.br", zlib.brotliCompressSync("br-body"));
  write("data.bin", Buffer.from("0123456789"));
  write("logo.png", Buffer.from("not really a png"));
  write("page.html", "<p>page</p>");
  write(".env", "SECRET=1");
  write(".well-known/security.txt", "Contact: a@example.com");
  write(".krate/manifest.json", "{}");
  write("server-manifest.json", "{}");
  write("api-node/hello.mjs", "export {}");
  fs.writeFileSync(path.join(tmp, "outside.txt"), "outside the site");
  fs.symlinkSync(path.join(tmp, "outside.txt"), path.join(dist, "escape.txt"));
});

afterAll(() => {
  fs.rmSync(tmp, { recursive: true, force: true });
});

function req(url: string, init: RequestInit = {}): Request {
  return new Request("http://localhost" + url, init);
}

describe("createNodeStaticLayer", () => {
  const layer = () => createNodeStaticLayer(dist);

  it("serves a file with type, length, validators and range support", async () => {
    const r = (await layer().serve(req("/data.bin"), "/data.bin"))!;
    expect(r.status).toBe(200);
    expect(r.headers.get("content-type")).toBe("application/octet-stream");
    expect(r.headers.get("content-length")).toBe("10");
    expect(r.headers.get("accept-ranges")).toBe("bytes");
    expect(r.headers.get("last-modified")).toMatch(/GMT$/);
    expect(await r.text()).toBe("0123456789");
  });

  it.each([
    ["dotfile", "/.env"],
    ["encoded dotfile", "/%2eenv"],
    ["dotfile inside a path", "/about/../.env"],
    ["krate internals", "/.krate/manifest.json"],
    ["server manifest", "/server-manifest.json"],
    ["api bundles", "/api-node/hello.mjs"],
    ["parent traversal", "/../outside.txt"],
    ["encoded traversal", "/%2e%2e/outside.txt"],
    ["backslash traversal", "/..%5coutside.txt"],
    ["symlink out of the site", "/escape.txt"],
    ["null byte", "/data.bin%00.html"],
  ])("does not serve %s", async (_name, p) => {
    const l = layer();
    expect(await l.serve(req(p), decodeURIComponentSafe(p))).toBeNull();
    expect(await l.exists(decodeURIComponentSafe(p))).toBe(false);
  });
});

function decodeURIComponentSafe(p: string): string {
  try {
    return decodeURIComponent(p);
  } catch {
    return p;
  }
}

// HTTP-level behaviour: requestFromNode and writeResponse through a real socket.

interface Reply {
  status: number;
  headers: http.IncomingHttpHeaders;
  rawHeaders: string[];
  body: Buffer;
}

function listen(server: http.Server): Promise<number> {
  return new Promise((resolve) => server.listen(0, "127.0.0.1", () => resolve((server.address() as net.AddressInfo).port)));
}

function call(port: number, p: string, headers: Record<string, string> = {}, opts: { method?: string; body?: string } = {}): Promise<Reply> {
  return new Promise((resolve, reject) => {
    const r = http.request({ host: "127.0.0.1", port, path: p, method: opts.method || "GET", headers }, (res) => {
      const chunks: Buffer[] = [];
      res.on("data", (c) => chunks.push(c));
      res.on("end", () => resolve({ status: res.statusCode || 0, headers: res.headers, rawHeaders: res.rawHeaders, body: Buffer.concat(chunks) }));
      res.on("error", reject);
    });
    r.on("error", reject);
    if (opts.body !== undefined) r.write(opts.body);
    r.end();
  });
}

async function withServer<T>(handler: KrateHandler, trustProxy: boolean, fn: (port: number) => Promise<T>): Promise<T> {
  const server = http.createServer(nodeRequestListener(handler, trustProxy));
  const port = await listen(server);
  try {
    return await fn(port);
  } finally {
    server.closeAllConnections();
    await new Promise((ok) => server.close(ok));
  }
}

describe("requestFromNode", () => {
  const echo: KrateHandler = async (request) =>
    Response.json({
      url: request.url,
      method: request.method,
      xff: request.headers.get("x-forwarded-for"),
      host: request.headers.get("x-forwarded-host"),
      proto: request.headers.get("x-forwarded-proto"),
      forwarded: request.headers.get("forwarded"),
      realIp: request.headers.get("x-real-ip"),
      body: request.method === "POST" ? await request.text() : null,
      cookie: request.headers.get("cookie"),
    });

  it("replaces client-supplied forwarding headers with what the socket shows", async () => {
    await withServer(echo, false, async (port) => {
      const r = await call(port, "/p?q=1", {
        "x-forwarded-for": "1.2.3.4",
        "x-forwarded-host": "evil.example",
        "x-forwarded-proto": "https",
        forwarded: "for=1.2.3.4",
        "x-real-ip": "1.2.3.4",
        host: "site.test",
      });
      const j = JSON.parse(r.body.toString());
      expect(j.xff).toBe("127.0.0.1");
      expect(j.host).toBe("site.test");
      expect(j.proto).toBe("http");
      expect(j.forwarded).toBeNull();
      expect(j.realIp).toBeNull();
      expect(j.url).toBe("http://site.test/p?q=1");
    });
  });
});
