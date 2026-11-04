// Web platform APIs for the embedded QuickJS runtime. Evaluated once per VM
// (as shared bytecode) before any user code. Go-backed helpers are named
// __go_*; everything binary crosses the boundary as base64 text because host
// strings are NUL-terminated.
(function () {
  'use strict';
  var G = globalThis;

  function def(name, value) {
    Object.defineProperty(G, name, { value: value, writable: true, configurable: true, enumerable: false });
  }
  function unsupported(api) {
    var e = new Error(api + ' is not supported by the embedded API runtime; set ssr.apiRuntime to "node" in krate.config.ts to use it');
    e.name = 'NotSupportedError';
    try { console.error('[krate] ' + e.message); } catch (_) {}
    throw e;
  }
  function stubClass(name) {
    var C = function () { unsupported(name); };
    Object.defineProperty(C, 'name', { value: name });
    def(name, C);
  }

  // ---- DOMException / events / abort ------------------------------------
  var DOM_CODES = { IndexSizeError: 1, HierarchyRequestError: 3, WrongDocumentError: 4, InvalidCharacterError: 5, NoModificationAllowedError: 7, NotFoundError: 8, NotSupportedError: 9, InvalidStateError: 11, SyntaxError: 12, InvalidModificationError: 13, NamespaceError: 14, InvalidAccessError: 15, TypeMismatchError: 17, SecurityError: 18, NetworkError: 19, AbortError: 20, URLMismatchError: 21, QuotaExceededError: 22, TimeoutError: 23, InvalidNodeTypeError: 24, DataCloneError: 25 };
  class DOMException extends Error {
    constructor(message, name) {
      super(message === undefined ? '' : String(message));
      Object.defineProperty(this, 'name', { value: name === undefined ? 'Error' : String(name), configurable: true });
      this.code = DOM_CODES[this.name] || 0;
    }
  }
  def('DOMException', DOMException);

  class Event {
    constructor(type, init) {
      this.type = String(type);
      this.defaultPrevented = false;
      this.cancelable = !!(init && init.cancelable);
      this.bubbles = !!(init && init.bubbles);
      this.target = null;
      this.currentTarget = null;
      this.timeStamp = Date.now();
    }
    preventDefault() { if (this.cancelable) this.defaultPrevented = true; }
    stopPropagation() {}
    stopImmediatePropagation() { this._stop = true; }
  }
  class EventTarget {
    constructor() { Object.defineProperty(this, '_l', { value: {}, enumerable: false }); }
    addEventListener(type, fn, opts) {
      if (!fn) return;
      var list = this._l[type] || (this._l[type] = []);
      for (var i = 0; i < list.length; i++) if (list[i].fn === fn) return;
      list.push({ fn: fn, once: !!(opts && typeof opts === 'object' && opts.once) });
    }
    removeEventListener(type, fn) {
      var list = this._l[type];
      if (!list) return;
      for (var i = 0; i < list.length; i++) if (list[i].fn === fn) { list.splice(i, 1); return; }
    }
    dispatchEvent(ev) {
      ev.target = this; ev.currentTarget = this;
      var list = (this._l[ev.type] || []).slice();
      for (var i = 0; i < list.length; i++) {
        var l = list[i];
        if (l.once) this.removeEventListener(ev.type, l.fn);
        try {
          if (typeof l.fn === 'function') l.fn.call(this, ev); else if (l.fn && typeof l.fn.handleEvent === 'function') l.fn.handleEvent(ev);
        } catch (e) { try { console.error(e && e.stack ? e.stack : String(e)); } catch (_) {} }
        if (ev._stop) break;
      }
      return !ev.defaultPrevented;
    }
  }
  def('Event', Event);
  def('EventTarget', EventTarget);

  var kAbortInit = {};
  class AbortSignal extends EventTarget {
    constructor(token) {
      if (token !== kAbortInit) throw new TypeError('Illegal constructor');
      super();
      this.aborted = false;
      this.reason = undefined;
      this.onabort = null;
    }
    throwIfAborted() { if (this.aborted) throw this.reason; }
    static abort(reason) { var s = new AbortSignal(kAbortInit); abortSignal(s, reason); return s; }
    static timeout(ms) {
      var s = new AbortSignal(kAbortInit);
      setTimeout(function () { abortSignal(s, new DOMException('The operation timed out.', 'TimeoutError')); }, ms);
      return s;
    }
    static any(signals) {
      var s = new AbortSignal(kAbortInit);
      for (var i = 0; i < signals.length; i++) {
        var sg = signals[i];
        if (sg.aborted) { abortSignal(s, sg.reason); return s; }
        sg.addEventListener('abort', (function (sg2) { return function () { abortSignal(s, sg2.reason); }; })(sg));
      }
      return s;
    }
  }
  function abortSignal(s, reason) {
    if (s.aborted) return;
    s.aborted = true;
    s.reason = reason === undefined ? new DOMException('This operation was aborted', 'AbortError') : reason;
    var ev = new Event('abort');
    if (typeof s.onabort === 'function') { try { s.onabort(ev); } catch (_) {} }
    s.dispatchEvent(ev);
  }
  class AbortController {
    constructor() { this.signal = new AbortSignal(kAbortInit); }
    abort(reason) { abortSignal(this.signal, reason); }
  }
  def('AbortSignal', AbortSignal);
  def('AbortController', AbortController);

  // ---- timers -------------------------------------------------------------
  var timers = [];
  var timerSeq = 0;
  class Timeout {
    constructor(id) { this._id = id; }
    ref() { return this; }
    unref() { return this; }
    hasRef() { return true; }
    refresh() { return this; }
    close() { clearTimer(this); return this; }
    [Symbol.toPrimitive]() { return this._id; }
  }
  function addTimer(fn, ms, args, repeat) {
    if (typeof fn !== 'function') throw new TypeError('The "callback" argument must be of type function');
    ms = Number(ms);
    if (!(ms >= 1)) ms = 1;
    if (ms > 2147483647) ms = 1;
    var t = new Timeout(++timerSeq);
    timers.push({ id: t._id, at: Date.now() + ms, fn: fn, args: args, repeat: repeat ? ms : 0, order: t._id });
    return t;
  }
  function clearTimer(h) {
    var id = h && typeof h === 'object' ? h._id : Number(h);
    for (var i = 0; i < timers.length; i++) if (timers[i].id === id) { timers.splice(i, 1); return; }
  }
  def('setTimeout', function (fn, ms) { return addTimer(fn, ms, Array.prototype.slice.call(arguments, 2), false); });
  def('setInterval', function (fn, ms) { return addTimer(fn, ms, Array.prototype.slice.call(arguments, 2), true); });
  def('setImmediate', function (fn) { return addTimer(fn, 0, Array.prototype.slice.call(arguments, 1), false); });
  def('clearTimeout', clearTimer);
  def('clearInterval', clearTimer);
  def('clearImmediate', clearTimer);
  def('queueMicrotask', function (fn) {
    if (typeof fn !== 'function') throw new TypeError('The "callback" argument must be of type function');
    Promise.resolve().then(fn).catch(function (e) { try { console.error(e && e.stack ? e.stack : String(e)); } catch (_) {} });
  });
  // Driver hooks used by the Go job loop.
  def('__krate_timers_next', function () {
    if (!timers.length) return -1;
    var min = Infinity;
    for (var i = 0; i < timers.length; i++) if (timers[i].at < min) min = timers[i].at;
    return Math.max(0, min - Date.now());
  });
  def('__krate_timers_run', function () {
    var now = Date.now();
    var due = timers.filter(function (t) { return t.at <= now; });
    due.sort(function (a, b) { return a.at - b.at || a.order - b.order; });
    for (var i = 0; i < due.length; i++) {
      var t = due[i];
      var idx = timers.indexOf(t);
      if (idx < 0) continue;
      if (t.repeat) { t.at = Date.now() + t.repeat; } else { timers.splice(idx, 1); }
      try { t.fn.apply(G, t.args); } catch (e) { try { console.error('Uncaught timer error: ' + (e && e.stack ? e.stack : String(e))); } catch (_) {} }
    }
    return timers.length;
  });
  def('__krate_timers_clear', function () { timers.length = 0; });

  var perfOrigin = Date.now();
  def('performance', {
    timeOrigin: perfOrigin,
    now: function () { return Date.now() - perfOrigin; },
    toJSON: function () { return { timeOrigin: perfOrigin }; }
  });

  // ---- base64 / bytes -----------------------------------------------------
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
  def('btoa', function (s) {
    s = String(s);
    var u8 = new Uint8Array(s.length);
    for (var i = 0; i < s.length; i++) {
      var c = s.charCodeAt(i);
      if (c > 255) throw new DOMException('Invalid character', 'InvalidCharacterError');
      u8[i] = c;
    }
    return bytesToB64(u8);
  });
  def('atob', function (s) {
    s = String(s).replace(/[\t\n\f\r ]/g, '');
    if (s.length % 4 === 1 || /[^A-Za-z0-9+\/=]/.test(s) || /=[^=]/.test(s)) throw new DOMException('The string to be decoded is not correctly encoded.', 'InvalidCharacterError');
    var u8 = b64ToBytes(s), out = '';
    for (var i = 0; i < u8.length; i += 8192) out += String.fromCharCode.apply(null, u8.subarray(i, i + 8192));
    return out;
  });

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

  // ---- UTF-8 --------------------------------------------------------------
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
  // utf8Decode decodes bytes[start..end); in stream mode an incomplete trailing
  // sequence is left unconsumed and its length returned through state.rest.
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
  class TextEncoder {
    get encoding() { return 'utf-8'; }
    encode(s) { return utf8Encode(s === undefined ? '' : String(s)); }
    encodeInto(s, dest) {
      var bytes = utf8Encode(String(s)), n = Math.min(bytes.length, dest.length);
      while (n > 0 && n < bytes.length && (bytes[n] & 0xc0) === 0x80) n--;
      dest.set(bytes.subarray(0, n));
      return { read: utf8Decode(bytes.subarray(0, n)).length, written: n };
    }
  }
  var LABELS = { 'utf-8': 'utf-8', 'utf8': 'utf-8', 'unicode-1-1-utf-8': 'utf-8', 'utf-16le': 'utf-16le', 'utf-16': 'utf-16le', 'ucs-2': 'utf-16le', 'latin1': 'windows-1252', 'iso-8859-1': 'windows-1252', 'ascii': 'windows-1252', 'us-ascii': 'windows-1252', 'windows-1252': 'windows-1252' };
  class TextDecoder {
    constructor(label, options) {
      var enc = LABELS[String(label === undefined ? 'utf-8' : label).trim().toLowerCase()];
      if (!enc) throw new RangeError('The "' + label + '" encoding is not supported by the embedded API runtime; set ssr.apiRuntime to "node" to use it');
      this._enc = enc;
      this.fatal = !!(options && options.fatal);
      this.ignoreBOM = !!(options && options.ignoreBOM);
      this._pending = null;
      this._seenBOM = false;
    }
    get encoding() { return this._enc; }
    decode(input, options) {
      var stream = !!(options && options.stream);
      var u8 = input === undefined ? new Uint8Array(0) : toBytes(input);
      if (!u8) throw new TypeError('The "input" argument must be an instance of ArrayBuffer or ArrayBufferView.');
      if (this._pending) { u8 = concatBytes([this._pending, u8]); this._pending = null; }
      var out;
      if (this._enc === 'utf-8') {
        var st = { rest: 0 };
        out = utf8Decode(u8, this.fatal, stream, st);
        if (st.rest) this._pending = u8.slice(u8.length - st.rest);
        if (!this.ignoreBOM && !this._seenBOM && out.charCodeAt(0) === 0xfeff) out = out.slice(1);
      } else if (this._enc === 'utf-16le') {
        var units = [], k = 0;
        for (; k + 1 < u8.length; k += 2) units.push(u8[k] | (u8[k + 1] << 8));
        if (k < u8.length) { if (stream) this._pending = u8.slice(k); else units.push(0xfffd); }
        out = '';
        for (var m = 0; m < units.length; m += 8192) out += String.fromCharCode.apply(null, units.slice(m, m + 8192));
        if (!this.ignoreBOM && !this._seenBOM && out.charCodeAt(0) === 0xfeff) out = out.slice(1);
      } else {
        out = '';
        for (var q = 0; q < u8.length; q += 8192) out += String.fromCharCode.apply(null, u8.subarray(q, q + 8192));
      }
      if (out.length) this._seenBOM = true;
      if (!stream) { this._seenBOM = false; }
      return out;
    }
  }
  def('TextEncoder', TextEncoder);
  def('TextDecoder', TextDecoder);

  // ---- structuredClone ----------------------------------------------------
  def('structuredClone', function (value, options) {
    if (options && options.transfer && options.transfer.length) unsupported('structuredClone transfer');
    var seen = new Map();
    function clone(v) {
      if (v === null || (typeof v !== 'object' && typeof v !== 'function')) {
        if (typeof v === 'symbol') throw new DOMException('Symbol could not be cloned.', 'DataCloneError');
        return v;
      }
      if (typeof v === 'function') throw new DOMException(String(v).slice(0, 60) + ' could not be cloned.', 'DataCloneError');
      if (seen.has(v)) return seen.get(v);
      var out, i;
      if (v instanceof Date) out = new Date(v.getTime());
      else if (v instanceof RegExp) out = new RegExp(v.source, v.flags);
      else if (v instanceof ArrayBuffer) out = v.slice(0);
      else if (ArrayBuffer.isView(v)) {
        var buf = clone(v.buffer);
        out = v instanceof DataView ? new DataView(buf, v.byteOffset, v.byteLength) : new v.constructor(buf, v.byteOffset, v.length);
      }
      else if (v instanceof Map) { out = new Map(); seen.set(v, out); v.forEach(function (val, key) { out.set(clone(key), clone(val)); }); return out; }
      else if (v instanceof Set) { out = new Set(); seen.set(v, out); v.forEach(function (val) { out.add(clone(val)); }); return out; }
      else if (v instanceof Error) { out = new (G[v.name] && G[v.name].prototype instanceof Error ? G[v.name] : Error)(v.message); if (v.stack) out.stack = v.stack; if ('cause' in v) out.cause = clone(v.cause); }
      else if (Array.isArray(v)) { out = new Array(v.length); seen.set(v, out); for (i = 0; i < v.length; i++) if (i in v) out[i] = clone(v[i]); return out; }
      else if (v instanceof Blob) { out = v; }
      else if (v instanceof Boolean || v instanceof Number || v instanceof String) out = Object(v.valueOf());
      else if (v instanceof Promise || v instanceof WeakMap || v instanceof WeakSet || v instanceof WeakRef) throw new DOMException('#<' + v.constructor.name + '> could not be cloned.', 'DataCloneError');
      else {
        out = {}; seen.set(v, out);
        var keys = Object.keys(v);
        for (i = 0; i < keys.length; i++) out[keys[i]] = clone(v[keys[i]]);
        return out;
      }
      seen.set(v, out);
      return out;
    }
    return clone(value);
  });

  // ---- URLSearchParams ----------------------------------------------------
  function formDecode(s) {
    s = s.replace(/\+/g, ' ');
    try { return decodeURIComponent(s); } catch (_) {
      // Lenient: decode valid %XX byte runs as UTF-8, keep the rest.
      return s.replace(/(%[0-9a-fA-F]{2})+/g, function (m) {
        var bytes = new Uint8Array(m.length / 3);
        for (var i = 0; i < bytes.length; i++) bytes[i] = parseInt(m.substr(i * 3 + 1, 2), 16);
        return utf8Decode(bytes);
      });
    }
  }
  function formEncode(s) {
    var bytes = utf8Encode(String(s)), out = '';
    for (var i = 0; i < bytes.length; i++) {
      var b = bytes[i];
      if ((b >= 0x30 && b <= 0x39) || (b >= 0x41 && b <= 0x5a) || (b >= 0x61 && b <= 0x7a) || b === 0x2a || b === 0x2d || b === 0x2e || b === 0x5f) out += String.fromCharCode(b);
      else if (b === 0x20) out += '+';
      else out += '%' + (b < 16 ? '0' : '') + b.toString(16).toUpperCase();
    }
    return out;
  }
  function parseForm(str) {
    var out = [];
    if (!str) return out;
    var pairs = str.split('&');
    for (var i = 0; i < pairs.length; i++) {
      if (!pairs[i]) continue;
      var eq = pairs[i].indexOf('=');
      var k = eq < 0 ? pairs[i] : pairs[i].slice(0, eq);
      var v = eq < 0 ? '' : pairs[i].slice(eq + 1);
      out.push([formDecode(k), formDecode(v)]);
    }
    return out;
  }
  class URLSearchParams {
    constructor(init) {
      Object.defineProperty(this, '_list', { value: [], writable: true, enumerable: false });
      Object.defineProperty(this, '_url', { value: null, writable: true, enumerable: false });
      if (init === undefined || init === null) return;
      if (init instanceof URLSearchParams) { this._list = init._list.map(function (p) { return [p[0], p[1]]; }); return; }
      if (typeof init === 'object' || typeof init === 'function') {
        if (typeof init[Symbol.iterator] === 'function') {
          for (var pair of init) {
            var arr = Array.from(pair);
            if (arr.length !== 2) throw new TypeError('Each query pair must be an iterable [name, value] tuple');
            this._list.push([String(arr[0]), String(arr[1])]);
          }
        } else {
          var keys = Object.keys(init);
          for (var i = 0; i < keys.length; i++) this._list.push([keys[i], String(init[keys[i]])]);
        }
        return;
      }
      var s = String(init);
      this._list = parseForm(s.charAt(0) === '?' ? s.slice(1) : s);
    }
    _changed() { if (this._url) this._url._setSearchFromParams(this.toString()); }
    get size() { return this._list.length; }
    append(name, value) { this._list.push([String(name), String(value)]); this._changed(); }
    delete(name, value) {
      name = String(name);
      var v = value === undefined ? undefined : String(value);
      this._list = this._list.filter(function (p) { return !(p[0] === name && (v === undefined || p[1] === v)); });
      this._changed();
    }
    get(name) { name = String(name); for (var i = 0; i < this._list.length; i++) if (this._list[i][0] === name) return this._list[i][1]; return null; }
    getAll(name) { name = String(name); return this._list.filter(function (p) { return p[0] === name; }).map(function (p) { return p[1]; }); }
    has(name, value) {
      name = String(name);
      var v = value === undefined ? undefined : String(value);
      return this._list.some(function (p) { return p[0] === name && (v === undefined || p[1] === v); });
    }
    set(name, value) {
      name = String(name); value = String(value);
      var found = false, out = [];
      for (var i = 0; i < this._list.length; i++) {
        var p = this._list[i];
        if (p[0] === name) { if (!found) { found = true; out.push([name, value]); } } else out.push(p);
      }
      if (!found) out.push([name, value]);
      this._list = out;
      this._changed();
    }
    sort() {
      var withIdx = this._list.map(function (p, i) { return [p, i]; });
      withIdx.sort(function (a, b) { return a[0][0] < b[0][0] ? -1 : a[0][0] > b[0][0] ? 1 : a[1] - b[1]; });
      this._list = withIdx.map(function (x) { return x[0]; });
      this._changed();
    }
    forEach(cb, thisArg) { for (var i = 0; i < this._list.length; i++) cb.call(thisArg, this._list[i][1], this._list[i][0], this); }
    *keys() { for (var i = 0; i < this._list.length; i++) yield this._list[i][0]; }
    *values() { for (var i = 0; i < this._list.length; i++) yield this._list[i][1]; }
    *entries() { for (var i = 0; i < this._list.length; i++) yield [this._list[i][0], this._list[i][1]]; }
    [Symbol.iterator]() { return this.entries(); }
    toString() { return this._list.map(function (p) { return formEncode(p[0]) + '=' + formEncode(p[1]); }).join('&'); }
  }
  def('URLSearchParams', URLSearchParams);

  // ---- URL ----------------------------------------------------------------
  function parseURL(input, base) {
    var r = __go_url_parse(String(input), base === undefined || base === null ? '' : String(base));
    return r ? JSON.parse(r) : null;
  }
  class URL {
    constructor(input, base) {
      if (arguments.length === 0) throw new TypeError('The "url" argument must be specified');
      var b = base;
      if (b !== undefined && b !== null && typeof b === 'object' && 'href' in b) b = b.href;
      var c = parseURL(input, b);
      if (!c) { var e = new TypeError('Invalid URL'); e.code = 'ERR_INVALID_URL'; e.input = String(input); throw e; }
      Object.defineProperty(this, '_c', { value: c, writable: true, enumerable: false });
      var sp = new URLSearchParams(c.search);
      sp._url = this;
      Object.defineProperty(this, '_sp', { value: sp, enumerable: false });
    }
    static canParse(input, base) { return parseURL(input, base) !== null; }
    static parse(input, base) { try { return new URL(input, base); } catch (_) { return null; } }
    _reparse(href) { var c = parseURL(href); if (c) { this._c = c; this._sp._list = parseForm(c.search.slice(1)); } }
    _setSearchFromParams(s) { var c = parseURL(this._c.href.split('?')[0].split('#')[0] + (s ? '?' + s : '') + this._c.hash); if (c) this._c = c; }
    get href() { return this._c.href; }
    set href(v) { var c = parseURL(v); if (!c) throw new TypeError('Invalid URL'); this._c = c; this._sp._list = parseForm(c.search.slice(1)); }
    get origin() { return this._c.origin; }
    get protocol() { return this._c.protocol; }
    set protocol(v) { var s = String(v).replace(/:.*$/, ''); this._reparse(s + ':' + this._c.href.slice(this._c.protocol.length)); }
    get username() { return this._c.username; }
    set username(v) { this._cred(String(v), this._c.password); }
    get password() { return this._c.password; }
    set password(v) { this._cred(this._c.username, String(v)); }
    _cred(u, p) {
      var c = this._c, rest = c.href.slice(c.protocol.length + 2);
      var at = rest.indexOf('@'), slash = rest.search(/[\/?#]/);
      if (at >= 0 && (slash < 0 || at < slash)) rest = rest.slice(at + 1);
      var cred = u ? encodeURIComponent(u) + (p ? ':' + encodeURIComponent(p) : '') + '@' : '';
      this._reparse(c.protocol + '//' + cred + rest);
    }
    get host() { return this._c.host; }
    set host(v) { this._reparse(this._c.protocol + '//' + (this._c.username ? this._c.username + (this._c.password ? ':' + this._c.password : '') + '@' : '') + String(v) + this._c.pathname + this._c.search + this._c.hash); }
    get hostname() { return this._c.hostname; }
    set hostname(v) { this.host = String(v) + (this._c.port ? ':' + this._c.port : ''); }
    get port() { return this._c.port; }
    set port(v) { this.host = this._c.hostname + (String(v) ? ':' + String(v) : ''); }
    get pathname() { return this._c.pathname; }
    set pathname(v) { v = String(v); this._reparse(this._c.href.slice(0, this._c.href.length - this._c.pathname.length - this._c.search.length - this._c.hash.length) + (v.charAt(0) === '/' ? v : '/' + v) + this._c.search + this._c.hash); }
    get search() { return this._c.search; }
    set search(v) { v = String(v); if (v && v.charAt(0) !== '?') v = '?' + v; this._reparse(this._c.href.split('?')[0].split('#')[0] + (v === '?' ? '' : v) + this._c.hash); }
    get searchParams() { return this._sp; }
    get hash() { return this._c.hash; }
    set hash(v) { v = String(v); if (v && v.charAt(0) !== '#') v = '#' + v; this._reparse(this._c.href.split('#')[0] + (v === '#' ? '' : v)); }
    toString() { return this._c.href; }
    toJSON() { return this._c.href; }
  }
  def('URL', URL);

  // ---- Headers ------------------------------------------------------------
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

  // ---- ReadableStream -----------------------------------------------------
  var kStream = Symbol('stream');
  class ReadableStreamDefaultReader {
    constructor(stream) {
      if (stream._locked) throw new TypeError('ReadableStream is locked');
      stream._locked = true;
      this._s = stream;
      this.closed = new Promise(function (res, rej) { stream._closedRes = res; stream._closedRej = rej; });
      this.closed.catch(function () {});
    }
    read() { if (!this._s) return Promise.reject(new TypeError('Reader released')); return this._s._read(); }
    cancel(reason) { return this._s ? this._s.cancel(reason, true) : Promise.resolve(); }
    releaseLock() { if (this._s) { this._s._locked = false; this._s = null; } }
  }
  class ReadableStream {
    constructor(source, strategy) {
      source = source || {};
      Object.defineProperty(this, '_q', { value: [], writable: true, enumerable: false });
      this._reads = [];
      this._state = 'readable';
      this._locked = false;
      this._err = undefined;
      this._src = source;
      this._pulling = false;
      this._hwm = strategy && typeof strategy.highWaterMark === 'number' ? strategy.highWaterMark : 1;
      var self = this;
      this._ctrl = {
        enqueue: function (chunk) {
          if (self._state !== 'readable') throw new TypeError('Invalid state: Controller is already closed');
          if (self._reads.length) self._reads.shift().resolve({ value: chunk, done: false });
          else self._q.push(chunk);
        },
        close: function () {
          if (self._state !== 'readable') throw new TypeError('Invalid state: Controller is already closed');
          self._state = 'closed';
          if (!self._q.length) self._finish();
        },
        error: function (e) { self._fail(e); },
        get desiredSize() { return self._state === 'readable' ? self._hwm - self._q.length : null; }
      };
      var started;
      try { started = source.start ? source.start(this._ctrl) : undefined; } catch (e) { this._fail(e); return; }
      this._started = Promise.resolve(started).then(function () { self._pull(); }, function (e) { self._fail(e); });
    }
    _finish() {
      this._state = 'closed';
      var r = this._reads; this._reads = [];
      for (var i = 0; i < r.length; i++) r[i].resolve({ value: undefined, done: true });
      if (this._closedRes) this._closedRes();
    }
    _fail(e) {
      if (this._state === 'errored') return;
      this._state = 'errored'; this._err = e; this._q.length = 0;
      var r = this._reads; this._reads = [];
      for (var i = 0; i < r.length; i++) r[i].reject(e);
      if (this._closedRej) this._closedRej(e);
    }
    _pull() {
      var self = this;
      if (this._state !== 'readable' || this._pulling || !this._src.pull) return;
      if (this._q.length >= this._hwm && !this._reads.length) return;
      this._pulling = true;
      var p;
      try { p = Promise.resolve(this._src.pull(this._ctrl)); } catch (e) { this._pulling = false; this._fail(e); return; }
      p.then(function () {
        self._pulling = false;
        if (self._state === 'readable' && (self._reads.length || self._q.length < self._hwm)) self._pull();
      }, function (e) { self._pulling = false; self._fail(e); });
    }
    _read() {
      var self = this;
      if (this._q.length) {
        var v = this._q.shift();
        if (this._state === 'closed' && !this._q.length) this._finish();
        else this._pull();
        return Promise.resolve({ value: v, done: false });
      }
      if (this._state === 'closed') return Promise.resolve({ value: undefined, done: true });
      if (this._state === 'errored') return Promise.reject(this._err);
      return new Promise(function (resolve, reject) { self._reads.push({ resolve: resolve, reject: reject }); self._pull(); });
    }
    get locked() { return this._locked; }
    getReader(opts) {
      if (opts && opts.mode) unsupported('ReadableStream BYOB reader');
      return new ReadableStreamDefaultReader(this);
    }
    cancel(reason, viaReader) {
      if (this._locked && !viaReader) return Promise.reject(new TypeError('ReadableStream is locked'));
      if (this._state === 'closed') return Promise.resolve();
      if (this._state === 'errored') return Promise.reject(this._err);
      this._q.length = 0;
      this._finish();
      try { return Promise.resolve(this._src.cancel ? this._src.cancel(reason) : undefined).then(function () {}); } catch (e) { return Promise.reject(e); }
    }
    tee() { unsupported('ReadableStream.tee'); }
    pipeTo() { unsupported('ReadableStream.pipeTo'); }
    pipeThrough() { unsupported('ReadableStream.pipeThrough'); }
    [Symbol.asyncIterator]() {
      var reader = this.getReader(), done = false;
      return {
        next: function () {
          return reader.read().then(function (r) { if (r.done) { done = true; reader.releaseLock(); } return r; });
        },
        return: function () { if (!done) { reader.cancel(); reader.releaseLock(); } return Promise.resolve({ value: undefined, done: true }); },
        [Symbol.asyncIterator]() { return this; }
      };
    }
    static from(iterable) {
      var it = iterable[Symbol.asyncIterator] ? iterable[Symbol.asyncIterator]() : iterable[Symbol.iterator]();
      return new ReadableStream({
        pull: function (c) { return Promise.resolve(it.next()).then(function (r) { if (r.done) c.close(); else c.enqueue(r.value); }); },
        cancel: function (r) { if (it.return) return it.return(r); }
      });
    }
  }
  def('ReadableStream', ReadableStream);
  def('ReadableStreamDefaultReader', ReadableStreamDefaultReader);
  stubClass('WritableStream');
  stubClass('TransformStream');
  stubClass('CompressionStream');
  stubClass('DecompressionStream');
  stubClass('WebSocket');
  stubClass('EventSource');

  function streamFromBytes(u8) {
    var sent = false;
    return new ReadableStream({ pull: function (c) { if (!sent) { sent = true; if (u8.length) c.enqueue(u8); } c.close(); } });
  }
  async function readAllBytes(stream) {
    var reader = stream.getReader(), chunks = [];
    for (;;) {
      var r = await reader.read();
      if (r.done) break;
      var b = typeof r.value === 'string' ? utf8Encode(r.value) : toBytes(r.value);
      if (!b) throw new TypeError('Received non-binary chunk from body stream');
      chunks.push(b);
    }
    reader.releaseLock();
    return concatBytes(chunks);
  }

  // ---- Blob / File / FormData --------------------------------------------
  class Blob {
    constructor(parts, options) {
      var list = [];
      if (parts !== undefined && parts !== null) {
        for (var p of parts) {
          if (p instanceof Blob) list.push(p._b);
          else { var b = toBytes(p); list.push(b ? b : utf8Encode(String(p))); }
        }
      }
      Object.defineProperty(this, '_b', { value: concatBytes(list), writable: true, enumerable: false });
      this.type = options && options.type ? String(options.type).toLowerCase() : '';
    }
    get size() { return this._b.length; }
    slice(start, end, type) {
      var b = new Blob([], { type: type || '' });
      b._b = this._b.slice(start, end);
      return b;
    }
    arrayBuffer() { var c = this._b.slice(); return Promise.resolve(c.buffer); }
    bytes() { return Promise.resolve(this._b.slice()); }
    text() { return Promise.resolve(utf8Decode(this._b)); }
    stream() { return streamFromBytes(this._b); }
  }
  class File extends Blob {
    constructor(parts, name, options) {
      super(parts, options);
      this.name = String(name);
      this.lastModified = options && options.lastModified !== undefined ? Number(options.lastModified) : Date.now();
    }
  }
  def('Blob', Blob);
  def('File', File);

  function toFormValue(name, value, filename) {
    if (value instanceof Blob) {
      if (!(value instanceof File) || filename !== undefined) value = new File([value], filename !== undefined ? String(filename) : 'blob', { type: value.type });
      return value;
    }
    return String(value);
  }
  class FormData {
    constructor(form) {
      if (form !== undefined) unsupported('FormData from an HTML form');
      Object.defineProperty(this, '_list', { value: [], writable: true, enumerable: false });
    }
    append(name, value, filename) { this._list.push([String(name), toFormValue(name, value, filename)]); }
    delete(name) { name = String(name); this._list = this._list.filter(function (p) { return p[0] !== name; }); }
    get(name) { name = String(name); for (var i = 0; i < this._list.length; i++) if (this._list[i][0] === name) return this._list[i][1]; return null; }
    getAll(name) { name = String(name); return this._list.filter(function (p) { return p[0] === name; }).map(function (p) { return p[1]; }); }
    has(name) { name = String(name); return this._list.some(function (p) { return p[0] === name; }); }
    set(name, value, filename) {
      name = String(name);
      var v = toFormValue(name, value, filename), found = false, out = [];
      for (var i = 0; i < this._list.length; i++) {
        var p = this._list[i];
        if (p[0] === name) { if (!found) { found = true; out.push([name, v]); } } else out.push(p);
      }
      if (!found) out.push([name, v]);
      this._list = out;
    }
    forEach(cb, thisArg) { for (var i = 0; i < this._list.length; i++) cb.call(thisArg, this._list[i][1], this._list[i][0], this); }
    *keys() { for (var i = 0; i < this._list.length; i++) yield this._list[i][0]; }
    *values() { for (var i = 0; i < this._list.length; i++) yield this._list[i][1]; }
    *entries() { for (var i = 0; i < this._list.length; i++) yield [this._list[i][0], this._list[i][1]]; }
    [Symbol.iterator]() { return this.entries(); }
  }
  def('FormData', FormData);

  function serializeMultipart(fd) {
    var boundary = '----KrateFormBoundary' + randomHex(12), parts = [];
    function esc(s) { return String(s).replace(/\r?\n|\r/g, '%0D%0A').replace(/"/g, '%22'); }
    for (var i = 0; i < fd._list.length; i++) {
      var name = fd._list[i][0], v = fd._list[i][1];
      if (typeof v === 'string') {
        parts.push(utf8Encode('--' + boundary + '\r\nContent-Disposition: form-data; name="' + esc(name) + '"\r\n\r\n' + v.replace(/\r?\n|\r/g, '\r\n') + '\r\n'));
      } else {
        parts.push(utf8Encode('--' + boundary + '\r\nContent-Disposition: form-data; name="' + esc(name) + '"; filename="' + esc(v.name) + '"\r\nContent-Type: ' + (v.type || 'application/octet-stream') + '\r\n\r\n'));
        parts.push(v._b);
        parts.push(utf8Encode('\r\n'));
      }
    }
    parts.push(utf8Encode('--' + boundary + '--\r\n'));
    return { bytes: concatBytes(parts), type: 'multipart/form-data; boundary=' + boundary };
  }
  function multipartFromParsed(parsed) {
    var res = JSON.parse(parsed), fd = new FormData();
    if (res.error) throw new TypeError('Could not parse content as FormData: ' + res.error);
    for (var j = 0; j < res.parts.length; j++) {
      var p = res.parts[j];
      if (p.filename !== undefined) fd._list.push([p.name, new File([b64ToBytes(p.data)], p.filename, { type: p.type || '' })]);
      else fd._list.push([p.name, utf8Decode(b64ToBytes(p.data))]);
    }
    return fd;
  }
  function parseFormBody(contentType, bytes) {
    var ct = String(contentType || '').toLowerCase();
    var fd = new FormData();
    if (ct.indexOf('application/x-www-form-urlencoded') === 0) {
      var list = parseForm(utf8Decode(bytes));
      for (var i = 0; i < list.length; i++) fd.append(list[i][0], list[i][1]);
      return fd;
    }
    if (ct.indexOf('multipart/form-data') === 0) {
      var parsed = __go_parse_multipart(String(contentType), bytesToB64(bytes));
      var res = JSON.parse(parsed);
      if (res.error) throw new TypeError('Could not parse content as FormData: ' + res.error);
      for (var j = 0; j < res.parts.length; j++) {
        var p = res.parts[j];
        if (p.filename !== undefined) fd._list.push([p.name, new File([b64ToBytes(p.data)], p.filename, { type: p.type || '' })]);
        else fd._list.push([p.name, utf8Decode(b64ToBytes(p.data))]);
      }
      return fd;
    }
    throw new TypeError('Could not parse content as FormData: unsupported content-type');
  }

  // ---- Body mixin / Request / Response ------------------------------------
  function randomHex(n) {
    var b = new Uint8Array(n);
    fillRandom(b);
    var s = '';
    for (var i = 0; i < n; i++) s += (b[i] < 16 ? '0' : '') + b[i].toString(16);
    return s;
  }
  function fillRandom(u8) {
    if (u8.length > 65536) throw new DOMException('The ArrayBufferView\'s byte length (' + u8.length + ') exceeds the number of bytes of entropy available via this API (65536)', 'QuotaExceededError');
    u8.set(b64ToBytes(__go_random(u8.length)));
  }

  // extractBody turns a BodyInit into { bytes | stream, type }.
  function extractBody(init) {
    if (init === undefined || init === null) return null;
    if (typeof init === 'string') return { str: init, type: 'text/plain;charset=UTF-8' };
    var b = toBytes(init);
    if (b) return { bytes: b.slice(), type: null };
    if (init instanceof Blob) return { bytes: init._b, type: init.type || null };
    if (init instanceof URLSearchParams) return { str: init.toString(), type: 'application/x-www-form-urlencoded;charset=UTF-8' };
    if (init instanceof FormData) { var m = serializeMultipart(init); return { bytes: m.bytes, type: m.type }; }
    if (init instanceof ReadableStream) return { stream: init, type: null };
    if (typeof init === 'object' && typeof init[Symbol.asyncIterator] === 'function') return { stream: ReadableStream.from(init), type: null };
    return { str: String(init), type: 'text/plain;charset=UTF-8' };
  }
  var kBody = Symbol('body');
  function bodyState(o) { return o[kBody]; }
  function consumeBody(o) {
    var st = bodyState(o);
    if (st.used) return Promise.reject(new TypeError('Body is unusable: Body has already been read'));
    st.used = true;
    if (st.stream) return readAllBytes(st.stream);
    return Promise.resolve(bodyBytes(st) || new Uint8Array(0));
  }
  // consumeText reads the body as a string without a bytes round trip when the
  // body already is (or can be produced as) text.
  function consumeText(o) {
    var st = bodyState(o);
    if (st.used) return Promise.reject(new TypeError('Body is unusable: Body has already been read'));
    st.used = true;
    if (st.stream) return readAllBytes(st.stream).then(function (b) { return utf8Decode(b); });
    if (st.str !== undefined) return Promise.resolve(st.str);
    if (st.lazyText) { var t = st.lazyText(); st.lazyText = null; st.lazy = null; return Promise.resolve(t); }
    return Promise.resolve(utf8Decode(bodyBytes(st) || new Uint8Array(0)));
  }
  function bodyBytes(st) {
    if (st.lazy) { st.bytes = b64ToBytes(st.lazy()); st.lazy = null; st.lazyText = null; }
    else if (st.bytes == null && st.str !== undefined) st.bytes = utf8Encode(st.str);
    return st.bytes;
  }
  var bodyMethods = {
    arrayBuffer: function () { return consumeBody(this).then(function (b) { return b.byteOffset === 0 && b.byteLength === b.buffer.byteLength ? b.buffer : b.slice().buffer; }); },
    bytes: function () { return consumeBody(this); },
    blob: function () { var self = this; return consumeBody(this).then(function (b) { return new Blob([b], { type: self.headers.get('content-type') || '' }); }); },
    text: function () { return consumeText(this); },
    json: function () { return consumeText(this).then(function (t) { return JSON.parse(t); }); },
    formData: function () {
      var self = this, ct = self.headers.get('content-type'), st = bodyState(self);
      if (st.lazyMultipart && String(ct || '').toLowerCase().indexOf('multipart/form-data') === 0 && !st.used) {
        st.used = true;
        return Promise.resolve().then(function () { return multipartFromParsed(st.lazyMultipart(String(ct))); });
      }
      if (String(ct || '').toLowerCase().indexOf('application/x-www-form-urlencoded') === 0) {
        return consumeText(self).then(function (t) { var fd = new FormData(), list = parseForm(t); for (var i = 0; i < list.length; i++) fd.append(list[i][0], list[i][1]); return fd; });
      }
      return consumeBody(self).then(function (b) { return parseFormBody(ct, b); });
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
        if (!st.stream) st.stream = streamFromBytes(bodyBytes(st) || new Uint8Array(0));
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
      Object.defineProperty(this, '_st', { value: status, enumerable: false });
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
      Object.defineProperty(r, '_st', { value: 0 });
      r._t = 'error';
      return r;
    }
  }
  installBody(Response);
  def('Response', Response);

  // ---- crypto -------------------------------------------------------------
  var HASHES = { 'SHA-1': 'sha1', 'SHA-256': 'sha256', 'SHA-384': 'sha384', 'SHA-512': 'sha512' };
  function hashName(alg) {
    var n = typeof alg === 'string' ? alg : alg && alg.name;
    n = String(n || '').toUpperCase();
    if (!HASHES[n]) throw new DOMException('Unrecognized algorithm name: ' + n + ' (the embedded API runtime supports SHA-1, SHA-256, SHA-384, SHA-512; set ssr.apiRuntime to "node" for more)', 'NotSupportedError');
    return HASHES[n];
  }
  function needBytes(data) {
    var b = toBytes(data);
    if (!b) throw new TypeError('The data argument must be an ArrayBuffer or ArrayBufferView');
    return b;
  }
  class CryptoKey {}
  var subtle = {
    digest: function (alg, data) {
      return new Promise(function (resolve) {
        var h = hashName(alg);
        resolve(b64ToBytes(__go_digest(h, bytesToB64(needBytes(data)))).buffer);
      });
    },
    importKey: function (format, keyData, algorithm, extractable, usages) {
      return new Promise(function (resolve) {
        if (format !== 'raw') throw new DOMException('importKey format "' + format + '" is not supported by the embedded API runtime; set ssr.apiRuntime to "node"', 'NotSupportedError');
        var name = String(typeof algorithm === 'string' ? algorithm : algorithm && algorithm.name).toUpperCase();
        if (name !== 'HMAC') throw new DOMException('Algorithm ' + name + ' is not supported by the embedded API runtime (HMAC only); set ssr.apiRuntime to "node"', 'NotSupportedError');
        var key = new CryptoKey();
        Object.defineProperty(key, '_k', { value: needBytes(keyData).slice() });
        Object.defineProperty(key, '_h', { value: hashName(algorithm.hash) });
        key.type = 'secret'; key.extractable = !!extractable; key.usages = usages || [];
        key.algorithm = { name: 'HMAC', hash: { name: algorithm.hash && algorithm.hash.name ? algorithm.hash.name : String(algorithm.hash) }, length: key._k.length * 8 };
        resolve(key);
      });
    },
    sign: function (alg, key, data) {
      return new Promise(function (resolve) {
        if (!(key instanceof CryptoKey) || !key._k) throw new DOMException('Unsupported key', 'InvalidAccessError');
        resolve(b64ToBytes(__go_hmac(key._h, bytesToB64(key._k), bytesToB64(needBytes(data)))).buffer);
      });
    },
    verify: function (alg, key, signature, data) {
      return subtle.sign(alg, key, data).then(function (mac) {
        var a = new Uint8Array(mac), b = needBytes(signature);
        if (a.length !== b.length) return false;
        var diff = 0;
        for (var i = 0; i < a.length; i++) diff |= a[i] ^ b[i];
        return diff === 0;
      });
    }
  };
  ['encrypt', 'decrypt', 'generateKey', 'deriveKey', 'deriveBits', 'exportKey', 'wrapKey', 'unwrapKey'].forEach(function (m) {
    subtle[m] = function () { return Promise.reject(new DOMException('crypto.subtle.' + m + ' is not supported by the embedded API runtime; set ssr.apiRuntime to "node" to use it', 'NotSupportedError')); };
  });
  def('CryptoKey', CryptoKey);
  def('crypto', {
    subtle: subtle,
    getRandomValues: function (arr) {
      var b = toBytes(arr);
      if (!b || arr instanceof Float32Array || arr instanceof Float64Array) throw new TypeError('The data argument must be an integer-type TypedArray');
      fillRandom(b);
      return arr;
    },
    randomUUID: function () {
      var b = new Uint8Array(16);
      fillRandom(b);
      b[6] = (b[6] & 0x0f) | 0x40; b[8] = (b[8] & 0x3f) | 0x80;
      var h = '';
      for (var i = 0; i < 16; i++) { h += (b[i] < 16 ? '0' : '') + b[i].toString(16); if (i === 3 || i === 5 || i === 7 || i === 9) h += '-'; }
      return h;
    }
  });

  // ---- fetch --------------------------------------------------------------
  def('fetch', async function fetch(input, init) {
    var req = new Request(input, init);
    if (req.signal.aborted) throw req.signal.reason;
    var bytes = null;
    if (req.method !== 'GET' && req.method !== 'HEAD' && !bodyState(req).empty) bytes = await consumeBody(req);
    var hdrs = [];
    req.headers.forEach(function (v, k) { hdrs.push([k, v]); });
    var opts = init || {};
    var payload = JSON.stringify({
      url: req.url, method: req.method, headers: hdrs, body: bytes ? bytesToB64(bytes) : '',
      cache: opts.cache || '', next: opts.next || null, redirect: req.redirect
    });
    var raw;
    try { raw = __go_fetch(payload); } catch (e) { var te = new TypeError('fetch failed'); te.cause = e; throw te; }
    var res = JSON.parse(raw);
    var body = NULL_BODY.indexOf(res.status) >= 0 || req.method === 'HEAD' ? null : b64ToBytes(res.body);
    var out = new Response(body, { status: res.status, statusText: res.statusText, headers: new Headers(res.headers) });
    Object.defineProperty(out, 'url', { value: res.url || req.url });
    Object.defineProperty(out, 'redirected', { value: !!res.redirected });
    return out;
  });

  // ---- console ------------------------------------------------------------
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
  var con = G.console;
  if (!con) { con = {}; def('console', con); }
  con.log = function () { console_log(line(arguments)); };
  con.info = con.log;
  con.debug = con.log;
  con.error = function () { console_error(line(arguments)); };
  con.warn = function () { console_warn(line(arguments)); };
  con.trace = con.error;

  // Internal surface for the API/middleware runner (apiruntime.js).
  Object.defineProperty(G, '__krate_web', {
    value: {
      kBody: kBody, bodyState: bodyState, bodyBytes: bodyBytes, multipartFromParsed: multipartFromParsed, bytesToB64: bytesToB64, b64ToBytes: b64ToBytes,
      toBytes: toBytes, utf8Encode: utf8Encode, utf8Decode: utf8Decode, concatBytes: concatBytes, parseForm: parseForm,
      abortSignal: abortSignal, kAbortInit: kAbortInit, ReadableStream: ReadableStream, normName: normName, normValue: normValue
    }, enumerable: false
  });
})();
