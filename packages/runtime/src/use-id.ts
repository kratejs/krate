// useId() for components that render on the client. Most useId() calls are
// resolved by the compiler to a literal that the HTML and the hydration code
// share; this covers the components the runtime itself creates, such as the
// rows of a keyed list.

interface IdScope {
  prefix: string;
  n: number;
}

let scope: IdScope | null = null;
let counter = 0;

const encoder = typeof TextEncoder !== 'undefined' ? new TextEncoder() : null;

/**
 * Percent-encodes every byte of an id part outside [A-Za-z0-9_.:$-], so a list
 * key such as "a b" still makes a valid id. The compiler applies the same rule.
 */
function idSafe(s: string): string {
  return s.replace(/[^A-Za-z0-9_.:$-]/gu, (c) => {
    const bytes = encoder ? Array.from(encoder.encode(c)) : [c.charCodeAt(0) & 0xff];
    return bytes.map((b) => '%' + b.toString(16).toUpperCase().padStart(2, '0')).join('');
  });
}

/**
 * Run `fn` with useId() numbering scoped to a list row. The compiler numbers
 * the useId() calls of a server-rendered row `<prefix>-u0`, `-u1`, ... in
 * render order, so a row adopted from the server keeps its ids.
 */
export function krateWithIdScope<T>(prefix: string, fn: () => T): T {
  if (!prefix) return fn();
  const saved = scope;
  scope = { prefix: idSafe(prefix), n: 0 };
  try {
    return fn();
  } finally {
    scope = saved;
  }
}

/** The compiled form of React's useId(). */
export function __krate_useId(): string {
  if (scope) return 'krate-' + scope.prefix + '-u' + scope.n++;
  return 'krate_u' + counter++;
}
