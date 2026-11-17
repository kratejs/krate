"use strict";
// Krate build-time JS worker. Reads framed requests on stdin, runs each one in
// a fresh vm context that has the globals of the embedded QuickJS runtime, and
// writes framed replies (and console lines) on stdout. A frame is a 4-byte
// big-endian length and a JSON header, followed for a request by a second
// length-prefixed frame holding the code.
const vm = require("vm");
const fs = require("fs");

const NODE_GLOBALS = __KRATE_NODE_GLOBALS__;

process.on("uncaughtException", (e) => writeErr("ERROR: " + describe(e)));
process.on("unhandledRejection", () => {});

function describe(e) {
  try {
    return (e && e.stack) || String(e);
  } catch (_) {
    return "error";
  }
}

function writeErr(line) {
  try {
    process.stderr.write(line + "\n");
  } catch (_) {}
}

function send(obj) {
  const body = Buffer.from(JSON.stringify(obj), "utf8");
  const head = Buffer.alloc(4);
  head.writeUInt32BE(body.length, 0);
  process.stdout.write(Buffer.concat([head, body]));
}

// The Web API classes of the embedded runtime that Node provides natively.
const SHARED = [
  "URL", "URLSearchParams", "TextEncoder", "TextDecoder", "structuredClone",
  "atob", "btoa", "DOMException", "Event", "EventTarget", "AbortSignal",
  "AbortController", "Headers", "Request", "Response", "WebSocket",
  "EventSource", "Blob", "File", "FormData", "ReadableStream",
  "ReadableStreamDefaultReader", "WritableStream", "TransformStream",
  "CompressionStream", "DecompressionStream", "CryptoKey", "crypto",
];

