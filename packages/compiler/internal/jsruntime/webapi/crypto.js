// Crypto feature: crypto.getRandomValues, randomUUID and crypto.subtle, with
// entropy, digests and HMAC in the Go host (__go_random, __go_digest,
// __go_hmac).
(function (G, K) {
  'use strict';
  var def = K.def, markUnsupported = K.markUnsupported, DOMException = K.DOMException, KR_UNSUPPORTED = K.KR_UNSUPPORTED;
  var toBytes = K.toBytes, bytesToB64 = K.bytesToB64, b64ToBytes = K.b64ToBytes, fillRandom = K.fillRandom;
  var goDigest = G.__go_digest, goHMAC = G.__go_hmac;

  var HASHES = { 'SHA-1': 'sha1', 'SHA-256': 'sha256', 'SHA-384': 'sha384', 'SHA-512': 'sha512' };
  function hashName(alg) {
    var n = typeof alg === 'string' ? alg : alg && alg.name;
    n = String(n || '').toUpperCase();
    if (!HASHES[n]) throw new DOMException(KR_UNSUPPORTED + 'Unrecognized algorithm name: ' + n + ' (the embedded API runtime supports SHA-1, SHA-256, SHA-384, SHA-512; set ssr.apiRuntime to "node" for more)', 'NotSupportedError');
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
        resolve(b64ToBytes(goDigest(h, bytesToB64(needBytes(data)))).buffer);
      });
    },
    importKey: function (format, keyData, algorithm, extractable, usages) {
      return new Promise(function (resolve) {
        if (format !== 'raw') throw new DOMException(KR_UNSUPPORTED + 'importKey format "' + format + '" is not supported by the embedded API runtime; set ssr.apiRuntime to "node"', 'NotSupportedError');
        var name = String(typeof algorithm === 'string' ? algorithm : algorithm && algorithm.name).toUpperCase();
        if (name !== 'HMAC') throw new DOMException(KR_UNSUPPORTED + 'Algorithm ' + name + ' is not supported by the embedded API runtime (HMAC only); set ssr.apiRuntime to "node"', 'NotSupportedError');
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
        resolve(b64ToBytes(goHMAC(key._h, bytesToB64(key._k), bytesToB64(needBytes(data)))).buffer);
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
    subtle[m] = markUnsupported(function () { return Promise.reject(new DOMException(KR_UNSUPPORTED + 'crypto.subtle.' + m + ' is not supported by the embedded API runtime; set ssr.apiRuntime to "node" to use it', 'NotSupportedError')); });
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

  K.mods.crypto = { CryptoKey: CryptoKey };
})(globalThis, globalThis[Symbol.for('krate.web')]);
