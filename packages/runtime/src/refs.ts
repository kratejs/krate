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

/**
 * Registers an element ref. The ref is attached now when `now` is true or the
 * element is in the document; otherwise it waits until a conditional branch
 * holding the element is shown. Returns the release function (detach plus
 * unregister), safe to call more than once.
 */
export function krateAttachRef(el: Element, ref: KrateRef, assign?: (v: unknown) => void, now?: boolean): () => void {
  const e: RefEntry = { el, ref, assign, on: false, undo: null };
  live.add(e);
  if (now || el.isConnected) attach(e);
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
