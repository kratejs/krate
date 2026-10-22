import { afterEach, describe, expect, it, vi } from 'vitest';
import { createSignal } from './signal.js';
import {
  __krateBeginAction,
  isActionPending,
  runActionTransition,
  setActionTransitionRunner,
} from './actions.js';
import { createOwner, runWithOwner, setBoundary } from './boundary.js';
import {
  __krateBoundary,
  __krateRead,
  __krateTransitionStarter,
  connectErrorBoundaries,
  connectActionTransitions,
  isInTransition,
  setSuspenseErrorHandler,
} from './suspense.js';

afterEach(() => {
  setActionTransitionRunner(null);
  connectErrorBoundaries(null);
  setSuspenseErrorHandler(null);
  vi.useRealTimers();
});

describe('one transition scope for transitions and actions', () => {
  it('keeps the action scope open while a useTransition async action runs', async () => {
    vi.useFakeTimers();
    connectActionTransitions({ setActionTransitionRunner, __krateBeginAction });
    const [pending, setPending] = createSignal(false);
    const start = __krateTransitionStarter(setPending);
    start(() => new Promise((r) => setTimeout(r, 100)));
    expect(isActionPending()).toBe(true);
    await vi.advanceTimersByTimeAsync(110);
    expect(pending()).toBe(false);
    expect(isActionPending()).toBe(false);
  });

  it('runs form actions as transitions with their value and error', async () => {
    connectActionTransitions({ setActionTransitionRunner, __krateBeginAction });
    const reported: unknown[] = [];
    setSuspenseErrorHandler((e) => {
      reported.push(e);
      return true;
    });
    let inTransition = false;
    const value = await runActionTransition(async () => {
      inTransition = isInTransition();
      return 42;
    });
    expect(value).toBe(42);
    expect(inTransition).toBe(true);
    await expect(runActionTransition(async () => Promise.reject(new Error('nope')))).rejects.toThrow('nope');
    await expect(
      runActionTransition(() => {
        throw new Error('sync');
      }),
    ).rejects.toThrow('sync');
    // The action runtime reports action errors; the transition does not again.
    expect(reported).toHaveLength(0);
    expect(isActionPending()).toBe(false);
  });
});

describe('error boundaries', () => {
  it('routes a rejected use() promise to the error boundary of the component', async () => {
    const { getOwner, handleError } = await import('./boundary.js');
    connectErrorBoundaries({ getOwner, handleError: (e, o) => handleError(e, o as Parameters<typeof handleError>[1]) });
    const caught: unknown[] = [];
    const parent = createOwner('Boundary', null);
    setBoundary(parent, (error) => {
      caught.push(error);
      return true;
    });
    const child = createOwner('Child', parent);
    let reject!: (e: unknown) => void;
    const p = new Promise((_, rej) => {
      reject = rej;
    });
    runWithOwner(child, () => {
      __krateBoundary(null, () => {
        __krateRead(p);
      });
    });
    reject(new Error('boom'));
    for (let i = 0; i < 20; i++) await Promise.resolve();
    expect(caught.map((e) => (e as Error).message)).toEqual(['boom']);
  });
});
