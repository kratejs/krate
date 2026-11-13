// URL feature: URL (parsed by the Go host through __go_url_parse) and
// URLSearchParams.
(function (G, K) {
  'use strict';
  var def = K.def, utf8Encode = K.utf8Encode, utf8Decode = K.utf8Decode;
  var goURLParse = G.__go_url_parse;

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

  function parseURL(input, base) {
    var r = goURLParse(String(input), base === undefined || base === null ? '' : String(base));
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

  K.mods.url = { URL: URL, URLSearchParams: URLSearchParams, parseForm: parseForm };
})(globalThis, globalThis[Symbol.for('krate.web')]);
