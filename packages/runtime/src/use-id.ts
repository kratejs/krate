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

/**
 * Run `fn` with useId() numbering scoped to a list row. The compiler numbers
 * the useId() calls of a server-rendered row `<prefix>-u0`, `-u1`, ... in
 * render order, so a row adopted from the server keeps its ids.
 */
export function krateWithIdScope<T>(prefix: string, fn: () => T): T {
  if (!prefix) return fn();
  const saved = scope;
  scope = { prefix, n: 0 };
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
