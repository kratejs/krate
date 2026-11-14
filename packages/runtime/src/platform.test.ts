import { describe, expect, it } from 'vitest';
import { after } from './after.js';
import { env as shimEnv } from './cloudflare-workers.js';
import { getCloudflareContext, getPlatform } from './platform.js';
import { runWithPlatform } from './server-handler.js';

function fakeContext() {
  const waited: Promise<unknown>[] = [];
  return { waited, ctx: { env: { DB: 'db-binding', GREETING: 'hi' }, cf: { country: 'NL' }, waitUntil: (p: Promise<unknown>) => waited.push(p) } };
}

describe('platform access', () => {
  it('returns an empty environment outside a request', () => {
    const p = getPlatform();
    expect(p.target).toBe('none');
    expect(p.env).toEqual({});
    expect(() => getCloudflareContext()).toThrow(/outside|no request|cloudflare target/);
  });

  it('exposes env, ctx and cf of the request being served', async () => {
    const { ctx } = fakeContext();
    const seen = await runWithPlatform('cloudflare', ctx, async () => {
      await Promise.resolve();
      const cf = getCloudflareContext<{ DB: string }>();
      return { db: cf.env.DB, country: cf.cf?.country, target: getPlatform().target };
    });
    expect(seen).toEqual({ db: 'db-binding', country: 'NL', target: 'cloudflare' });
    expect(getPlatform().target).toBe('none');
  });

  it('refuses getCloudflareContext on other targets', () => {
    const { ctx } = fakeContext();
    expect(() => runWithPlatform('node', ctx, () => getCloudflareContext())).toThrow(/"node"/);
  });

  it('backs after() with ctx.waitUntil', async () => {
    const { ctx, waited } = fakeContext();
    let ran = false;
    runWithPlatform('cloudflare', ctx, () => after(() => { ran = true; }));
    expect(waited).toHaveLength(1);
    await waited[0];
    expect(ran).toBe(true);
  });

  it('serves the cloudflare:workers stand-in from the same request env', () => {
    const { ctx } = fakeContext();
    expect(runWithPlatform('cloudflare', ctx, () => shimEnv.DB)).toBe('db-binding');
    expect(() => shimEnv.DB).toThrow(/cloudflare target/);
  });
});
