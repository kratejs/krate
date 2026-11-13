// The host child runtime: serves the Krate host call protocol (see
// packages/native/PROTOCOL.md) for TypeScript host code running on Node, Bun or
// Deno. It is the JS twin of packages/native/gohost: the shell starts it with
// KRATE_GOHOST_SOCKET set, it listens there (a unix socket, or on Windows a
// loopback TCP port guarded by a token: Node, Bun and Deno cannot use AF_UNIX
// there), writes "ready" to stdout and exits when stdin reaches EOF or on
// SIGTERM.
//
// The codec is packages/app/src/wire.ts, bundled in by the build (see
// bundle_test.go), so there is one implementation of the frame format.
//
// A bundle imports this module first (it installs globalThis.__krateHost),
// registers its functions, then calls start().
import { createServer, request as httpRequest } from "node:http";
import type { IncomingMessage, Server, ServerResponse } from "node:http";
import { unlinkSync } from "node:fs";
import { timingSafeEqual } from "node:crypto";
import { Readable } from "node:stream";
import { createClient } from "../../../app/src/client.ts";
import type { Client } from "../../../app/src/client.ts";
import { buildFrame, decodeFrame, encodeValue } from "../../../app/src/wire.ts";
import type { EncodeCtx } from "../../../app/src/wire.ts";

export { buildFrame, decodeFrame, decodeFrames, encodeFrame, encodeValue } from "../../../app/src/wire.ts";

type Kind = "func" | "stream";
interface Ctx {
  signal: AbortSignal;
  window: string;
}
interface Entry {
  fn: (this: Ctx, ...args: unknown[]) => unknown;
  kind: Kind;
}
interface HostSignal {
  get(): unknown;
  subscribe(fn: (v: unknown) => void): (() => void) | void;
}

const SOCKET_ENV = "KRATE_GOHOST_SOCKET";
const TOKEN_ENV = "KRATE_GOHOST_TOKEN";
const TOKEN_HDR = "x-krate-token";
const FRAMES = "application/x-krate-frames";
const WINDOW_HDR = "x-krate-window";
const STREAM_PREFIX = "/__krate/stream/";
const STREAM_TTL_MS = 30_000;
const MAX_SIGNAL_NAMES = 256;

const fns = new Map<string, Entry>();
const sigs = new Map<string, HostSignal>();

const PARENT_ENV = "KRATE_GOHOST_PARENT";

// Endpoint is a parsed transport address: "unix:<path>" (or a bare path), or
// "tcp:<loopback host>:<port>?token=<hex>".
interface Endpoint {
  socketPath?: string;
  host?: string;
  port?: number;
  token?: string;
}

function isLoopback(host: string): boolean {
  return host === "127.0.0.1" || host === "::1" || host === "[::1]" || /^127\.\d+\.\d+\.\d+$/.test(host);
}

// parseEndpoint reads an address; it throws on anything that is not a unix
// path or a loopback tcp address.
function parseEndpoint(addr: string): Endpoint {
  if (addr.startsWith("tcp:")) {
    let rest = addr.slice("tcp:".length);
    let token = "";
    const q = rest.indexOf("?");
    if (q >= 0) {
      token = new URLSearchParams(rest.slice(q + 1)).get("token") ?? "";
      rest = rest.slice(0, q);
    }
    const colon = rest.lastIndexOf(":");
    const host = colon > 0 ? rest.slice(0, colon).replace(/^\[|\]$/g, "") : "";
    const port = colon > 0 ? Number(rest.slice(colon + 1)) : NaN;
    if (!host || !isLoopback(host) || !Number.isInteger(port) || port < 0 || port > 65535) {
      throw new Error(`bad tcp address ${JSON.stringify(addr)}: need a loopback IP and a port`);
    }
    return { host, port, token };
  }
  const path = addr.startsWith("unix:") ? addr.slice("unix:".length) : addr;
  if (!path) throw new Error("empty socket address");
  return { socketPath: path };
}

// tokenOk checks the request token of a tcp transport in constant time.
function tokenOk(got: string | string[] | undefined, want: string): boolean {
  if (!want || typeof got !== "string") return false;
  const a = Buffer.from(got);
  const b = Buffer.from(want);
  return a.length === b.length && timingSafeEqual(a, b);
}

