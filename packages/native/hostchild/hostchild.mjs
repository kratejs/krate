// Code generated from src/runtime.ts and packages/app/src/wire.ts. DO NOT EDIT.
// Regenerate: go test ./hostchild -run TestBundleUpToDate -update
// src/runtime.ts
import { createServer } from "node:http";
import { unlinkSync } from "node:fs";

// ../../app/src/wire.ts
var TAGS = ["$b", "$d", "$m", "$s"];
var ESCAPED = TAGS.map((t) => t + "$");
var te = new TextEncoder();
var td = new TextDecoder("utf-8", { fatal: true });
function fail(root, path, msg) {
  throw new TypeError(`${root}${path ? " at " + path : ""}: ${msg}`);
}
function setOwn(o, k, v) {
  if (k === "__proto__") Object.defineProperty(o, k, { value: v, enumerable: true, writable: true, configurable: true });
  else o[k] = v;
}
function addBlob(ctx, bytes) {
  ctx.blobs.push(bytes);
  return { $b: ctx.blobs.length - 1 };
}
function encodeValue(v, ctx, root = "value") {
  return enc(v, ctx, root, "", []);
}
function enc(v, ctx, root, path, stack) {
  switch (typeof v) {
    case "string":
    case "boolean":
      return v;
    case "number":
      if (!Number.isFinite(v)) fail(root, path, `cannot encode ${String(v)} (numbers must be finite)`);
      return v;
    case "undefined":
      return null;
    case "bigint":
      return fail(root, path, "cannot encode bigint");
    case "function":
      return fail(root, path, "cannot encode a function");
    case "symbol":
      return fail(root, path, "cannot encode a symbol");
  }
  if (v === null) return null;
  const o = v;
  if (o instanceof Uint8Array) return addBlob(ctx, o);
  if (o instanceof ArrayBuffer) return addBlob(ctx, new Uint8Array(o.slice(0)));
  if (ArrayBuffer.isView(o)) {
    const view = o;
    return addBlob(ctx, new Uint8Array(view.buffer.slice(view.byteOffset, view.byteOffset + view.byteLength)));
  }
  if (o instanceof Date) {
    if (Number.isNaN(o.getTime())) fail(root, path, "cannot encode an invalid Date");
    const year = o.getUTCFullYear();
    if (year < 0 || year > 9999) fail(root, path, `cannot encode a Date in year ${year} (0000 to 9999 only)`);
    return { $d: o.toISOString() };
  }
  if (stack.includes(o)) fail(root, path, "cannot encode a circular structure");
  stack.push(o);
  try {
    if (Array.isArray(o)) {
      return o.map((x, i) => enc(x, ctx, root, `${path}[${i}]`, stack));
    }
    if (o instanceof Map) {
      const pairs = [];
      let i = 0;
      for (const [k, val] of o) {
        pairs.push([enc(k, ctx, root, `${path}.<key ${i}>`, stack), enc(val, ctx, root, `${path}.<value ${i}>`, stack)]);
        i++;
      }
      return { $m: pairs };
    }
    if (o instanceof Set) {
      const items = [];
      let i = 0;
      for (const x of o) items.push(enc(x, ctx, root, `${path}.<item ${i++}>`, stack));
      return { $s: items };
    }
    const proto = Object.getPrototypeOf(o);
    if (proto !== Object.prototype && proto !== null) {
      const name = proto.constructor && proto.constructor.name || "unknown";
      fail(root, path, `cannot encode an instance of ${name} (only plain objects, arrays, Date, Map, Set and Uint8Array)`);
    }
    const keys = Object.keys(o);
    const out = {};
    for (const k of keys) {
      const val = o[k];
      if (val === void 0) continue;
      setOwn(out, k, enc(val, ctx, root, `${path}.${k}`, stack));
    }
    if (keys.length === 1 && out[keys[0]] !== void 0) {
      const k = keys[0];
      if (TAGS.includes(k)) return { [k + "$"]: out };
      if (ESCAPED.includes(k)) return { [k]: out };
    }
    return out;
  } finally {
    stack.pop();
  }
}
function buildFrame(tree, blobs) {
  if (blobs.length > 65535) throw new TypeError(`too many binary values in one frame (${blobs.length} > 65535)`);
  const json = te.encode(JSON.stringify(tree));
  let size = 4 + json.length + 2;
  for (const b of blobs) size += 4 + b.length;
  const out = new Uint8Array(size);
  const dv = new DataView(out.buffer);
  dv.setUint32(0, json.length);
  out.set(json, 4);
  let p = 4 + json.length;
  dv.setUint16(p, blobs.length);
  p += 2;
  for (const b of blobs) {
    dv.setUint32(p, b.length);
    out.set(b, p + 4);
    p += 4 + b.length;
  }
  return out;
}
function encodeFrame(value, root = "value") {
  const ctx = { blobs: [] };
  const tree = encodeValue(value, ctx, root);
  return buildFrame(tree, ctx.blobs);
}
var RFC3339 = /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)$/;
function dec(t, blobs) {
  if (t === null || typeof t !== "object") return t;
  if (Array.isArray(t)) return t.map((x) => dec(x, blobs));
  const o = t;
  const keys = Object.keys(o);
  if (keys.length === 1) {
    const k = keys[0];
    const v = o[k];
    if (k === "$b") {
      if (typeof v !== "number" || !Number.isInteger(v) || v < 0 || v >= blobs.length) {
        throw new Error(`bad blob index ${JSON.stringify(v)}`);
      }
      return blobs[v];
    }
    if (k === "$d") {
      if (typeof v !== "string") throw new Error("bad $d value");
      const d = RFC3339.test(v) ? new Date(v) : /* @__PURE__ */ new Date(NaN);
      if (Number.isNaN(d.getTime())) throw new Error(`bad date ${JSON.stringify(v)}`);
      return d;
    }
    if (k === "$m") {
      if (!Array.isArray(v)) throw new Error("bad $m value");
      return new Map(v.map((p) => {
        if (!Array.isArray(p) || p.length !== 2) throw new Error("bad $m entry");
        return [dec(p[0], blobs), dec(p[1], blobs)];
      }));
    }
    if (k === "$s") {
      if (!Array.isArray(v)) throw new Error("bad $s value");
      return new Set(v.map((x) => dec(x, blobs)));
    }
    if (ESCAPED.includes(k)) {
      if (v === null || typeof v !== "object" || Array.isArray(v)) throw new Error(`bad ${k} wrapper`);
      const inner = {};
      for (const ik of Object.keys(v)) setOwn(inner, ik, dec(v[ik], blobs));
      return inner;
    }
  }
  const out = {};
  for (const k of keys) setOwn(out, k, dec(o[k], blobs));
  return out;
}
function decodeParts(json, blobs) {
  return dec(JSON.parse(td.decode(json)), blobs);
}
function decodeFrame(buf) {
  const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
  const trunc = () => new Error("truncated frame");
  if (buf.length < 4) throw trunc();
  const n = dv.getUint32(0);
  if (buf.length < 4 + n + 2) throw trunc();
  const json = buf.subarray(4, 4 + n);
  let p = 4 + n;
  const k = dv.getUint16(p);
  p += 2;
  const blobs = [];
  for (let i = 0; i < k; i++) {
    if (buf.length < p + 4) throw trunc();
    const m = dv.getUint32(p);
    p += 4;
    if (buf.length < p + m) throw trunc();
    blobs.push(buf.subarray(p, p + m));
    p += m;
  }
  return { value: decodeParts(json, blobs), size: p };
}
function decodeFrames(buf) {
  const out = [];
  let p = 0;
  while (p < buf.length) {
    const { value, size } = decodeFrame(buf.subarray(p));
    out.push(value);
    p += size;
  }
  return out;
}

