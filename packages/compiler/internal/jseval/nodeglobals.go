package jseval

// nodeGlobalsPrelude fills in the Node globals a config file commonly reads:
// process.cwd/argv/platform, the CommonJS module/exports pair (so a config that
// mixes imports with `module.exports = cfg` still loads), and a small Buffer.
// It is a format string: the first verb is the argv JSON, the second the quoted
// platform. The script must not contain any other percent sign.
const nodeGlobalsPrelude = `(function (g) {
  var p = g.process || (g.process = { env: {} });
  p.cwd = function () { return g.__krateCwd; };
  p.argv = %s;
  p.platform = %s;
  if (!p.version) p.version = 'v20.0.0';
  if (!p.versions) p.versions = { node: '20.0.0' };
  if (!g.module) { g.module = { exports: {} }; g.exports = g.module.exports; }
  if (g.Buffer) return;
  var B64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
  function utf8Encode(str) {
    var out = [];
    for (var i = 0; i < str.length; i++) {
      var c = str.charCodeAt(i);
      if (c >= 0xd800 && c < 0xdc00 && i + 1 < str.length) {
        var d = str.charCodeAt(i + 1);
        if (d >= 0xdc00 && d < 0xe000) { c = 0x10000 + ((c - 0xd800) << 10) + (d - 0xdc00); i++; }
      }
      if (c < 0x80) out.push(c);
      else if (c < 0x800) out.push(0xc0 | (c >> 6), 0x80 | (c & 63));
      else if (c < 0x10000) out.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
      else out.push(0xf0 | (c >> 18), 0x80 | ((c >> 12) & 63), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
    }
    return out;
  }
  function utf8Decode(b) {
    var s = '';
    for (var i = 0; i < b.length;) {
      var c = b[i++];
      if (c >= 0xf0) {
        c = ((c & 7) << 18) | ((b[i++] & 63) << 12) | ((b[i++] & 63) << 6) | (b[i++] & 63);
        c -= 0x10000;
        s += String.fromCharCode(0xd800 + (c >> 10), 0xdc00 + (c & 1023));
      } else if (c >= 0xe0) {
        s += String.fromCharCode(((c & 15) << 12) | ((b[i++] & 63) << 6) | (b[i++] & 63));
      } else if (c >= 0xc0) {
        s += String.fromCharCode(((c & 31) << 6) | (b[i++] & 63));
      } else s += String.fromCharCode(c);
    }
    return s;
  }
  function b64Encode(b) {
    var s = '';
    for (var i = 0; i < b.length; i += 3) {
      var n = (b[i] << 16) | ((b[i + 1] || 0) << 8) | (b[i + 2] || 0);
      s += B64.charAt(n >> 18) + B64.charAt((n >> 12) & 63) + (i + 1 < b.length ? B64.charAt((n >> 6) & 63) : '=') + (i + 2 < b.length ? B64.charAt(n & 63) : '=');
    }
    return s;
  }
  function b64Decode(str) {
    str = String(str).replace(/-/g, '+').replace(/_/g, '/').replace(/[^A-Za-z0-9+\/]/g, '');
    var out = [];
    for (var i = 0; i < str.length; i += 4) {
      var a = B64.indexOf(str.charAt(i)), b = B64.indexOf(str.charAt(i + 1));
      var c = i + 2 < str.length ? B64.indexOf(str.charAt(i + 2)) : -1, d = i + 3 < str.length ? B64.indexOf(str.charAt(i + 3)) : -1;
      out.push((a << 2) | (b >> 4));
      if (c >= 0) out.push(((b & 15) << 4) | (c >> 2));
      if (d >= 0) out.push(((c & 3) << 6) | d);
    }
    return out;
  }
  function hexEncode(b) { var s = ''; for (var i = 0; i < b.length; i++) s += (b[i] < 16 ? '0' : '') + b[i].toString(16); return s; }
  function hexDecode(s) { var out = []; for (var i = 0; i + 1 < s.length; i += 2) out.push(parseInt(s.substr(i, 2), 16)); return out; }
  function bytesOf(v, enc) {
    if (typeof v === 'string') {
      enc = enc || 'utf8';
      if (enc === 'base64' || enc === 'base64url') return b64Decode(v);
      if (enc === 'hex') return hexDecode(v);
      return utf8Encode(v);
    }
    return Array.prototype.slice.call(v);
  }
  class Buffer extends Uint8Array {
    toString(enc) {
      var b = Array.prototype.slice.call(this);
      enc = enc || 'utf8';
      if (enc === 'base64') return b64Encode(b);
      if (enc === 'base64url') return b64Encode(b).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
      if (enc === 'hex') return hexEncode(b);
      return utf8Decode(b);
    }
    toJSON() { return { type: 'Buffer', data: Array.prototype.slice.call(this) }; }
    static from(v, enc) { return new Buffer(bytesOf(v, enc)); }
    static alloc(n, fill) { var b = new Buffer(n); if (fill) b.fill(fill); return b; }
    static isBuffer(v) { return v instanceof Buffer; }
    static byteLength(v, enc) { return bytesOf(v, enc).length; }
    static concat(list) {
      var out = [];
      for (var i = 0; i < list.length; i++) out = out.concat(Array.prototype.slice.call(list[i]));
      return new Buffer(out);
    }
  }
  g.Buffer = Buffer;
})(globalThis);`
