/**
 * React compatibility runtime.
 *
 * Compiled React libraries (for example `@radix-ui/react-*`) and React source
 * that the compiler could not lower statically resolve to this module. It
 * implements the React API surface on top of Krate's DOM factory, signals, and
 * context so that code can execute without shipping React.
 *
 * Krate components run once and update through signals, so hooks here are
 * signal-backed rather than re-render based. Pure APIs (`createElement`,
 * `Children`, `cloneElement`, `isValidElement`, `createRef`, `createContext`)
 * behave as expected. `Suspense`, `lazy` and `use` suspend through the
 * Suspense runtime (see suspense.ts): a component that reads a pending promise
 * renders a placeholder that is replaced once the promise settles, and the
 * nearest `<Suspense>` shows its fallback until all of its placeholders
 * resolved. `useTransition`/`startTransition` support async actions and
 * `useDeferredValue` defers getter values. Class components are intentionally
 * minimal.
 */
import { h, type Component } from './dom.js';
import { createSignal, createEffect, onCleanup } from './signal.js';
import { krateSetRef, type KrateRef } from './refs.js';
import { krateEventRoot } from './events.js';
import { createContext as krateCreateContext } from './context.js';
import { cloneElement as krateCloneElement } from './class-helpers.js';
import { jsx as krateJsx } from './jsx-runtime.js';
import {
  createBoundary,
  createDeferredValue,
  createExternalStore,
  isSuspenseSignal,
  readValue,
  reportSuspenseError,
  startTransition as krateStartTransition,
  trackThenable,
  useTransition as krateUseTransition,
  withReplayScope,
  type Boundary,
  type Thenable,
} from './suspense.js';

/** React's `Fragment` marker; `createElement(Fragment, ...)` yields a fragment. */
export const Fragment: unique symbol = Symbol.for('krate.react.fragment');

function fragmentChildren(children: unknown[]): DocumentFragment {
  const frag = document.createDocumentFragment();
  for (const child of children) {
    if (child == null || child === false || child === true) continue;
    if (child instanceof Node) frag.appendChild(child);
    else if (Array.isArray(child)) frag.appendChild(fragmentChildren(child));
    else frag.appendChild(document.createTextNode(String(child)));
  }
  return frag;
}

export type ReactChild = unknown;

/**
 * `React.createElement(type, config, ...children)`. Returns a Krate DOM node
 * (elements) or the result of the component function, matching `h`.
 */
export function createElement(
  type: unknown,
  config?: Record<string, unknown> | null,
  ...children: unknown[]
): Node {
  if (type === Fragment) return fragmentChildren(children);
  const props: Record<string, unknown> = config ? { ...config } : {};
  if (children.length === 1) props.children = children[0];
  else if (children.length > 1) props.children = children;
  return render(type, props);
}

export function jsx(type: unknown, props: Record<string, unknown> | null, _key?: unknown): Node {
  return render(type, props);
}

export const jsxs = jsx;

export function jsxDEV(
  type: unknown,
  props: Record<string, unknown> | null,
  _key?: unknown,
  _isStatic?: boolean,
  _source?: unknown,
  _self?: unknown
): Node {
  return render(type, props);
}

// render creates an element, or calls a component. A component that suspends
// (a pending use() or lazy load) becomes a placeholder comment that re-renders
// the component when the promise settles; the nearest <Suspense> claims it.
function render(type: unknown, props: Record<string, unknown> | null): Node {
  if (type === Fragment) {
    const kids = props?.children;
    return fragmentChildren(kids === undefined ? [] : Array.isArray(kids) ? kids : [kids]);
  }
  if (typeof type !== 'function') {
    return krateJsx(type as string | Component<Record<string, unknown>>, props);
  }
  const replay = { list: [], i: 0 };
  const attempt = (): Node =>
    withReplayScope(replay, () => krateJsx(type as Component<Record<string, unknown>>, props));
  try {
    return attempt();
  } catch (error) {
    if (!isSuspenseSignal(error)) throw error;
    return suspendedPlaceholder(error.thenable, attempt);
  }
}

const SUSPENDED = '__krateSuspended';

interface SuspendedState {
  boundary: Boundary | null;
}

type SuspendedMarker = Comment & { [SUSPENDED]?: SuspendedState };

