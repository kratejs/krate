import { describe, it, expect, beforeEach } from 'vitest';
import { createSignal, createEffect, disposeAll, krateHookState } from './signal';
import { createEffectDeps } from './effects';

const tick = () => new Promise((r) => setTimeout(r, 0));
async function settle(): Promise<void> {
  for (let i = 0; i < 4; i++) await tick();
}

describe('mixed update lanes', () => {
  beforeEach(() => disposeAll());

  it('keeps a promise-driven React state write when a plain signal is written in a later microtask', async () => {
    const [user, setUser] = createSignal('nobody', krateHookState);
    const [flag, setFlag] = createSignal(false);
    const text: string[] = [];
    const effects: string[] = [];
    createEffect(() => void text.push(user() + '/' + flag()));
    createEffectDeps(() => void effects.push('user ' + user()), () => [user()]);
    await settle();
    text.length = 0;
    effects.length = 0;
    await Promise.resolve().then(() => {
      setUser('grace');
      queueMicrotask(() => setFlag(true));
    });
    await settle();
    expect(user()).toBe('grace');
    expect(text[text.length - 1]).toBe('grace/true');
    expect(effects).toEqual(['user grace']);
  });

  it('keeps the React state write when the plain write comes first', async () => {
    const [user, setUser] = createSignal('nobody', krateHookState);
    const [flag, setFlag] = createSignal(false);
    const seen: string[] = [];
    createEffect(() => void seen.push(user() + '/' + flag()));
    await settle();
    await Promise.resolve().then(() => {
      setFlag(true);
      setUser('grace');
    });
    await settle();
    expect(seen[seen.length - 1]).toBe('grace/true');
    expect(user()).toBe('grace');
  });

  it('commits a React write made while a microtask flush is already queued', async () => {
    const [user, setUser] = createSignal('nobody', krateHookState);
    const [flag, setFlag] = createSignal(0);
    const seen: string[] = [];
    createEffect(() => void seen.push(user() + '/' + flag()));
    createEffectDeps(() => void seen.push('effect ' + user()), () => [user()]);
    await settle();
    seen.length = 0;
    setFlag(1);
    await Promise.resolve();
    setUser('grace');
    await settle();
    expect(seen).toContain('effect grace');
    expect(user()).toBe('grace');
  });
});
