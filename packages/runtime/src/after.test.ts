import { AsyncLocalStorage } from 'node:async_hooks';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { after, runAfterTasks } from './after.js';
import type { RevalidateScope } from './cache-scope.js';

const STORE_KEY = Symbol.for('krate.revalidate.store');

afterEach(() => {
  delete (globalThis as Record<symbol, unknown>)[STORE_KEY];
});

describe('after()', () => {
  it('queues tasks on the request scope instead of running them', async () => {
    const store = new AsyncLocalStorage<RevalidateScope>();
    (globalThis as Record<symbol, unknown>)[STORE_KEY] = store;
    const scope: RevalidateScope = { kind: 'api', paths: [], tags: [] };
    const ran: string[] = [];
    store.run(scope, () => {
      after(() => ran.push('fn'));
      after(Promise.resolve().then(() => ran.push('promise')));
    });
    expect(scope.after).toHaveLength(2);
    await Promise.resolve();
    expect(ran).toEqual(['promise']);
    await runAfterTasks(scope);
    expect(ran).toEqual(['promise', 'fn']);
    expect(scope.after).toEqual([]);
  });

  it('keeps running later tasks when one throws', async () => {
    const err = vi.spyOn(console, 'error').mockImplementation(() => {});
    const scope: RevalidateScope = {
      kind: 'api',
      paths: [],
      tags: [],
      after: [
        () => {
          throw new Error('boom');
        },
        () => {
          ran = true;
        },
      ],
    };
    let ran = false;
    await runAfterTasks(scope);
    expect(ran).toBe(true);
    expect(err).toHaveBeenCalledOnce();
    err.mockRestore();
  });

  it('runs on a later turn when no request is active', async () => {
    let ran = false;
    after(() => {
      ran = true;
    });
    expect(ran).toBe(false);
    await new Promise((r) => setTimeout(r, 0));
    expect(ran).toBe(true);
  });

  it('rejects anything but a function or a promise', () => {
    expect(() => after(42 as unknown as () => void)).toThrow(TypeError);
  });
});
