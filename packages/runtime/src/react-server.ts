/**
 * The `react` module of a server bundle (an SSR page or server component).
 *
 * The compiler maps `import { ... } from "react"` in a server bundle onto this
 * module. It exports the part of React's API that has a meaning while a server
 * renders: effects are no-ops (React's server renderer never runs them),
 * `cache` memoizes per request, and the transition/optimistic/action hooks
 * return their initial state. Names that would behave differently from React
 * here (useState, useRef, createContext, ...) are not exported, so importing
 * them fails the build instead of silently misbehaving.
 */
import { Suspense, Fragment, jsx, createSignal, createEffect, createMemo, onCleanup, onMount } from './server.js';
import { currentRenderScope, type CacheScope } from './cache-scope.js';

export { Suspense, Fragment, createSignal, createEffect, createMemo, onCleanup, onMount };

type AnyFn = (...args: any[]) => any;

/** Effects never run while a server renders. */
export function useEffect(_effect: () => unknown, _deps?: readonly unknown[] | null): void {}
export function useLayoutEffect(_effect: () => unknown, _deps?: readonly unknown[] | null): void {}
export function useInsertionEffect(_effect: () => unknown, _deps?: readonly unknown[] | null): void {}

export function createElement(type: any, props?: Record<string, any> | null, ...children: unknown[]): unknown {
  const p: Record<string, any> = { ...(props || {}) };
  delete p.key;
  delete p.ref;
  if (children.length === 1) p.children = children[0];
  else if (children.length > 1) p.children = children;
  return jsx(type, p);
}

export function forwardRef<P = any>(render: (props: P, ref: unknown) => unknown): (props: P) => unknown {
  return (props: P) => render(props, null);
}

export function memo<C>(component: C, _compare?: unknown): C {
  return component;
}

export function startTransition(fn: () => unknown): void {
  fn();
}

export function useTransition(): [boolean, (fn: () => unknown) => void] {
  return [false, startTransition];
}

export function useOptimistic<T>(passthrough: T, _reducer?: unknown): [T, (action: unknown) => void] {
  return [passthrough, () => {}];
}

export function useActionState<S>(_action: unknown, initialState: S, _permalink?: string): [S, () => void, boolean] {
  return [initialState, () => {}, false];
}

let idCounter = 0;

/** Ids are unique within the process; server-rendered output is never hydrated by React. */
export function useId(): string {
  return ':R' + (idCounter++).toString(36) + ':';
}

/**
 * use(promise) cannot suspend a synchronous server render. A promise that
 * already settled through React's own bookkeeping is read; anything else
 * points the author at an async component.
 */
export function use<T>(usable: PromiseLike<T> | { _currentValue?: T; defaultValue?: T }): T {
  const u = usable as any;
  if (u && typeof u.then === 'function') {
    if (u.status === 'fulfilled') return u.value as T;
    if (u.status === 'rejected') throw u.reason;
    throw new Error('use(promise) cannot wait for a promise while a Krate server renders. Make the component async and await the promise.');
  }
  if (u && ('_currentValue' in u || 'defaultValue' in u)) return (u._currentValue ?? u.defaultValue) as T;
  throw new Error('use() received something that is neither a promise nor a context.');
}

interface CallNode {
  done: boolean;
  value?: unknown;
  error?: unknown;
  failed?: boolean;
  next?: Map<unknown, CallNode>;
}

const perRequest = new WeakMap<CacheScope, WeakMap<AnyFn, CallNode>>();

/**
 * React's cache(): memoizes fn by argument identity for the request being
 * rendered. Results and thrown errors are both remembered, so a promise
 * returned by fn is shared by every caller in the request. Outside a render
 * (an API route, module scope) there is no request to scope the memo to, so
 * fn runs on every call, as in React.
 */
export function cache<F extends AnyFn>(fn: F): F {
  const wrapped: AnyFn = function (this: unknown, ...args: unknown[]) {
    const scope = currentRenderScope();
    if (!scope) return fn.apply(this, args);
    let byFn = perRequest.get(scope);
    if (!byFn) {
      byFn = new WeakMap();
      perRequest.set(scope, byFn);
    }
    let node = byFn.get(wrapped);
    if (!node) {
      node = { done: false };
      byFn.set(wrapped, node);
    }
    for (const arg of args) {
      if (!node.next) node.next = new Map();
      let child = node.next.get(arg);
      if (!child) {
        child = { done: false };
        node.next.set(arg, child);
      }
      node = child;
    }
    if (node.done) {
      if (node.failed) throw node.error;
      return node.value;
    }
    try {
      node.value = fn.apply(this, args);
    } catch (e) {
      node.failed = true;
      node.error = e;
      node.done = true;
      throw e;
    }
    node.done = true;
    return node.value;
  };
  return wrapped as unknown as F;
}

export default {
  Suspense, Fragment, createElement, forwardRef, memo, cache, use, useId, startTransition, useTransition,
  useOptimistic, useActionState, useEffect, useLayoutEffect, useInsertionEffect,
};