// src/runtime.ts
var SOCKET_ENV = "KRATE_GOHOST_SOCKET";
var FRAMES = "application/x-krate-frames";
var WINDOW_HDR = "x-krate-window";
var STREAM_PREFIX = "/__krate/stream/";
var STREAM_TTL_MS = 3e4;
var MAX_SIGNAL_NAMES = 256;
var fns = /* @__PURE__ */ new Map();
var sigs = /* @__PURE__ */ new Map();
var registry = {
  register(id, fn, kind) {
    if (typeof id !== "string" || typeof fn !== "function") {
      throw new TypeError("__krateHost.register(id, fn, kind): id must be a string and fn a function");
    }
    const k = kind === void 0 ? "func" : kind;
    if (k !== "func" && k !== "stream") throw new TypeError('__krateHost.register: kind must be "func" or "stream"');
    fns.set(id, { fn, kind: k });
  },
  registerSignal(id, sig) {
    if (typeof id !== "string" || !sig || typeof sig.get !== "function" || typeof sig.subscribe !== "function") {
      throw new TypeError("__krateHost.registerSignal(id, sig): id must be a string and sig needs get() and subscribe()");
    }
    sigs.set(id, sig);
  }
};
Object.defineProperty(globalThis, "__krateHost", { value: registry, writable: true, configurable: true, enumerable: false });
var ProtocolError = class extends Error {
  code;
  constructor(code, message) {
    super(message);
    this.code = code;
  }
};
function errInfo(e) {
  const o = { code: "internal", message: "", data: null };
  try {
    if (e !== null && (typeof e === "object" || typeof e === "function")) {
      const x = e;
      if (typeof x.code === "string" && x.code) o.code = x.code;
      o.message = typeof x.message === "string" ? x.message : String(e);
      try {
        const d = JSON.stringify(x.data);
        if (d !== void 0) o.data = JSON.parse(d);
      } catch {
      }
    } else {
      o.message = String(e);
    }
  } catch {
    o.message = "a value that cannot be converted to a string was thrown";
  }
  return o;
}
var dev = process.env.KRATE_DEV === "1";
function logError(what, e) {
  const info = errInfo(e);
  if (info.code === "internal" || info.code === "bad_result") {
    const stack = dev && e && typeof e.stack === "string" ? e.stack : info.message;
    console.error(`${what}: ${stack}`);
  }
}
function frame(tree, value) {
  const ctx = { blobs: [] };
  if (value) tree[value.key] = encodeValue(value.v, ctx, "the result");
  return buildFrame(tree, ctx.blobs);
}
function errFrame(head, e) {
  const info = errInfo(e);
  const ctx = { blobs: [] };
  let data = null;
  try {
    data = encodeValue(info.data, ctx, "data");
  } catch {
    ctx.blobs = [];
  }
  return buildFrame({ ...head, err: { code: info.code, message: info.message, data } }, ctx.blobs);
}
function resultFrame(head, key, v) {
  try {
    return frame({ ...head }, { key, v });
  } catch (e) {
    const err = new ProtocolError("bad_result", e.message.replace(/^the result/, "cannot encode the result"));
    return errFrame(head, err);
  }
}
function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => resolve(Buffer.concat(chunks)));
    req.on("error", reject);
  });
}
function badRequest(res, status, code, message) {
  if (res.headersSent) {
    res.end();
    return;
  }
  res.writeHead(status, { "content-type": FRAMES });
  res.end(buildFrame({ error: { code, message } }, []));
}
function write(res, data) {
  return new Promise((resolve) => {
    if (res.destroyed || res.writableEnded) return resolve(false);
    res.write(data, (err) => resolve(!err));
  });
}
function flushHead(res, status = 200) {
  res.writeHead(status, { "content-type": FRAMES, "cache-control": "no-store" });
  res.flushHeaders();
}
function windowOf(req) {
  const w = req.headers[WINDOW_HDR];
  return Array.isArray(w) ? w[0] ?? "" : w ?? "";
}
function parseCalls(body) {
  let v;
  try {
    v = decodeFrame(body).value;
  } catch (e) {
    throw new ProtocolError("bad_request", e.message);
  }
  const calls = v?.calls;
  if (v === null || typeof v !== "object" || !Array.isArray(calls)) {
    throw new ProtocolError("bad_request", "the body must be an object with a calls array");
  }
  const seen = /* @__PURE__ */ new Set();
  return calls.map((c) => {
    const o = c;
    if (!c || typeof o.id !== "number" || !Number.isInteger(o.id) || typeof o.fn !== "string") {
      throw new ProtocolError("bad_request", "a call needs id and fn");
    }
    if (seen.has(o.id)) throw new ProtocolError("bad_request", `duplicate call id ${o.id}`);
    seen.add(o.id);
    return { id: o.id, fn: o.fn, args: Array.isArray(o.args) ? o.args : [] };
  });
}
var streams = /* @__PURE__ */ new Map();
var inflight = /* @__PURE__ */ new Map();
var nextStream = 0;
async function handleCall(req, res) {
  let calls;
  try {
    calls = parseCalls(await readBody(req));
  } catch (e) {
    const pe = e;
    return badRequest(res, 400, pe.code ?? "bad_request", pe.message);
  }
  const window = windowOf(req);
  const own = [];
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
      const ac2 = new AbortController();
      const rec = {
        id,
        window,
        fn: ent.fn,
        args: c.args,
        ac: ac2,
        opened: false,
        timer: setTimeout(() => {
          if (!rec.opened) closeStream(rec);
        }, STREAM_TTL_MS)
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
    let out;
    try {
      const v = await ent.fn.apply({ signal: ac.signal, window }, c.args);
      out = resultFrame({ id: c.id }, "ok", v);
    } catch (e) {
      logError(c.fn, e);
      out = errFrame({ id: c.id }, ac.signal.aborted && !e?.code ? new ProtocolError("canceled", "canceled") : e);
    } finally {
      inflight.delete(key);
    }
    await write(res, out);
  });
  await Promise.all(jobs);
  res.end();
}
async function handleCancel(req, res) {
  let v;
  try {
    v = decodeFrame(await readBody(req)).value;
  } catch (e) {
    return badRequest(res, 400, "bad_request", e.message);
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
function closeStream(st) {
  clearTimeout(st.timer);
  streams.delete(st.id);
  st.ac.abort();
}
async function handleStream(req, res, id) {
  const st = streams.get(id);
  if (!st || st.opened) {
    return badRequest(res, 404, "not_found", "unknown or already opened stream");
  }
  st.opened = true;
  clearTimeout(st.timer);
  flushHead(res);
  let gone = false;
  let it;
  const stop = () => {
    gone = true;
    st.ac.abort();
    streams.delete(st.id);
    try {
      void Promise.resolve(it?.return?.()).catch(() => {
      });
    } catch {
    }
  };
  res.on("close", () => {
    if (!res.writableFinished) stop();
  });
  try {
    const r = st.fn.apply({ signal: st.ac.signal, window: st.window }, st.args);
    if (r && typeof r[Symbol.asyncIterator] === "function") {
      it = r[Symbol.asyncIterator]();
    } else if (r && typeof r[Symbol.iterator] === "function") {
      it = r[Symbol.iterator]();
    } else {
      throw new ProtocolError("bad_result", "a stream function must return an async iterable");
    }
    for (; ; ) {
      const n = await it.next();
      if (gone) return;
      if (n.done) {
        await write(res, buildFrame({ end: true }, []));
        break;
      }
      if (!await write(res, resultFrame({}, "v", n.value))) {
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
function handleSignals(req, res, url) {
  const names = [];
  for (const n of (url.searchParams.get("names") ?? "").split(",")) {
    if (n && !names.includes(n)) names.push(n);
  }
  if (names.length === 0 || names.length > MAX_SIGNAL_NAMES) {
    return badRequest(res, 400, "bad_request", `names must list 1 to ${MAX_SIGNAL_NAMES} signals`);
  }
  flushHead(res);
  const pending = /* @__PURE__ */ new Map();
  const unsubs = [];
  let scheduled = false;
  let closed = false;
  const flush = () => {
    scheduled = false;
    if (closed) return;
    for (const [name, v] of pending) res.write(resultFrame({ sig: name }, "v", v));
    pending.clear();
  };
  const push = (name, v) => {
    pending.delete(name);
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
      else unsubs.push(() => {
      });
      push(name, sig.get());
    } catch (e) {
      res.write(errFrame({ sig: name }, e));
    }
  }
  if (unsubs.length === 0) res.end();
}
function route(req, res) {
  const url = new URL(req.url ?? "/", "http://hostchild");
  const p = url.pathname;
  const method = (want) => {
    if (req.method === want) return true;
    res.writeHead(405, { allow: want, "content-type": "text/plain" });
    res.end("method not allowed");
    return false;
  };
  const run = (f) => {
    Promise.resolve().then(f).catch((e) => {
      console.error("krate hostchild:", e);
      badRequest(res, 500, "internal", String(e?.message ?? e));
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
      const out = { funcs: [], streams: [], signals: [...sigs.keys()] };
      for (const [id, e] of fns) (e.kind === "stream" ? out.streams : out.funcs).push(id);
      res.writeHead(200, { "content-type": "application/json" });
      res.end(JSON.stringify(out));
    }
  } else {
    res.writeHead(404, { "content-type": "text/plain" });
    res.end("not found");
  }
}
var server;
function shutdown(socket, code) {
  try {
    server?.closeAllConnections?.();
    server?.close();
  } catch {
  }
  try {
    unlinkSync(socket);
  } catch {
  }
  process.exit(code);
}
function start() {
  const socket = process.env[SOCKET_ENV];
  if (!socket) {
    console.error(`krate hostchild: ${SOCKET_ENV} is not set; this program is started by the Krate shell`);
    process.exit(1);
  }
  try {
    unlinkSync(socket);
  } catch {
  }
  server = createServer(route);
  server.on("error", (e) => {
    console.error("krate hostchild: listening on " + socket + ":", e);
    process.exit(1);
  });
  server.listen(socket, () => {
    process.stdout.write("ready\n");
  });
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
export {
  buildFrame,
  decodeFrame,
  decodeFrames,
  encodeFrame,
  encodeValue,
  start
};
