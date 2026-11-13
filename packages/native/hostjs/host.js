// Host-side runtime for hostjs: the __krateHost registry and the call, stream
// and signal drivers the Go loop invokes. Every global it defines is named
// __kh_*. Values cross as the JSON tree of the wire protocol (PROTOCOL.md)
// plus a side list of binary blobs moved by host functions, so bytes never go
// through base64 when the engine supports raw transfer.
(function (G) {
  'use strict';
  var K = G[Symbol.for('krate.web')];
  var host = K.host, toHost = K.toHost, fromHost = K.fromHost;

  var fns = new Map();
  var sigs = new Map();
  var calls = new Map();
  var subs = new Map();
  var saved = null;

  function def(name, value) {
    Object.defineProperty(G, name, { value: value, writable: true, configurable: true, enumerable: false });
  }

  var registry = {
    register: function (id, fn, kind) {
      if (typeof id !== 'string' || typeof fn !== 'function') throw new TypeError('__krateHost.register(id, fn, kind): id must be a string and fn a function');
      kind = kind === undefined ? 'func' : kind;
      if (kind !== 'func' && kind !== 'stream') throw new TypeError('__krateHost.register: kind must be "func" or "stream"');
      fns.set(id, { fn: fn, kind: kind });
    },
    registerSignal: function (id, sig) {
      if (typeof id !== 'string' || !sig || typeof sig.get !== 'function' || typeof sig.subscribe !== 'function') {
        throw new TypeError('__krateHost.registerSignal(id, sig): id must be a string and sig needs get() and subscribe()');
      }
      sigs.set(id, sig);
    }
  };
  def('__krateHost', registry);

  // Encoding. Mirrors packages/app/src/wire.ts.
  var TAGS = ['$b', '$d', '$m', '$s'];
  var ESCAPED = ['$b$', '$d$', '$m$', '$s$'];

  function bad(path, msg) {
    var e = new Error('cannot encode the result' + (path ? ' at ' + path : '') + ': ' + msg);
    e.code = 'bad_result';
    throw e;
  }
  // put sets an own property, including "__proto__", which a plain
  // assignment would turn into a prototype change.
  function put(o, k, v) {
    if (k === '__proto__') Object.defineProperty(o, k, { value: v, enumerable: true, writable: true, configurable: true });
    else o[k] = v;
  }
  function addBlob(st, u8) { st.blobs.push(u8); return { $b: st.blobs.length - 1 }; }

  function enc(v, st, path, stack) {
    switch (typeof v) {
      case 'string': case 'boolean': return v;
      case 'number':
        if (!isFinite(v)) bad(path, 'numbers must be finite, got ' + String(v));
        return v;
      case 'undefined': return null;
      case 'bigint': return bad(path, 'bigint is not supported');
      case 'function': return bad(path, 'a function');
      case 'symbol': return bad(path, 'a symbol');
    }
    if (v === null) return null;
    if (v instanceof Uint8Array) return addBlob(st, v);
    if (v instanceof ArrayBuffer) return addBlob(st, new Uint8Array(v));
    if (ArrayBuffer.isView(v)) return addBlob(st, new Uint8Array(v.buffer, v.byteOffset, v.byteLength));
    if (v instanceof Date) {
      if (isNaN(v.getTime())) bad(path, 'an invalid Date');
      return { $d: v.toISOString() };
    }
    if (stack.indexOf(v) >= 0) bad(path, 'a circular structure');
    stack.push(v);
    try {
      if (Array.isArray(v)) {
        var arr = new Array(v.length);
        for (var i = 0; i < v.length; i++) arr[i] = enc(v[i], st, path + '[' + i + ']', stack);
        return arr;
      }
      if (v instanceof Map) {
        var pairs = [], n = 0;
        v.forEach(function (val, k) {
          pairs.push([enc(k, st, path + '.<key ' + n + '>', stack), enc(val, st, path + '.<value ' + n + '>', stack)]);
          n++;
        });
        return { $m: pairs };
      }
      if (v instanceof Set) {
        var items = [], m = 0;
        v.forEach(function (x) { items.push(enc(x, st, path + '.<item ' + (m++) + '>', stack)); });
        return { $s: items };
      }
      var proto = Object.getPrototypeOf(v);
      if (proto !== Object.prototype && proto !== null) {
        var name = (proto.constructor && proto.constructor.name) || 'unknown';
        bad(path, 'an instance of ' + name + ' (only plain objects, arrays, Date, Map, Set and Uint8Array)');
      }
      var keys = Object.keys(v), out = {}, kept = [];
      for (var j = 0; j < keys.length; j++) {
        var val2 = v[keys[j]];
        if (val2 === undefined) continue;
        put(out, keys[j], enc(val2, st, path + '.' + keys[j], stack));
        kept.push(keys[j]);
      }
      if (kept.length === 1) {
        var k = kept[0];
        if (TAGS.indexOf(k) >= 0) { var w = {}; w[k + '$'] = out; return w; }
        if (ESCAPED.indexOf(k) >= 0) { var w2 = {}; w2[k] = out; return w2; }
      }
      return out;
    } finally {
      stack.pop();
    }
  }

  // Decoding of call arguments.
  function dec(t, cid) {
    if (t === null || typeof t !== 'object') return t;
    if (Array.isArray(t)) { for (var i = 0; i < t.length; i++) t[i] = dec(t[i], cid); return t; }
    var keys = Object.keys(t);
    if (keys.length === 1) {
      var k = keys[0], v = t[k];
      if (k === '$b') return fromHost(host(G.__kh_blob, cid, v));
      if (k === '$d') return new Date(v);
      if (k === '$m') return new Map(v.map(function (p) { return [dec(p[0], cid), dec(p[1], cid)]; }));
      if (k === '$s') return new Set(v.map(function (x) { return dec(x, cid); }));
      if (ESCAPED.indexOf(k) >= 0) {
        var inner = {}, ik = Object.keys(v);
        for (var j = 0; j < ik.length; j++) put(inner, ik[j], dec(v[ik[j]], cid));
        return inner;
      }
    }
    for (var n = 0; n < keys.length; n++) t[keys[n]] = dec(t[keys[n]], cid);
    return t;
  }

  // Outcome kinds, matching outcome.kind in Go.
  var VALUE = 0, FAIL = 1, END = 2;

  function errInfo(e) {
    var o = { code: 'internal', message: '', stack: '' };
    try {
      if (e !== null && (typeof e === 'object' || typeof e === 'function')) {
        if (typeof e.code === 'string' && e.code) o.code = e.code;
        o.message = typeof e.message === 'string' ? e.message : String(e);
        if (typeof e.stack === 'string') o.stack = e.stack;
        try {
          var d = JSON.stringify(e.data);
          if (d !== undefined) o.data = JSON.parse(d);
        } catch (_) {}
      } else {
        o.message = String(e);
      }
    } catch (_) {
      o.message = 'a value that cannot be converted to a string was thrown';
    }
    return JSON.stringify(o);
  }

  function settleValue(id, v) {
    var st = { blobs: [] };
    var json = JSON.stringify(enc(v, st, '', []));
    for (var i = 0; i < st.blobs.length; i++) host(G.__kh_out_blob, id, toHost(st.blobs[i]));
    G.__kh_settle(id, VALUE, json);
  }
  function settleFail(id, e) { G.__kh_settle(id, FAIL, errInfo(e)); }

  function finish(id, v) {
    var st = calls.get(id);
    if (!st || st.dead) return;
    try { settleValue(id, v); } catch (e) { settleFail(id, e); }
    if (st.kind === 'func') calls.delete(id);
  }
  function fail(id, e) {
    var st = calls.get(id);
    if (!st || st.dead) return;
    calls.delete(id);
    settleFail(id, e);
  }

  function toIterator(r) {
    if (r && typeof r[Symbol.asyncIterator] === 'function') return r[Symbol.asyncIterator]();
    if (r && typeof r[Symbol.iterator] === 'function') return r[Symbol.iterator]();
    var e = new Error('a stream function must return an async iterable');
    e.code = 'bad_result';
    throw e;
  }

  def('__kh_invoke', function __kh_invoke (id, fid, argsJSON, windowID) {
    var ent = fns.get(fid);
    if (!ent) {
      var nf = new Error('no such function ' + fid);
      nf.code = 'not_found';
      G.__kh_settle(id, FAIL, errInfo(nf));
      return;
    }
    var ac = new AbortController();
    var st = { kind: ent.kind, ac: ac, dead: false, it: null };
    calls.set(id, st);
    var ctx = { signal: ac.signal, window: windowID };
    var args;
    try { args = dec(JSON.parse(argsJSON), id); } catch (e) { fail(id, e); return; }
    if (ent.kind === 'stream') {
      st.it = Promise.resolve().then(function () { return toIterator(ent.fn.apply(ctx, args)); });
      st.it.catch(function (e) { fail(id, e); });
      return;
    }
    var p;
    try { p = Promise.resolve(ent.fn.apply(ctx, args)); } catch (e2) { p = Promise.reject(e2); }
    p.then(function (v) { finish(id, v); }, function (e3) { fail(id, e3); });
  });

  def('__kh_pull', function __kh_pull (id) {
    var st = calls.get(id);
    if (!st || st.dead || !st.it) return;
    st.it.then(function (it) { return it.next(); }).then(function (r) {
      if (st.dead) return;
      if (r.done) { calls.delete(id); G.__kh_settle(id, END, ''); return; }
      try { settleValue(id, r.value); } catch (e) { fail(id, e); }
    }, function (e) { fail(id, e); });
  });

  // Cancels a call or ends a stream early.
  def('__kh_cancel', function __kh_cancel (id) {
    var st = calls.get(id);
    if (!st) return;
    calls.delete(id);
    st.dead = true;
    try { st.ac.abort(); } catch (_) {}
    if (st.it) {
      st.it.then(function (it) { return typeof it.return === 'function' ? it.return() : undefined; }).catch(function () {});
    }
  });

  def('__kh_subscribe', function __kh_subscribe (sid, sigID) {
    var sig = sigs.get(sigID);
    if (!sig) return 'no such signal ' + sigID;
    function emit(v) {
      try { settleValue(sid, v); } catch (e) { settleFail(sid, e); }
    }
    var rec = { sig: sigID, un: null, emit: emit };
    subs.set(sid, rec);
    rec.un = sig.subscribe(emit);
    emit(sig.get());
    return '';
  });

  def('__kh_unsubscribe', function __kh_unsubscribe (sid) {
    var rec = subs.get(sid);
    if (!rec) return;
    subs.delete(sid);
    if (typeof rec.un === 'function') rec.un();
  });

  // In-process SDK and host calls. __krateHost.call/stream/signal invoke the
  // shell's scheme registry directly (no HTTP, no permission gate: host code
  // is trusted). Arguments and results use the same encoding as the wire.
  var bridge = new Map();

  function bridgeError(text) {
    var info;
    try { info = JSON.parse(text); } catch (_) { info = { message: String(text) }; }
    var e = new Error(info.message || 'host call failed');
    e.code = info.code || 'internal';
    if (info.data !== undefined) e.data = info.data;
    return e;
  }

  function bridgeOpen(op, id, args, h) {
    if (typeof id !== 'string') throw new TypeError('__krateHost: id must be a string');
    if (args === undefined) args = [];
    if (!Array.isArray(args)) throw new TypeError('__krateHost: arguments must be an array');
    var st = { blobs: [] };
    var json = JSON.stringify(enc(args, st, 'arguments', []));
    var rid = G.__kh_bnew();
    for (var i = 0; i < st.blobs.length; i++) host(G.__kh_out_blob, rid, toHost(st.blobs[i]));
    bridge.set(rid, h);
    G.__kh_bcall(rid, op, id, json);
    return rid;
  }
  function bridgeClose(rid) {
    if (!bridge.has(rid)) return;
    bridge.delete(rid);
    G.__kh_bcall(rid, 3, '', '');
  }

  def('__kh_bdone', function __kh_bdone (rid, kind, text) {
    var h = bridge.get(rid);
    if (!h) return;
    if (kind === VALUE) {
      var v;
      try { v = dec(JSON.parse(text), rid); } catch (e) { bridge.delete(rid); G.__kh_bfree(rid); h.fail(e); return; }
      if (h.once) { bridge.delete(rid); G.__kh_bfree(rid); }
      h.value(v);
    } else if (kind === END) {
      bridge.delete(rid); G.__kh_bfree(rid);
      h.end();
    } else {
      bridge.delete(rid); G.__kh_bfree(rid);
      h.fail(bridgeError(text));
    }
  });

  registry.call = function (id, args) {
    return new Promise(function (resolve, reject) {
      bridgeOpen(0, id, args, { once: true, value: resolve, fail: reject, end: function () {} });
    });
  };

  registry.stream = function (id, args) {
    var queue = [], waiting = [], finished = false, failure = null, rid = null;
    function pump() {
      while (waiting.length) {
        if (queue.length) { waiting.shift().resolve({ value: queue.shift(), done: false }); }
        else if (failure) { waiting.shift().reject(failure); failure = null; }
        else if (finished) { waiting.shift().resolve({ value: undefined, done: true }); }
        else break;
      }
    }
    function open() {
      if (rid !== null || finished) return;
      rid = bridgeOpen(1, id, args, {
        value: function (v) { queue.push(v); pump(); },
        fail: function (e) { finished = true; failure = e; pump(); },
        end: function () { finished = true; pump(); }
      });
    }
    var it = {
      next: function () {
        return new Promise(function (resolve, reject) {
          try { open(); } catch (e) { finished = true; reject(e); return; }
          waiting.push({ resolve: resolve, reject: reject });
          pump();
        });
      },
      return: function () {
        finished = true; queue.length = 0;
        if (rid !== null) bridgeClose(rid);
        pump();
        return Promise.resolve({ value: undefined, done: true });
      }
    };
    it[Symbol.asyncIterator] = function () { return it; };
    return it;
  };

  registry.signal = function (id) {
    var last, have = false, subs2 = [], rid = null;
    function fan(v) { last = v; have = true; subs2.slice().forEach(function (f) { try { f(v); } catch (_) {} }); }
    rid = bridgeOpen(2, id, [], { value: fan, fail: function () {}, end: function () {} });
    return {
      get: function () { return have ? last : undefined; },
      subscribe: function (fn) {
        subs2.push(fn);
        if (have) { try { fn(last); } catch (_) {} }
        return function () { var i = subs2.indexOf(fn); if (i >= 0) subs2.splice(i, 1); };
      },
      close: function () { if (rid !== null) bridgeClose(rid); subs2.length = 0; }
    };
  };

  // Called by the wrapper Load puts around a bundle; keeps the thrown stack,
  // which the engine's error message lacks.
  def('__kh_load_failed', function __kh_load_failed (e) {
    def('__kh_load_error', errInfo(e));
  });

  def('__kh_list', function __kh_list () {
    var out = { funcs: [], streams: [], signals: [] };
    fns.forEach(function (e, id) { (e.kind === 'stream' ? out.streams : out.funcs).push(id); });
    sigs.forEach(function (_, id) { out.signals.push(id); });
    return JSON.stringify(out);
  });

  // Reload support. begin snapshots the registry and stops the old code's
  // timers; end(true) keeps the new registry and carries signal values and
  // subscriptions over, end(false) restores the snapshot.
  def('__kh_load_begin', function __kh_load_begin () {
    saved = { fns: fns, sigs: sigs };
    fns = new Map();
    sigs = new Map();
    G.__krate_timers_clear();
  });
  def('__kh_load_end', function __kh_load_end (ok) {
    var old = saved;
    saved = null;
    if (!old) return;
    if (!ok) { fns = old.fns; sigs = old.sigs; return; }
    sigs.forEach(function (neu, id) {
      var prev = old.sigs.get(id);
      if (prev && prev !== neu && typeof neu.set === 'function') {
        try { neu.set(prev.get()); } catch (_) {}
      }
    });
    subs.forEach(function (rec) {
      var neu = sigs.get(rec.sig), prev = old.sigs.get(rec.sig);
      if (!neu || neu === prev) return;
      if (typeof rec.un === 'function') rec.un();
      rec.un = neu.subscribe(rec.emit);
      rec.emit(neu.get());
    });
  });
})(globalThis);