// parentFetch is a fetch for the shell's private SDK endpoint (the value of
// KRATE_GOHOST_PARENT: unix:<path> or tcp:127.0.0.1:<port>?token=<hex>). fetch
// itself cannot reach a unix socket on every runtime, node:http can on Node,
// Bun and Deno.
function parentFetch(addr: string): (input: string, init?: RequestInit) => Promise<Response> {
  let ep: Endpoint | undefined;
  let epErr: Error | undefined;
  try {
    ep = parseEndpoint(addr);
  } catch (e) {
    epErr = new Error(`${PARENT_ENV}=${JSON.stringify(addr)}: ${(e as Error).message}`);
  }
  return (input, init) =>
    new Promise<Response>((resolve, reject) => {
      if (!ep) {
        reject(epErr);
        return;
      }
      const url = new URL(input, "http://krate-parent");
      const headers: Record<string, string> = {};
      new Headers(init?.headers).forEach((v, k) => (headers[k] = v));
      if (ep.token) headers[TOKEN_HDR] = ep.token;
      const target = ep.socketPath ? { socketPath: ep.socketPath } : { host: ep.host, port: ep.port };
      const req = httpRequest({ ...target, path: url.pathname + url.search, method: init?.method ?? "GET", headers }, (res) => {
        const h = new Headers();
        for (const [k, v] of Object.entries(res.headers)) {
          if (Array.isArray(v)) for (const x of v) h.append(k, x);
          else if (v !== undefined) h.set(k, v);
        }
        const status = res.statusCode ?? 502;
        const empty = status === 204 || status === 205 || status === 304;
        if (empty) res.resume();
        resolve(new Response(empty ? null : (Readable.toWeb(res) as unknown as ReadableStream<Uint8Array>), { status, headers: h }));
      });
      req.on("error", reject);
      const sig = init?.signal;
      if (sig) {
        const abort = () => {
          req.destroy();
          reject(sig.reason ?? new Error("aborted"));
        };
        if (sig.aborted) return abort();
        sig.addEventListener("abort", abort, { once: true });
        req.on("close", () => sig.removeEventListener("abort", abort));
      }
      const body = init?.body;
      if (body === undefined || body === null) req.end();
      else req.end(typeof body === "string" ? body : Buffer.from(body as Uint8Array));
    });
}

let parentClient: Client | undefined;

// parent is the client for the shell's SDK socket, created on first use.
function parent(): Client {
  if (parentClient) return parentClient;
  const addr = process.env[PARENT_ENV];
  if (!addr) {
    const e = new Error(`the Krate SDK is not available: ${PARENT_ENV} is not set (this program is started by the Krate shell)`);
    (e as { code?: string }).code = "unavailable";
    throw e;
  }
  return (parentClient = createClient({ fetch: parentFetch(addr) }));
}

function localCtx(): Ctx {
  return { signal: new AbortController().signal, window: "" };
}

const registry = {
  // call, stream and signal are the in-process bridge @krate/app uses in host
  // code (see packages/app/src/client.ts, createHostClient). Ids this program
  // registered run in place; every other id is forwarded to the shell over the
  // parent socket, which serves the SDK ("krate/..."). Host code is trusted, so
  // the shell applies no permission gate on that socket.
  call(id: string, args: unknown[] = []): Promise<unknown> {
    if (typeof id !== "string") return Promise.reject(new TypeError("__krateHost: id must be a string"));
    const ent = fns.get(id);
    if (ent && ent.kind === "func") return Promise.resolve().then(() => ent.fn.apply(localCtx(), args));
    try {
      return parent().call(id, args);
    } catch (e) {
      return Promise.reject(e);
    }
  },
  stream(id: string, args: unknown[] = []): AsyncIterable<unknown> {
    const ent = fns.get(id);
    if (ent && ent.kind === "stream") return ent.fn.apply(localCtx(), args) as AsyncIterable<unknown>;
    return parent().stream(id, args);
  },
  signal(id: string): HostSignal {
    const local = sigs.get(id);
    if (local) return local;
    const s = parent().signal(id);
    return { get: () => s(), subscribe: (fn) => s.subscribe(fn) };
  },
  register(id: string, fn: Entry["fn"], kind?: Kind): void {
    if (typeof id !== "string" || typeof fn !== "function") {
      throw new TypeError("__krateHost.register(id, fn, kind): id must be a string and fn a function");
    }
    const k = kind === undefined ? "func" : kind;
    if (k !== "func" && k !== "stream") throw new TypeError('__krateHost.register: kind must be "func" or "stream"');
    fns.set(id, { fn, kind: k });
  },
  registerSignal(id: string, sig: HostSignal): void {
    if (typeof id !== "string" || !sig || typeof sig.get !== "function" || typeof sig.subscribe !== "function") {
      throw new TypeError("__krateHost.registerSignal(id, sig): id must be a string and sig needs get() and subscribe()");
    }
    sigs.set(id, sig);
  },
};
Object.defineProperty(globalThis, "__krateHost", { value: registry, writable: true, configurable: true, enumerable: false });