function suspendedPlaceholder(thenable: Thenable<unknown>, attempt: () => Node): Comment {
  const marker = document.createComment('krate:suspended') as SuspendedMarker;
  const state: SuspendedState = { boundary: null };
  marker[SUSPENDED] = state;
  const settle = (): void => {
    let next: Node;
    try {
      next = attempt();
    } catch (error) {
      if (isSuspenseSignal(error)) {
        error.thenable.then(settle, settle);
        return;
      }
      reportSuspenseError(error, state.boundary);
      next = document.createComment('krate:error');
    }
    if (state.boundary) claimSuspended(next, state.boundary);
    if (marker.parentNode) marker.parentNode.replaceChild(next, marker);
    marker[SUSPENDED] = undefined;
    state.boundary?.release();
  };
  thenable.then(settle, settle);
  return marker;
}

// claimSuspended attributes every unclaimed placeholder under root to the
// boundary and holds it once per placeholder. Inner boundaries render (and
// claim) first, so the innermost boundary wins.
function claimSuspended(root: Node, boundary: Boundary | null): number {
  let claimed = 0;
  const visit = (node: Node): void => {
    if (node.nodeType === 8) {
      const state = (node as SuspendedMarker)[SUSPENDED];
      if (state && !state.boundary) {
        if (boundary) {
          state.boundary = boundary;
          boundary.hold();
        }
        claimed++;
      }
      return;
    }
    for (let c = node.firstChild; c; c = c.nextSibling) visit(c);
  };
  visit(root);
  return claimed;
}

/** `React.Children` helpers over arrays and single values. */
export const Children = {
  map(children: unknown, fn: (child: unknown, index: number) => unknown): unknown[] {
    return toArray(children).map((child, i) => fn(child, i));
  },
  forEach(children: unknown, fn: (child: unknown, index: number) => void): void {
    toArray(children).forEach((child, i) => fn(child, i));
  },
  count(children: unknown): number {
    return toArray(children).length;
  },
  toArray(children: unknown): unknown[] {
    return toArray(children);
  },
  only(children: unknown): unknown {
    const list = toArray(children);
    if (list.length !== 1) {
      throw new Error(`React.Children.only expected a single child, got ${list.length}`);
    }
    return list[0];
  },
};

function toArray(children: unknown): unknown[] {
  if (children == null || children === false || children === true) return [];
  if (Array.isArray(children)) {
    const out: unknown[] = [];
    for (const child of children) out.push(...toArray(child));
    return out;
  }
  return [children];
}

export function cloneElement(
  element: Node,
  props?: Record<string, unknown> | null,
  ...children: unknown[]
): Node {
  return krateCloneElement(element, props, ...children);
}

export function isValidElement(value: unknown): boolean {
  return value instanceof Node || (typeof value === 'object' && value !== null);
}

export interface RefObject<T> {
  current: T | null;
}

export function createRef<T = unknown>(): RefObject<T> {
  return { current: null };
}

export function forwardRef(render: unknown): unknown {
  // Krate binds refs at compile time; render is returned unchanged.
  return render;
}

export function memo<P>(component: Component<P>, _compare?: unknown): Component<P> {
  return component;
}

/**
 * `React.lazy(() => import('./X'))`. The loader runs on the first render (as
 * in React); until the module resolves the component suspends to the nearest
 * `<Suspense>`. The module's default export must be a component; a rejected
 * import or a non-component default is reported to the nearest error
 * boundary.
 */
export function lazy<P>(loader: () => PromiseLike<{ default: unknown }>): Component<P> {
  let payload: Thenable<{ default: unknown }> | null = null;
  const Lazy = (props: P): Node => {
    if (!payload) payload = trackThenable(loader() as Thenable<{ default: unknown }>);
    const mod = readValue<{ default: unknown }>(payload);
    const impl = mod && typeof mod === 'object' ? mod.default : undefined;
    if (typeof impl !== 'function') {
      throw new Error(
        'lazy: the module did not have a component as its default export. ' +
          'Lazy element type must resolve to a class or function; export the component with `export default`.',
      );
    }
    return render(impl, props as unknown as Record<string, unknown>);
  };
  return Lazy as unknown as Component<P>;
}

/** StrictMode renders its children; Krate has no double-render checks. */
export const StrictMode = Fragment;

/**
 * Profiler renders its children. `onRender` is never called: Krate does not
 * re-render components, so there are no commit timings to report.
 */
export function Profiler(props: { id?: string; onRender?: unknown; children?: unknown }): Node {
  return fragmentChildren(toArray(props.children));
}

