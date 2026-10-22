/**
 * React ref semantics for elements and imperative handles.
 *
 * - Object refs get `current = node` on attach and `current = null` on detach.
 * - Callback refs are called with the node on attach. On detach, a React 19
 *   cleanup function returned by the callback is called instead; otherwise the
 *   callback is called with null.
 * - A ref that is neither (a plain variable target) uses the `assign` setter.
 *
 * Every attached element ref is registered so a conditional branch swapped out
 * of the document detaches its refs and swapping it back re-attaches them
 * (krateRefsLeave / krateRefsEnter). Refs are called untracked, so a callback
 * reading signals never subscribes the surrounding effect.
 */
import { createEffect } from './signal.js';

export type KrateRef = ((value: unknown) => unknown) | { current: unknown } | null | undefined;

interface RefEntry {
  el: unknown;
  ref: KrateRef;
  assign?: (v: unknown) => void;
  on: boolean;
  undo: (() => void) | null;
}

const live = new Set<RefEntry>();

function untracked(fn: () => void): void {
  // A throwaway effect gives fn its own (discarded) tracking scope.
  let err: unknown;
  let failed = false;
  createEffect(() => {
    try {
      fn();
    } catch (x) {
      err = x;
      failed = true;
    }
  })();
  if (failed) throw err;
}

function attach(e: RefEntry): void {
  if (e.on) return;
  e.on = true;
  const r = e.ref;
  untracked(() => {
    if (typeof r === 'function') {
      const ret = r(e.el);
      e.undo = typeof ret === 'function' ? (ret as () => void) : () => r(null);
    } else if (r && typeof r === 'object') {
      r.current = e.el;
      e.undo = () => {
        r.current = null;
      };
    } else if (e.assign) {
      const set = e.assign;
      set(e.el);
      e.undo = () => set(null);
    } else {
      e.undo = null;
    }
  });
}

function detach(e: RefEntry): void {
  if (!e.on) return;
  e.on = false;
  const undo = e.undo;
  e.undo = null;
  if (undo) untracked(undo);
}

/**
 * Attaches `value` (an element or an imperative handle) to `ref` and returns
 * the detach function. Used by useImperativeHandle.
 */
export function krateSetRef(ref: KrateRef, value: unknown): () => void {
  const e: RefEntry = { el: value, ref, on: false, undo: null };
  attach(e);
  return () => detach(e);
}

// Hydration refs waiting for krateRefsFlush; null when no hydration script is
// holding them.
let deferred: RefEntry[] | null = null;

/**
 * Starts holding hydration refs (called at the start of a page's hydration
 * script) until krateRefsFlush. Components mounted later attach at once.
 */
export function krateRefsHold(): void {
  if (deferred) return;
  deferred = [];
  // Safety net if the script stops before its flush.
  queueMicrotask(krateRefsFlush);
}

/**
 * Attaches the hydration refs queued so far whose element is in the
 * document. The hydration script calls it after every component has bound its
 * DOM (and before the effect commit), so a ref on server-rendered content that
 * a binding replaced (a re-rendered list) is never attached, as in React.
 */
export function krateRefsFlush(): void {
  const list = deferred;
  deferred = null;
  if (!list) return;
  for (const e of list) {
    if (live.has(e) && (e.el as Node).isConnected) attach(e);
  }
}

/**
 * Registers an element ref. mode 'now' attaches immediately (a node built by
 * h() that is about to be inserted); 'defer' queues it for krateRefsFlush
 * while a hydration script holds refs (krateRefsHold); otherwise it attaches when the element is in the document. A
 * ref on a detached node waits until a conditional branch holding it is
 * shown. Returns the release function (detach plus unregister), safe to call
 * more than once.
 */
export function krateAttachRef(el: Element, ref: KrateRef, assign?: (v: unknown) => void, mode?: 'now' | 'defer'): () => void {
  const e: RefEntry = { el, ref, assign, on: false, undo: null };
  live.add(e);
  if (mode === 'defer' && deferred) {
    deferred.push(e);
  } else if (mode === 'now' || el.isConnected) {
    attach(e);
  }
  return () => {
    live.delete(e);
    detach(e);
  };
}

/** Detaches refs of registered elements inside `root` (a branch leaving the document). */
export function krateRefsLeave(root: Node): void {
  for (const e of live) {
    if (e.on && root.contains(e.el as Node)) detach(e);
  }
}

/** Attaches registered refs whose element is now in the document. */
export function krateRefsEnter(): void {
  for (const e of live) {
    if (!e.on && (e.el as Node).isConnected) attach(e);
  }
}
