import { describe, it, expect, beforeEach } from 'vitest';
import { createSignal, createReducer, createEffect, disposeAll, krateFlushSync } from './signal';
import {
  createEffectDeps,
  createMemoDeps,
  createCallbackDeps,
  krateOwner,
  krateCondActivity,
  krateSlotReplaced,
  krateDepsOf,
  flushPassive,
} from './effects';
import { h, disposeNode } from './dom';

const tick = () => new Promise((r) => setTimeout(r, 0));
const microtask = () => Promise.resolve();
// Lets the commit microtask and the passive macrotask run.
async function settle(): Promise<void> {
  for (let i = 0; i < 3; i++) await tick();
}

// A component that logs every effect phase for deps().
function logged(log: string[], name: string, deps: () => unknown[]): void {
  createEffectDeps(
    () => {
      log.push(`${name} insertion`);
      return () => log.push(`${name} insertion cleanup`);
    },
    deps,
    'insertion',
  );
  createEffectDeps(
    () => {
      log.push(`${name} layout`);
      return () => log.push(`${name} layout cleanup`);
    },
    deps,
    'layout',
  );
  createEffectDeps(() => {
    log.push(`${name} passive`);
    return () => log.push(`${name} passive cleanup`);
  }, deps);
}

describe('effect phases and ordering', () => {
  beforeEach(() => disposeAll());

  it('mounts insertion, layout, then passive effects, children before parents', async () => {
    const log: string[] = [];
    const Child = () => {
      logged(log, 'child', () => []);
      return h('i', null);
    };
    const Parent = () => {
      logged(log, 'parent', () => []);
      return h('div', null, h(Child, null));
    };
    h(Parent, null);
    expect(log).toEqual([]);
    await microtask();
    // Layout work is done before the next macrotask (before paint).
    expect(log).toEqual(['child insertion', 'parent insertion', 'child layout', 'parent layout']);
    await settle();
    expect(log).toEqual([
      'child insertion',
      'parent insertion',
      'child layout',
      'parent layout',
      'child passive',
      'parent passive',
    ]);
  });

  it('runs siblings in document order and nested children post-order', async () => {
    const log: string[] = [];
    const Leaf = (p: Record<string, unknown>) => {
      createEffectDeps(() => void log.push(String(p.name)), () => []);
      return h('i', null);
    };
    const Mid = (p: Record<string, unknown>) => {
      createEffectDeps(() => void log.push(String(p.name)), () => []);
      return h('b', null, h(Leaf, { name: String(p.name) + '.leaf' }));
    };
    const Root = () => {
      createEffectDeps(() => void log.push('root'), () => []);
      return h('div', null, h(Mid, { name: 'a' }), h(Mid, { name: 'b' }));
    };
    h(Root, null);
    await settle();
    expect(log).toEqual(['a.leaf', 'a', 'b.leaf', 'b', 'root']);
  });

  it('re-runs with cleanups in React commit order on update', async () => {
    const log: string[] = [];
    const [n, setN] = createSignal(0);
    const Child = () => {
      logged(log, 'child', () => [n()]);
      return h('i', null);
    };
    const Parent = () => {
      logged(log, 'parent', () => [n()]);
      return h('div', null, h(Child, null));
    };
    h(Parent, null);
    await settle();
    log.length = 0;
    setN(1);
    await settle();
    expect(log).toEqual([
      'child insertion cleanup',
      'child insertion',
      'child layout cleanup',
      'parent insertion cleanup',
      'parent insertion',
      'parent layout cleanup',
      'child layout',
      'parent layout',
      'child passive cleanup',
      'parent passive cleanup',
      'child passive',
      'parent passive',
    ]);
  });

  it('runs layout effects after DOM bindings of the same update', async () => {
    const [n, setN] = createSignal(0);
    const el = h('p', null, () => String(n())) as HTMLElement;
    const seen: string[] = [];
    createEffectDeps(() => void seen.push(el.textContent || ''), () => [n()], 'layout');
    await settle();
    setN(5);
    await settle();
    expect(seen).toEqual(['0', '5']);
  });

  it('flushes pending passive effects before the next commit', async () => {
    const log: string[] = [];
    const [n, setN] = createSignal(0);
    createEffectDeps(() => {
      log.push(`layout ${n()}`);
      if (n() === 0) setN(1);
    }, () => [n()], 'layout');
    createEffectDeps(() => void log.push(`passive ${n()}`), () => [n()]);
    await settle();
    // The layout effect's update commits before paint; the first commit's
    // passive effect is flushed before that commit starts.
    expect(log).toEqual(['layout 0', 'passive 1', 'layout 1', 'passive 1']);
  });

  it('runs an empty-deps effect once and its cleanup on unmount', async () => {
    const log: string[] = [];
    const [n, setN] = createSignal(0);
    const Comp = () => {
      createEffectDeps(() => {
        log.push('mount');
        return () => log.push('unmount');
      }, () => []);
      return h('span', null, () => String(n()));
    };
    const node = h(Comp, null);
    await settle();
    setN(1);
    await settle();
    expect(log).toEqual(['mount']);
    disposeNode(node);
    await settle();
    expect(log).toEqual(['mount', 'unmount']);
  });

  it('unmounts parents before children, layout cleanups before passive', async () => {
    const log: string[] = [];
    const Child = () => {
      logged(log, 'child', () => []);
      return h('i', null);
    };
    const Parent = () => {
      logged(log, 'parent', () => []);
      return h('div', null, h(Child, null));
    };
    const [show, setShow] = createSignal(true);
    const host = document.createElement('div');
    // A list-like binding: re-running replaces (and disposes) its content.
    createEffect(() => {
      while (host.firstChild) {
        const c = host.firstChild;
        host.removeChild(c);
        disposeNode(c);
      }
      if (show()) host.appendChild(h(Parent, null));
    });
    await settle();
    log.length = 0;
    setShow(false);
    await microtask();
    expect(log).toEqual([
      'parent insertion cleanup',
      'parent layout cleanup',
      'child insertion cleanup',
      'child layout cleanup',
    ]);
    await settle();
    expect(log.slice(4)).toEqual(['parent passive cleanup', 'child passive cleanup']);
  });

  it('never runs effects of an unmounted component again', async () => {
    const log: string[] = [];
    const [n, setN] = createSignal(0);
    const Comp = () => {
      createEffectDeps(() => void log.push(`run ${n()}`), () => [n()]);
      return h('i', null);
    };
    const node = h(Comp, null);
    await settle();
    disposeNode(node);
    setN(1);
    await settle();
    expect(log).toEqual(['run 0']);
  });
});

