import { describe, it, expect, beforeEach } from 'vitest';
import {
  createSignal,
  createEffect,
  createMemo,
  onCleanup,
  disposeAll,
} from './signal';

const flush = () => new Promise((r) => setTimeout(r, 0));

describe('createSignal', () => {
  beforeEach(() => disposeAll());

  it('reads and writes values', () => {
    const [n, setN] = createSignal(1);
    expect(n()).toBe(1);
    setN(2);
    expect(n()).toBe(2);
    setN((p) => p + 3);
    expect(n()).toBe(5);
  });

  it('does not notify when the value is unchanged', async () => {
    const [n, setN] = createSignal(1);
    let runs = 0;
    createEffect(() => {
      n();
      runs++;
    });
    await flush();
    const before = runs;
    setN(1);
    await flush();
    expect(runs).toBe(before);
  });

  it('runs dependent effects with the new value', async () => {
    const [n, setN] = createSignal(0);
    let seen = -1;
    createEffect(() => {
      seen = n();
    });
    setN(7);
    await flush();
    expect(seen).toBe(7);
  });
});

describe('createMemo', () => {
  beforeEach(() => disposeAll());

  it('caches until a dependency changes', async () => {
    const [n, setN] = createSignal(2);
    let computations = 0;
    const doubled = createMemo(() => {
      computations++;
      return n() * 2;
    });
    await flush();
    expect(doubled()).toBe(4);
    expect(doubled()).toBe(4);
    expect(computations).toBe(1);
    setN(5);
    await flush();
    expect(doubled()).toBe(10);
  });
});

describe('onCleanup / disposeAll', () => {
  it('runs cleanups when the world is disposed', async () => {
    let cleaned = false;
    createEffect(() => {
      onCleanup(() => {
        cleaned = true;
      });
    });
    await flush();
    disposeAll();
    expect(cleaned).toBe(true);
  });
});