// SETUP runs inside each context; it mirrors webapi/console.js and
// webapi/timers.js of the embedded runtime on top of the host functions.
const SETUP = `(function (host) {
  var G = globalThis;
  function def(name, value) {
    Object.defineProperty(G, name, { value: value, writable: true, configurable: true, enumerable: false });
  }
  function fmt(a) {
    if (typeof a === 'string') return a;
    if (a instanceof Error) return a.stack || (a.name + ': ' + a.message);
    if (typeof a === 'function') return '[Function: ' + (a.name || 'anonymous') + ']';
    if (typeof a === 'bigint') return a.toString() + 'n';
    if (typeof a === 'symbol') return a.toString();
    if (a === undefined) return 'undefined';
    try { var s = JSON.stringify(a); return s === undefined ? String(a) : s; } catch (_) { return String(a); }
  }
  function line(args) { return Array.prototype.map.call(args, fmt).join(' '); }
  var con = {};
  con.log = function () { host.log(0, line(arguments)); };
  con.info = con.log;
  con.debug = con.log;
  con.error = function () { host.log(2, line(arguments)); };
  con.warn = function () { host.log(1, line(arguments)); };
  con.trace = con.error;
  def('console', con);
  function addTimer(fn, ms, args, repeat) {
    if (typeof fn !== 'function') throw new TypeError('The "callback" argument must be of type function');
    ms = Number(ms);
    if (!(ms >= 1)) ms = 1;
    if (ms > 2147483647) ms = 1;
    return host.timer(function () { fn.apply(undefined, args); }, ms, repeat);
  }
  def('setTimeout', (0, function (fn, ms) { return addTimer(fn, ms, Array.prototype.slice.call(arguments, 2), false); }));
  def('setInterval', function (fn, ms) { return addTimer(fn, ms, Array.prototype.slice.call(arguments, 2), true); });
  def('setImmediate', function (fn) { return addTimer(fn, 0, Array.prototype.slice.call(arguments, 1), false); });
  def('clearTimeout', function (h) { host.clear(h); });
  def('clearInterval', function (h) { host.clear(h); });
  def('clearImmediate', function (h) { host.clear(h); });
  def('queueMicrotask', function (fn) {
    if (typeof fn !== 'function') throw new TypeError('The "callback" argument must be of type function');
    Promise.resolve().then(fn).catch(function (e) { try { console.error(e && e.stack ? e.stack : String(e)); } catch (_) {} });
  });
  var perfOrigin = Date.now();
  def('performance', {
    timeOrigin: perfOrigin,
    now: function () { return Date.now() - perfOrigin; },
    toJSON: function () { return { timeOrigin: perfOrigin }; }
  });
  def('fetch', function fetch(input, init) { return host.fetch(input, init); });
  // QuickJS has no Intl or WebAssembly, and its locale methods ignore the
  // locale: match them, so output does not depend on the engine. Code that
  // needs Intl fails here as in QuickJS and runs in a Node process instead.
  delete G.Intl;
  delete G.WebAssembly;
  var p2 = function (n) { return (n < 10 ? '0' : '') + n; };
  var dp = Date.prototype;
  function method(proto, name, fn) {
    Object.defineProperty(proto, name, { value: fn, writable: true, configurable: true, enumerable: false });
  }
  function localeDate(d) {
    var y = d.getFullYear();
    return p2(d.getMonth() + 1) + '/' + p2(d.getDate()) + '/' + (y < 0 ? '-' + String(-y).padStart(4, '0') : String(y).padStart(4, '0'));
  }
  function localeTime(d) {
    var h = d.getHours();
    return p2(h % 12 || 12) + ':' + p2(d.getMinutes()) + ':' + p2(d.getSeconds()) + ' ' + (h < 12 ? 'AM' : 'PM');
  }
  function valid(d) { return !isNaN(dp.getTime.call(d)); }
  method(dp, 'toLocaleString', function toLocaleString() { return valid(this) ? localeDate(this) + ', ' + localeTime(this) : 'Invalid Date'; });
  method(dp, 'toLocaleDateString', function toLocaleDateString() { return valid(this) ? localeDate(this) : 'Invalid Date'; });
  method(dp, 'toLocaleTimeString', function toLocaleTimeString() { return valid(this) ? localeTime(this) : 'Invalid Date'; });
  var dateString = dp.toString, timeString = dp.toTimeString;
  var zoneName = / \\([^)]*\\)$/;
  method(dp, 'toString', function toString() { return dateString.call(this).replace(zoneName, ''); });
  method(dp, 'toTimeString', function toTimeString() { return timeString.call(this).replace(zoneName, ''); });
  var numString = Number.prototype.toString;
  method(Number.prototype, 'toLocaleString', function toLocaleString() { return numString.call(this); });
  var bigString = BigInt.prototype.toString;
  method(BigInt.prototype, 'toLocaleString', function toLocaleString() { return bigString.call(this); });
  method(String.prototype, 'localeCompare', function localeCompare(that) {
    var a = Array.from(String(this).normalize()), b = Array.from(String(that).normalize());
    for (var i = 0; i < a.length && i < b.length; i++) {
      var x = a[i].codePointAt(0), y = b[i].codePointAt(0);
      if (x !== y) return x < y ? -1 : 1;
    }
    return a.length === b.length ? 0 : a.length < b.length ? -1 : 1;
  });
  def('_go_readFile', function (p, opts) { return host.readFile(p, opts); });
})`;

const setupScript = new vm.Script(SETUP, { filename: "krate-worker-setup.js" });
const nodeGlobalsScript = new vm.Script(NODE_GLOBALS, { filename: "krate-node-globals.js" });
const BODY_METHODS = ["json", "text", "arrayBuffer", "blob", "formData", "bytes"];

// newState tracks what an evaluation left pending: timers, fetches and body
// reads. The await loop waits while any is outstanding.
function newState() {
  // trap is set while the evaluation may read a global the embedded engine
  // has and this runtime lacks: its console lines wait in logs until the
  // reply, and are dropped when trapped (it then runs in the embedded engine).
  const st = { pending: 0, timers: new Map(), aborts: new Set(), wake: null, trap: false, trapped: false, logs: [] };
  st.poke = () => {
    const w = st.wake;
    st.wake = null;
    if (w) w();
  };
  st.done = () => {
    st.pending--;
    st.poke();
  };
  return st;
}

