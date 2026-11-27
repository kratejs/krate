// Crypto feature: crypto.getRandomValues, randomUUID and crypto.subtle, with
// entropy, digests, HMAC and PBKDF2 in the Go host (__go_random, __go_digest,
// __go_hmac, __go_pbkdf2).
(function (G, K) {
  'use strict';
  var def = K.def, markUnsupported = K.markUnsupported, DOMException = K.DOMException, KR_UNSUPPORTED = K.KR_UNSUPPORTED;
  var toBytes = K.toBytes, toHost = K.toHost, fromHost = K.fromHost, host = K.host, fillRandom = K.fillRandom;
  var goDigest = G.__go_digest, goHMAC = G.__go_hmac, goPBKDF2 = G.__go_pbkdf2;

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
  // The default HMAC key length is the hash's block size.
  var BLOCK_BITS = { sha1: 512, sha256: 512, sha384: 1024, sha512: 1024 };
  function algName(alg) {
    return String(typeof alg === 'string' ? alg : alg && alg.name).toUpperCase();
  }
  function hashLabel(h) {
    return h && h.name ? h.name : String(h);
  }
  function unsupportedAlg(name, what) {
    return new DOMException(KR_UNSUPPORTED + 'Algorithm ' + name + ' is not supported by the embedded API runtime (' + what + '); set ssr.apiRuntime to "node"', 'NotSupportedError');
  }
  function needUsage(key, usage) {
    if (!(key instanceof CryptoKey) || !key._k) throw new DOMException('Unsupported key', 'InvalidAccessError');
    if (key.usages.indexOf(usage) < 0) throw new DOMException('The key does not allow ' + usage, 'InvalidAccessError');
  }
  // Key usages are listed in the specification's order, without repeats.
  var USAGE_ORDER = ['encrypt', 'decrypt', 'sign', 'verify', 'deriveKey', 'deriveBits', 'wrapKey', 'unwrapKey'];
  function normUsages(usages) {
    var given = Array.prototype.slice.call(usages || []).map(String);
    return USAGE_ORDER.filter(function (u) { return given.indexOf(u) >= 0; });
  }
  function makeKey(kind, bytes, hash, extractable, usages, algorithm) {
    var key = new CryptoKey();
    Object.defineProperty(key, '_k', { value: bytes });
    Object.defineProperty(key, '_kind', { value: kind });
    if (hash) Object.defineProperty(key, '_h', { value: hash });
    key.type = 'secret'; key.extractable = !!extractable; key.usages = normUsages(usages);
    key.algorithm = algorithm;
    return key;
  }
  function hmacKey(bytes, algorithm, extractable, usages) {
    var h = hashName(algorithm.hash);
    return makeKey('HMAC', bytes, h, extractable, usages, { name: 'HMAC', hash: { name: hashLabel(algorithm.hash) }, length: bytes.length * 8 });
  }
  function pbkdf2Bits(alg, key, length) {
    if (algName(alg) !== 'PBKDF2') throw unsupportedAlg(algName(alg), 'PBKDF2 only');
    if (key._kind !== 'PBKDF2') throw new DOMException('The key is not a PBKDF2 key', 'InvalidAccessError');
    if (length === null || length === undefined || length % 8 !== 0) throw new DOMException('length must be a multiple of 8', 'OperationError');
    var iterations = Number(alg.iterations);
    if (!(iterations >= 1)) throw new DOMException('iterations must be at least 1', 'OperationError');
    var salt = toBytes(alg.salt);
    if (!salt) throw new TypeError('salt must be an ArrayBuffer or ArrayBufferView');
    var h = hashName(alg.hash);
    if (length === 0) return new ArrayBuffer(0);
    return fromHost(host(goPBKDF2, h, toHost(key._k), toHost(salt), Math.floor(iterations), length / 8)).buffer;
  }
  class CryptoKey {}
  var subtle = {
    digest: function (alg, data) {
      return new Promise(function (resolve) {
        var h = hashName(alg);
        resolve(fromHost(host(goDigest, h, toHost(needBytes(data)))).buffer);
      });
    },
    importKey: function (format, keyData, algorithm, extractable, usages) {
      return new Promise(function (resolve) {
        if (format !== 'raw') throw new DOMException(KR_UNSUPPORTED + 'importKey format "' + format + '" is not supported by the embedded API runtime; set ssr.apiRuntime to "node"', 'NotSupportedError');
        var name = algName(algorithm);
        var bytes = needBytes(keyData).slice();
        if (name === 'HMAC') {
          resolve(hmacKey(bytes, algorithm, extractable, usages));
          return;
        }
        if (name === 'PBKDF2') {
          if (extractable) throw new DOMException('PBKDF2 keys are not extractable', 'SyntaxError');
          resolve(makeKey('PBKDF2', bytes, '', false, usages, { name: 'PBKDF2' }));
          return;
        }
        throw unsupportedAlg(name, 'HMAC and PBKDF2 only');
      });
    },
    exportKey: function (format, key) {
      return new Promise(function (resolve) {
        if (format !== 'raw') throw new DOMException(KR_UNSUPPORTED + 'exportKey format "' + format + '" is not supported by the embedded API runtime; set ssr.apiRuntime to "node"', 'NotSupportedError');
        if (!(key instanceof CryptoKey) || !key._k) throw new DOMException('Unsupported key', 'InvalidAccessError');
        if (!key.extractable) throw new DOMException('The key is not extractable', 'InvalidAccessError');
        resolve(key._k.slice().buffer);
      });
    },
    sign: function (alg, key, data) {
      return new Promise(function (resolve) {
        if (!(key instanceof CryptoKey) || !key._k || key._kind !== 'HMAC') throw new DOMException('Unsupported key', 'InvalidAccessError');
        resolve(fromHost(host(goHMAC, key._h, toHost(key._k), toHost(needBytes(data)))).buffer);
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
    },
    deriveBits: function (alg, key, length) {
      return new Promise(function (resolve) {
        needUsage(key, 'deriveBits');
        resolve(pbkdf2Bits(alg, key, length));
      });
    },
    deriveKey: function (alg, key, derivedKeyType, extractable, usages) {
      return new Promise(function (resolve) {
        needUsage(key, 'deriveKey');
        var name = algName(derivedKeyType);
        if (name !== 'HMAC') throw unsupportedAlg(name, 'deriveKey derives HMAC keys only');
        var length = derivedKeyType.length;
        if (length === undefined) length = BLOCK_BITS[hashName(derivedKeyType.hash)];
        var bits = pbkdf2Bits(alg, key, length);
        resolve(hmacKey(new Uint8Array(bits), derivedKeyType, extractable, usages));
      });
    }
  };
  ['encrypt', 'decrypt', 'generateKey', 'wrapKey', 'unwrapKey'].forEach(function (m) {
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
      b[6] = (b[6] & 0x0F) | 0x40; b[8] = (b[8] & 0x3F) | 0x80;
      var h = '';
      for (var i = 0; i < 16; i++) { h += (b[i] < 16 ? '0' : '') + b[i].toString(16); if (i === 3 || i === 5 || i === 7 || i === 9) h += '-'; }
      return h;
    }
  });

  K.mods.crypto = { CryptoKey: CryptoKey };
})(globalThis, globalThis[Symbol.for('krate.web')]);
