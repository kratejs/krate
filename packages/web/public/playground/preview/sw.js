// Playground preview service worker.
//
// The playground page sends each successful build here; the preview iframe
// loads /playground/preview/<session>/<page>, and this worker answers with the
// built files. The build output uses root-absolute URLs (/styles.x.css,
// /chunks/a.js), so requests from a controlled preview document are matched
// against the same session's files whatever their URL. Everything else passes
// through to the network. Files live in the Cache API so they survive the
// worker being stopped when idle.

const CACHE = "krate-playground-preview-v1";
const STORE = "/__pg/";
const SESSION_TTL_MS = 2 * 24 * 3600 * 1000;

const TYPES = {
  html: "text/html; charset=utf-8",
  js: "text/javascript; charset=utf-8",
  mjs: "text/javascript; charset=utf-8",
  css: "text/css; charset=utf-8",
  json: "application/json; charset=utf-8",
  txt: "text/plain; charset=utf-8",
  svg: "image/svg+xml",
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  gif: "image/gif",
  webp: "image/webp",
  ico: "image/x-icon",
  woff: "font/woff",
  woff2: "font/woff2",
  xml: "application/xml; charset=utf-8",
  map: "application/json",
};

const typeOf = (path) => TYPES[/\.([a-z0-9]+)$/i.exec(path)?.[1]?.toLowerCase()] || "application/octet-stream";

self.addEventListener("install", () => {
  self.skipWaiting();
});
self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener("message", (event) => {
  const msg = event.data || {};
  const port = event.ports && event.ports[0];
  if (msg.type === "update") {
    event.waitUntil(
      store(msg.sid, msg.files).then(
        () => port && port.postMessage({ ok: true }),
        (err) => port && port.postMessage({ ok: false, error: String(err && err.message ? err.message : err) }),
      ),
    );
  } else if (msg.type === "ping") {
    if (port) port.postMessage({ ok: true });
  }
});

async function store(sid, files) {
  if (!/^[a-z0-9]{4,32}$/.test(sid)) throw new Error("bad session id");
  const cache = await caches.open(CACHE);
  const keys = await cache.keys();
  const mine = STORE + sid + "/";
  const now = Date.now();
  for (const req of keys) {
    const path = new URL(req.url).pathname;
    if (path.startsWith(mine)) await cache.delete(req);
    else if (path.endsWith("/.touch")) {
      const res = await cache.match(req);
      const t = res ? Number(await res.text()) : 0;
      if (now - t > SESSION_TTL_MS) {
        const prefix = path.slice(0, path.length - ".touch".length);
        for (const other of keys) if (new URL(other.url).pathname.startsWith(prefix)) await cache.delete(other);
      }
    }
  }
  for (const [path, body] of Object.entries(files)) {
    await cache.put(
      new Request(location.origin + mine + path),
      new Response(body, { headers: { "content-type": typeOf(path), "cache-control": "no-store" } }),
    );
  }
  await cache.put(new Request(location.origin + mine + ".touch"), new Response(String(now)));
}

function sessionOf(pathname) {
  const m = /^\/playground\/preview\/([a-z0-9]{4,32})(\/.*)?$/.exec(pathname);
  return m ? { sid: m[1], rest: m[2] || "/" } : null;
}

async function lookup(sid, rest) {
  const cache = await caches.open(CACHE);
  const p = decodeURIComponent(rest).replace(/^\/+/, "");
  const tries = [];
  if (p === "" || p.endsWith("/")) tries.push(p + "index.html");
  else {
    tries.push(p);
    if (!/\.[a-z0-9]+$/i.test(p)) tries.push(p + ".html", p + "/index.html");
  }
  for (const t of tries) {
    const hit = await cache.match(location.origin + STORE + sid + "/" + t);
    if (hit) return { res: hit, path: t };
  }
  return null;
}

