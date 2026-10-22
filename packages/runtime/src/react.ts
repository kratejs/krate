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
 * behave as expected; scheduling APIs (`useTransition`, `startTransition`,
 * `useDeferredValue`) are best-effort. Class components are intentionally
 * minimal.
 */
import { h, type Component } from './dom.js';
import { createSignal, createEffect, createReducer, isSignalGetter } from './signal.js';
import { createEffectDeps, type EffectPhase } from './effects.js';
import { krateSetRef, type KrateRef } from './refs.js';
import { krateEventRoot } from './events.js';
import { createContext as krateCreateContext } from './context.js';
import { cloneElement as krateCloneElement } from './class-helpers.js';
import { jsx as krateJsx } from './jsx-runtime.js';
import {
  __krateActionState,
  __krateOptimistic,
  requestFormReset as krateRequestFormReset,
  type FormStatus,
} from './actions.js';

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
  return krateJsx(type as string | Component<Record<string, unknown>>, props);
}

export const jsx = krateJsx;
export const jsxs = krateJsx;

export function jsxDEV(
  type: unknown,
  props: Record<string, unknown> | null,
  _key?: unknown,
  _isStatic?: boolean,
  _source?: unknown,
  _self?: unknown
): Node {
  return krateJsx(type as string | Component<Record<string, unknown>>, props);
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

export function lazy(loader: unknown): unknown {
  // A synchronous passthrough: Krate cannot await a dynamic import at render
  // time. Dynamic/lazy components require the SSR/islands path.
  return loader;
}

export const StrictMode = Fragment;
export const Profiler = Fragment;
export const Suspense = Fragment;

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

export function useReducer<S, A, I = S>(
  reducer: (state: S, action: A) => S,
  initial: I,
  init?: (arg: I) => S
): [() => S, (action: A) => void] {
  // Stable dispatch, Object.is bail-out and the lazy initializer come from
  // createReducer.
  return createReducer(reducer, initial, init);
}

// Components run once, so a dependency array passed here is captured once.
// Elements that are signal getters (state from useState above) are read on
// every check, which keeps those dependencies live.
function depsThunk(deps?: readonly unknown[] | (() => unknown) | null): (() => unknown) | null {
  if (deps == null) return null;
  if (typeof deps === 'function') return deps;
  return () => deps.map((d) => (isSignalGetter(d) ? (d as () => unknown)() : d));
}

/**
 * Memoized value. The component body runs once, so the factory runs once and
 * the value is stable for the component's lifetime (React recomputes only
 * when a dependency changes, which a single run cannot observe).
 */
export function useMemo<T>(factory: () => T, _deps?: unknown[]): T {
  return factory();
}

/** Memoized callback: one identity for the component's lifetime. */
export function useCallback<T extends (...args: never[]) => unknown>(fn: T, _deps?: unknown[]): T {
  return fn;
}

export function useRef<T = unknown>(initial?: T): RefObject<T> {
  return { current: initial ?? null };
}

type EffectCallback = () => void | (() => void);

function effectHook(phase: EffectPhase) {
  return (effect: EffectCallback, deps?: readonly unknown[] | null): void => {
    createEffectDeps(effect, depsThunk(deps), phase);
  };
}

/** Passive effect: runs after paint, re-runs when a live dependency changes. */
export const useEffect: (effect: EffectCallback, deps?: readonly unknown[] | null) => void = effectHook('passive');
/** Layout effect: runs synchronously after the DOM is updated, before paint. */
export const useLayoutEffect: (effect: EffectCallback, deps?: readonly unknown[] | null) => void = effectHook('layout');
/** Insertion effect: runs before layout effects in the same commit. */
export const useInsertionEffect: (effect: EffectCallback, deps?: readonly unknown[] | null) => void = effectHook('insertion');

export function useImperativeHandle<T>(
  ref: RefObject<T> | ((v: T | null) => unknown) | null | undefined,
  create: () => T,
  deps?: readonly unknown[] | (() => unknown) | null
): void {
  // React implements handles as layout effects: attached (object or callback
  // ref, with React 19 callback cleanup) in the layout phase, re-created when
  // a dependency changes, detached on cleanup.
  createEffectDeps(() => (ref ? krateSetRef(ref as KrateRef, create()) : undefined), depsThunk(deps), 'layout');
}

export function useDebugValue(_value?: unknown): void {
  // no-op
}

export function useSyncExternalStore<T>(
  subscribe: (onChange: () => void) => () => void,
  getSnapshot: () => T
): () => T {
  const [get, set] = createSignal(getSnapshot());
  createEffect(() => {
    const unsubscribe = subscribe(() => set(getSnapshot()));
    return () => unsubscribe();
  });
  return get;
}

export function useTransition(): [() => boolean, (fn: () => void) => void] {
  const [pending, setPending] = createSignal(false);
  const start = (fn: () => void): void => {
    setPending(true);
    try {
      fn();
    } finally {
      setPending(false);
    }
  };
  return [pending, start];
}

/**
 * React 19 `useOptimistic`. `state` may be a value or a getter; optimistic
 * updates apply on top of it while an action is pending and are dropped when
 * the action settles.
 */
export function useOptimistic<T, A = T>(
  state: T | (() => T),
  update?: (state: T, action: A) => T,
): [() => T, (action: A) => void] {
  const base = typeof state === 'function' ? (state as () => T) : () => state as T;
  const [value, setValue] = createSignal(base());
  return [value, __krateOptimistic<T, A>(base, (v) => setValue(() => v), update)];
}

/**
 * React 19 `useActionState`: returns `[state, dispatch, isPending]` getters and
 * a dispatch usable as a form action. Calls are queued in order and each
 * receives the previous state; server actions receive it as their first
 * argument over the action transport.
 */
export function useActionState<State, Payload>(
  action: (prev: State, payload: Payload) => State | Promise<State>,
  initial: State,
  _permalink?: string,
): [() => State, (payload: Payload) => void, () => boolean] {
  const [state, setState] = createSignal(initial);
  const [pending, setPending] = createSignal(false);
  const dispatch = __krateActionState<State>(
    action as (prev: State, payload: unknown) => State | Promise<State>,
    state,
    (v) => setState(() => v),
    setPending,
  );
  return [state, dispatch as (payload: Payload) => void, pending];
}

/**
 * React DOM `useFormStatus`. Source components are lowered by the compiler to
 * a form-scoped status bound to the component's element; called from compiled
 * code without an element, it reports an idle status.
 */
export function useFormStatus(): FormStatus {
  return { pending: false, data: null, method: null, action: null };
}

/** React DOM `requestFormReset(form)`. */
export const requestFormReset = krateRequestFormReset;

export function useDeferredValue<T>(value: T): T {
  return value;
}

export function startTransition(fn: () => void): void {
  fn();
}

export function use<T>(value: T | ReactContext<T>): T {
  const ctx = value as ReactContext<T>;
  if (ctx && typeof ctx.useContext === 'function') return ctx.useContext();
  return value as T;
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
