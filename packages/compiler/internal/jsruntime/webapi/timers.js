// Timers feature: setTimeout and friends, queueMicrotask and performance. The
// Go job loop drives them through the __krate_timers_* hooks in core.js.
(function (G, K) {
  'use strict';
  var def = K.def;
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
  // (0, function) keeps the functions anonymous, as they always were.
  var setTimeout = (0, function (fn, ms) { return addTimer(fn, ms, Array.prototype.slice.call(arguments, 2), false); });
  def('setTimeout', setTimeout);
  def('setInterval', function (fn, ms) { return addTimer(fn, ms, Array.prototype.slice.call(arguments, 2), true); });
  def('setImmediate', function (fn) { return addTimer(fn, 0, Array.prototype.slice.call(arguments, 1), false); });
  def('clearTimeout', clearTimer);
  def('clearInterval', clearTimer);
  def('clearImmediate', clearTimer);
  def('queueMicrotask', function (fn) {
    if (typeof fn !== 'function') throw new TypeError('The "callback" argument must be of type function');
    Promise.resolve().then(fn).catch(function (e) { try { console.error(e && e.stack ? e.stack : String(e)); } catch (_) {} });
  });

  var perfOrigin = Date.now();
  def('performance', {
    timeOrigin: perfOrigin,
    now: function () { return Date.now() - perfOrigin; },
    toJSON: function () { return { timeOrigin: perfOrigin }; }
  });

  K.mods.timers = {
    setTimeout: setTimeout,
    __krate_timers_next: function () {
      if (!timers.length) return -1;
      var min = Infinity;
      for (var i = 0; i < timers.length; i++) if (timers[i].at < min) min = timers[i].at;
      return Math.max(0, min - Date.now());
    },
    // Runs the earliest due timer only. The Go loop drains microtasks before
    // the next call, so a timer's promise callbacks run before the next timer,
    // as in Node and browsers.
    __krate_timers_run: function () {
      var now = Date.now();
      var t = null, idx = -1;
      for (var i = 0; i < timers.length; i++) {
        var c = timers[i];
        if (c.at <= now && (t === null || c.at < t.at || (c.at === t.at && c.order < t.order))) { t = c; idx = i; }
      }
      if (t !== null) {
        if (t.repeat) { t.at = now + t.repeat; t.order = ++timerSeq; } else { timers.splice(idx, 1); }
        try { t.fn.apply(G, t.args); } catch (e) { try { console.error('Uncaught timer error: ' + (e && e.stack ? e.stack : String(e))); } catch (_) {} }
      }
      return timers.length;
    },
    __krate_timers_clear: function () { timers.length = 0; }
  };
})(globalThis, globalThis[Symbol.for('krate.web')]);
