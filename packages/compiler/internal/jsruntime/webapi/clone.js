// structuredClone feature. Also installed in the bare build-time VM, so it uses
// nothing but core.js.
(function (G, K) {
  'use strict';
  var DOMException = K.DOMException;
  K.def('structuredClone', function (value, options) {
    if (options && options.transfer && options.transfer.length) K.unsupported('structuredClone transfer');
    var fd = K.loaded('formdata'), Blob = fd ? fd.Blob : null;
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
      else if (Blob && v instanceof Blob) { out = v; }
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
  K.mods.clone = {};
})(globalThis, globalThis[Symbol.for('krate.web')]);
