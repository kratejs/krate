import type { CSSScope, CSSFlagsScope } from './signal.js';

let contextId = 0;

/** Marks Krate context objects so `use(x)` can tell them from promises. */
export const KRATE_CONTEXT: unique symbol = Symbol.for('krate.context');

export interface Context<T> {
  /** Context marker (see {@link KRATE_CONTEXT}). */
  $$typeof?: typeof KRATE_CONTEXT;
  /** React 19: the context itself renders as its provider (`<Ctx value>`). */
  (props: { value: T; children?: any }): Node;
  id: number;
  defaultValue: T;
  Provider: (props: { value: T; children?: any }) => Node;
  /** Renders `children(value)` with the nearest provider value. */
  Consumer: (props: { children?: any }) => Node;
  useContext: () => T;
  /** Alias for {@link Context.useContext}. */
  use: () => T;
  /**
   * Like useContext, but returns a getter that keeps reading the provider's
   * live value after the call (inside a list row rendered by
   * {@link krateContextRows}).
   */
  useContextLive: () => () => T;
}

/** A provider value that is read through a getter on every use. */
class LiveContext {
  constructor(readonly get: () => unknown) {}
}

function toNode(v: unknown): Node {
  if (v instanceof Node) return v;
  const frag = document.createDocumentFragment();
  if (Array.isArray(v)) {
    for (const item of v) frag.appendChild(toNode(item));
  } else if (v != null && v !== false && v !== true) {
    frag.appendChild(document.createTextNode(String(v)));
  }
  return frag;
}

const contextStack: Map<number, unknown[]> = new Map();

// Reset all context stacks to their default values.
// Called during SPA navigation to prevent stale context leaking across pages.
export function resetContexts(): void {
  for (const [id, stack] of contextStack) {
    if (stack.length > 0) {
      const defaultVal = stack[0];
      contextStack.set(id, [defaultVal]);
    }
  }
}

/**
 * Create a context that carries a CSS scope handle (see {@link CSSScope}). The
 * default is a harmless placeholder - a `<Provider>` value always wins - so no
 * cast is needed:
 *
 * ```ts
 * const TabsCtx = createScope<string>();
 * // provide:  <TabsCtx.Provider value={createCSSChoice('a')}>...</Provider>
 * // consume:  const tab = TabsCtx.use();
 * ```
 */
export function createScope<T>(): Context<CSSScope<T>> {
  return createContext<CSSScope<T>>({} as CSSScope<T>);
}

/** Like {@link createScope} but for a `createCSSFlags` handle. */
export function createScopeFlags<K extends string>(): Context<CSSFlagsScope<K>> {
  return createContext<CSSFlagsScope<K>>({} as CSSFlagsScope<K>);
}

export function createContext<T>(defaultValue: T): Context<T> {
  const id = contextId++;
  contextStack.set(id, [defaultValue]);

  const useContext = (): T => {
    const stack = contextStack.get(id);
    if (stack && stack.length > 0) {
      const top = stack[stack.length - 1];
      return (top instanceof LiveContext ? top.get() : top) as T;
    }
    return defaultValue;
  };

  const useContextLive = (): (() => T) => {
    const stack = contextStack.get(id);
    const top = stack && stack.length > 0 ? stack[stack.length - 1] : defaultValue;
    if (top instanceof LiveContext) return () => top.get() as T;
    return () => top as T;
  };

  const Provider = (props: { value: T; children?: any }): Node => {
    const stack = contextStack.get(id) || [defaultValue];
    stack.push(props.value);
    contextStack.set(id, stack);

    // Create a container and render children
    const container = document.createElement('div');
    container.style.display = 'contents';
    // Read once: a lazy `children` getter renders under this provider.
    const children = props.children;
    if (children) {
      for (const child of Array.isArray(children) ? children : [children]) {
        if (child instanceof Node) {
          container.appendChild(child);
        } else if (child != null) {
          container.appendChild(document.createTextNode(String(child)));
        }
      }
    }

    // Pop the context value synchronously after rendering children.
    // Children are already rendered (passed as pre-built DOM nodes), so
    // any effects that read context have already done so during their
    // synchronous creation. Synchronous pop fixes the nested Provider race:
    // with microtask pop, code after an inner Provider but within the outer
    // Provider's scope would incorrectly see the inner value still on the stack.
    stack.pop();
    if (stack.length === 0) {
      stack.push(defaultValue);
    }

    return container;
  };
  const Consumer = (props: { children?: any }): Node => {
    let fn = props.children;
    if (Array.isArray(fn)) fn = fn.find((c) => typeof c === 'function');
    return typeof fn === 'function' ? toNode(fn(useContext())) : toNode(null);
  };
  const ctx = ((props: { value: T; children?: any }) => Provider(props)) as Context<T>;
  ctx.$$typeof = KRATE_CONTEXT;
  ctx.id = id;
  ctx.defaultValue = defaultValue;
  ctx.Provider = Provider;
  ctx.Consumer = Consumer;
  ctx.useContext = useContext;
  ctx.use = useContext;
  ctx.useContextLive = useContextLive;
  return ctx;
}

/**
 * Wraps a list row render function so the provider values of the enclosing
 * `<Ctx.Provider value={...}>` (given as getters over the provider's state)
 * are visible to the rows it creates, and stay live when the state changes.
 * The compiler flattens providers at build time, so rows rendered later by
 * the list runtime have no provider on the stack otherwise.
 */
export function krateContextRows<A extends unknown[], R>(
  entries: Array<[Context<any>, () => unknown]>,
  render: (...args: A) => R,
): (...args: A) => R {
  return (...args: A): R => {
    const pushed: unknown[][] = [];
    for (const [ctx, get] of entries) {
      const stack = contextStack.get(ctx.id);
      if (!stack) continue;
      stack.push(new LiveContext(get));
      pushed.push(stack);
    }
    try {
      return render(...args);
    } finally {
      for (const stack of pushed) stack.pop();
    }
  };
}