/**
 * A Suspense boundary. Children that suspend render as placeholders; while
 * any placeholder under this boundary is pending the fallback is shown, and
 * the content is revealed once all of them resolved (siblings together).
 */
export function Suspense(props: { fallback?: unknown; children?: unknown }): Node {
  const content = fragmentChildren(toArray(props.children));
  const fallback = fragmentChildren(toArray(props.fallback));
  const start = document.createComment('suspense');
  const end = document.createComment('/suspense');
  let stash: DocumentFragment;
  // apply swaps the nodes between the markers with the stashed branch.
  const apply = (): void => {
    const parent = start.parentNode;
    if (!parent) return;
    const taken = document.createDocumentFragment();
    let n = start.nextSibling;
    while (n && n !== end) {
      const next = n.nextSibling;
      taken.appendChild(n);
      n = next;
    }
    parent.insertBefore(stash, end);
    stash = taken;
  };
  const pending = claimSuspended(content, null);
  const boundary = createBoundary('compat', apply, { anchor: start, initialContent: pending === 0 });
  claimSuspended(content, boundary);
  const out = document.createDocumentFragment();
  out.appendChild(start);
  if (pending === 0) {
    out.appendChild(content);
    stash = fallback;
  } else {
    out.appendChild(fallback);
    stash = content;
  }
  out.appendChild(end);
  return out;
}

// --- context ---

export interface ReactContext<T> {
  readonly _id: number;
  readonly defaultValue: T;
  readonly Provider: (props: { value: T; children?: unknown }) => Node;
  readonly Consumer: (props: { children: (value: T) => unknown }) => unknown;
  useContext(): T;
  use(): T;
}

export function createContext<T>(defaultValue: T): ReactContext<T> {
  const ctx = krateCreateContext<T>(defaultValue);
  return {
    _id: ctx.id,
    defaultValue,
    Provider: (props) => ctx.Provider(props),
    Consumer: (props) => props.children(ctx.useContext()),
    useContext: ctx.useContext,
    use: ctx.useContext,
  };
}

export function useContext<T>(ctx: ReactContext<T>): T {
  return ctx.useContext();
}

// --- hooks ---

export function useState<T>(
  initial: T | (() => T)
): [() => T, (value: T | ((prev: T) => T)) => void] {
  const value = typeof initial === 'function' ? (initial as () => T)() : initial;
  return createSignal(value);
}

export function useReducer<S, A>(
  reducer: (state: S, action: A) => S,
  initial: S
): [() => S, (action: A) => void] {
  const [get, set] = createSignal(initial);
  const dispatch = (action: A): void => set((prev) => reducer(prev as S, action));
  return [get, dispatch];
}

export function useMemo<T>(factory: () => T, _deps?: unknown[]): T {
  return factory();
}

export function useCallback<T extends (...args: never[]) => unknown>(fn: T, _deps?: unknown[]): T {
  return fn;
}

export function useRef<T = unknown>(initial?: T): RefObject<T> {
  return { current: initial ?? null };
}

export function useEffect(effect: () => void | (() => void), _deps?: unknown[]): void {
  createEffect(effect);
}

export const useLayoutEffect = useEffect;
export const useInsertionEffect = useEffect;

/**
 * Attaches the handle returned by `create` to `ref` (object or callback ref,
 * with React 19 callback cleanup) and detaches it on unmount. With a deps
 * array (or deps thunk) the handle is re-created only when a dependency changes.
 */
export function useImperativeHandle<T>(
  ref: RefObject<T> | ((v: T | null) => unknown) | null | undefined,
  create: () => T,
  deps?: unknown[] | (() => unknown[])
): void {
  const readDeps = typeof deps === 'function' ? deps : deps ? () => deps : null;
  let prev: unknown[] | null = null;
  let undo: (() => void) | null = null;
  const run = (): void => {
    if (undo) undo();
    undo = ref ? krateSetRef(ref as KrateRef, create()) : null;
  };
  createEffect(() => {
    const next = readDeps ? readDeps() : null;
    if (prev && next && next.length === prev.length && next.every((v, i) => Object.is(v, prev![i]))) return;
    prev = next;
    run();
  });
  onCleanup(() => {
    if (undo) undo();
    undo = null;
  });
}

export function useDebugValue(_value?: unknown): void {
  // no-op
}

/**
 * `useSyncExternalStore` returning a snapshot getter. Snapshots are compared
 * with Object.is, a change between the first read and the subscription is
 * picked up, and the store is unsubscribed on teardown.
 */
