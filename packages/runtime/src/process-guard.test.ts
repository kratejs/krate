import { describe, expect, it } from 'vitest';
import { AsyncLocalStorage } from 'node:async_hooks';
import { installProcessGuards } from './process-guard';
import type { RequestContext } from './request-context';

const STORE = Symbol.for('krate.request.store');

function fakeProcess() {
  const handlers: Record<string, (...a: any[]) => void> = {};
  const exits: Array<number | undefined> = [];
  return {
    handlers,
    exits,
    on: (e: string, fn: (...a: any[]) => void) => void (handlers[e] = fn),
    exit: (code?: number) => void exits.push(code),
  };
}

describe('installProcessGuards', () => {
  it('logs an unhandled rejection and keeps the process', () => {
    const p = fakeProcess();
    const logs: string[] = [];
    installProcessGuards(p as any, (...a) => logs.push(a.join(' ')));
    p.handlers.unhandledRejection(new Error('lost'));
    expect(p.exits).toEqual([]);
    expect(logs[0]).toContain('unhandled promise rejection');
    expect(logs[0]).toContain('lost');
  });

  it('survives an uncaught exception that belongs to a request, naming it', async () => {
    const store = new AsyncLocalStorage<RequestContext>();
    const g = globalThis as Record<symbol, unknown>;
    const before = g[STORE];
    g[STORE] = store;
    try {
      const p = fakeProcess();
      const logs: string[] = [];
      installProcessGuards(p as any, (...a) => logs.push(a.join(' ')));
      const ctx = { url: '/admin/', method: 'GET' } as RequestContext;
      await store.run(ctx, async () => {
        p.handlers.uncaughtException(new Error('late throw'));
      });
      expect(p.exits).toEqual([]);
      expect(logs[0]).toContain('GET /admin/');
      expect(logs[0]).toContain('late throw');
    } finally {
      g[STORE] = before;
    }
  });

  it('exits on an uncaught exception outside any request', () => {
    const p = fakeProcess();
    const logs: string[] = [];
    installProcessGuards(p as any, (...a) => logs.push(a.join(' ')));
    p.handlers.uncaughtException(new Error('corrupt'));
    expect(p.exits).toEqual([1]);
    expect(logs[0]).toContain('outside any request');
  });
});
