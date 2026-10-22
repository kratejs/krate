import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  Profiler,
  StrictMode,
  Suspense,
  createElement,
  jsx,
  lazy,
  startTransition,
  use,
  useDeferredValue,
  useState,
  useSyncExternalStore,
  useTransition,
  createContext,
} from './react.js';
import { setFallbackThrottle, setSuspenseErrorHandler } from './suspense.js';

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

function mountInto(node: Node): HTMLElement {
  const root = document.createElement('div');
  root.appendChild(node);
  document.body.innerHTML = '';
  document.body.appendChild(root);
  return root;
}

beforeEach(() => setFallbackThrottle(0));

afterEach(() => {
  setSuspenseErrorHandler(null);
  vi.useRealTimers();
});

describe('compat Suspense and use()', () => {
  it('shows the fallback until use(promise) resolves, then renders the value', async () => {
    const d = deferred<string>();
    const Name = (): Node => createElement('b', null, use(d.promise));
    const root = mountInto(
      createElement(Suspense, { fallback: createElement('i', null, 'loading') }, createElement(Name, null)),
    );
    expect(root.textContent).toBe('loading');
    d.resolve('Ada');
    await flush();
    expect(root.textContent).toBe('Ada');
    expect(root.querySelector('b')?.textContent).toBe('Ada');
  });

  it('reveals siblings together', async () => {
    const a = deferred<string>();
    const b = deferred<string>();
    const A = (): Node => createElement('span', null, use(a.promise));
    const B = (): Node => createElement('span', null, use(b.promise));
    const root = mountInto(
      createElement(Suspense, { fallback: 'wait' }, createElement(A, null), createElement(B, null)),
    );
    a.resolve('a');
    await flush();
    expect(root.textContent).toBe('wait');
    b.resolve('b');
    await flush();
    expect(root.textContent).toBe('ab');
  });

  it('suspends the innermost boundary only', async () => {
    const d = deferred<string>();
    const Slow = (): Node => createElement('span', null, use(d.promise));
    const root = mountInto(
      createElement(
        Suspense,
        { fallback: 'outer' },
        createElement('p', null, 'shell'),
        createElement(Suspense, { fallback: 'inner' }, createElement(Slow, null)),
      ),
    );
    expect(root.textContent).toBe('shellinner');
    d.resolve('done');
    await flush();
    expect(root.textContent).toBe('shelldone');
  });

  it('does not re-suspend on a resolved promise and allows conditional use()', async () => {
    const p = Promise.resolve('v');
    await flush();
    let renders = 0;
    const C = (props: { on: boolean }): Node => {
      renders++;
      return createElement('span', null, props.on ? use(p) : 'off');
    };
    // First read tracks the promise; it is already resolved by the next read.
    mountInto(createElement(Suspense, { fallback: 'x' }, createElement(C, { on: true })));
    await flush();
    const root = mountInto(createElement(Suspense, { fallback: 'x' }, createElement(C, { on: true })));
    expect(root.textContent).toBe('v');
    const off = mountInto(createElement(C, { on: false }));
    expect(off.textContent).toBe('off');
    expect(renders).toBeGreaterThan(0);
  });

  it('reads a context with use()', () => {
    const Ctx = createContext('ctx');
    const C = (): Node => createElement('span', null, use(Ctx));
    expect(mountInto(createElement(C, null)).textContent).toBe('ctx');
  });

  it('routes a rejected promise to the error handler', async () => {
    const errors: unknown[] = [];
    setSuspenseErrorHandler((e) => {
      errors.push(e);
      return true;
    });
    const d = deferred<string>();
    const C = (): Node => createElement('span', null, use(d.promise));
    mountInto(createElement(Suspense, { fallback: 'x' }, createElement(C, null)));
    d.reject(new Error('bad'));
    await flush();
    expect((errors[0] as Error).message).toBe('bad');
  });

  it('jsx() renders through the same suspending path', async () => {
    const d = deferred<string>();
    const C = (): Node => jsx('span', { children: use(d.promise) });
    const root = mountInto(jsx(Suspense, { fallback: 'f', children: jsx(C, {}) }));
    expect(root.textContent).toBe('f');
    d.resolve('ok');
    await flush();
    expect(root.textContent).toBe('ok');
  });
});

describe('compat lazy()', () => {
  it('starts loading on first render and suspends until the module loads', async () => {
    const d = deferred<{ default: (p: { name: string }) => Node }>();
    let calls = 0;
    const Lazy = lazy<{ name: string }>(() => {
      calls++;
      return d.promise;
    });
    expect(calls).toBe(0);
    const root = mountInto(createElement(Suspense, { fallback: 'loading' }, createElement(Lazy, { name: 'x' })));
    expect(calls).toBe(1);
    expect(root.textContent).toBe('loading');
    d.resolve({ default: (p) => createElement('em', null, 'hi ' + p.name) });
    await flush();
    expect(root.textContent).toBe('hi x');
    expect(calls).toBe(1);
  });

  it('reports a module without a component default export', async () => {
    const errors: unknown[] = [];
    setSuspenseErrorHandler((e) => {
      errors.push(e);
      return true;
    });
    const Lazy = lazy(() => Promise.resolve({ default: 42 }));
    mountInto(createElement(Suspense, { fallback: 'x' }, createElement(Lazy, null)));
    await flush();
    expect(String((errors[0] as Error).message)).toContain('default export');
  });

  it('reports a failed import', async () => {
    const errors: unknown[] = [];
    setSuspenseErrorHandler((e) => {
      errors.push(e);
      return true;
    });
    const Lazy = lazy(() => Promise.reject(new Error('chunk failed')));
    mountInto(createElement(Suspense, { fallback: 'x' }, createElement(Lazy, null)));
    await flush();
    expect((errors[0] as Error).message).toBe('chunk failed');
  });
});

describe('compat transitions and deferred values', () => {
  it('useTransition stays pending until the async action settles', async () => {
    vi.useFakeTimers();
    const [isPending, start] = useTransition();
    start(async () => {
      await new Promise((r) => setTimeout(r, 100));
    });
    expect(isPending()).toBe(true);
    await vi.advanceTimersByTimeAsync(100);
    expect(isPending()).toBe(false);
  });

  it('startTransition runs the action', () => {
    let ran = false;
    startTransition(() => {
      ran = true;
    });
    expect(ran).toBe(true);
  });

  it('useDeferredValue defers a getter and passes plain values through', async () => {
    vi.useFakeTimers();
    const [q, setQ] = useState('a');
    const d = useDeferredValue(q) as () => string;
    setQ('b');
    await flush();
    expect(d()).toBe('a');
    await vi.advanceTimersByTimeAsync(1);
    expect(d()).toBe('b');
    expect(useDeferredValue(5)).toBe(5);
  });

  it('useSyncExternalStore reads and follows the store', () => {
    let v = 1;
    let notify: () => void = () => {};
    const get = useSyncExternalStore(
      (fn) => {
        notify = fn;
        return () => {};
      },
      () => v,
    );
    expect(get()).toBe(1);
    v = 2;
    notify();
    expect(get()).toBe(2);
  });

  it('StrictMode and Profiler render their children', () => {
    expect(mountInto(createElement(StrictMode, null, 'a')).textContent).toBe('a');
    const onRender = vi.fn();
    expect(mountInto(createElement(Profiler, { id: 'p', onRender }, 'b')).textContent).toBe('b');
    expect(onRender).not.toHaveBeenCalled();
  });
});