// Errors. The shape matches the Go side: code, message and JSON data.

class ProtocolError extends Error {
  code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

function errInfo(e: unknown): { code: string; message: string; data: unknown } {
  const o = { code: "internal", message: "", data: null as unknown };
  try {
    if (e !== null && (typeof e === "object" || typeof e === "function")) {
      const x = e as { code?: unknown; message?: unknown; data?: unknown };
      if (typeof x.code === "string" && x.code) o.code = x.code;
      o.message = typeof x.message === "string" ? x.message : String(e);
      try {
        const d = JSON.stringify(x.data);
        if (d !== undefined) o.data = JSON.parse(d);
      } catch {
        // data that is not JSON is dropped
      }
    } else {
      o.message = String(e);
    }
  } catch {
    o.message = "a value that cannot be converted to a string was thrown";
  }
  return o;
}

const dev = process.env.KRATE_DEV === "1";

function logError(what: string, e: unknown): void {
  const info = errInfo(e);
  if (info.code === "internal" || info.code === "bad_result") {
    const stack = dev && e && typeof (e as Error).stack === "string" ? (e as Error).stack : info.message;
    console.error(`${what}: ${stack}`);
  }
}

// Frames.

function frame(tree: Record<string, unknown>, value?: { key: string; v: unknown }): Uint8Array {
  const ctx: EncodeCtx = { blobs: [] };
  if (value) tree[value.key] = encodeValue(value.v, ctx, "the result");
  return buildFrame(tree, ctx.blobs);
}

function errFrame(head: Record<string, unknown>, e: unknown): Uint8Array {
  const info = errInfo(e);
  const ctx: EncodeCtx = { blobs: [] };
  let data: unknown = null;
  try {
    data = encodeValue(info.data, ctx, "data");
  } catch {
    ctx.blobs = [];
  }
  return buildFrame({ ...head, err: { code: info.code, message: info.message, data } }, ctx.blobs);
}

// resultFrame encodes a value, turning an encoding failure into an error frame.
function resultFrame(head: Record<string, unknown>, key: string, v: unknown): Uint8Array {
  try {
    return frame({ ...head }, { key, v });
  } catch (e) {
    const err = new ProtocolError("bad_result", (e as Error).message.replace(/^the result/, "cannot encode the result"));
    return errFrame(head, err);
  }
}

// HTTP plumbing.

function readBody(req: IncomingMessage): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    req.on("data", (c: Buffer) => chunks.push(c));
    req.on("end", () => resolve(Buffer.concat(chunks)));
    req.on("error", reject);
  });
}

function badRequest(res: ServerResponse, status: number, code: string, message: string): void {
  if (res.headersSent) {
    res.end();
    return;
  }
  res.writeHead(status, { "content-type": FRAMES });
  res.end(buildFrame({ error: { code, message } }, []));
}

// write sends bytes and resolves once the socket accepted them (back pressure).
function write(res: ServerResponse, data: Uint8Array): Promise<boolean> {
  return new Promise((resolve) => {
    if (res.destroyed || res.writableEnded) return resolve(false);
    res.write(data, (err) => resolve(!err));
  });
}