function makeContext(st, env, trap) {
  const ctx = vm.createContext({});
  for (const k of SHARED) {
    if (globalThis[k] !== undefined) {
      Object.defineProperty(ctx, k, { value: globalThis[k], writable: true, configurable: true, enumerable: false });
    }
  }
  const host = {
    log(kind, text) {
      if (st.trap) {
        st.logs.push({ t: "log", k: kind, s: String(text) });
      } else {
        send({ t: "log", k: kind, s: String(text) });
      }
    },
    // st.timers maps each live timer to whether the wait counts it: an
    // unref'd timer does not keep a Node process alive, so it does not keep
    // the wait going either.
    timer(cb, ms, repeat) {
      st.pending++;
      let h;
      const run = () => {
        if (!repeat && st.timers.has(h)) {
          if (st.timers.get(h)) st.pending--;
          st.timers.delete(h);
        }
        try {
          cb();
        } catch (e) {
          if (st.trap) {
            st.logs.push({ err: "ERROR: " + describe(e) });
          } else {
            writeErr("ERROR: " + describe(e));
          }
        }
        st.poke();
      };
      h = repeat ? setInterval(run, ms) : setTimeout(run, ms);
      st.timers.set(h, true);
      const unref = h.unref, ref = h.ref;
      h.unref = function () {
        if (st.timers.get(h) === true) {
          st.timers.set(h, false);
          st.pending--;
          st.poke();
        }
        return unref.call(h);
      };
      h.ref = function () {
        if (st.timers.get(h) === false) {
          st.timers.set(h, true);
          st.pending++;
        }
        return ref.call(h);
      };
      return h;
    },
    clear(h) {
      if (st.timers.has(h)) {
        if (st.timers.get(h)) st.pending--;
        st.timers.delete(h);
        clearTimeout(h);
        clearInterval(h);
      }
    },
    fetch(input, init) {
      let opts = init;
      if (typeof AbortController === "function") {
        const ac = new AbortController();
        st.aborts.add(ac);
        let signal = ac.signal;
        if (init && init.signal && typeof AbortSignal.any === "function") {
          signal = AbortSignal.any([init.signal, ac.signal]);
        }
        opts = Object.assign({}, init, { signal });
      }
      st.pending++;
      let p;
      try {
        p = globalThis.fetch(input, opts);
      } catch (e) {
        st.done();
        throw e;
      }
      return p.then((res) => {
        for (const m of BODY_METHODS) {
          const f = res[m];
          if (typeof f !== "function") continue;
          res[m] = function () {
            st.pending++;
            let q;
            try {
              q = f.apply(this, arguments);
            } catch (e) {
              st.done();
              throw e;
            }
            return Promise.resolve(q).finally(st.done);
          };
        }
        return res;
      }).finally(st.done);
    },
    readFile(p, opts) {
      const enc = (opts && opts.encoding) || "utf-8";
      if (enc !== "utf-8" && enc !== "utf8" && enc !== "") {
        throw new Error("unsupported encoding: " + enc);
      }
      try {
        return fs.readFileSync(p, "utf8");
      } catch (e) {
        throw new Error("failed to read " + p + ": " + e.message);
      }
    },
  };
  setupScript.runInContext(ctx)(host);
  nodeGlobalsScript.runInContext(ctx);
  if (env) {
    vm.runInContext("process.env = " + JSON.stringify(env) + ";", ctx);
  }
  // Reading one of these globals (typeof included) marks the evaluation
  // trapped; assigning it defines it, as code polyfilling it would.
  for (const name of trap || []) {
    if (Object.prototype.hasOwnProperty.call(ctx, name)) continue;
    Object.defineProperty(ctx, name, {
      configurable: true,
      enumerable: false,
      get() {
        st.trapped = true;
        return undefined;
      },
      set(v) {
        Object.defineProperty(ctx, name, { value: v, writable: true, configurable: true, enumerable: false });
      },
    });
  }
  return ctx;
}

function cleanup(st) {
  for (const h of st.timers.keys()) {
    clearTimeout(h);
    clearInterval(h);
  }
  st.timers.clear();
  for (const ac of st.aborts) {
    try {
      ac.abort();
    } catch (_) {}
  }
  st.aborts.clear();
  st.pending = 0;
}

function errText(e) {
  try {
    return String(e);
  } catch (_) {
    return "Error";
  }
}

const tick = () => new Promise((r) => setImmediate(r));

