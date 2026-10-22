import { describe, it, expect, beforeEach } from 'vitest';
import { createSignal, createReducer, createEffect, disposeAll, krateHookState, krateFlushSync } from './signal';
import { createEffectDeps, krateOwner, krateMount, krateCondActivity, krateOnUnmount } from './effects';

const tick = () => new Promise((r) => setTimeout(r, 0));
async function settle(): Promise<void> {
  for (let i = 0; i < 4; i++) await tick();
}

// Runs fn as if inside a native click listener (a discrete event).
function inClick(fn: () => void): void {
  const btn = document.createElement('button');
  document.body.appendChild(btn);
  // Browsers expose the event being dispatched as window.event; happy-dom
  // does not, so provide it the way a browser would.
  btn.addEventListener('click', (e) => {
    const w = window as unknown as { event?: Event };
    const prev = w.event;
    w.event = e;
    try {
      fn();
    } finally {
      w.event = prev;
    }
  });
  btn.click();
  btn.remove();
}

describe('render snapshots for React state', () => {
  beforeEach(() => disposeAll());

  it('reads the committed value in handlers, so repeated setState(x + 1) adds one', async () => {
    const [count, setCount] = createSignal(0, krateHookState);
    inClick(() => {
      setCount(count() + 1);
      setCount(count() + 1);
      setCount(count() + 1);
    });
    await settle();
    expect(count()).toBe(1);
  });

  it('keeps functional updaters on the latest pending value', async () => {
    const [count, setCount] = createSignal(0, krateHookState);
    inClick(() => {
      setCount((c) => c + 1);
      setCount((c) => c + 1);
      expect(count()).toBe(0); // still the render's snapshot
    });
    await settle();
    expect(count()).toBe(2);
  });

  it('lets bindings (the render phase) see the latest value', async () => {
    const [count, setCount] = createSignal(0, krateHookState);
    const seen: number[] = [];
    createEffect(() => void seen.push(count()));
    inClick(() => setCount(5));
    await settle();
    expect(seen).toEqual([0, 5]);
  });

  it('runs cleanups with the values of the render their effect ran for', async () => {
    const [room, setRoom] = createSignal('general', krateHookState);
    const log: string[] = [];
    createEffectDeps(() => {
      log.push('connect ' + room());
      return () => log.push('disconnect ' + room());
    }, () => [room()]);
    await settle();
    inClick(() => setRoom('random'));
    await settle();
    inClick(() => setRoom('music'));
    await settle();
    expect(log).toEqual(['connect general', 'disconnect general', 'connect random', 'disconnect random', 'connect music']);
  });

  it('gives a second effect the committed value while the first sets state', async () => {
    const [text] = createSignal('hello', krateHookState);
    const [words, setWords] = createSignal(0, krateHookState);
    const log: string[] = [];
    createEffectDeps(() => {
      log.push('count ' + text());
      setWords(1);
    }, () => [text()]);
    createEffectDeps(() => void log.push('words ' + words()), () => [words()]);
    await settle();
    expect(log).toEqual(['count hello', 'words 0', 'words 1']);
  });

  it('does not change native signal semantics', () => {
    const [n, setN] = createSignal(0);
    setN(n() + 1);
    setN(n() + 1);
    expect(n()).toBe(2);
  });

  it('applies reducer options and the lazy initializer', async () => {
    const [s, dispatch] = createReducer((st: number, a: number) => st + a, 2, (x: number) => x * 10, krateHookState);
    expect(s()).toBe(20);
    inClick(() => {
      dispatch(1);
      expect(s()).toBe(20);
    });
    await settle();
    expect(s()).toBe(21);
  });
});

describe('update lanes', () => {
  beforeEach(() => disposeAll());

  it('flushes passive effects of a discrete update before the handler microtask', async () => {
    const [n, setN] = createSignal(0, krateHookState);
    const log: string[] = [];
    createEffectDeps(() => void log.push('passive ' + n()), () => [n()]);
    await settle();
    log.length = 0;
    inClick(() => {
      setN((v) => v + 1);
      queueMicrotask(() => log.push('microtask'));
    });
    await settle();
    expect(log).toEqual(['passive 1', 'microtask']);
  });

  it('commits a non-discrete update after the microtasks queued with it', async () => {
    const [n, setN] = createSignal(0, krateHookState);
    const log: string[] = [];
    createEffectDeps(() => void log.push('layout ' + n()), () => [n()], 'layout');
    await settle();
    log.length = 0;
    setTimeout(() => {
      setN(10);
      queueMicrotask(() => log.push('microtask'));
    }, 0);
    await settle();
    expect(log).toEqual(['microtask', 'layout 10']);
  });

  it('flushes synchronously on demand', () => {
    const [n, setN] = createSignal(0, krateHookState);
    const seen: number[] = [];
    createEffect(() => void seen.push(n()));
    setN(3);
    krateFlushSync();
    expect(seen).toEqual([0, 3]);
    expect(n()).toBe(3);
  });
});

describe('remountable conditional branches', () => {
  beforeEach(() => disposeAll());

  it('unmounts on hide and mounts a fresh instance on show', async () => {
    const log: string[] = [];
    const setters: Array<(v: number) => void> = [];
    const getters: Array<() => number> = [];
    const unmounted: Array<string | null> = [];
    krateOnUnmount((_owner, id) => unmounted.push(id));
    krateOwner('P', null);
    krateMount('C', 'P', [['5', 'c', 1]], null, () => {
      const [count, setCount] = createSignal(0, krateHookState);
      setters.push(setCount);
      getters.push(count);
      createEffectDeps(() => {
        log.push('mount ' + count());
        return () => log.push('unmount ' + count());
      }, () => []);
    });
    krateOwner(null);
    await settle();
    setters[0](2);
    await settle();
    expect(getters[0]()).toBe(2);
    krateCondActivity('5', 'a');
    await settle();
    krateCondActivity('5', 'c');
    await settle();
    expect(getters.length).toBe(2);
    expect(getters[1]()).toBe(0);
    expect(log).toEqual(['mount 0', 'unmount 0', 'mount 0']);
    expect(unmounted).toEqual(['C']);
  });

  it('defers a component in the initially hidden branch until it is shown', async () => {
    let runs = 0;
    krateOwner('P', null);
    krateCondActivity('9', 'c');
    krateMount('Alt', 'P', [['9', 'a', 0]], null, () => {
      runs++;
    });
    krateOwner(null);
    await settle();
    expect(runs).toBe(0);
    krateCondActivity('9', 'a');
    expect(runs).toBe(1);
  });

  it('remounts nested components under their fresh parent', async () => {
    const order: string[] = [];
    krateOwner('P', null);
    krateMount('C', 'P', [['5', 'c', 1]], null, () => void order.push('C'));
    krateMount('G', 'C', [['5', 'c', 1]], null, () => void order.push('G'));
    krateOwner(null);
    krateCondActivity('5', 'a');
    krateCondActivity('5', 'c');
    expect(order).toEqual(['C', 'G', 'C', 'G']);
  });

  it('disposes the bindings of an unmounted instance', async () => {
    const [n, setN] = createSignal(0);
    const seen: number[] = [];
    krateOwner('P', null);
    krateMount('C', 'P', [['5', 'c', 1]], null, () => {
      createEffect(() => void seen.push(n()));
    });
    krateOwner(null);
    krateCondActivity('5', 'a');
    setN(1);
    await settle();
    expect(seen).toEqual([0]);
  });
});
