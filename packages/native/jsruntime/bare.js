// Build-time expression VMs (NewBare) replace the built-ins whose result
// depends on the build machine's locale or time zone with functions that
// throw, so a folded value never depends on where the build ran. The
// evaluator then leaves such expressions to run time.
(function () {
  'use strict';
  function stub(proto, owner, names) {
    names.forEach(function (name) {
      // A method the engine lacks (BigInt toLocaleString) is added too, so
      // it does not fall back to the locale-free Object.prototype version.
      var d = Object.getOwnPropertyDescriptor(proto, name) || { writable: true, enumerable: false, configurable: true };
      var msg = owner + '.prototype.' + name + ' depends on the locale or time zone, so it is not evaluated at build time';
      var fn = { [name]: function () { throw new TypeError(msg); } }[name];
      Object.defineProperty(proto, name, { value: fn, writable: d.writable, enumerable: d.enumerable, configurable: d.configurable });
    });
  }
  stub(String.prototype, 'String', ['localeCompare', 'toLocaleLowerCase', 'toLocaleUpperCase']);
  stub(Number.prototype, 'Number', ['toLocaleString']);
  stub(BigInt.prototype, 'BigInt', ['toLocaleString']);
  stub(Array.prototype, 'Array', ['toLocaleString']);
  stub(Object.getPrototypeOf(Int8Array.prototype), 'TypedArray', ['toLocaleString']);
  stub(Date.prototype, 'Date', ['toString', 'toDateString', 'toTimeString', 'toLocaleString', 'toLocaleDateString', 'toLocaleTimeString']);
})();