describe('dependency comparison (Object.is)', () => {
  beforeEach(() => disposeAll());

  it('treats NaN as equal to NaN', async () => {
    const [tick$, setTick] = createSignal(0);
    let runs = 0;
    createEffectDeps(() => void runs++, () => {
      tick$();
      return [NaN];
    });
    await settle();
    setTick(1);
    await settle();
    expect(runs).toBe(1);
  });

  it('treats +0 and -0 as different', async () => {
    const [neg, setNeg] = createSignal(false);
    let runs = 0;
    createEffectDeps(() => void runs++, () => [neg() ? -0 : 0]);
    await settle();
    setNeg(true);
    await settle();
    expect(runs).toBe(2);
  });

  it('compares by identity, not structure', async () => {
    const [obj, setObj] = createSignal({ a: 1 });
    let runs = 0;
    createEffectDeps(() => void runs++, () => [obj()]);
    await settle();
    setObj({ a: 1 });
    await settle();
    expect(runs).toBe(2);
  });

  it('re-runs when the array length changes', async () => {
    const [items, setItems] = createSignal<number[]>([1]);
    let runs = 0;
    createEffectDeps(() => void runs++, () => items());
    await settle();
    setItems([1, 2]);
    await settle();
    expect(runs).toBe(2);
  });

  it('skips the effect when the deps did not change even if the thunk re-ran', async () => {
    const [a, setA] = createSignal(1);
    let runs = 0;
    createEffectDeps(() => void runs++, () => [a() > 0]);
    await settle();
    setA(2);
    await settle();
    expect(runs).toBe(1);
    setA(-1);
    await settle();
    expect(runs).toBe(2);
  });
});

