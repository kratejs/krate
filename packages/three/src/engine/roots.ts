// The registry of mounted roots, keyed by canvas.

import type { TrackedPromise } from './promise.js';
import type { AppliedConfiguration } from './configuration.js';
import type { RootStore } from './store.js';

export interface OffscreenCanvasLike extends EventTarget {}

/** open, then closing (cancellable by configure/render), then disposed. */
export type RootStateMachine = { status: 'open' } | { status: 'closing'; token: symbol } | { status: 'disposed' };

export interface Root {
  store: RootStore;
  state: RootStateMachine;
  /** Settles once all accepted configuration has been applied */
  ready: TrackedPromise<unknown>;
  configuration: AppliedConfiguration;
  /** Whether the root built `gl` and so disposes it on unmount */
  ownsRenderer: boolean;
  /** Stops following devicePixelRatio changes */
  unwatchDpr?: () => void;
  /** Tears down the rendered tree */
  unrender?: () => void;
}

export const _roots = new Map<HTMLCanvasElement | OffscreenCanvasLike, Root>();
