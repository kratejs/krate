// @vitest-environment node
import fs from "node:fs";
import http from "node:http";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import zlib from "node:zlib";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { createNodeStaticLayer, defaultDistDir, nodeRequestListener, requestFromNode, startNodeServer, writeResponse } from "./node-server.js";
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

  it("answers HEAD without a body and refuses other methods", async () => {
    const head = (await layer().serve(req("/data.bin", { method: "HEAD" }), "/data.bin"))!;
    expect(head.status).toBe(200);
    expect(head.headers.get("content-length")).toBe("10");
    expect(await head.text()).toBe("");
    expect(await layer().serve(req("/data.bin", { method: "POST", body: "x" }), "/data.bin")).toBeNull();
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

  it("publishes .well-known", async () => {
    const r = (await layer().serve(req("/.well-known/security.txt"), "/.well-known/security.txt"))!;
    expect(r.status).toBe(200);
    expect(await r.text()).toContain("Contact");
  });

  it("redirects a directory to its slash form and keeps the query", async () => {
    const r = (await layer().serve(req("/about?x=1"), "/about"))!;
    expect(r.status).toBe(301);
    expect(r.headers.get("location")).toBe("about/?x=1");
  });

  it("serves a directory index, and redirects /dir/index.html to /dir/", async () => {
    const l = layer();
    const dir = (await l.serve(req("/about/"), "/about/"))!;
    expect(await dir.text()).toBe("<h1>about</h1>");
    const idx = (await l.serve(req("/about/index.html?a=b"), "/about/index.html"))!;
    expect(idx.status).toBe(301);
    expect(idx.headers.get("location")).toBe("./?a=b");
    expect(await l.serve(req("/empty/"), "/empty/")).toBeNull();
  });

  it("does not answer a file requested with a trailing slash", async () => {
    expect(await layer().serve(req("/data.bin/"), "/data.bin/")).toBeNull();
  });

  it("handles ranges: closed, suffix, open-ended, unsatisfiable, multiple", async () => {
    const l = layer();
    const get = (range: string) => l.serve(req("/data.bin", { headers: { range } }), "/data.bin");
    const a = (await get("bytes=2-4"))!;
    expect(a.status).toBe(206);
    expect(a.headers.get("content-range")).toBe("bytes 2-4/10");
    expect(await a.text()).toBe("234");
    const suffix = (await get("bytes=-3"))!;
    expect(suffix.headers.get("content-range")).toBe("bytes 7-9/10");
    expect(await suffix.text()).toBe("789");
    const open = (await get("bytes=8-"))!;
    expect(await open.text()).toBe("89");
    const past = (await get("bytes=50-60"))!;
    expect(past.status).toBe(416);
    expect(past.headers.get("content-range")).toBe("bytes */10");
    const multi = (await get("bytes=0-1,3-4"))!;
    expect(multi.status).toBe(200);
    expect(await multi.text()).toBe("0123456789");
    const end = (await get("bytes=0-99"))!;
    expect(end.headers.get("content-range")).toBe("bytes 0-9/10");
  });

  it("answers 304 for an If-Modified-Since at or after the file time", async () => {
    const l = layer();
    const first = (await l.serve(req("/data.bin"), "/data.bin"))!;
    const lm = first.headers.get("last-modified")!;
    const r = (await l.serve(req("/data.bin", { headers: { "if-modified-since": lm } }), "/data.bin"))!;
    expect(r.status).toBe(304);
    expect(await r.text()).toBe("");
    const old = (await l.serve(req("/data.bin", { headers: { "if-modified-since": "Thu, 01 Jan 1970 00:00:00 GMT" } }), "/data.bin"))!;
    expect(old.status).toBe(200);
  });

  describe("precompressed siblings", () => {
    const get = async (headers: Record<string, string>) =>
      (await layer().serve(req("/app.abc123.js", { headers }), "/app.abc123.js"))!;

    it("prefers brotli, then gzip, keeping the original content type", async () => {
      const br = await get({ "accept-encoding": "gzip, br" });
      expect(br.headers.get("content-encoding")).toBe("br");
      expect(br.headers.get("content-type")).toBe("text/javascript; charset=utf-8");
      expect(br.headers.get("vary")).toBe("Accept-Encoding");
      expect(zlib.brotliDecompressSync(Buffer.from(await br.arrayBuffer())).toString()).toBe("br-body");

      const gz = await get({ "accept-encoding": "gzip" });
      expect(gz.headers.get("content-encoding")).toBe("gzip");
      expect(zlib.gunzipSync(Buffer.from(await gz.arrayBuffer())).toString()).toBe("gz-body");
    });

    it("serves the plain file when the client refuses the coding (q=0) or sends none", async () => {
      const refused = await get({ "accept-encoding": "br;q=0, gzip;q=0" });
      expect(refused.headers.get("content-encoding")).toBeNull();
      expect(await refused.text()).toContain("0123456789");
      const none = await get({});
      expect(none.headers.get("content-encoding")).toBeNull();
      expect(none.headers.get("vary")).toBe("Accept-Encoding");
    });

    it("honours a wildcard", async () => {
      const r = await get({ "accept-encoding": "*" });
      expect(r.headers.get("content-encoding")).toBe("br");
    });

    it("never uses a compressed sibling for a range request or a page carrying flash state", async () => {
      const ranged = await get({ "accept-encoding": "br", range: "bytes=0-3" });
      expect(ranged.headers.get("content-encoding")).toBeNull();
      expect(ranged.status).toBe(206);
      const flashed = await get({ "accept-encoding": "br", cookie: "krate_flash=" + Buffer.from('{"a":"x","r":"hi"}').toString("base64url") });
      expect(flashed.headers.get("content-encoding")).toBeNull();
    });

    it("does not add Vary for a type that is not compressible", async () => {
      const r = (await layer().serve(req("/logo.png"), "/logo.png"))!;
      expect(r.headers.get("content-type")).toBe("image/png");
      expect(r.headers.get("vary")).toBeNull();
    });
  });

  it("exists() maps URLs to files and folder indexes, and isAsset() only to non-HTML files", async () => {
    const l = layer();
    expect(await l.exists("/")).toBe(true);
    expect(await l.exists("/about")).toBe(true);
    expect(await l.exists("/about/")).toBe(true);
    expect(await l.exists("/page.html")).toBe(true);
    expect(await l.exists("/empty")).toBe(false);
    expect(await l.exists("/nope")).toBe(false);
    expect(await l.isAsset("/app.abc123.js")).toBe(true);
    expect(await l.isAsset("/page.html")).toBe(false);
    expect(await l.isAsset("/about")).toBe(false);
    expect(await l.isAsset("/.env")).toBe(false);
    expect(await l.isAsset("/missing.js")).toBe(false);
    expect(await l.isAsset("/about/")).toBe(false);
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

  it("keeps them when the server sits behind a trusted proxy", async () => {
    await withServer(echo, true, async (port) => {
      const r = await call(port, "/", { "x-forwarded-for": "9.9.9.9", "x-forwarded-proto": "https", "x-real-ip": "9.9.9.9" });
      const j = JSON.parse(r.body.toString());
      expect(j.xff).toBe("9.9.9.9");
      expect(j.proto).toBe("https");
      expect(j.realIp).toBe("9.9.9.9");
    });
  });

  it("streams a request body to the handler and joins repeated headers", async () => {
    await withServer(echo, false, async (port) => {
      const r = await call(port, "/", { "content-type": "text/plain" }, { method: "POST", body: "hello body" });
      expect(JSON.parse(r.body.toString()).body).toBe("hello body");
    });
  });

  it("builds the URL from the Host header, falling back to localhost", () => {
    const fake = (headers: http.IncomingHttpHeaders, url: string) =>
      ({ headers, url, method: "get", socket: { remoteAddress: "10.0.0.1" } }) as unknown as http.IncomingMessage;
    expect(requestFromNode(fake({ host: "a.test:8080" }, "/x?y=2"), true).url).toBe("http://a.test:8080/x?y=2");
    expect(requestFromNode(fake({}, "/"), true).url).toBe("http://localhost/");
    expect(requestFromNode(fake({}, "/"), true).method).toBe("GET");
    const tls = { headers: { host: "s.test" }, url: "/", method: "GET", socket: { encrypted: true, remoteAddress: "10.0.0.1" } } as unknown as http.IncomingMessage;
    expect(requestFromNode(tls, false).url).toBe("https://s.test/");
    expect(requestFromNode(tls, false).headers.get("x-forwarded-proto")).toBe("https");
  });
});

describe("writeResponse", () => {
  const html = "<p>" + "hello ".repeat(200) + "</p>";

  it("gzips compressible bodies for clients that accept it and fixes the headers", async () => {
    const h: KrateHandler = async () => new Response(html, { headers: { "content-type": "text/html; charset=utf-8", "content-length": String(html.length), vary: "Cookie" } });
    await withServer(h, false, async (port) => {
      const r = await call(port, "/", { "accept-encoding": "gzip" });
      expect(r.headers["content-encoding"]).toBe("gzip");
      expect(r.headers["content-length"]).toBeUndefined();
      expect(r.headers.vary).toBe("Cookie, Accept-Encoding");
      expect(zlib.gunzipSync(r.body).toString()).toBe(html);
      const plain = await call(port, "/", {});
      expect(plain.headers["content-encoding"]).toBeUndefined();
      expect(plain.body.toString()).toBe(html);
      expect(plain.headers.vary).toBe("Cookie, Accept-Encoding");
    });
  });

  it("does not compress images, ranges, partial content or an already encoded body", async () => {
    const png: KrateHandler = async () => new Response("png-bytes", { headers: { "content-type": "image/png" } });
    const partial: KrateHandler = async () => new Response("part", { status: 206, headers: { "content-type": "text/plain", "content-range": "bytes 0-3/10" } });
    const encoded: KrateHandler = async () => new Response("xx", { headers: { "content-type": "text/css", "content-encoding": "br" } });
    const plain: KrateHandler = async () => new Response(html, { headers: { "content-type": "text/plain" } });
    await withServer(png, false, async (port) => {
      const r = await call(port, "/", { "accept-encoding": "gzip" });
      expect(r.headers["content-encoding"]).toBeUndefined();
      expect(r.headers.vary).toBeUndefined();
    });
    await withServer(partial, false, async (port) => {
      const r = await call(port, "/", { "accept-encoding": "gzip" });
      expect(r.status).toBe(206);
      expect(r.headers["content-encoding"]).toBeUndefined();
    });
    await withServer(encoded, false, async (port) => {
      const r = await call(port, "/", { "accept-encoding": "gzip" });
      expect(r.headers["content-encoding"]).toBe("br");
    });
    await withServer(plain, false, async (port) => {
      const r = await call(port, "/", { "accept-encoding": "gzip", range: "bytes=0-3" });
      expect(r.headers["content-encoding"]).toBeUndefined();
    });
  });

  it("sends every Set-Cookie as its own header and nothing for HEAD", async () => {
    const h: KrateHandler = async () => {
      const headers = new Headers({ "content-type": "text/plain" });
      headers.append("set-cookie", "a=1; Path=/; HttpOnly");
      headers.append("set-cookie", "b=2; Path=/; Secure");
      return new Response("body", { headers });
    };
    await withServer(h, false, async (port) => {
      const r = await call(port, "/", {});
      expect(r.headers["set-cookie"]).toEqual(["a=1; Path=/; HttpOnly", "b=2; Path=/; Secure"]);
      const head = await call(port, "/", {}, { method: "HEAD" });
      expect(head.status).toBe(200);
      expect(head.body.length).toBe(0);
    });
  });

  it("flushes each chunk of a streamed page as soon as it exists, also through gzip", async () => {
    // The second chunk is produced only after the client saw the first one, so
    // a server that buffered the response would never finish.
    for (const enc of ["identity", "gzip"]) {
      let release!: () => void;
      const gate = new Promise<void>((ok) => (release = ok));
      const h: KrateHandler = async () => {
        const enqueue = new TextEncoder();
        let step = 0;
        const stream = new ReadableStream<Uint8Array>({
          async pull(controller) {
            if (step === 0) controller.enqueue(enqueue.encode("<p>first</p>"));
            else if (step === 1) {
              await gate;
              controller.enqueue(enqueue.encode("<p>second</p>"));
            } else controller.close();
            step++;
          },
        });
        return new Response(stream, { headers: { "content-type": "text/html" } });
      };
      const server = http.createServer(nodeRequestListener(h, false));
      const port = await listen(server);
      try {
        const body = await new Promise<string>((resolve, reject) => {
          const r = http.request({ host: "127.0.0.1", port, path: "/", headers: { "accept-encoding": enc } }, (res) => {
            const stream: NodeJS.ReadableStream = enc === "gzip" ? res.pipe(zlib.createGunzip()) : res;
            let seen = "";
            stream.on("data", (c: Buffer) => {
              seen += c.toString();
              if (seen.includes("first")) release();
            });
            stream.on("end", () => resolve(seen));
            stream.on("error", reject);
          });
          r.on("error", reject);
          r.end();
        });
        expect(body, enc).toBe("<p>first</p><p>second</p>");
      } finally {
        server.closeAllConnections();
        await new Promise((ok) => server.close(ok));
      }
    }
  }, 10000);

  it("answers 500 when the handler throws before the response starts", async () => {
    const h: KrateHandler = async () => {
      throw new Error("handler exploded");
    };
    const spy = console.error;
    console.error = () => {};
    try {
      await withServer(h, false, async (port) => {
        const r = await call(port, "/", {});
        expect(r.status).toBe(500);
        expect(r.body.toString()).toBe("Internal Server Error\n");
        expect(r.body.toString()).not.toContain("exploded");
      });
    } finally {
      console.error = spy;
    }
  });

  it("aborts the connection when the body fails after headers were sent", async () => {
    const h: KrateHandler = async () => {
      let sent = false;
      const stream = new ReadableStream<Uint8Array>({
        pull(controller) {
          if (!sent) {
            sent = true;
            controller.enqueue(new TextEncoder().encode("partial"));
          } else controller.error(new Error("render failed midway"));
        },
      });
      return new Response(stream, { headers: { "content-type": "text/plain" } });
    };
    await withServer(h, false, async (port) => {
      await expect(call(port, "/", {})).rejects.toBeTruthy();
    });
  });

  it("writeResponse is exported for hosts that own the socket", () => {
    expect(typeof writeResponse).toBe("function");
  });
});

describe("startNodeServer and defaultDistDir", () => {
  it("binds PORT/HOST, serves the handler and reports the port", async () => {
    const probe = net.createServer();
    const port = await listen(probe as unknown as http.Server);
    await new Promise((ok) => probe.close(ok));
    const prev = { PORT: process.env.PORT, HOST: process.env.HOST };
    process.env.PORT = String(port);
    process.env.HOST = "127.0.0.1";
    const log = console.log;
    const lines: string[] = [];
    console.log = (...a: unknown[]) => lines.push(a.join(" "));
    let server: http.Server | undefined;
    try {
      server = await startNodeServer({ handler: async () => new Response("up"), distDir: dist, trustProxy: false });
      const r = await call(port, "/", {});
      expect(r.body.toString()).toBe("up");
      expect(lines.join("\n")).toContain(`http://127.0.0.1:${port}`);
      expect(server.headersTimeout).toBe(10000);
    } finally {
      console.log = log;
      server?.closeAllConnections();
      await new Promise((ok) => (server ? server.close(ok) : ok(undefined)));
      if (prev.PORT === undefined) delete process.env.PORT;
      else process.env.PORT = prev.PORT;
      if (prev.HOST === undefined) delete process.env.HOST;
      else process.env.HOST = prev.HOST;
    }
  });

  it("fails to start when the port is taken", async () => {
    const blocker = http.createServer();
    const port = await listen(blocker);
    const prev = { PORT: process.env.PORT, HOST: process.env.HOST };
    process.env.PORT = String(port);
    process.env.HOST = "127.0.0.1";
    try {
      await expect(startNodeServer({ handler: async () => new Response("x"), distDir: dist, trustProxy: false })).rejects.toMatchObject({ code: "EADDRINUSE" });
    } finally {
      await new Promise((ok) => blocker.close(ok));
      if (prev.PORT === undefined) delete process.env.PORT;
      else process.env.PORT = prev.PORT;
      if (prev.HOST === undefined) delete process.env.HOST;
      else process.env.HOST = prev.HOST;
    }
  });

  it("defaultDistDir is two levels above dist/.krate/server, unless KRATE_DIST is set", () => {
    const prev = process.env.KRATE_DIST;
    delete process.env.KRATE_DIST;
    try {
      const meta = "file://" + path.join(dist, ".krate", "server", "server.mjs");
      expect(defaultDistDir(meta)).toBe(path.resolve(dist));
      process.env.KRATE_DIST = "/srv/site";
      expect(defaultDistDir(meta)).toBe(path.resolve("/srv/site"));
    } finally {
      if (prev === undefined) delete process.env.KRATE_DIST;
      else process.env.KRATE_DIST = prev;
    }
  });
});