describe('state semantics', () => {
  beforeEach(() => disposeAll());

  it('bails out of updates that set the same value (Object.is)', async () => {
    const [v, setV] = createSignal(NaN);
    let runs = 0;
    createEffect(() => {
      v();
      runs++;
    });
    setV(NaN);
    await settle();
    expect(runs).toBe(1);
    setV(0);
    await settle();
    setV(-0);
    await settle();
    expect(runs).toBe(3);
  });

  it('applies functional updates in order', async () => {
    const [n, setN] = createSignal(0);
    setN((x) => x + 1);
    setN((x) => x + 1);
    setN((x) => x * 10);
    expect(n()).toBe(20);
  });

  it('batches several updates into one commit', async () => {
    const [a, setA] = createSignal(0);
    const [b, setB] = createSignal(0);
    const log: string[] = [];
    createEffectDeps(() => void log.push(`effect ${a()} ${b()}`), () => [a(), b()]);
    createEffect(() => void log.push(`binding ${a()} ${b()}`));
    await settle();
    log.length = 0;
    // Event handler, timeout and promise callbacks all batch.
    setA(1);
    setB(1);
    setA(2);
    await settle();
    expect(log).toEqual(['binding 2 1', 'effect 2 1']);
    log.length = 0;
    await new Promise<void>((resolve) =>
      setTimeout(() => {
        setA(3);
        setB(3);
        resolve();
      }, 0),
    );
    await Promise.resolve().then(() => {
      setA(4);
    });
    await settle();
    // One commit per task: the timeout's two writes, then the promise's.
    expect(log).toEqual(['binding 3 3', 'effect 3 3', 'binding 4 3', 'effect 4 3']);
  });

  it('gives useReducer a stable dispatch, a bail-out and a lazy initializer', async () => {
    const [count, dispatch] = createReducer(
      (s: number, a: 'inc' | 'same') => (a === 'inc' ? s + 1 : s),
      5,
      (arg: number) => arg * 2,
    );
    expect(count()).toBe(10);
    let runs = 0;
    createEffectDeps(() => void runs++, () => [count()]);
    const first = dispatch;
    await settle();
    dispatch('same');
    await settle();
    expect(runs).toBe(1);
    dispatch('inc');
    await settle();
    expect(runs).toBe(2);
    expect(count()).toBe(11);
    expect(dispatch).toBe(first);
  });

  it('flushes synchronously with krateFlushSync', () => {
    const log: string[] = [];
    const [n, setN] = createSignal(0);
    createEffectDeps(() => void log.push(`layout ${n()}`), () => [n()], 'layout');
    krateFlushSync();
    setN(1);
    krateFlushSync();
    expect(log).toEqual(['layout 0', 'layout 1']);
  });
});

describe('createMemoDeps / createCallbackDeps', () => {
  beforeEach(() => disposeAll());

  it('recomputes synchronously when deps change, not when other reads change', () => {
    const [a, setA] = createSignal(1);
    const [other, setOther] = createSignal(100);
    let computations = 0;
    const memo = createMemoDeps(() => {
      computations++;
      return a() * 2 + other();
    }, () => [a()]);
    expect(memo()).toBe(102);
    setA(2);
    // Same update, before any flush.
    expect(memo()).toBe(104);
    setOther(0);
    expect(memo()).toBe(104);
    expect(computations).toBe(2);
  });

  it('notifies readers when deps change', async () => {
    const [a, setA] = createSignal(1);
    const memo = createMemoDeps(() => a() + 1, () => [a()]);
    const seen: number[] = [];
    createEffect(() => void seen.push(memo()));
    setA(5);
    await settle();
    expect(seen).toEqual([2, 6]);
  });

  it('keeps callback identity until deps change', async () => {
    const [a, setA] = createSignal(1);
    const [b, setB] = createSignal(1);
    const cb = createCallbackDeps((x: number) => x + a(), () => [a()]);
    const first = cb();
    expect(cb()).toBe(first);
    setB(2);
    expect(cb()).toBe(first);
    setA(2);
    const second = cb();
    expect(second).not.toBe(first);
    expect(second(1)).toBe(3);
    // An effect that depends on the callback re-runs only on identity change.
    let runs = 0;
    createEffectDeps(() => void runs++, () => [cb()]);
    await settle();
    setB(3);
    await settle();
    expect(runs).toBe(1);
    setA(3);
    await settle();
    expect(runs).toBe(2);
    void b;
  });
});