export function useSyncExternalStore<T>(
  subscribe: (onChange: () => void) => (() => void) | void,
  getSnapshot: () => T,
  getServerSnapshot?: () => T
): () => T {
  return createExternalStore(subscribe, getSnapshot, getServerSnapshot);
}

/**
 * `[isPending, startTransition]`. isPending (a getter) stays true until the
 * action passed to startTransition settles, including async actions, and
 * until every overlapping transition started here settled.
 */
export function useTransition(): [() => boolean, (fn: () => unknown) => void] {
  return krateUseTransition();
}

export function useOptimistic<T>(
  initial: T,
  update?: (state: T, action: T) => T,
): [() => T, (action: T) => void] {
  const [state, setState] = createSignal(initial);
  const add = (action: T): void => {
    setState((prev) => (update ? update(prev, action) : action));
  };
  return [state, add];
}

export function useActionState<State, Payload>(
  action: (prev: State, payload: Payload) => State | Promise<State>,
  initial: State,
): [() => State, (payload: Payload) => Promise<void>, () => boolean] {
  const [state, setState] = createSignal(initial);
  const [pending, setPending] = createSignal(false);
  const dispatch = async (payload: Payload): Promise<void> => {
    setPending(true);
    try {
      setState(await action(state(), payload));
    } finally {
      setPending(false);
    }
  };
  return [state, dispatch, pending];
}

export function useFormStatus(): {
  pending: boolean;
  data: FormData | null;
  method: string | null;
  action: string | null;
} {
  return { pending: false, data: null, method: null, action: null };
}

/**
 * `useDeferredValue(value, initialValue?)`. A getter value is deferred: the
 * returned getter keeps the previous value during urgent updates and catches
 * up in a later task, starting from `initialValue` when given. A plain value
 * cannot change without a re-render in Krate and is returned as is.
 */
export function useDeferredValue<T>(value: T | (() => T), ...initial: [] | [T]): T | (() => T) {
  if (typeof value === 'function') return createDeferredValue(value as () => T, ...initial);
  return value;
}

/** Top-level `startTransition`; async actions keep the transition pending. */
export function startTransition(fn: () => unknown): void {
  krateStartTransition(fn);
}

/**
 * React 19 `use()`: reads a context, or unwraps a promise. A pending promise
 * suspends the calling component (it re-renders when the promise settles); a
 * rejected one throws its reason to the nearest error boundary. The same
 * promise object never suspends again once it resolved. Unlike hooks, `use`
 * may be called conditionally.
 */
export function use<T>(value: PromiseLike<T> | ReactContext<T>): T {
  const ctx = value as ReactContext<T>;
  if (ctx && typeof ctx.useContext === 'function') return ctx.useContext();
  if (value && typeof (value as PromiseLike<T>).then === 'function') return readValue<T>(value);
  throw new Error('An unsupported type was passed to use(): ' + String(value));
}

export function flushSync<T>(fn: () => T): T {
  return fn();
}

export function createPortal(children: unknown, container: Element | DocumentFragment): Element | DocumentFragment {
  // Handlers inside a portal outside the app root still need delegation.
  const root = typeof document !== 'undefined' ? document.getElementById('root') : null;
  if (container.nodeType === 1 && !(root && root.contains(container))) krateEventRoot(container);
  if (children instanceof Node) container.appendChild(children);
  else if (Array.isArray(children)) {
    for (const child of children) {
      if (child instanceof Node) container.appendChild(child);
      else if (child != null) container.appendChild(document.createTextNode(String(child)));
    }
  } else if (children != null) {
    container.appendChild(document.createTextNode(String(children)));
  }
  return container;
}

/** Minimal class base so compiled code that `extends React.Component` resolves. */
export class ComponentBase {
  props: Record<string, unknown>;
  state: Record<string, unknown>;
  constructor(props?: Record<string, unknown>) {
    this.props = props ?? {};
    this.state = {};
  }
  setState(update: unknown): void {
    const next =
      typeof update === 'function'
        ? (update as (s: Record<string, unknown>, p: Record<string, unknown>) => Record<string, unknown>)(
            this.state,
            this.props
          )
        : (update as Record<string, unknown>);
    this.state = { ...this.state, ...next };
  }
  render(): Node | null {
    return null;
  }
}

export const reactCompat = { h, Fragment };
