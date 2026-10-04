import { describe, it, expect, beforeEach } from 'vitest';
import {
  resolvePersist,
  readPersisted,
  writePersisted,
  clearPersisted,
  readDurableState,
  type StorageLike,
} from './storage';

function memory(): StorageLike {
  const map = new Map<string, string>();
  return {
    getItem: (k) => (map.has(k) ? (map.get(k) as string) : null),
    setItem: (k, v) => void map.set(k, v),
    removeItem: (k) => void map.delete(k),
  };
}

describe('persisted signals', () => {
  it('writes and reads through the configured store', () => {
    const store = memory();
    const p = resolvePersist<number>({ key: 'count', store });
    writePersisted(p, 41);
    expect(readPersisted(p)).toEqual({ found: true, value: 41 });
    clearPersisted(p);
    expect(readPersisted(p).found).toBe(false);
  });

  it('accepts a bare key spec', () => {
    const p = resolvePersist<{ a: number }>('obj');
    expect(p.key).toBe('obj');
    expect(p.store).toBe('local');
  });
});

describe('readDurableState', () => {
  beforeEach(() => {
    delete (globalThis as { __KRATE_STATE__?: unknown }).__KRATE_STATE__;
  });

  it('prefers the durable value, including plain strings', () => {
    (globalThis as { __KRATE_STATE__?: Record<string, unknown> }).__KRATE_STATE__ =
      { count: 5, theme: 'dark' };

    expect(readDurableState<number>('count', JSON.parse)).toEqual({
      found: true,
      value: 5,
    });
    // "dark" is the plain value, not JSON; the fallback must still return it.
    expect(readDurableState<string>('theme', JSON.parse)).toEqual({
      found: true,
      value: 'dark',
    });
  });

  it('reports absent keys', () => {
    expect(readDurableState('missing', JSON.parse).found).toBe(false);
  });
});
