// Encoding feature: atob, btoa, TextEncoder and TextDecoder. The byte and
// UTF-8 helpers themselves live in core.js, since most features use them.
(function (G, K) {
  'use strict';
  var def = K.def, DOMException = K.DOMException, KR_UNSUPPORTED = K.KR_UNSUPPORTED;
  var bytesToB64 = K.bytesToB64, b64ToBytes = K.b64ToBytes, toBytes = K.toBytes, concatBytes = K.concatBytes;
  var utf8Encode = K.utf8Encode, utf8Decode = K.utf8Decode;

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
      if (!enc) throw new RangeError(KR_UNSUPPORTED + 'The "' + label + '" encoding is not supported by the embedded API runtime; set ssr.apiRuntime to "node" to use it');
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

  K.mods.encoding = { TextEncoder: TextEncoder, TextDecoder: TextDecoder };
})(globalThis, globalThis[Symbol.for('krate.web')]);
