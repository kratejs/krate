import { afterEach, describe, expect, it, vi } from 'vitest';
import { createSignal } from './signal.js';
import {
  __krateBeginAction,
  isActionPending,
  runActionTransition,
  setActionTransitionRunner,
} from './actions.js';
import {
  __krateTransitionStarter,
  connectActionTransitions,
  isInTransition,
  setSuspenseErrorHandler,
} from './suspense.js';

afterEach(() => {
  setActionTransitionRunner(null);
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
