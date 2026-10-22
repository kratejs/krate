import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createSignal, createEffect } from './signal.js';
import { createContext } from './context.js';
import { createResource } from './resource.js';
import {
  __krateBoundary,
  __krateLazy,
  __krateLazyPayload,
  __krateRead,
  __krateTransitionStarter,
  __krateUse,
  createBoundary,
  createDeferredValue,
  createExternalStore,
  getBoundary,
  isInTransition,
  readValue,
  registerBoundary,
  resetBoundaries,
  runAsyncTransition,
  setFallbackThrottle,
  setLazyChunkImporter,
  setSuspenseErrorHandler,
  startTransition,
  trackThenable,
  isSuspenseSignal,
  type Boundary,
} from './suspense.js';

async function flush(n = 20): Promise<void> {
  for (let i = 0; i < n; i++) await Promise.resolve();
}

function deferred<T>(): { promise: Promise<T>; resolve: (v: T) => void; reject: (e: unknown) => void } {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

// A boundary whose visible state is recorded in `shown`.
function testBoundary(id = 'b', initialContent = true): { b: Boundary; shown: string[] } {
  const shown: string[] = [];
  const b = createBoundary(id, (content) => shown.push(content ? 'content' : 'fallback'), { initialContent });
  return { b, shown };
}

// Reveal throttling has its own test; elsewhere reveals are immediate.
beforeEach(() => setFallbackThrottle(0));

afterEach(() => {
  setSuspenseErrorHandler(null);
  resetBoundaries();
  vi.useRealTimers();
});

describe('thenables and use()', () => {
  it('caches a resolved promise by identity', async () => {
    const p = Promise.resolve(7);
    let thrown: unknown;
    try {
      readValue(p);
    } catch (e) {
      thrown = e;
    }
    expect(isSuspenseSignal(thrown)).toBe(true);
    await flush();
    expect(readValue(p)).toBe(7);
    expect(readValue(p)).toBe(7);
  });

  it('throws the rejection reason', async () => {
    const p = Promise.reject(new Error('boom'));
    trackThenable(p);
    p.catch(() => {});
    await flush();
    expect(() => readValue(p)).toThrow('boom');
  });

  it('reads contexts and passes plain values through', () => {
    const Ctx = createContext('dflt');
    expect(readValue(Ctx)).toBe('dflt');
    expect(readValue(3)).toBe(3);
  });

  it('honours React-style pre-resolved thenables', () => {
    const t = { status: 'fulfilled' as const, value: 'x', then() {} };
    expect(readValue(t)).toBe('x');
  });
});

describe('boundaries', () => {
  it('shows the fallback while suspended and reveals after settling', async () => {
    const { b, shown } = testBoundary();
    const d = deferred<void>();
    b.suspend(d.promise);
    expect(shown).toEqual(['fallback']);
    expect(b.showingContent()).toBe(false);
    d.resolve();
    await flush();
    expect(shown).toEqual(['fallback', 'content']);
  });

  it('reveals siblings together', async () => {
    const { b, shown } = testBoundary();
    const a = deferred<void>();
    const c = deferred<void>();
    b.suspend(a.promise);
    b.suspend(c.promise);
    a.resolve();
    await flush();
    expect(b.showingContent()).toBe(false);
    c.resolve();
    await flush();
    expect(shown).toEqual(['fallback', 'content']);
  });

  it('keeps revealed content during a transition and waits for it', async () => {
    const { b, shown } = testBoundary();
    const d = deferred<void>();
    const tr = runAsyncTransition(() => {
      b.suspend(d.promise, isInTransition());
    });
    expect(shown).toEqual([]);
    await flush();
    expect(tr.pending()).toBe(true);
    d.resolve();
    await flush();
    expect(tr.pending()).toBe(false);
    expect(shown).toEqual([]);
  });
});

describe('component runs with use()', () => {
  it('suspends the boundary until the promise resolves, then renders with the value', async () => {
    const { b, shown } = testBoundary('x', false);
    const d = deferred<{ name: string }>();
    const rendered: string[] = [];
    // Stub the compiled boundary lookup with the test controller.
    runIn(b, () => {
      const user = __krateUse(() => __krateRead<{ name: string }>(d.promise));
      createEffect(() => rendered.push(user().name));
    });
    expect(rendered).toEqual([]);
    expect(shown).toEqual([]);
    d.resolve({ name: 'Ada' });
    await flush();
    expect(rendered).toEqual(['Ada']);
    expect(shown).toEqual(['content']);
  });

  it('reuses an uncached promise by position on retry', async () => {
    let created = 0;
    const resolvers: Array<(v: number) => void> = [];
    const make = (): Promise<number> => {
      created++;
      return new Promise<number>((r) => resolvers.push(r));
    };
    let value: (() => number) | null = null;
    __krateBoundary(null, () => {
      value = __krateUse(() => __krateRead<number>(make()));
    });
    expect(created).toBe(1);
    resolvers[0](5);
    await flush();
    // The retry evaluated make() again but kept the first promise.
    expect(created).toBe(3);
    expect(value!()).toBe(5);
  });

  it('allows conditional and repeated use() in one expression', async () => {
    const a = Promise.resolve('a');
    const c = Promise.resolve('c');
    const [flag, setFlag] = createSignal(true);
    let v: (() => string) | null = null;
    __krateBoundary(null, () => {
      v = __krateUse(() => (flag() ? __krateRead<string>(a) : __krateRead<string>(c)));
    });
    await flush();
    expect(v!()).toBe('a');
    setFlag(false);
    await flush();
    expect(v!()).toBe('c');
  });

  it('routes a rejected promise to the error handler', async () => {
    const errors: unknown[] = [];
    setSuspenseErrorHandler((e) => {
      errors.push(e);
      return true;
    });
    const d = deferred<string>();
    __krateBoundary(null, () => {
      __krateUse(() => __krateRead<string>(d.promise));
    });
    d.reject(new Error('nope'));
    await flush();
    expect(errors.map((e) => (e as Error).message)).toEqual(['nope']);
  });

  it('suspends nested boundaries independently (innermost wins)', async () => {
    const outer = testBoundary('outer', false);
    const inner = testBoundary('inner', false);
    const d = deferred<string>();
    runIn(outer.b, () => {});
    runIn(inner.b, () => {
      __krateUse(() => __krateRead<string>(d.promise));
    });
    await flush();
    expect(outer.shown).toEqual(['content']);
    expect(inner.shown).toEqual([]);
    d.resolve('ok');
    await flush();
    expect(inner.shown).toEqual(['content']);
  });

  it('shows the fallback again for a non-transition update and keeps content in a transition', async () => {
    const { b, shown } = testBoundary('r', false);
    const first = Promise.resolve('one');
    const [p, setP] = createSignal<Promise<string>>(first);
    let text: (() => string) | null = null;
    runIn(b, () => {
      text = __krateUse(() => __krateRead<string>(p()));
    });
    await flush();
    expect(shown).toEqual(['content']);
    expect(text!()).toBe('one');

    // Urgent update: the fallback comes back while the new promise is pending.
    const second = deferred<string>();
    setP(second.promise);
    await flush();
    expect(shown).toEqual(['content', 'fallback']);
    expect(text!()).toBe('one');
    second.resolve('two');
    await flush();
    expect(shown).toEqual(['content', 'fallback', 'content']);
    expect(text!()).toBe('two');

    // Transition update: the old content stays and the transition waits.
    const third = deferred<string>();
    const [pending, setPending] = createSignal(false);
    const start = __krateTransitionStarter(setPending);
    start(() => setP(third.promise));
    await flush();
    expect(pending()).toBe(true);
    expect(shown).toEqual(['content', 'fallback', 'content']);
    expect(text!()).toBe('two');
    third.resolve('three');
    await flush();
    expect(text!()).toBe('three');
    expect(pending()).toBe(false);
    expect(shown).toEqual(['content', 'fallback', 'content']);
  });
});

// Runs fn as a compiled component inside a test boundary.
function runIn(b: Boundary, fn: () => void): void {
  registerBoundary(b);
  __krateBoundary(b.id, fn);
}

describe('resources under Suspense', () => {
  it('suspends the creating component boundary while a read resource loads', async () => {
    const { b, shown } = testBoundary('res', true);
    const d = deferred<string>();
    let read: (() => string | undefined) | null = null;
    runIn(b, () => {
      const [user] = createResource(() => 1, () => d.promise);
      read = user;
      createEffect(() => user());
    });
    expect(shown).toEqual(['fallback']);
    d.resolve('Ada');
    await flush();
    expect(read!()).toBe('Ada');
    expect(shown).toEqual(['fallback', 'content']);
  });
});

describe('async transitions', () => {
  beforeEach(() => vi.useFakeTimers());

  it('keeps isPending true until an async action settles, including work after await', async () => {
    const [pending, setPending] = createSignal(false);
    const start = __krateTransitionStarter(setPending);
    const [count, setCount] = createSignal(0);
    start(async () => {
      await new Promise((r) => setTimeout(r, 100));
      setCount(1);
      await new Promise((r) => setTimeout(r, 100));
    });
    expect(pending()).toBe(true);
    await vi.advanceTimersByTimeAsync(150);
    expect(count()).toBe(1);
    expect(pending()).toBe(true);
    await vi.advanceTimersByTimeAsync(100);
    expect(pending()).toBe(false);
  });

  it('stays pending until all overlapping transitions settle', async () => {
    const [pending, setPending] = createSignal(false);
    const start = __krateTransitionStarter(setPending);
    start(() => new Promise((r) => setTimeout(r, 100)));
    start(() => new Promise((r) => setTimeout(r, 300)));
    await vi.advanceTimersByTimeAsync(150);
    expect(pending()).toBe(true);
    await vi.advanceTimersByTimeAsync(200);
    expect(pending()).toBe(false);
  });

  it('settles a synchronous transition after its effect flush', async () => {
    const [pending, setPending] = createSignal(false);
    const start = __krateTransitionStarter(setPending);
    const [v, setV] = createSignal(0);
    let seenInTransition = false;
    createEffect(() => {
      if (v() > 0) seenInTransition = isInTransition();
    });
    start(() => setV(1));
    expect(pending()).toBe(true);
    await flush();
    expect(seenInTransition).toBe(true);
    expect(pending()).toBe(false);
    expect(isInTransition()).toBe(false);
  });

  it('top-level startTransition supports async functions', async () => {
    const [v, setV] = createSignal('a');
    startTransition(async () => {
      await new Promise((r) => setTimeout(r, 50));
      setV('b');
    });
    await vi.advanceTimersByTimeAsync(60);
    expect(v()).toBe('b');
  });

  it('runAsyncTransition exposes pending and a promise and reports errors', async () => {
    const errors: unknown[] = [];
    setSuspenseErrorHandler((e) => {
      errors.push(e);
      return true;
    });
    const ok = runAsyncTransition(() => new Promise((r) => setTimeout(r, 10)));
    const bad = runAsyncTransition(async () => {
      throw new Error('fail');
    });
    expect(ok.pending()).toBe(true);
    await vi.advanceTimersByTimeAsync(20);
    await expect(ok.promise).resolves.toBeUndefined();
    await expect(bad.promise).rejects.toThrow('fail');
    expect(bad.pending()).toBe(false);
    expect(errors).toHaveLength(1);
  });
});

describe('deferred values', () => {
  beforeEach(() => vi.useFakeTimers());

  it('lags behind an urgent update and catches up in a later task', async () => {
    const [q, setQ] = createSignal('a');
    const d = createDeferredValue(q);
    expect(d()).toBe('a');
    setQ('ab');
    await flush();
    expect(d()).toBe('a');
    setQ('abc');
    await flush();
    await vi.advanceTimersByTimeAsync(1);
    expect(d()).toBe('abc');
  });

  it('starts from initialValue and then switches to the value', async () => {
    const [q] = createSignal('real');
    const d = createDeferredValue(q, 'initial');
    expect(d()).toBe('initial');
    await vi.advanceTimersByTimeAsync(1);
    expect(d()).toBe('real');
  });

  it('does not defer updates made inside a transition', async () => {
    const [q, setQ] = createSignal(1);
    const d = createDeferredValue(q);
    startTransition(() => setQ(2));
    await flush();
    expect(d()).toBe(2);
  });
});

describe('external stores', () => {
  it('subscribes, tracks snapshots by Object.is and catches changes before subscribe', async () => {
    let state = { n: 1 };
    const listeners = new Set<() => void>();
    let subscribed = 0;
    let unsubscribed = 0;
    const subscribe = (fn: () => void): (() => void) => {
      subscribed++;
      // A change between the first read and the subscription.
      state = { n: 2 };
      listeners.add(fn);
      return () => {
        unsubscribed++;
        listeners.delete(fn);
      };
    };
    const snap = createExternalStore(subscribe, () => state);
    expect(subscribed).toBe(1);
    expect(snap().n).toBe(2);
    const before = snap();
    listeners.forEach((fn) => fn());
    expect(snap()).toBe(before);
    state = { n: 3 };
    listeners.forEach((fn) => fn());
    expect(snap().n).toBe(3);
    const { disposeAll } = await import('./signal.js');
    disposeAll();
    expect(unsubscribed).toBe(1);
  });

  it('stores function snapshots without calling them', () => {
    const fn = (): number => 1;
    const snap = createExternalStore(() => () => {}, () => fn);
    expect(snap()).toBe(fn);
  });
});

describe('lazy chunks', () => {
  it('holds the boundary until the chunk loads', async () => {
    const d = deferred<unknown>();
    setLazyChunkImporter(() => d.promise);
    installFindSlot('<!--suspense:L--><!--k:sL--><p>fallback</p><!--/k:sL--><!--/suspense:L--><template data-kc="sL" data-kb="c"><p>content</p></template>');
    const b = getBoundary('L');
    __krateLazy('/chunk-a.js', 'L');
    await flush();
    expect(b.showingContent()).toBe(false);
    d.resolve({});
    await flush();
    expect(b.showingContent()).toBe(true);
    expect(document.body.textContent).toContain('content');
    expect(document.body.querySelector('#root > p')?.textContent).toBe('content');
  });

  it('reports a failed chunk to the error handler', async () => {
    const errors: unknown[] = [];
    setSuspenseErrorHandler((e) => {
      errors.push(e);
      return true;
    });
    setLazyChunkImporter(() => Promise.reject(new Error('404')));
    __krateLazy('/missing.js', null);
    await flush();
    expect((errors[0] as Error).message).toBe('404');
  });
});

describe('reveal throttling', () => {
  it('keeps a fallback at least the throttle time before revealing', async () => {
    vi.useFakeTimers();
    setFallbackThrottle(300);
    const { b, shown } = testBoundary('th', true);
    const d = deferred<void>();
    b.suspend(d.promise);
    expect(shown).toEqual(['fallback']);
    d.resolve();
    await vi.advanceTimersByTimeAsync(100);
    expect(shown).toEqual(['fallback']);
    await vi.advanceTimersByTimeAsync(250);
    expect(shown).toEqual(['fallback', 'content']);
  });
});

describe('lazy loaders', () => {
  it('calls a promise loader once and suspends the run until it resolves', async () => {
    const { b, shown } = testBoundary('lp', false);
    let calls = 0;
    const d = deferred<{ default: () => null }>();
    const loader = (): Promise<{ default: () => null }> => {
      calls++;
      return d.promise;
    };
    let ran = 0;
    runIn(b, () => {
      __krateRead(__krateLazyPayload(loader));
      ran++;
    });
    await flush();
    expect(ran).toBe(0);
    d.resolve({ default: () => null });
    await flush();
    expect(ran).toBe(1);
    expect(calls).toBe(1);
    expect(shown).toEqual(['content']);
  });

  it('rejects a loader that does not resolve to a component', async () => {
    const errors: unknown[] = [];
    setSuspenseErrorHandler((e) => {
      errors.push(e);
      return true;
    });
    __krateBoundary(null, () => {
      __krateRead(__krateLazyPayload(() => Promise.resolve({ default: 1 })));
    });
    await flush();
    expect(String((errors[0] as Error).message)).toContain('default export');
  });

  it('starts loading a chunk only when its boundary is connected', async () => {
    let imported = 0;
    setLazyChunkImporter(() => {
      imported++;
      return Promise.resolve({});
    });
    installFindSlot('');
    const tpl = document.createElement('template');
    tpl.innerHTML = '<!--suspense:H--><!--k:sH--><p>wait</p><!--/k:sH--><!--/suspense:H-->';
    const start = tpl.content.childNodes[1];
    (globalThis as { findSlot?: (id: string) => Node | null }).findSlot = () => start;
    __krateLazy('/hidden-chunk.js', 'H');
    await flush();
    expect(imported).toBe(0);
    document.getElementById('root')!.appendChild(tpl.content);
    await new Promise((r) => setTimeout(r, 0));
    await flush();
    expect(imported).toBe(1);
  });
});

describe('compiled boundary markers', () => {
  it('swaps the fallback and content around the boundary slot', async () => {
    installFindSlot('<!--suspense:M--><!--k:sM--><p>content</p><!--/k:sM--><!--/suspense:M--><template data-kc="sM" data-kb="a"><p>wait</p></template>');
    const b = getBoundary('M');
    expect(b.showingContent()).toBe(true);
    const d = deferred<void>();
    b.suspend(d.promise);
    // As in React: revealed content is hidden in place, the fallback follows.
    const ps = Array.from(document.querySelectorAll('#root > p')) as HTMLElement[];
    expect(ps.map((p) => p.textContent)).toEqual(['content', 'wait']);
    expect(ps[0].style.getPropertyValue('display')).toBe('none');
    expect(ps[0].style.getPropertyPriority('display')).toBe('important');
    d.resolve();
    await flush();
    const after = Array.from(document.querySelectorAll('#root > p')) as HTMLElement[];
    expect(after.map((p) => p.textContent)).toEqual(['content']);
    expect(after[0]).toBe(ps[0]);
    expect(after[0].style.getPropertyValue('display')).toBe('');
  });
});

function installFindSlot(html: string): void {
  document.body.innerHTML = '<div id="root">' + html + '</div>';
  (globalThis as { findSlot?: (id: string) => Node | null }).findSlot = (id: string) => {
    const w = document.createTreeWalker(document.body, NodeFilter.SHOW_COMMENT);
    while (w.nextNode()) {
      if (w.currentNode.nodeValue === 'k:' + id) return w.currentNode;
    }
    return null;
  };
}
