// Console feature: console methods backed by the Go host (console_log,
// console_error, console_warn).
(function (G, K) {
  'use strict';
  var def = K.def;
  function fmt(a) {
    if (typeof a === 'string') return a;
    if (a instanceof Error) return a.stack || (a.name + ': ' + a.message);
    if (typeof a === 'function') return '[Function: ' + (a.name || 'anonymous') + ']';
    if (typeof a === 'bigint') return a.toString() + 'n';
    if (typeof a === 'symbol') return a.toString();
    if (a === undefined) return 'undefined';
    try { var s = JSON.stringify(a); return s === undefined ? String(a) : s; } catch (_) { return String(a); }
  }
  function line(args) { return Array.prototype.map.call(args, fmt).join(' '); }
  var goLog = G.console_log, goError = G.console_error, goWarn = G.console_warn;
  // Read the descriptor, not G.console, which may be this feature's own
  // loading accessor.
  var d = Object.getOwnPropertyDescriptor(G, 'console');
  var con = d && d.value;
  if (!con) { con = {}; def('console', con); }
  con.log = function () { goLog(line(arguments)); };
  con.info = con.log;
  con.debug = con.log;
  con.error = function () { goError(line(arguments)); };
  con.warn = function () { goWarn(line(arguments)); };
  con.trace = con.error;

  K.mods.console = {};
})(globalThis, globalThis[Symbol.for('krate.web')]);
