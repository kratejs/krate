import { describe, it, expect, beforeEach } from 'vitest';
import { createSignal, createEffect, createMemo, disposeAll, krateFlushSync, onCleanup } from './signal';

const tick = () => new Promise((r) => setTimeout(r, 0));

describe('effect update loops', () => {
  beforeEach(() => disposeAll());

  it('throws Maximum update depth exceeded for two effects that feed each other', () => {
    const [a, setA] = createSignal(0);
    const [b, setB] = createSignal(0);
    createEffect(() => setB(a() + 1));
    createEffect(() => setA(b() + 1));
    expect(() => krateFlushSync()).toThrow(/Maximum update depth exceeded/);
    // The queue was dropped: nothing is pending and later writes still work.
    expect(() => krateFlushSync()).not.toThrow();
    let seen = -1;
    createEffect(() => {
      seen = 1;
    });
    expect(seen).toBe(1);
  });

  it('keeps the event loop responsive after the loop is reported', async () => {
    const [a, setA] = createSignal(0);
    const [b, setB] = createSignal(0);
    createEffect(() => setB(a() + 1));
    createEffect(() => setA(b() + 1));
    try {
      krateFlushSync();
    } catch {
      // reported
    }
    let ticked = false;
    setTimeout(() => (ticked = true), 0);
    await tick();
    expect(ticked).toBe(true);
  });

  it('names the looping effect in the message', () => {
    const [a, setA] = createSignal(0);
    createEffect(function bumpCounter() {
      setA(a() + 1);
    });
    // A self-write only reruns once; add a partner to make it loop.
    const [b, setB] = createSignal(0);
    createEffect(function ping() {
      setB(a() + 1);
    });
    createEffect(function pong() {
      setA(b() + 1);
    });
    expect(() => krateFlushSync()).toThrow(/ping|pong|bumpCounter/);
  });

  it('still settles a long but finite cascade', () => {
    const sigs = Array.from({ length: 30 }, () => createSignal(0));
    for (let i = 1; i < sigs.length; i++) {
      createEffect(() => sigs[i][1](sigs[i - 1][0]() + 1));
    }
    sigs[0][1](1);
    krateFlushSync();
    expect(sigs[29][0]()).toBe(30);
  });
});

describe('effect dependency tracking', () => {
  beforeEach(() => disposeAll());

  it('drops dependencies the last run did not read', () => {
    const [cond, setCond] = createSignal(true);
    const [x, setX] = createSignal(0);
    const [y] = createSignal(0);
    let runs = 0;
    createEffect(() => {
      runs++;
      cond() ? x() : y();
    });
    expect(runs).toBe(1);
    setCond(false);
    krateFlushSync();
    expect(runs).toBe(2);
    setX(5);
    krateFlushSync();
    setX(6);
    krateFlushSync();
    expect(runs).toBe(2);
  });
});

describe('nested effects', () => {
  beforeEach(() => disposeAll());

  it('disposes an inner effect when the parent effect re-runs', () => {
    const [a, setA] = createSignal(0);
    const [b, setB] = createSignal(0);
    let innerRuns = 0;
    createEffect(() => {
      a();
      createEffect(() => {
        b();
        innerRuns++;
      });
    });
    expect(innerRuns).toBe(1);
    setA(1);
    krateFlushSync();
    setA(2);
    krateFlushSync();
    expect(innerRuns).toBe(3);
    innerRuns = 0;
    setB(1);
    krateFlushSync();
    expect(innerRuns).toBe(1);
  });

  it('runs the inner effect cleanup when the parent re-runs', () => {
    const [a, setA] = createSignal(0);
    const log: string[] = [];
    createEffect(() => {
      const v = a();
      createEffect(() => {
        onCleanup(() => log.push('clean' + v));
      });
    });
    setA(1);
    krateFlushSync();
    expect(log).toEqual(['clean0']);
  });
});

describe('createMemo', () => {
  beforeEach(() => disposeAll());

  it('is current on a synchronous read after a write', () => {
    const [a, setA] = createSignal(1);
    const m = createMemo(() => a() * 10);
    expect(m()).toBe(10);
    setA(5);
    expect(m()).toBe(50);
  });

  it('memoizes between writes and propagates to effects once', () => {
    const [a, setA] = createSignal(1);
    let computes = 0;
    const m = createMemo(() => {
      computes++;
      return a() % 2;
    });
    let runs = 0;
    createEffect(() => {
      m();
      runs++;
    });
    expect(m()).toBe(1);
    expect(m()).toBe(1);
    expect(computes).toBe(1);
    setA(3);
    krateFlushSync();
    expect(runs).toBe(1);
    setA(4);
    krateFlushSync();
    expect(runs).toBe(2);
  });
});
