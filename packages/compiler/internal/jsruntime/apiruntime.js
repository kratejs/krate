// Request/response orchestration shared by the embedded API and middleware
// runners. Evaluated once per VM after webapi.js.
(function () {
  'use strict';
  var G = globalThis;
  var W = G.__krate_web;
  var METHODS = ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'];

  function parseCookies(header) {
    var jar = new Map();
    var parts = String(header || '').split(';');
    for (var i = 0; i < parts.length; i++) {
      var eq = parts[i].indexOf('=');
      if (eq <= 0) continue;
      var name = parts[i].slice(0, eq).trim();
      if (name && !jar.has(name)) jar.set(name, parts[i].slice(eq + 1).trim());
    }
    return jar;
  }
  function cookieApi(headers) {
    var jar = parseCookies(headers.get('cookie'));
    return {
      get: function (name) { return jar.has(String(name)) ? { name: String(name), value: jar.get(String(name)) } : undefined; },
      getAll: function (name) {
        var out = [];
        jar.forEach(function (v, k) { if (name === undefined || k === String(name)) out.push({ name: k, value: v }); });
        return out;
      },
      has: function (name) { return jar.has(String(name)); },
      get size() { return jar.size; }
    };
  }

  // makeRequest builds a web Request from the Go envelope. The body arrives in
  // the __krate_body global (base64) and is decoded lazily on first read.
  G.__krate_make_request = function (raw) {
    var hasBody = raw.method !== 'GET' && raw.method !== 'HEAD';
    var req = new Request(raw.url, { method: raw.method, headers: raw.headers });
    if (hasBody && raw.hasBody) {
      req[W.kBody] = {
        empty: false, bytes: null, stream: null, used: false,
        lazy: function () { return __go_body_b64(); },
        lazyText: function () { return __go_body_text(); },
        lazyMultipart: function (ct) { return __go_body_multipart(ct); }
      };
    }
    Object.defineProperty(req, 'cookies', { value: cookieApi(req.headers), enumerable: false });
    Object.defineProperty(req, 'path', { value: raw.path || new URL(raw.url).pathname, enumerable: false });
    return req;
  };

  var S = { phase: 'idle' };
  var abortController = null;

  function errMsg(e) {
    if (e && typeof e === 'object' && 'message' in e) return String(e.message);
    return e === undefined ? 'undefined thrown' : String(e);
  }
  // notFound(), forbidden() and unauthorized() end the request with that status.
  var FALLBACK_LABEL = { 401: 'Unauthorized', 403: 'Forbidden', 404: 'Not Found' };
  function fallbackStatus(e) {
    if (!e || typeof e !== 'object') return 0;
    var d = e.digest;
    if (d === 'NEXT_NOT_FOUND' || d === 'NEXT_HTTP_ERROR_FALLBACK;404' || e.__krateNotFound === true) return 404;
    if (d === 'NEXT_HTTP_ERROR_FALLBACK;403' || e.__krateHTTPStatus === 403) return 403;
    if (d === 'NEXT_HTTP_ERROR_FALLBACK;401' || e.__krateHTTPStatus === 401) return 401;
    return 0;
  }
  function fail(e) {
    if (S.phase !== 'pending') return;
    var fb = fallbackStatus(e);
    if (fb) {
      setHead(fb, '', [['content-type', 'application/json'], ['cache-control', 'no-store']], { str: JSON.stringify({ error: FALLBACK_LABEL[fb] }) });
      return;
    }
    S.phase = 'error';
    S.error = errMsg(e);
    S.stack = e && e.stack ? String(e.stack) : '';
  }

  function headPairs(h) { return h._pairs(); }

  function setHead(status, statusText, headers, body) {
    S.phase = 'ready';
    S.head = { status: status, statusText: statusText || '', headers: headers };
    if (body && body.stream) { S.reader = body.stream.getReader(); S.head.stream = true; }
    else if (body && body.str !== undefined) S.head.text = body.str;
    else if (body && body.bytes && body.bytes.length) S.head.body = W.bytesToB64(body.bytes);
  }

  function fromResponse(res) {
    var st = W.bodyState(res);
    var body = null;
    if (!st.empty) {
      if (st.used) throw new TypeError('Response body is already used');
      st.used = true;
      if (st.stream) body = { stream: st.stream };
      else if (st.str !== undefined && st.bytes == null && st.str.indexOf('\0') < 0) body = { str: st.str };
      else body = { bytes: W.bodyBytes(st) };
    }
    setHead(res.status, res.statusText, headPairs(res.headers), body);
  }

  function fromValue(v) {
    if (v === undefined || v === null) { S.implicit = true; setHead(204, '', [], null); return; }
    var json = JSON.stringify(v);
    if (json === undefined) { setHead(204, '', [], null); return; }
    setHead(200, '', [['content-type', 'application/json']], json.indexOf('\0') < 0 ? { str: json } : { bytes: W.utf8Encode(json) });
  }

  // A Response-shaped object that is not a real Response (older route code
  // returned { status, headers, text() }): copy it through the real class.
  function isForeignResponse(v) {
    return v && typeof v === 'object' && typeof v.status === 'number' && !Array.isArray(v) &&
      (typeof v.text === 'function' || typeof v.arrayBuffer === 'function' || 'body' in v || 'headers' in v);
  }
  function foreignHeaders(h) {
    if (!h) return new Headers();
    if (h instanceof Headers) return h;
    if (typeof h.entries === 'function') return new Headers(h.entries());
    return new Headers(h);
  }

  function settle(v) {
    if (S.phase !== 'pending') return;
    try {
      if (v instanceof Response) fromResponse(v);
      else if (isForeignResponse(v)) {
        var hdrs = foreignHeaders(v.headers);
        var bodyP = typeof v.arrayBuffer === 'function' ? v.arrayBuffer() : typeof v.text === 'function' ? v.text() : v.body;
        Promise.resolve(bodyP).then(function (b) {
          try {
            var empty = v.status === 204 || v.status === 304 || b === undefined || b === null || b === '' || (b.byteLength === 0);
            fromResponse(new Response(empty ? null : b, { status: v.status < 200 ? 200 : v.status, headers: hdrs }));
          } catch (e) { fail(e); }
        }, fail);
      } else fromValue(v);
    } catch (e) { fail(e); }
  }

  // Node-style handler(req, res), kept for default exports with two params.
  function runLegacy(fn, raw) {
    var headers = {};
    raw.headers.forEach(function (p) {
      var k = p[0];
      if (k === 'set-cookie') (headers[k] = headers[k] || []).push(p[1]);
      else headers[k] = headers[k] ? headers[k] + ', ' + p[1] : p[1];
    });
    var bodyText = raw.hasBody ? __go_body_text() : '';
    var listeners = {};
    var req = {
      url: raw.url.replace(/^[a-z]+:\/\/[^\/]+/i, ''), method: raw.method, headers: headers, body: bodyText,
      socket: { remoteAddress: raw.remoteAddr || '127.0.0.1' },
      on: function (ev, cb) { (listeners[ev] = listeners[ev] || []).push(cb); return req; },
      once: function (ev, cb) { return req.on(ev, cb); },
      setEncoding: function () { return req; }
    };
    var outHeaders = {}, chunks = [], finished = false;
    var res = {
      statusCode: 200, statusMessage: '', headersSent: false,
      setHeader: function (k, v) { outHeaders[String(k).toLowerCase()] = Array.isArray(v) ? v.map(String) : String(v); return res; },
      getHeader: function (k) { return outHeaders[String(k).toLowerCase()]; },
      getHeaders: function () { return outHeaders; },
      hasHeader: function (k) { return String(k).toLowerCase() in outHeaders; },
      removeHeader: function (k) { delete outHeaders[String(k).toLowerCase()]; },
      writeHead: function (status, a, b) {
        res.statusCode = status;
        var h = typeof a === 'object' ? a : b;
        if (h) Object.keys(h).forEach(function (k) { res.setHeader(k, h[k]); });
        return res;
      },
      flushHeaders: function () {},
      status: function (n) { res.statusCode = n; return res; },
      write: function (c) { if (c !== undefined && c !== null) chunks.push(typeof c === 'string' ? W.utf8Encode(c) : W.toBytes(c) || W.utf8Encode(String(c))); return true; },
      end: function (c) { if (finished) return res; if (c) res.write(c); finished = true; finish(); return res; },
      json: function (o) { if (!res.hasHeader('content-type')) res.setHeader('content-type', 'application/json'); return res.end(JSON.stringify(o)); },
      send: function (o) {
        if (o !== null && typeof o === 'object' && !W.toBytes(o)) return res.json(o);
        return res.end(o === undefined ? '' : o);
      },
      on: function () { return res; }, once: function () { return res; }, emit: function () { return false; }
    };
    function finish() {
      if (S.phase !== 'pending') return;
      var pairs = [];
      Object.keys(outHeaders).forEach(function (k) {
        var v = outHeaders[k];
        if (Array.isArray(v)) v.forEach(function (x) { pairs.push([k, x]); }); else pairs.push([k, v]);
      });
      setHead(res.statusCode, res.statusMessage, pairs, { bytes: W.concatBytes(chunks) });
    }
    var result = fn(req, res);
    var after = function (v) {
      if (finished || S.phase !== 'pending') return;
      if (v !== undefined && v !== null) { settle(v); return; }
      // Node leaves the socket open until res.end(); with no end and no
      // pending work the request would hang, so fail it loudly instead.
      Promise.resolve().then(function () {}).then(function () {
        if (!finished && S.phase === 'pending') {
          var more = G.__krate_timers_next();
          if (more < 0) fail(new Error('legacy handler returned without calling res.end()'));
        }
      });
    };
    if (result && typeof result.then === 'function') result.then(after, fail); else after(result);
    // Deliver the request body to stream-style readers.
    Promise.resolve().then(function () {
      if (listeners.data && bodyText) listeners.data.forEach(function (cb) { cb(bodyText); });
      if (listeners.end) listeners.end.forEach(function (cb) { cb(); });
    });
  }

  function allowList(ns) {
    var out = [];
    for (var i = 0; i < METHODS.length; i++) if (typeof ns[METHODS[i]] === 'function') out.push(METHODS[i]);
    if (out.indexOf('GET') >= 0 && out.indexOf('HEAD') < 0) out.push('HEAD');
    if (out.length && out.indexOf('OPTIONS') < 0) out.push('OPTIONS');
    return out;
  }

  // __krate_begin starts a handler. Returns '' when the request is running
  // (poll __krate_phase) or a JSON object for an immediate answer.
  G.__krate_begin = function (routeId, method, rawJson) {
    G.__krate_timers_clear();
    abortController = null;
    S = { phase: 'pending', head: null, reader: null, chunk: null, headOnly: false };
    var ns = G.__krate_modules ? G.__krate_modules[routeId] : null;
    if (!ns) return JSON.stringify({ status: 500, error: 'API route module not loaded' });
    var m = String(method).toUpperCase();
    var fn = ns[m], named = typeof fn === 'function';
    if (!named && m === 'HEAD' && typeof ns.GET === 'function') { fn = ns.GET; named = true; S.headOnly = true; }
    if (!named) {
      var d = ns.__default || ns.default;
      if (typeof d === 'function') fn = d;
      else {
        var allow = allowList(ns);
        if (m === 'OPTIONS' && allow.length) return JSON.stringify({ status: 204, allow: allow });
        if (allow.length) return JSON.stringify({ status: 405, allow: allow, error: 'Method ' + m + ' Not Allowed' });
        return JSON.stringify({ status: 500, error: 'API route must export a handler function (GET, POST, etc. or default).' });
      }
    }
    var raw = JSON.parse(rawJson);
    try {
      if (!named && fn.length >= 2) {
        S.legacy = true;
        runLegacy(fn, raw);
        return '';
      }
      var request = G.__krate_make_request(raw);
      abortController = { signal: request.signal };
      var ctxArg = { params: raw.params || {} };
      Promise.resolve().then(function () { return fn(request, ctxArg); }).then(settle, fail);
    } catch (e) { fail(e); }
    return '';
  };

  G.__krate_phase = function () { return S.phase; };
  G.__krate_abort = function () {
    if (abortController) {
      try { W.abortSignal(abortController.signal, new DOMException('The operation was aborted.', 'AbortError')); } catch (_) {}
    }
  };
  G.__krate_head = function () {
    if (S.phase === 'error') return JSON.stringify({ error: S.error, stack: S.stack });
    return JSON.stringify(S.head);
  };
  G.__krate_chunk_start = function () {
    S.chunk = null;
    if (!S.reader) { S.chunk = { done: true }; return; }
    S.reader.read().then(function (r) {
      if (r.done) { S.chunk = { done: true }; return; }
      var b = typeof r.value === 'string' ? W.utf8Encode(r.value) : W.toBytes(r.value);
      if (!b) { S.chunk = { error: 'response stream yielded a non-binary chunk' }; return; }
      S.chunk = { data: W.bytesToB64(b) };
    }, function (e) { S.chunk = { error: errMsg(e) }; });
  };
  G.__krate_chunk = function () { return S.chunk ? JSON.stringify(S.chunk) : ''; };
  G.__krate_finish = function () {
    if (S.reader) { try { S.reader.cancel(); } catch (_) {} }
    S = { phase: 'idle' };
    abortController = null;
    G.__krate_timers_clear();
  };

  // ---- middleware -------------------------------------------------------
  var mw = { fn: null, matchers: null };

  // Called by Go after the middleware module has been evaluated.
  G.__krate_mw_init = function (fn, cfg, matcherExport) {
    mw.fn = typeof fn === 'function' ? fn : null;
    mw.matchers = null;
    if (!mw.fn) return 'middleware.js does not export a middleware function';
    var m = matcherExport !== undefined ? matcherExport : cfg && cfg.matcher;
    if (m === undefined || m === null) return '';
    if (!Array.isArray(m)) m = [m];
    try {
      mw.matchers = m.map(function (e) {
        if (e && typeof e === 'object') {
          if (e.has || e.missing || e.locale === false) throw new Error('middleware matcher "has"/"missing"/"locale" conditions are not supported by the embedded runtime; use a path-only matcher or set ssr.middlewareRuntime to "node"');
          return G.__krate_compile_matcher(e.source);
        }
        return G.__krate_compile_matcher(e);
      });
    } catch (e) { return 'invalid middleware matcher: ' + errMsg(e); }
    return '';
  };
  G.__krate_mw_has_matcher = function () { return mw.matchers !== null; };

  G.__krate_mw_begin = function (rawJson) {
    G.__krate_timers_clear();
    var raw = JSON.parse(rawJson);
    if (mw.matchers) {
      var hit = false;
      for (var i = 0; i < mw.matchers.length; i++) if (mw.matchers[i].test(raw.path)) { hit = true; break; }
      if (!hit) { S = { phase: 'idle' }; return 'skip'; }
    }
    S = { phase: 'pending', head: null, reader: null, chunk: null };
    abortController = null;
    try {
      var request = G.__krate_make_request(raw);
      Promise.resolve().then(function () { return mw.fn(request); }).then(settle, fail);
    } catch (e) { fail(e); }
    return '';
  };
  G.__krate_mw_head = function () {
    if (S.phase === 'error') return JSON.stringify({ kind: 'error', error: S.error, stack: S.stack });
    var h = S.head, next = false, rewrite = '', hdrs = [];
    if (S.reader) return JSON.stringify({ kind: 'error', error: 'middleware cannot return a streaming Response body' });
    for (var i = 0; i < h.headers.length; i++) {
      var k = h.headers[i][0];
      if (k === 'x-middleware-next') next = true;
      else if (k === 'x-middleware-rewrite') rewrite = h.headers[i][1];
      else hdrs.push(h.headers[i]);
    }
    if (next || rewrite) return JSON.stringify({ kind: 'continue', headers: hdrs, rewrite: rewrite });
    if (h.status === 204 && !h.body && !hdrs.length && S.implicit) return JSON.stringify({ kind: 'continue', headers: [] });
    return JSON.stringify({ kind: 'response', status: h.status, statusText: h.statusText, headers: hdrs, body: h.body || '', text: h.text || '' });
  };
})();
