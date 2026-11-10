// Streams feature: ReadableStream and its default reader, plus stubs for the
// stream classes the embedded runtime lacks.
(function (G, K) {
  'use strict';
  var def = K.def, unsupported = K.unsupported, markUnsupported = K.markUnsupported, stubClass = K.stubClass;
  var toBytes = K.toBytes, concatBytes = K.concatBytes, utf8Encode = K.utf8Encode;

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
  markUnsupported(ReadableStream.prototype.tee);
  markUnsupported(ReadableStream.prototype.pipeTo);
  markUnsupported(ReadableStream.prototype.pipeThrough);
  def('ReadableStream', ReadableStream);
  def('ReadableStreamDefaultReader', ReadableStreamDefaultReader);
  stubClass('WritableStream');
  stubClass('TransformStream');
  stubClass('CompressionStream');
  stubClass('DecompressionStream');

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

  K.mods.streams = { ReadableStream: ReadableStream, streamFromBytes: streamFromBytes, readAllBytes: readAllBytes };
})(globalThis, globalThis[Symbol.for('krate.web')]);