// settle waits until the until expression holds, the evaluation has nothing
// left pending, or the deadline passes.
async function settle(st, ctx, until, deadline) {
  const done = () => {
    if (!until) return false;
    try {
      return !!until.runInContext(ctx, { timeout: Math.max(1, deadline - Date.now()) });
    } catch (_) {
      return false;
    }
  };
  for (;;) {
    await tick();
    if (done() || Date.now() >= deadline) return;
    if (st.pending <= 0) {
      await new Promise((r) => setTimeout(r, 0));
      await tick();
      if (done() || st.pending <= 0) return;
    }
    await new Promise((r) => {
      st.wake = r;
      setTimeout(r, Math.max(1, Math.min(50, deadline - Date.now())));
    });
  }
}

async function handle(req, code) {
  const st = newState();
  const reply = { t: "r", id: req.id, threw: false, err: "", unsupported: false, reads: [] };
  const timeout = req.timeoutMs > 0 ? req.timeoutMs : 2147483647;
  const deadline = Date.now() + timeout;
  let ctx;
  try {
    st.trap = Array.isArray(req.trap) && req.trap.length > 0;
    ctx = makeContext(st, req.env, req.trap);
    let script;
    try {
      // A module runs as a strict function so its top-level bindings stay
      // out of the global object, as in an ES module. The prefix keeps line
      // numbers; source that only parses as a module (import.meta, top-level
      // await) is reported unsupported and runs in the embedded engine.
      const src = req.module ? '(function () {"use strict";' + code + "\n})();" : code;
      script = new vm.Script(src, { filename: req.filename || "<eval>" });
    } catch (e) {
      reply.unsupported = true;
      reply.err = errText(e);
      send(reply);
      return;
    }
    try {
      script.runInContext(ctx, { timeout });
    } catch (e) {
      // A throw while a module evaluates rejects its evaluation promise,
      // which the embedded engine does not report either.
      if (!req.module) {
        reply.threw = true;
        reply.err = errText(e);
      }
    }
    if (req.await && !reply.threw) {
      let until = null;
      if (req.until) {
        try {
          until = new vm.Script(req.until);
        } catch (_) {}
      }
      await settle(st, ctx, until, deadline);
    }
    for (const expr of req.reads || []) {
      try {
        const v = vm.runInContext(expr, ctx, { timeout: Math.max(1, deadline - Date.now()) });
        if (v === undefined || v === null) {
          reply.reads.push(null);
        } else {
          reply.reads.push({ s: typeof v === "string" ? v : String(v), str: typeof v === "string" });
        }
      } catch (e) {
        reply.reads.push({ e: errText(e) });
      }
    }
  } catch (e) {
    reply.threw = true;
    reply.err = errText(e);
  } finally {
    cleanup(st);
  }
  if (st.trapped) {
    send({ t: "r", id: req.id, threw: false, err: "", unsupported: true, reads: [] });
    return;
  }
  for (const l of st.logs) {
    if (l.err) writeErr(l.err);
    else send(l);
  }
  send(reply);
}

let buf = Buffer.alloc(0);
let pendingHead = null;
let busy = Promise.resolve();

function pump() {
  for (;;) {
    if (buf.length < 4) return;
    const n = buf.readUInt32BE(0);
    if (buf.length < 4 + n) return;
    const frame = buf.subarray(4, 4 + n);
    buf = buf.subarray(4 + n);
    if (pendingHead === null) {
      pendingHead = JSON.parse(frame.toString("utf8"));
      continue;
    }
    const req = pendingHead;
    const code = frame.toString("utf8");
    pendingHead = null;
    busy = busy.then(() => handle(req, code));
  }
}

process.stdin.on("data", (chunk) => {
  buf = buf.length ? Buffer.concat([buf, chunk]) : chunk;
  pump();
});
process.stdin.on("end", () => process.exit(0));
process.stdin.on("error", () => process.exit(0));
// The globals an evaluation sees: code that reads one QuickJS has and this
// runtime lacks runs in the embedded engine.
let globalNames = [];
try {
  const st = newState();
  globalNames = Object.getOwnPropertyNames(vm.runInContext("globalThis", makeContext(st, {})));
  cleanup(st);
} catch {}
send({ t: "ready", v: process.versions.bun ? "bun " + process.versions.bun : "node " + process.version, g: globalNames });