// Runs inside the preview document; stringified into each HTML response.
function relay(sid) {
  var prefix = "/playground/preview/" + sid;
  var parentWin = window.parent;
  function send(msg) {
    msg.pg = 1;
    msg.sid = sid;
    try {
      parentWin.postMessage(msg, location.origin);
    } catch {}
  }
  function fmt(v) {
    if (typeof v === "string") return v;
    if (v instanceof Error) return v.stack || v.name + ": " + v.message;
    if (v === undefined) return "undefined";
    try {
      var s = JSON.stringify(v);
      return s === undefined ? String(v) : s.length > 800 ? s.slice(0, 800) + "..." : s;
    } catch {
      return String(v);
    }
  }
  ["log", "info", "warn", "error", "debug"].forEach(function (level) {
    var orig = console[level];
    console[level] = function () {
      var args = Array.prototype.slice.call(arguments).map(fmt);
      send({ type: "console", level, text: args.join(" ") });
      return orig.apply(console, arguments);
    };
  });
  window.addEventListener("error", function (e) {
    var where = e.filename ? " (" + e.filename.replace(location.origin, "") + ":" + e.lineno + ")" : "";
    send({ type: "console", level: "error", text: (e.message || "Error") + where });
  });
  window.addEventListener("unhandledrejection", function (e) {
    send({ type: "console", level: "error", text: "Unhandled rejection: " + fmt(e.reason) });
  });
  function nav() {
    var p = location.pathname.indexOf(prefix) === 0 ? location.pathname.slice(prefix.length) || "/" : location.pathname;
    send({ type: "nav", path: p + location.search + location.hash, title: document.title });
  }
  document.addEventListener("DOMContentLoaded", nav);
  window.addEventListener("load", nav);
  window.addEventListener("popstate", nav);
  ["pushState", "replaceState"].forEach(function (k) {
    var orig = history[k];
    history[k] = function () {
      var r = orig.apply(history, arguments);
      nav();
      return r;
    };
  });
  // Root-absolute links would leave the preview; send them to this session.
  document.addEventListener(
    "click",
    function (e) {
      if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
      var a = e.target && e.target.closest ? e.target.closest("a[href]") : null;
      if (!a || (a.target && a.target !== "_self") || a.hasAttribute("download")) return;
      var href = a.getAttribute("href");
      if (!href || href.charAt(0) !== "/" || href.charAt(1) === "/" || href.indexOf("/playground/preview/") === 0) return;
      e.preventDefault();
      e.stopImmediatePropagation();
      location.assign(prefix + href);
    },
    true,
  );
}

function inject(html, sid) {
  const script = `<script>(${relay.toString()})(${JSON.stringify(sid)});</script>`;
  const m = /<head[^>]*>/i.exec(html);
  if (m) return html.slice(0, m.index + m[0].length) + script + html.slice(m.index + m[0].length);
  return script + html;
}

async function serve(sid, rest, request) {
  const hit = await lookup(sid, rest);
  if (!hit) {
    const notFound = await lookup(sid, "/404.html");
    const body = notFound ? await notFound.res.text() : "<h1>404</h1><p>No such page in this build.</p>";
    return new Response(inject(body, sid), { status: 404, headers: { "content-type": TYPES.html, "cache-control": "no-store" } });
  }
  if (hit.path.endsWith(".html")) {
    return new Response(request.method === "HEAD" ? null : inject(await hit.res.text(), sid), {
      headers: { "content-type": TYPES.html, "cache-control": "no-store" },
    });
  }
  return hit.res;
}

self.addEventListener("fetch", (event) => {
  const request = event.request;
  if (request.method !== "GET" && request.method !== "HEAD") return;
  const url = new URL(request.url);
  if (url.origin !== location.origin) return;

  const own = sessionOf(url.pathname);
  if (request.mode === "navigate") {
    if (own) event.respondWith(serve(own.sid, own.rest, request));
    return;
  }

  event.respondWith(
    (async () => {
      if (own) return serve(own.sid, own.rest, request);
      const client = event.clientId ? await self.clients.get(event.clientId) : null;
      const from = client ? sessionOf(new URL(client.url).pathname) : null;
      if (from) {
        const hit = await lookup(from.sid, url.pathname);
        if (hit) return hit.res;
      }
      return fetch(request);
    })(),
  );
});
