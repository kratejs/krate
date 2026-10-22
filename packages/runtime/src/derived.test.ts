import { describe, it, expect, beforeEach } from 'vitest';
import { createSignal, createEffect, disposeAll, krateDerived, krateHook, isDerivedGetter, isSignalGetter } from './signal';
import { createEffectDeps } from './effects';

const tick = () => new Promise((r) => setTimeout(r, 0));
// Lets the signal flush, the commit microtask and the passive macrotask run.
async function flush(): Promise<void> {
  for (let i = 0; i < 3; i++) await tick();
}

describe('krateDerived', () => {
  beforeEach(() => disposeAll());

  it('is lazy and recomputes only after a source changed', () => {
    const [n, setN] = createSignal(2);
    let runs = 0;
    const doubled = krateDerived(() => {
      runs++;
      return n() * 2;
    });
    expect(runs).toBe(0);
    expect(doubled()).toBe(4);
    expect(doubled()).toBe(4);
    expect(runs).toBe(1);
    setN(5);
    expect(runs).toBe(1);
    expect(doubled()).toBe(10);
    expect(runs).toBe(2);
  });

  it('is current synchronously after a write, before any flush', () => {
    const [raw, setRaw] = createSignal('12');
    const value = krateDerived(() => Number(raw()));
    expect(value()).toBe(12);
    setRaw('7');
    expect(value()).toBe(7);
  });

  it('chains derived values without glitches', async () => {
    const [items, setItems] = createSignal(['a', 'b']);
    const n = krateDerived(() => items().length);
    const label = krateDerived(() => (n() > 1 ? 'items' : 'item'));
    const seen: string[] = [];
    createEffect(() => {
      // Reads both the source and the derived chain: never an inconsistent pair.
      seen.push(`${items().length}:${n()} ${label()}`);
    });
    setItems(['a']);
    await flush();
    setItems(['a', 'b', 'c']);
    await flush();
    expect(seen).toEqual(['2:2 items', '1:1 item', '3:3 items']);
  });

  it('notifies readers when a source changes', async () => {
    const [q, setQ] = createSignal('');
    const shown = krateDerived(() => ['ada', 'grace'].filter((x) => x.includes(q())));
    const lengths: number[] = [];
    createEffect(() => {
      lengths.push(shown().length);
    });
    setQ('gr');
    await flush();
    setQ('zz');
    await flush();
    expect(lengths).toEqual([2, 1, 0]);
  });

  it('feeds effect dependency arrays with plain values (Object.is)', async () => {
    const [raw, setRaw] = createSignal('12');
    const value = krateDerived(() => Number(raw()));
    const log: string[] = [];
    createEffectDeps(
      () => {
        log.push(String(value()));
      },
      () => [value()],
    );
    await flush();
    setRaw('abc');
    await flush();
    setRaw('xyz'); // NaN again: Object.is(NaN, NaN), no re-run
    await flush();
    setRaw('7');
    await flush();
    expect(log).toEqual(['12', 'NaN', '7']);
  });

  it('is recognized as a reactive getter', () => {
    const d = krateDerived(() => 1);
    expect(isDerivedGetter(d)).toBe(true);
    expect(isSignalGetter(d)).toBe(true);
    expect(isDerivedGetter(() => 1)).toBe(false);
  });

  it('still answers after disposal (untracked)', () => {
    const [n, setN] = createSignal(1);
    const d = krateDerived(() => n() + 1);
    expect(d()).toBe(2);
    disposeAll();
    setN(4);
    expect(d()).toBe(5);
  });
});

describe('krateDerived render identity', () => {
  beforeEach(() => disposeAll());

  it('recomputes a render literal when any trigger changes', () => {
    const [page] = createSignal(1);
    const [theme, setTheme] = createSignal('light');
    const inline = krateDerived(
      () => ({ page: page() }),
      () => [page(), theme()],
    );
    const first = inline();
    expect(inline()).toBe(first);
    setTheme('dark');
    const second = inline();
    expect(second).not.toBe(first);
    expect(second).toEqual({ page: 1 });
  });
});

describe('krateHook', () => {
  beforeEach(() => disposeAll());

  it('passes a derived getter through', () => {
    const [n, setN] = createSignal(0);
    const result = krateDerived(() => ({ n: n() }));
    const h = krateHook(result);
    expect(h).toBe(result);
    setN(3);
    expect(h().n).toBe(3);
  });

  it('wraps a plain value (including a plain function)', () => {
    const fn = () => 42;
    expect(krateHook(fn)()).toBe(fn);
    expect(krateHook([1, 2])()).toEqual([1, 2]);
  });
});
