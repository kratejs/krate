// Events feature: DOMException, Event, EventTarget, AbortController and
// AbortSignal.
(function (G, K) {
  'use strict';
  var def = K.def, DOMException = K.DOMException;
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
      K.need('timers').setTimeout(function () { abortSignal(s, new DOMException('The operation timed out.', 'TimeoutError')); }, ms);
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

  K.mods.events = { Event: Event, EventTarget: EventTarget, AbortSignal: AbortSignal, AbortController: AbortController, abortSignal: abortSignal, kAbortInit: kAbortInit };
})(globalThis, globalThis[Symbol.for('krate.web')]);
