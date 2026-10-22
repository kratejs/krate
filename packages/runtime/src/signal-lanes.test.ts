import { describe, it, expect, beforeEach } from 'vitest';
// Only signal.ts: the commit snapshot of React state must not depend on the
// effect scheduler (effects.ts) being loaded.
import { createSignal, createEffect, disposeAll, krateHookState } from './signal';

const tick = () => new Promise((r) => setTimeout(r, 0));
async function settle(): Promise<void> {
  for (let i = 0; i < 4; i++) await tick();
}

describe('React state commits without the effect scheduler', () => {
  beforeEach(() => disposeAll());

  it('keeps a promise-driven state write when a plain signal is written in a later microtask', async () => {
    const [user, setUser] = createSignal('nobody', krateHookState);
    const [flag, setFlag] = createSignal(false);
    const seen: string[] = [];
    createEffect(() => void seen.push(user() + '/' + flag()));
    await Promise.resolve().then(() => {
      setUser('grace');
      queueMicrotask(() => setFlag(true));
    });
    await settle();
    // Read outside rendering: the committed value.
    expect(user()).toBe('grace');
    expect(seen[seen.length - 1]).toBe('grace/true');
  });

  it('commits a state write made outside any event', async () => {
    const [n, setN] = createSignal(0, krateHookState);
    setTimeout(() => setN(4), 0);
    await settle();
    expect(n()).toBe(4);
  });
});
