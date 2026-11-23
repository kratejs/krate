import { createCipheriv, createHash, randomBytes } from 'node:crypto';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { sealBoundArgs } from './action-seal.js';
import { openInlineToken, openSealedToken } from './server-handler.js';

// The server renderer seals closed-over values of an inline server action
// like this (see compiler/internal/build/inline_actions.go).
function seal(id: string, values: unknown[], secret: string): string {
  const key = createHash('sha256').update('krate-inline-action\0' + secret).digest();
  const iv = randomBytes(12);
  const cipher = createCipheriv('aes-256-gcm', key, iv);
  cipher.setAAD(Buffer.from(id));
  const body = Buffer.concat([cipher.update(JSON.stringify(values), 'utf8'), cipher.final()]);
  return 'k1.' + Buffer.concat([iv, body, cipher.getAuthTag()]).toString('base64url');
}

const entry = { id: 'a1', export: 'inline_0', file: 'x.mjs', inline: true, captures: ['id', 'when', 'gone'] };

describe('inline server action closure tokens', () => {
  it('opens a token sealed by the server renderer and restores special values', async () => {
    const token = seal('a1', ['p1', { $: 'd', v: '1970-01-01T00:00:00.000Z' }, { $: 'u' }], 'secret-secret-secret');
    const out = await openInlineToken(token, entry, 'secret-secret-secret');
    expect(out[0]).toBe('p1');
    expect(out[1]).toEqual(new Date(0));
    expect(out[2]).toBeUndefined();
  });

  it('rejects a token for another action, another key, or tampered bytes', async () => {
    const token = seal('a1', ['p1', 1, 2], 'secret-secret-secret');
    await expect(openInlineToken(token, { ...entry, id: 'a2' }, 'secret-secret-secret')).rejects.toThrow();
    await expect(openInlineToken(token, entry, 'another-secret-value')).rejects.toThrow();
    const flipped = token.slice(0, -4) + (token.endsWith('AAAA') ? 'BBBB' : 'AAAA');
    await expect(openInlineToken(flipped, entry, 'secret-secret-secret')).rejects.toThrow();
    await expect(openInlineToken(null, entry, 'secret-secret-secret')).rejects.toThrow();
  });
});

describe('sealed bound arguments of a module action', () => {
  const secret = 'bound-secret-bound-secret-1234';
  const prev = process.env.KRATE_ACTION_SECRET;
  beforeAll(() => { process.env.KRATE_ACTION_SECRET = secret; });

  afterAll(() => { if (prev === undefined) delete process.env.KRATE_ACTION_SECRET; else process.env.KRATE_ACTION_SECRET = prev; });
  const moduleEntry = { id: 'act1', export: 'del', file: 'x.mjs' };

  it('round-trips values and keeps Date, undefined and bigint', async () => {
    const token = sealBoundArgs('act1', ['item-7', { when: new Date(0), none: undefined, big: 5n }], 'props.action');
    expect(token.startsWith('k1.')).toBe(true);
    expect(token).not.toContain('item-7');
    const out = (await openSealedToken(token, moduleEntry, secret)) as any[];
    expect(out[0]).toBe('item-7');
    expect(out[1]).toEqual({ when: new Date(0), none: undefined, big: 5n });
  });

  it('rejects a tampered token and a token sealed for another action', async () => {
    const token = sealBoundArgs('act1', ['item-7'], 'props.action');
    await expect(openSealedToken(token, { ...moduleEntry, id: 'act2' }, secret)).rejects.toThrow();
    const flipped = token.slice(0, -4) + (token.endsWith('AAAA') ? 'BBBB' : 'AAAA');
    await expect(openSealedToken(flipped, moduleEntry, secret)).rejects.toThrow();
    await expect(openSealedToken('k1.AAAA', moduleEntry, secret)).rejects.toThrow();
  });

  it('names the prop and the value that cannot be sealed', () => {
    expect(() => sealBoundArgs('act1', ['ok', () => 1], 'props.action')).toThrow(/Prop `props\.action` is a server action bound to a value.*argument 2, which is a function/);
    expect(() => sealBoundArgs('act1', [new Map()], 'p')).toThrow(/a Map instance/);
  });
});