describe('hydration owners', () => {
  beforeEach(() => disposeAll());

  it('orders hydrated components children before parents', async () => {
    const log: string[] = [];
    krateOwner('P', null);
    createEffectDeps(() => void log.push('parent'), () => []);
    krateOwner('C1', 'P');
    createEffectDeps(() => void log.push('child 1'), () => []);
    krateOwner('G', 'C1');
    createEffectDeps(() => void log.push('grandchild'), () => []);
    krateOwner('C2', 'P');
    createEffectDeps(() => void log.push('child 2'), () => []);
    krateOwner(null);
    await settle();
    expect(log).toEqual(['grandchild', 'child 1', 'child 2', 'parent']);
  });

  it('cleans up effects in a hidden branch and re-runs them when shown', async () => {
    const log: string[] = [];
    const [n, setN] = createSignal(0);
    krateOwner('P', null);
    krateOwner('C', 'P', [['5', 'c', 1]]);
    createEffectDeps(() => {
      log.push(`layout ${n()}`);
      return () => log.push('layout cleanup');
    }, () => [n()], 'layout');
    createEffectDeps(() => {
      log.push(`passive ${n()}`);
      return () => log.push('passive cleanup');
    }, () => [n()]);
    krateOwner(null);
    await settle();
    expect(log).toEqual(['layout 0', 'passive 0']);
    log.length = 0;
    krateCondActivity('5', 'a');
    await settle();
    expect(log).toEqual(['layout cleanup', 'passive cleanup']);
    log.length = 0;
    setN(1); // state survives while hidden; no effects run
    await settle();
    expect(log).toEqual([]);
    krateCondActivity('5', 'c');
    await settle();
    expect(log).toEqual(['layout 1', 'passive 1']);
  });

  it('does not run effects of a component in the initially hidden branch', async () => {
    const log: string[] = [];
    krateOwner('P', null);
    krateCondActivity('7', 'c');
    krateOwner('Alt', 'P', [['7', 'a', 0]]);
    createEffectDeps(() => void log.push('alt'), () => []);
    krateOwner(null);
    await settle();
    expect(log).toEqual([]);
    krateCondActivity('7', 'a');
    await settle();
    expect(log).toEqual(['alt']);
  });

  it('skips hydrated list items whose list was re-rendered on the client', async () => {
    const log: string[] = [];
    krateOwner('P', null);
    krateSlotReplaced('8');
    krateOwner('Item', 'P', [], '8');
    createEffectDeps(() => void log.push('stale item'), () => []);
    krateOwner(null);
    await settle();
    expect(log).toEqual([]);
  });

  it('runs every cleanup on page teardown', async () => {
    const log: string[] = [];
    krateOwner('P', null);
    createEffectDeps(() => () => void log.push('passive'), () => []);
    createEffectDeps(() => () => void log.push('layout'), () => [], 'layout');
    krateOwner(null);
    await settle();
    disposeAll();
    expect(log).toEqual(['layout', 'passive']);
  });
});

describe('krateDepsOf', () => {
  it('reads every prop except children, or wraps a plain value', () => {
    let reads = 0;
    const props = {
      get a() {
        reads++;
        return 1;
      },
      b: 'x',
      children: [],
    };
    expect(krateDepsOf(props)).toEqual([1, 'x']);
    expect(reads).toBe(1);
    expect(krateDepsOf(3)).toEqual([3]);
    expect(krateDepsOf([1, 2])).toEqual([[1, 2]]);
  });
});

describe('flushPassive', () => {
  beforeEach(() => disposeAll());

  it('runs pending passive effects immediately', async () => {
    const log: string[] = [];
    createEffectDeps(() => void log.push('passive'), () => []);
    await microtask();
    expect(log).toEqual([]);
    flushPassive();
    expect(log).toEqual(['passive']);
  });
});