function flushHead(res: ServerResponse, status = 200): void {
  res.writeHead(status, { "content-type": FRAMES, "cache-control": "no-store" });
  res.flushHeaders();
}

function windowOf(req: IncomingMessage): string {
  const w = req.headers[WINDOW_HDR];
  return Array.isArray(w) ? (w[0] ?? "") : (w ?? "");
}

// Calls.

interface ParsedCall {
  id: number;
  fn: string;
  args: unknown[];
}

function parseCalls(body: Uint8Array): ParsedCall[] {
  let v: unknown;
  try {
    v = decodeFrame(body).value;
  } catch (e) {
    throw new ProtocolError("bad_request", (e as Error).message);
  }
  const calls = (v as { calls?: unknown } | null)?.calls;
  if (v === null || typeof v !== "object" || !Array.isArray(calls)) {
    throw new ProtocolError("bad_request", "the body must be an object with a calls array");
  }
  const seen = new Set<number>();
  return calls.map((c: unknown) => {
    const o = c as { id?: unknown; fn?: unknown; args?: unknown };
    if (!c || typeof o.id !== "number" || !Number.isInteger(o.id) || typeof o.fn !== "string") {
      throw new ProtocolError("bad_request", "a call needs id and fn");
    }
    if (seen.has(o.id)) throw new ProtocolError("bad_request", `duplicate call id ${o.id}`);
    seen.add(o.id);
    return { id: o.id, fn: o.fn, args: Array.isArray(o.args) ? o.args : [] };
  });
}

interface StreamRec {
  id: string;
  window: string;
  fn: Entry["fn"];
  args: unknown[];
  ac: AbortController;
  opened: boolean;
  timer: ReturnType<typeof setTimeout>;
}

const streams = new Map<string, StreamRec>();
const inflight = new Map<string, AbortController>(); // window + "\0" + call id
let nextStream = 0;

async function handleCall(req: IncomingMessage, res: ServerResponse): Promise<void> {
  let calls: ParsedCall[];
  try {
    calls = parseCalls(await readBody(req));
  } catch (e) {
    const pe = e as ProtocolError;
    return badRequest(res, 400, pe.code ?? "bad_request", pe.message);
  }
  const window = windowOf(req);
  const own: AbortController[] = [];
  res.on("close", () => {
    if (!res.writableFinished) for (const ac of own) ac.abort();
  });
  flushHead(res);

  const jobs = calls.map(async (c) => {
    const ent = fns.get(c.fn);
    if (!ent) {
      await write(res, errFrame({ id: c.id }, new ProtocolError("not_found", `no such function ${JSON.stringify(c.fn)}`)));
      return;
    }
    if (ent.kind === "stream") {
      const id = "s" + ++nextStream;
      const ac = new AbortController();
      const rec: StreamRec = {
        id, window, fn: ent.fn, args: c.args, ac, opened: false,
        timer: setTimeout(() => {
          if (!rec.opened) closeStream(rec);
        }, STREAM_TTL_MS),
      };
      rec.timer.unref?.();
      streams.set(id, rec);
      await write(res, buildFrame({ id: c.id, stream: id }, []));
      return;
    }
    const ac = new AbortController();
    own.push(ac);
    const key = window + "\0" + c.id;
    inflight.set(key, ac);
    let out: Uint8Array;
    try {
      const v = await ent.fn.apply({ signal: ac.signal, window }, c.args);
      out = resultFrame({ id: c.id }, "ok", v);
    } catch (e) {
      logError(c.fn, e);
      out = errFrame({ id: c.id }, ac.signal.aborted && !(e as { code?: unknown })?.code ? new ProtocolError("canceled", "canceled") : e);
    } finally {
      inflight.delete(key);
    }
    await write(res, out);
  });
  await Promise.all(jobs);
  res.end();
}

async function handleCancel(req: IncomingMessage, res: ServerResponse): Promise<void> {
  let v: { ids?: unknown; streams?: unknown };
  try {
    v = decodeFrame(await readBody(req)).value as typeof v;
  } catch (e) {
    return badRequest(res, 400, "bad_request", (e as Error).message);
  }
  const window = windowOf(req);
  if (Array.isArray(v?.ids)) for (const id of v.ids) inflight.get(window + "\0" + id)?.abort();
  if (Array.isArray(v?.streams)) {
    for (const id of v.streams) {
      const st = streams.get(String(id));
      if (st && st.window === window) closeStream(st);
    }
  }
  res.writeHead(204);
  res.end();
}

