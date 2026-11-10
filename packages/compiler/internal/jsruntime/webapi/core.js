// Shared internals of the web API modules. Installed first in every VM. It
// defines no Web API globals itself: it publishes the helpers the feature
// modules share on a non-enumerable, symbol-keyed object (K), and installs the
// accessors that load a feature the first time one of its globals is read.
// Go-backed helpers are named __go_*. Bytes cross the boundary as
// ArrayBuffers copied by the host, or as base64 text when the host cannot
// (toHost, fromHost).
(function () {
  'use strict';
  var G = globalThis;
  var K = { mods: {} };
  Object.defineProperty(G, Symbol.for('krate.web'), { value: K });
  var BIN = G.__krate_bin === true;
  delete G.__krate_bin;

  // The loader is a Go host function; the bare build-time VM has none.
  var loadFeature = typeof G.__krate_load_feature === 'function' ? G.__krate_load_feature : null;
  if (loadFeature) delete G.__krate_load_feature;

  // need returns a feature's internal exports, loading it on first use.
  function need(name) {
    var m = K.mods[name];
    if (m) return m;
    if (!loadFeature) throw new Error('[krate] the ' + name + ' web APIs are not available here');
    var msg = loadFeature(name);
    if (msg) throw new Error('[krate] loading the ' + name + ' web APIs failed: ' + msg);
    return K.mods[name];
  }
  // loaded returns a feature's exports only when it is already installed, for
  // instanceof checks: nothing can be an instance of a class not yet defined.
  function loaded(name) { return K.mods[name]; }

  var lazyGetters = {};
  function isLazy(name) {
    var d = Object.getOwnPropertyDescriptor(G, name);
    return !!d && d.get !== undefined && d.get === lazyGetters[name];
  }
  // def installs a global with the attributes of a built-in. A lazily loaded
  // module only replaces its own accessor, so a value user code assigned (or
  // a global it deleted) before the feature loaded is left alone.
  function def(name, value) {
    if (Object.prototype.hasOwnProperty.call(lazyGetters, name) && !isLazy(name)) return;
    Object.defineProperty(G, name, { value: value, writable: true, configurable: true, enumerable: false });
  }
  // lazy installs a loading accessor for each global of each feature in spec
  // ({ feature: [names] }).
  function lazy(spec) {
    var features = Object.keys(spec);
    for (var f = 0; f < features.length; f++) {
      var names = spec[features[f]];
      for (var i = 0; i < names.length; i++) lazyGlobal(features[f], names[i]);
    }
  }
  function lazyGlobal(feature, name) {
    var get = function () {
      need(feature);
      if (isLazy(name)) { delete G[name]; return undefined; }
      return G[name];
    };
    lazyGetters[name] = get;
    Object.defineProperty(G, name, {
      configurable: true, enumerable: false, get: get,
      set: function (v) { Object.defineProperty(G, name, { value: v, writable: true, configurable: true, enumerable: false }); }
    });
  }

  // KR7160 is the diagnostic builds report for the same APIs, so build-time
  // and run-time messages agree.
  var KR_UNSUPPORTED = '[KR7160] ';
  // Stubs carry this marker so tools can tell them from real implementations.
  var kUnsupported = Symbol.for('krate.unsupported');
  function markUnsupported(fn) {
    Object.defineProperty(fn, kUnsupported, { value: true });
    return fn;
  }
  function unsupported(api) {
    var e = new Error(KR_UNSUPPORTED + api + ' is not supported by the embedded API runtime; set ssr.apiRuntime to "node" in krate.config.ts to use it');
    e.name = 'NotSupportedError';
    try { console.error('[krate] ' + e.message); } catch (_) {}
    throw e;
  }
  function stubClass(name) {
    var C = function () { unsupported(name); };
    Object.defineProperty(C, 'name', { value: name });
    def(name, markUnsupported(C));
  }

  // DOMException is shared: encoding, crypto, clone and events all throw it.
  // The events feature publishes it as a global.
  var DOM_CODES = { IndexSizeError: 1, HierarchyRequestError: 3, WrongDocumentError: 4, InvalidCharacterError: 5, NoModificationAllowedError: 7, NotFoundError: 8, NotSupportedError: 9, InvalidStateError: 11, SyntaxError: 12, InvalidModificationError: 13, NamespaceError: 14, InvalidAccessError: 15, TypeMismatchError: 17, SecurityError: 18, NetworkError: 19, AbortError: 20, URLMismatchError: 21, QuotaExceededError: 22, TimeoutError: 23, InvalidNodeTypeError: 24, DataCloneError: 25 };
  class DOMException extends Error {
    constructor(message, name) {
      super(message === undefined ? '' : String(message));
      Object.defineProperty(this, 'name', { value: name === undefined ? 'Error' : String(name), configurable: true });
      this.code = DOM_CODES[this.name] || 0;
    }
  }

  // Base64 and bytes.
  var B64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
  var B64REV = new Int16Array(128).fill(-1);
  for (var bi = 0; bi < 64; bi++) B64REV[B64.charCodeAt(bi)] = bi;
  function bytesToB64(u8) {
    var out = [];
    var n = u8.length, i = 0, chunk = '';
    for (; i + 2 < n; i += 3) {
      var v = (u8[i] << 16) | (u8[i + 1] << 8) | u8[i + 2];
      chunk += B64[v >> 18] + B64[(v >> 12) & 63] + B64[(v >> 6) & 63] + B64[v & 63];
      if (chunk.length >= 8192) { out.push(chunk); chunk = ''; }
    }
    if (n - i === 1) {
      var a = u8[i] << 16;
      chunk += B64[a >> 18] + B64[(a >> 12) & 63] + '==';
    } else if (n - i === 2) {
      var b = (u8[i] << 16) | (u8[i + 1] << 8);
      chunk += B64[b >> 18] + B64[(b >> 12) & 63] + B64[(b >> 6) & 63] + '=';
    }
    out.push(chunk);
    return out.join('');
  }
  function b64ToBytes(s) {
    var n = s.length;
    while (n > 0 && s.charCodeAt(n - 1) === 61) n--;
    var out = new Uint8Array((n * 3) >> 2);
    var o = 0, acc = 0, bits = 0;
    for (var i = 0; i < n; i++) {
      var c = s.charCodeAt(i);
      var v = c < 128 ? B64REV[c] : -1;
      if (v < 0) continue;
      acc = (acc << 6) | v; bits += 6;
      if (bits >= 8) { bits -= 8; out[o++] = (acc >> bits) & 255; acc &= (1 << bits) - 1; }
    }
    return o === out.length ? out : out.subarray(0, o);
  }
  // toHost prepares bytes for a binary host argument; fromHost turns a binary
  // host result (an ArrayBuffer, or base64 text) into a Uint8Array.
  function toHost(u8) { return BIN ? u8 : bytesToB64(u8); }
  function fromHost(v) { return typeof v === 'string' ? b64ToBytes(v) : new Uint8Array(v); }
  // host calls a binary host function. Those report failure by returning
  // undefined, never by throwing from Go.
  function host(fn) {
    var r = fn.apply(undefined, Array.prototype.slice.call(arguments, 1));
    if (r === undefined) throw new Error(G.__go_host_error());
    return r;
  }
  function toBytes(src) {
    if (src instanceof Uint8Array) return src;
    if (src instanceof ArrayBuffer || (typeof SharedArrayBuffer !== 'undefined' && src instanceof SharedArrayBuffer)) return new Uint8Array(src);
    if (ArrayBuffer.isView(src)) return new Uint8Array(src.buffer, src.byteOffset, src.byteLength);
    return null;
  }
  function concatBytes(list) {
    var total = 0, i;
    for (i = 0; i < list.length; i++) total += list[i].length;
    var out = new Uint8Array(total), o = 0;
    for (i = 0; i < list.length; i++) { out.set(list[i], o); o += list[i].length; }
    return out;
  }

  // UTF-8.
  function utf8Encode(str) {
    var n = str.length, len = 0, i, c;
    for (i = 0; i < n; i++) {
      c = str.charCodeAt(i);
      if (c < 0x80) len += 1;
      else if (c < 0x800) len += 2;
      else if (c >= 0xd800 && c <= 0xdbff && i + 1 < n && (str.charCodeAt(i + 1) & 0xfc00) === 0xdc00) { len += 4; i++; }
      else len += 3;
    }
    var out = new Uint8Array(len), o = 0;
    for (i = 0; i < n; i++) {
      c = str.charCodeAt(i);
      if (c >= 0xd800 && c <= 0xdbff && i + 1 < n && (str.charCodeAt(i + 1) & 0xfc00) === 0xdc00) {
        c = 0x10000 + ((c & 0x3ff) << 10) + (str.charCodeAt(++i) & 0x3ff);
      } else if (c >= 0xd800 && c <= 0xdfff) {
        c = 0xfffd;
      }
      if (c < 0x80) out[o++] = c;
      else if (c < 0x800) { out[o++] = 0xc0 | (c >> 6); out[o++] = 0x80 | (c & 63); }
      else if (c < 0x10000) { out[o++] = 0xe0 | (c >> 12); out[o++] = 0x80 | ((c >> 6) & 63); out[o++] = 0x80 | (c & 63); }
      else { out[o++] = 0xf0 | (c >> 18); out[o++] = 0x80 | ((c >> 12) & 63); out[o++] = 0x80 | ((c >> 6) & 63); out[o++] = 0x80 | (c & 63); }
    }
    return out;
  }
  // utf8Decode decodes bytes; in stream mode an incomplete trailing sequence
  // is left unconsumed and its length returned through state.rest.
  function utf8Decode(u8, fatal, stream, state) {
    var parts = [], units = [], i = 0, n = u8.length;
    function bad() {
      if (fatal) throw new TypeError('The encoded data was not valid for encoding utf-8');
      units.push(0xfffd);
    }
    while (i < n) {
      var b = u8[i], need = 0, cp = 0, lo = 0x80, hi = 0xbf;
      if (b < 0x80) { units.push(b); i++; }
      else {
        if (b >= 0xc2 && b <= 0xdf) { need = 1; cp = b & 0x1f; }
        else if (b >= 0xe0 && b <= 0xef) { need = 2; cp = b & 0xf; if (b === 0xe0) lo = 0xa0; if (b === 0xed) hi = 0x9f; }
        else if (b >= 0xf0 && b <= 0xf4) { need = 3; cp = b & 7; if (b === 0xf0) lo = 0x90; if (b === 0xf4) hi = 0x8f; }
        else { bad(); i++; continue; }
        var j = i + 1, ok = true, got = 0;
        while (got < need) {
          if (j >= n) { ok = null; break; }
          var cb = u8[j];
          if (cb < lo || cb > hi) { ok = false; break; }
          lo = 0x80; hi = 0xbf;
          cp = (cp << 6) | (cb & 0x3f); j++; got++;
        }
        if (ok === null) {
          if (stream) { if (state) state.rest = n - i; i = n; break; }
          bad(); i = n; break;
        }
        if (!ok) { bad(); i = j; continue; }
        if (cp >= 0x10000) { cp -= 0x10000; units.push(0xd800 + (cp >> 10), 0xdc00 + (cp & 0x3ff)); }
        else units.push(cp);
        i = j;
      }
      if (units.length >= 8192) { parts.push(String.fromCharCode.apply(null, units)); units = []; }
    }
    if (units.length) parts.push(String.fromCharCode.apply(null, units));
    return parts.join('');
  }

  // fillRandom reads entropy from the Go host; the crypto and formdata
  // features register __go_random before they load.
  function fillRandom(u8) {
    if (u8.length > 65536) throw new DOMException('The ArrayBufferView\'s byte length (' + u8.length + ') exceeds the number of bytes of entropy available via this API (65536)', 'QuotaExceededError');
    u8.set(fromHost(host(G.__go_random, u8.length)));
  }
  function randomHex(n) {
    var b = new Uint8Array(n);
    fillRandom(b);
    var s = '';
    for (var i = 0; i < n; i++) s += (b[i] < 16 ? '0' : '') + b[i].toString(16);
    return s;
  }

  // Timer driver hooks for the Go job loop. They work before the timers
  // feature loads (nothing is scheduled yet), so driving the loop never loads it.
  function timerHook(name, idle) {
    Object.defineProperty(G, name, {
      value: function () { var t = K.mods.timers; return t ? t[name]() : idle; },
      writable: true, configurable: true, enumerable: false
    });
  }
  timerHook('__krate_timers_next', -1);
  timerHook('__krate_timers_run', 0);
  timerHook('__krate_timers_clear', undefined);

  K.need = need; K.loaded = loaded; K.def = def; K.lazy = lazy;
  K.KR_UNSUPPORTED = KR_UNSUPPORTED; K.markUnsupported = markUnsupported; K.unsupported = unsupported; K.stubClass = stubClass;
  K.DOMException = DOMException;
  K.bytesToB64 = bytesToB64; K.b64ToBytes = b64ToBytes; K.toBytes = toBytes; K.concatBytes = concatBytes;
  K.bin = BIN; K.toHost = toHost; K.fromHost = fromHost; K.host = host;
  K.utf8Encode = utf8Encode; K.utf8Decode = utf8Decode;
  K.fillRandom = fillRandom; K.randomHex = randomHex;
})();
