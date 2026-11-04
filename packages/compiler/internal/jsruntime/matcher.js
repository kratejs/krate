// Next-compatible middleware matcher: a path-to-regexp subset (:name, :name*,
// :name+, :name?, (regex) groups, literal text) compiled to a RegExp. Shared by
// the embedded runtime and the Node/Bun sidecars.
function __krate_compile_matcher(src) {
  if (typeof src !== 'string') throw new Error('middleware matcher must be a string or { source }');
  if (src.charAt(0) !== '/') throw new Error('middleware matcher "' + src + '" must start with "/"');
  var out = '', i = 0, n = src.length;
  function esc(c) { return c.replace(/[.*+?^${}()|[\]\\\/]/g, '\\$&'); }
  while (i < n) {
    var c = src.charAt(i);
    var prefix = '';
    if ((c === '/' || c === '.') && i + 1 < n && (src.charAt(i + 1) === ':' || src.charAt(i + 1) === '(')) { prefix = c; i++; c = src.charAt(i); }
    if (c === ':' || c === '(') {
      var pattern = '';
      if (c === ':') {
        var j = i + 1;
        while (j < n && /[A-Za-z0-9_]/.test(src.charAt(j))) j++;
        i = j;
        if (src.charAt(i) === '(') {
          var depth = 0, k = i;
          for (; k < n; k++) { var ch = src.charAt(k); if (ch === '\\') { k++; continue; } if (ch === '(') depth++; else if (ch === ')') { depth--; if (depth === 0) break; } }
          pattern = src.slice(i + 1, k); i = k + 1;
        }
      } else {
        var depth2 = 0, k2 = i;
        for (; k2 < n; k2++) { var ch2 = src.charAt(k2); if (ch2 === '\\') { k2++; continue; } if (ch2 === '(') depth2++; else if (ch2 === ')') { depth2--; if (depth2 === 0) break; } }
        pattern = src.slice(i + 1, k2); i = k2 + 1;
      }
      var mod = src.charAt(i);
      if (mod === '*' || mod === '+' || mod === '?') i++; else mod = '';
      var body = pattern || '[^\\/]+?';
      var p = prefix ? esc(prefix) : '';
      if (mod === '*') out += '(?:' + p + '(' + body + '(?:' + esc(prefix || '/') + body + ')*))?';
      else if (mod === '+') out += p + '(' + body + '(?:' + esc(prefix || '/') + body + ')*)';
      else if (mod === '?') out += '(?:' + p + '(' + body + '))?';
      else out += p + '(' + body + ')';
      continue;
    }
    if (c === '*') { out += '.*'; i++; continue; }
    out += esc(c); i++;
  }
  return new RegExp('^' + out + '\\/?$', 'i');
}
