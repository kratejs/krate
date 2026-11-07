import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  createSignal,
  createEffect,
  onCleanup,
  disposeAll,
  krateFlushSync,
  krateOnSettle,
  setOwnerHooks,
} from './signal';

// Settle hooks cannot be unregistered; this one counts flushes for the whole file.
let settleRuns = 0;
let settleThrows = false;
let settleAfterThrow = 0;
krateOnSettle(() => {
  settleRuns++;
});
krateOnSettle(() => {
  if (settleThrows) throw new Error('settle boom');
});
krateOnSettle(() => {
  settleAfterThrow++;
});

describe('a throwing effect does not strand the others', () => {
  beforeEach(() => {
    disposeAll();
    settleRuns = 0;
    settleThrows = false;
    settleAfterThrow = 0;
  });
  afterEach(() => {
    setOwnerHooks(null);
    vi.restoreAllMocks();
  });

  it('runs the effects queued behind it, now and on later updates', () => {
    const [a, setA] = createSignal(0);
    const seen: number[] = [];
    createEffect(() => {
      if (a() > 0) throw new Error('boom ' + a());
    });
    createEffect(() => {
      seen.push(a());
    });
    expect(seen).toEqual([0]);

    setA(1);
    expect(() => krateFlushSync()).toThrow('boom 1');
    expect(seen).toEqual([0, 1]);

    // The second effect was not left marked as queued: it still reacts.
    setA(2);
    expect(() => krateFlushSync()).toThrow('boom 2');
    expect(seen).toEqual([0, 1, 2]);
  });

  it('keeps an effect before and after the thrower working', () => {
    const [a, setA] = createSignal(0);
    const log: string[] = [];
    createEffect(() => log.push('first ' + a()));
    createEffect(() => {
      if (a() === 1) throw new Error('middle');
    });
    createEffect(() => log.push('last ' + a()));
    setA(1);
    expect(() => krateFlushSync()).toThrow('middle');
    setA(2);
    krateFlushSync();
    expect(log).toEqual(['first 0', 'last 0', 'first 1', 'last 1', 'first 2', 'last 2']);
  });

  it('rethrows the first error and logs the others', () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => {});
    const [a, setA] = createSignal(0);
    createEffect(() => {
      if (a() > 0) throw new Error('one');
    });
    createEffect(() => {
      if (a() > 0) throw new Error('two');
    });
    setA(1);
    expect(() => krateFlushSync()).toThrow('one');
    expect(logged).toHaveBeenCalledTimes(1);
    expect(String((logged.mock.calls[0][0] as Error).message)).toBe('two');
  });

  it('still runs the settle hooks before rethrowing', () => {
    const [a, setA] = createSignal(0);
    createEffect(() => {
      if (a() > 0) throw new Error('boom');
    });
    setA(1);
    const before = settleRuns;
    expect(() => krateFlushSync()).toThrow('boom');
    expect(settleRuns).toBe(before + 1);
  });

  it('reports a throw from a settle hook after running the others', () => {
    settleThrows = true;
    const [a, setA] = createSignal(0);
    createEffect(() => void a());
    setA(1);
    expect(() => krateFlushSync()).toThrow('settle boom');
    expect(settleAfterThrow).toBe(1);
  });

  it('hands owned errors to the boundary and does not throw', () => {
    const handled: unknown[] = [];
    setOwnerHooks({
      current: () => 'boundary',
      run: (_o, fn) => fn(),
      cleanup: () => {},
      error: (_o, err) => {
        handled.push(err);
        return true;
      },
    });
    const [a, setA] = createSignal(0);
    const seen: number[] = [];
    createEffect(() => {
      if (a() > 0) throw new Error('owned');
    });
    createEffect(() => {
      seen.push(a());
    });
    setA(1);
    expect(() => krateFlushSync()).not.toThrow();
    expect(handled).toHaveLength(1);
    expect(seen).toEqual([0, 1]);
  });

  it('rethrows an owned error the boundary declines', () => {
    setOwnerHooks({
      current: () => 'boundary',
      run: (_o, fn) => fn(),
      cleanup: () => {},
      error: () => false,
    });
    const [a, setA] = createSignal(0);
    const seen: number[] = [];
    createEffect(() => {
      if (a() > 0) throw new Error('declined');
    });
    createEffect(() => {
      seen.push(a());
    });
    setA(1);
    expect(() => krateFlushSync()).toThrow('declined');
    expect(seen).toEqual([0, 1]);
  });

  it('reports the effect error first when an update loop aborts the same flush', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    const [a, setA] = createSignal(0);
    const [b, setB] = createSignal(0);
    createEffect(() => {
      if (a() > 0) throw new Error('thrower');
    });
    createEffect(() => setB(a() + 1));
    createEffect(() => setA(b() + 1));
    expect(() => krateFlushSync()).toThrow('thrower');
    // The loop was dropped: the queue is empty and effects still react.
    const seen: number[] = [];
    const [c, setC] = createSignal(0);
    createEffect(() => {
      seen.push(c());
    });
    setC(5);
    krateFlushSync();
    expect(seen).toEqual([0, 5]);
  });
});

describe('a throwing cleanup does not strand the others', () => {
  beforeEach(() => disposeAll());
  afterEach(() => vi.restoreAllMocks());

  it('runs the remaining cleanups of an effect that re-runs', () => {
    const [a, setA] = createSignal(0);
    const log: string[] = [];
    createEffect(() => {
      a();
      onCleanup(() => {
        log.push('first');
        throw new Error('cleanup boom');
      });
      onCleanup(() => log.push('second'));
    });
    setA(1);
    expect(() => krateFlushSync()).toThrow('cleanup boom');
    expect(log).toEqual(['first', 'second']);
  });

  it('keeps tearing down the page when a cleanup throws', () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => {});
    const log: string[] = [];
    createEffect(() => {
      onCleanup(() => {
        log.push('a');
        throw new Error('teardown boom');
      });
      onCleanup(() => log.push('a2'));
    });
    createEffect(() => {
      onCleanup(() => log.push('b'));
    });
    expect(() => disposeAll()).not.toThrow();
    expect(log).toEqual(['a', 'a2', 'b']);
    expect(logged).toHaveBeenCalledTimes(1);
  });
});