function closeStream(st: StreamRec): void {
  clearTimeout(st.timer);
  streams.delete(st.id);
  st.ac.abort();
}

async function handleStream(req: IncomingMessage, res: ServerResponse, id: string): Promise<void> {
  const st = streams.get(id);
  if (!st || st.opened) {
    return badRequest(res, 404, "not_found", "unknown or already opened stream");
  }
  st.opened = true;
  clearTimeout(st.timer);
  flushHead(res);
  let gone = false;
  let it: AsyncIterator<unknown> | undefined;
  const stop = () => {
    gone = true;
    st.ac.abort();
    streams.delete(st.id);
    // The iterator's return() runs the generator's finally blocks.
    try {
      void Promise.resolve(it?.return?.()).catch(() => {});
    } catch {
      // ignored
    }
  };
  res.on("close", () => {
    if (!res.writableFinished) stop();
  });
  try {
    const r = st.fn.apply({ signal: st.ac.signal, window: st.window }, st.args) as
      | AsyncIterable<unknown>
      | Iterable<unknown>
      | undefined;
    if (r && typeof (r as AsyncIterable<unknown>)[Symbol.asyncIterator] === "function") {
      it = (r as AsyncIterable<unknown>)[Symbol.asyncIterator]();
    } else if (r && typeof (r as Iterable<unknown>)[Symbol.iterator] === "function") {
      it = (r as Iterable<unknown>)[Symbol.iterator]() as unknown as AsyncIterator<unknown>;
    } else {
      throw new ProtocolError("bad_result", "a stream function must return an async iterable");
    }
    for (;;) {
      const n = await it.next();
      if (gone) return;
      if (n.done) {
        await write(res, buildFrame({ end: true }, []));
        break;
      }
      if (!(await write(res, resultFrame({}, "v", n.value)))) {
        stop();
        return;
      }
    }
  } catch (e) {
    if (!gone) {
      logError(id, e);
      await write(res, errFrame({}, e));
    }
  }
  streams.delete(st.id);
  res.end();
}

// Signals.

function handleSignals(req: IncomingMessage, res: ServerResponse, url: URL): void {
  const names: string[] = [];
  for (const n of (url.searchParams.get("names") ?? "").split(",")) {
    if (n && !names.includes(n)) names.push(n);
  }
  if (names.length === 0 || names.length > MAX_SIGNAL_NAMES) {
    return badRequest(res, 400, "bad_request", `names must list 1 to ${MAX_SIGNAL_NAMES} signals`);
  }
  flushHead(res);
  const pending = new Map<string, unknown>();
  const unsubs: Array<() => void> = [];
  let scheduled = false;
  let closed = false;
  const flush = () => {
    scheduled = false;
    if (closed) return;
    for (const [name, v] of pending) res.write(resultFrame({ sig: name }, "v", v));
    pending.clear();
  };
  const push = (name: string, v: unknown) => {
    pending.delete(name); // keep the latest value last, like the Go queue
    pending.set(name, v);
    if (!scheduled) {
      scheduled = true;
      queueMicrotask(flush);
    }
  };
  res.on("close", () => {
    closed = true;
    for (const u of unsubs) {
      try {
        u();
      } catch {
        // ignored
      }
    }
  });
  for (const name of names) {
    const sig = sigs.get(name);
    if (!sig) {
      res.write(errFrame({ sig: name }, new ProtocolError("not_found", `no such signal ${JSON.stringify(name)}`)));
      continue;
    }
    try {
      const un = sig.subscribe((v) => push(name, v));
      if (typeof un === "function") unsubs.push(un);
      else unsubs.push(() => {});
      push(name, sig.get());
    } catch (e) {
      res.write(errFrame({ sig: name }, e));
    }
  }
  if (unsubs.length === 0) res.end();
}

// Server.

let serverToken = "";

