// Fetch feature: Headers, Request, Response and fetch (performed by the Go
// host through __go_fetch). The API and middleware runners always use it.
// Streams and FormData load only when a body needs them.
(function (G, K) {
  'use strict';
  var def = K.def, unsupported = K.unsupported, stubClass = K.stubClass;
  var toBytes = K.toBytes, utf8Encode = K.utf8Encode, utf8Decode = K.utf8Decode, toHost = K.toHost, fromHost = K.fromHost, host = K.host;
  var urlMod = K.need('url'), URL = urlMod.URL, URLSearchParams = urlMod.URLSearchParams, parseForm = urlMod.parseForm;
  var AbortController = K.need('events').AbortController;
  var goFetch = G.__go_fetch;

  var TOKEN = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;
  function normName(name) {
    name = String(name);
    if (!TOKEN.test(name)) throw new TypeError('Invalid header name: ' + name);
    return name.toLowerCase();
  }
  function normValue(v) {
    v = String(v).replace(/^[\t\n\r ]+|[\t\n\r ]+$/g, '');
    if (/[\0\r\n]/.test(v)) throw new TypeError('Invalid header value');
    return v;
  }
  class Headers {
    constructor(init) {
      Object.defineProperty(this, '_m', { value: new Map(), enumerable: false });
      if (init === undefined || init === null) return;
      if (init instanceof Headers) { init._m.forEach(function (vals, k) { this._m.set(k, vals.slice()); }, this); return; }
      if (typeof init !== 'object' && typeof init !== 'function') throw new TypeError('The provided value is not of type \'(record<ByteString, ByteString> or sequence<sequence<ByteString>>)\'');
      if (typeof init[Symbol.iterator] === 'function') {
        for (var pair of init) {
          var arr = Array.from(pair);
          if (arr.length !== 2) throw new TypeError('Header pair must have exactly two items');
          this.append(arr[0], arr[1]);
        }
      } else {
        var keys = Object.keys(init);
        for (var i = 0; i < keys.length; i++) this.append(keys[i], init[keys[i]]);
      }
    }
    append(name, value) {
      var k = normName(name), v = normValue(value), cur = this._m.get(k);
      if (cur) cur.push(v); else this._m.set(k, [v]);
    }
    set(name, value) { this._m.set(normName(name), [normValue(value)]); }
    get(name) {
      var vals = this._m.get(normName(name));
      return vals ? vals.join(', ') : null;
    }
    getSetCookie() { return (this._m.get('set-cookie') || []).slice(); }
    has(name) { return this._m.has(normName(name)); }
    delete(name) { this._m.delete(normName(name)); }
    _pairs() {
      var keys = Array.from(this._m.keys()).sort(), out = [];
      for (var i = 0; i < keys.length; i++) {
        var vals = this._m.get(keys[i]);
        if (keys[i] === 'set-cookie') for (var j = 0; j < vals.length; j++) out.push([keys[i], vals[j]]);
        else out.push([keys[i], vals.join(', ')]);
      }
      return out;
    }
    forEach(cb, thisArg) { var p = this._pairs(); for (var i = 0; i < p.length; i++) cb.call(thisArg, p[i][1], p[i][0], this); }
    *keys() { var p = this._pairs(); for (var i = 0; i < p.length; i++) yield p[i][0]; }
    *values() { var p = this._pairs(); for (var i = 0; i < p.length; i++) yield p[i][1]; }
    *entries() { var p = this._pairs(); for (var i = 0; i < p.length; i++) yield [p[i][0], p[i][1]]; }
    [Symbol.iterator]() { return this.entries(); }
  }
  def('Headers', Headers);

  // extractBody turns a BodyInit into { bytes | stream, type }.
  function extractBody(init) {
    if (init === undefined || init === null) return null;
    if (typeof init === 'string') return { str: init, type: 'text/plain;charset=UTF-8' };
    var b = toBytes(init);
    if (b) return { bytes: b.slice(), type: null };
    var fd = K.loaded('formdata'), rs = K.loaded('streams');
    if (fd && init instanceof fd.Blob) return { bytes: init._b, type: init.type || null };
    if (init instanceof URLSearchParams) return { str: init.toString(), type: 'application/x-www-form-urlencoded;charset=UTF-8' };
    if (fd && init instanceof fd.FormData) { var m = fd.serializeMultipart(init); return { bytes: m.bytes, type: m.type }; }
    if (rs && init instanceof rs.ReadableStream) return { stream: init, type: null };
    if (typeof init === 'object' && typeof init[Symbol.asyncIterator] === 'function') return { stream: K.need('streams').ReadableStream.from(init), type: null };
    return { str: String(init), type: 'text/plain;charset=UTF-8' };
  }
  var kBody = Symbol('body');
  function bodyState(o) { return o[kBody]; }
  function consumeBody(o) {
    var st = bodyState(o);
    if (st.used) return Promise.reject(new TypeError('Body is unusable: Body has already been read'));
    st.used = true;
    if (st.stream) return K.need('streams').readAllBytes(st.stream);
    return Promise.resolve(bodyBytes(st) || new Uint8Array(0));
  }
  // consumeText reads the body as a string without a bytes round trip when the
  // body already is (or can be produced as) text.
  function consumeText(o) {
    var st = bodyState(o);
    if (st.used) return Promise.reject(new TypeError('Body is unusable: Body has already been read'));
    st.used = true;
    if (st.stream) return K.need('streams').readAllBytes(st.stream).then(function (b) { return utf8Decode(b); });
    if (st.str !== undefined) return Promise.resolve(st.str);
    if (st.lazyText) { var t = st.lazyText(); st.lazyText = null; st.lazy = null; return Promise.resolve(t); }
    return Promise.resolve(utf8Decode(bodyBytes(st) || new Uint8Array(0)));
  }
  function bodyBytes(st) {
    if (st.lazy) { st.bytes = st.lazy(); st.lazy = null; st.lazyText = null; }
    else if (st.bytes == null && st.str !== undefined) st.bytes = utf8Encode(st.str);
    return st.bytes;
  }
  var bodyMethods = {
    arrayBuffer: function () { return consumeBody(this).then(function (b) { return b.byteOffset === 0 && b.byteLength === b.buffer.byteLength ? b.buffer : b.slice().buffer; }); },
    bytes: function () { return consumeBody(this); },
    blob: function () { var self = this; return consumeBody(this).then(function (b) { return new (K.need('formdata').Blob)([b], { type: self.headers.get('content-type') || '' }); }); },
    text: function () { return consumeText(this); },
    json: function () { return consumeText(this).then(function (t) { return JSON.parse(t); }); },
    formData: function () {
      var self = this, ct = self.headers.get('content-type'), st = bodyState(self);
      if (st.lazyMultipart && String(ct || '').toLowerCase().indexOf('multipart/form-data') === 0 && !st.used) {
        st.used = true;
        return Promise.resolve().then(function () { return K.need('formdata').multipartFromParsed(st.lazyMultipart(String(ct))); });
      }
      if (String(ct || '').toLowerCase().indexOf('application/x-www-form-urlencoded') === 0) {
        return consumeText(self).then(function (t) { var fd = new (K.need('formdata').FormData)(), list = parseForm(t); for (var i = 0; i < list.length; i++) fd.append(list[i][0], list[i][1]); return fd; });
      }
      return consumeBody(self).then(function (b) { return K.need('formdata').parseFormBody(ct, b); });
    }
  };
  function installBody(C) {
    Object.keys(bodyMethods).forEach(function (k) {
      Object.defineProperty(C.prototype, k, { value: bodyMethods[k], writable: true, configurable: true, enumerable: false });
    });
    Object.defineProperty(C.prototype, 'body', {
      configurable: true, enumerable: true,
      get: function () {
        var st = bodyState(this);
        if (st.empty) return null;
        if (!st.stream) st.stream = K.need('streams').streamFromBytes(bodyBytes(st) || new Uint8Array(0));
        return st.stream;
      }
    });
    Object.defineProperty(C.prototype, 'bodyUsed', {
      configurable: true, enumerable: true,
      get: function () { return bodyState(this).used; }
    });
  }
  function initBody(o, body, headers) {
    var ex = extractBody(body);
    if (!ex) { o[kBody] = { empty: true, bytes: null, stream: null, used: false }; return; }
    o[kBody] = { empty: false, bytes: ex.bytes || null, str: ex.str, stream: ex.stream || null, used: false };
    if (ex.type && !headers.has('content-type')) headers.set('content-type', ex.type);
  }
  function cloneBody(from, to) {
    var st = bodyState(from);
    if (st.used) throw new TypeError('Response.clone: Body has already been consumed.');
    if (st.stream) unsupported('cloning a Request/Response with a stream body');
    to[kBody] = { empty: st.empty, bytes: bodyBytes(st), stream: null, used: false };
  }

  var METHODS = ['DELETE', 'GET', 'HEAD', 'OPTIONS', 'POST', 'PUT'];
  class Request {
    constructor(input, init) {
      init = init || {};
      var url, method = 'GET', headers, body = null, signal = null, fromReq = null;
      if (input instanceof Request) {
        fromReq = input;
        url = input.url; method = input.method; headers = new Headers(input.headers); signal = input.signal;
        var ist = bodyState(input);
        if (!ist.empty) body = ist;
      } else {
        var u;
        try { u = new URL(String(input)); } catch (e) { var er = new TypeError('Failed to parse URL from ' + String(input)); er.cause = e; throw er; }
        if (u.username || u.password) throw new TypeError('Request cannot be constructed from a URL that includes credentials: ' + String(input));
        url = u.href;
      }
      if (init.method !== undefined) {
        var m = String(init.method);
        if (!TOKEN.test(m)) throw new TypeError('\'' + m + '\' is not a valid HTTP method.');
        var up = m.toUpperCase();
        if (up === 'CONNECT' || up === 'TRACE' || up === 'TRACK') throw new TypeError('\'' + m + '\' HTTP method is unsupported.');
        method = METHODS.indexOf(up) >= 0 ? up : m;
      }
      if (init.headers !== undefined) headers = new Headers(init.headers);
      else if (!headers) headers = new Headers();
      if (init.signal !== undefined && init.signal !== null) signal = init.signal;
      var hasBody = init.body !== undefined && init.body !== null;
      if (hasBody && (method === 'GET' || method === 'HEAD')) throw new TypeError('Request with GET/HEAD method cannot have body.');
      Object.defineProperty(this, '_h', { value: headers, enumerable: false });
      Object.defineProperty(this, '_m', { value: method, enumerable: false });
      Object.defineProperty(this, '_u', { value: url, enumerable: false });
      var ac = signal;
      if (!ac) { ac = new AbortController().signal; }
      Object.defineProperty(this, '_s', { value: ac, enumerable: false });
      Object.defineProperty(this, '_o', { value: { redirect: init.redirect || (fromReq && fromReq.redirect) || 'follow', cache: init.cache || 'default', credentials: init.credentials || 'same-origin', mode: init.mode || 'cors', referrer: init.referrer === undefined ? 'about:client' : init.referrer, keepalive: !!init.keepalive, integrity: init.integrity || '' }, enumerable: false });
      if (hasBody) initBody(this, init.body, headers);
      else if (body) { this[kBody] = { empty: false, bytes: bodyBytes(body), stream: body.stream, used: false }; body.used = true; }
      else this[kBody] = { empty: true, bytes: null, stream: null, used: false };
    }
    get method() { return this._m; }
    get url() { return this._u; }
    get headers() { return this._h; }
    get signal() { return this._s; }
    get redirect() { return this._o.redirect; }
    get cache() { return this._o.cache; }
    get credentials() { return this._o.credentials; }
    get mode() { return this._o.mode; }
    get referrer() { return this._o.referrer; }
    get keepalive() { return this._o.keepalive; }
    get integrity() { return this._o.integrity; }
    get destination() { return ''; }
    get duplex() { return 'half'; }
    clone() {
      var c = new Request(this.url, { method: this.method, headers: this.headers, signal: this.signal });
      cloneBody(this, c);
      return c;
    }
  }
  installBody(Request);
  def('Request', Request);

  var NULL_BODY = [101, 204, 205, 304];
  class Response {
    constructor(body, init) {
      init = init || {};
      var status = init.status === undefined ? 200 : Number(init.status);
      if (!Number.isInteger(status) || status < 200 || status > 599) throw new RangeError('init["status"] must be in the range of 200 to 599, inclusive.');
      var headers = new Headers(init.headers);
      if (body !== undefined && body !== null && NULL_BODY.indexOf(status) >= 0) throw new TypeError('Response constructor: Invalid response status code ' + status);
      Object.defineProperty(this, '_h', { value: headers, enumerable: false });
      Object.defineProperty(this, '_st', { value: status, writable: true, enumerable: false });
      Object.defineProperty(this, '_stt', { value: init.statusText === undefined ? '' : String(init.statusText), enumerable: false });
      Object.defineProperty(this, '_t', { value: 'default', writable: true, enumerable: false });
      initBody(this, body, headers);
    }
    get status() { return this._st; }
    get statusText() { return this._stt; }
    get ok() { return this._st >= 200 && this._st <= 299; }
    get headers() { return this._h; }
    get type() { return this._t; }
    get url() { return ''; }
    get redirected() { return false; }
    clone() {
      var c = new Response(null, { status: this._st < 200 ? 200 : this._st, statusText: this._stt, headers: this._h });
      cloneBody(this, c);
      return c;
    }
    static json(data, init) {
      var text = JSON.stringify(data);
      if (text === undefined) throw new TypeError('Value is not JSON serializable');
      var h = new Headers(init && init.headers);
      if (!h.has('content-type')) h.set('content-type', 'application/json');
      return new Response(text, { status: init && init.status, statusText: init && init.statusText, headers: h });
    }
    static redirect(url, status) {
      var u;
      try { u = new URL(String(url)); } catch (e) { var er = new TypeError('Failed to parse URL from ' + String(url)); er.cause = e; throw er; }
      status = status === undefined ? 302 : Number(status);
      if ([301, 302, 303, 307, 308].indexOf(status) < 0) throw new RangeError('Invalid status code ' + status);
      var r = new Response(null, { status: status });
      r._h.set('location', u.href);
      return r;
    }
    static error() {
      var r = new Response(null, { status: 200 });
      r._st = 0;
      r._t = 'error';
      return r;
    }
  }
  installBody(Response);
  def('Response', Response);

  def('fetch', async function fetch(input, init) {
    var req = new Request(input, init);
    if (req.signal.aborted) throw req.signal.reason;
    var bytes = null;
    if (req.method !== 'GET' && req.method !== 'HEAD' && !bodyState(req).empty) bytes = await consumeBody(req);
    var hdrs = [];
    req.headers.forEach(function (v, k) { hdrs.push([k, v]); });
    var opts = init || {};
    var payload = JSON.stringify({
      url: req.url, method: req.method, headers: hdrs,
      cache: opts.cache || '', next: opts.next || null, redirect: req.redirect
    });
    var raw;
    try { raw = host(goFetch, payload, toHost(bytes || new Uint8Array(0))); } catch (e) { var te = new TypeError('fetch failed'); te.cause = e; throw te; }
    var res = JSON.parse(raw);
    // The body follows the head through __go_take.
    var data = fromHost(host(G.__go_take));
    var body = NULL_BODY.indexOf(res.status) >= 0 || req.method === 'HEAD' ? null : data;
    var out = new Response(body, { status: res.status, statusText: res.statusText, headers: new Headers(res.headers) });
    Object.defineProperty(out, 'url', { value: res.url || req.url });
    Object.defineProperty(out, 'redirected', { value: !!res.redirected });
    return out;
  });

  stubClass('WebSocket');
  stubClass('EventSource');

  K.mods.fetch = { kBody: kBody, bodyState: bodyState, bodyBytes: bodyBytes, normName: normName, normValue: normValue };
})(globalThis, globalThis[Symbol.for('krate.web')]);