function route(req: IncomingMessage, res: ServerResponse): void {
  if (serverToken && !tokenOk(req.headers[TOKEN_HDR], serverToken)) {
    res.writeHead(401, { "content-type": "text/plain" });
    res.end("unauthorized");
    return;
  }
  const url = new URL(req.url ?? "/", "http://hostchild");
  const p = url.pathname;
  const method = (want: string): boolean => {
    if (req.method === want) return true;
    res.writeHead(405, { allow: want, "content-type": "text/plain" });
    res.end("method not allowed");
    return false;
  };
  const run = (f: () => unknown) => {
    Promise.resolve()
      .then(f)
      .catch((e) => {
        console.error("krate hostchild:", e);
        badRequest(res, 500, "internal", String((e as Error)?.message ?? e));
      });
  };
  if (p === "/__krate/call") {
    if (method("POST")) run(() => handleCall(req, res));
  } else if (p === "/__krate/cancel") {
    if (method("POST")) run(() => handleCancel(req, res));
  } else if (p.startsWith(STREAM_PREFIX)) {
    if (method("GET")) run(() => handleStream(req, res, decodeURIComponent(p.slice(STREAM_PREFIX.length))));
  } else if (p === "/__krate/signals") {
    if (method("GET")) run(() => handleSignals(req, res, url));
  } else if (p === "/__krate/hostchild/list") {
    if (method("GET")) {
      const out = { funcs: [] as string[], streams: [] as string[], signals: [...sigs.keys()] };
      for (const [id, e] of fns) (e.kind === "stream" ? out.streams : out.funcs).push(id);
      res.writeHead(200, { "content-type": "application/json" });
      res.end(JSON.stringify(out));
    }
  } else {
    res.writeHead(404, { "content-type": "text/plain" });
    res.end("not found");
  }
}

let server: Server | undefined;

function shutdown(socket: string, code: number): void {
  try {
    server?.closeAllConnections?.();
    server?.close();
  } catch {
    // ignored
  }
  if (socket) {
    try {
      unlinkSync(socket);
    } catch {
      // the socket may already be gone
    }
  }
  process.exit(code);
}

// start listens on KRATE_GOHOST_SOCKET and reports ready. Call it once, after
// the host functions are registered.
export function start(): void {
  const addr = process.env[SOCKET_ENV];
  if (!addr) {
    console.error(`krate hostchild: ${SOCKET_ENV} is not set; this program is started by the Krate shell`);
    process.exit(1);
  }
  let ep: Endpoint;
  try {
    ep = parseEndpoint(addr);
  } catch (e) {
    console.error(`krate hostchild: ${SOCKET_ENV}: ${(e as Error).message}`);
    process.exit(1);
  }
  const tcp = ep.socketPath === undefined;
  const socket = ep.socketPath ?? "";
  if (tcp) {
    serverToken = process.env[TOKEN_ENV] ?? "";
    if (!serverToken) {
      console.error(`krate hostchild: ${TOKEN_ENV} is not set; a tcp transport needs it`);
      process.exit(1);
    }
  } else {
    try {
      unlinkSync(socket);
    } catch {
      // no stale socket
    }
  }
  server = createServer(route);
  server.on("error", (e) => {
    console.error("krate hostchild: listening on " + addr + ":", e);
    process.exit(1);
  });
  const onListening = () => {
    if (tcp) {
      const a = server?.address();
      const port = a && typeof a === "object" ? a.port : 0;
      process.stdout.write(`ready tcp:${ep.host!.includes(":") ? "[" + ep.host + "]" : ep.host}:${port}\n`);
    } else {
      process.stdout.write("ready\n");
    }
  };
  if (tcp) server.listen(ep.port!, ep.host!, onListening);
  else server.listen(socket, onListening);
  const leave = () => shutdown(socket, 0);
  process.stdin.on("end", leave);
  process.stdin.on("close", leave);
  process.stdin.on("error", leave);
  process.stdin.resume();
  process.on("SIGTERM", leave);
  process.on("SIGINT", leave);
  process.on("unhandledRejection", (e) => console.error("krate hostchild: unhandled rejection:", e));
  process.on("uncaughtException", (e) => console.error("krate hostchild: uncaught exception:", e));
}
