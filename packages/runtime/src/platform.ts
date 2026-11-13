/**
 * Platform access for server code, exported from `@krate/runtime/server`.
 *
 * The portable server handler runs every request inside an AsyncLocalStorage
 * scope published on `globalThis[Symbol.for('krate.platform.store')]` (the
 * same pattern as the request store), so pages, API routes, server actions
 * and middleware all see the same `{ env, ctx, cf }` of the request in flight.
 * On the cloudflare target `env` holds the Worker bindings (D1, KV, R2,
 * Durable Objects, secrets); it is never copied into `process.env`.
 */

/** The deploy target the handler runs on, or "none" outside a handler (dev, tests). */
export type PlatformTarget = 'cloudflare' | 'vercel' | 'netlify' | 'node' | 'none';

/** Keeps the instance alive for work that outlives the response. */
export interface PlatformExecutionContext {
  waitUntil(work: Promise<unknown>): void;
  /** Cloudflare only: let the request fall through to the origin when the Worker throws. */
  passThroughOnException?(): void;
}

/**
 * Augment this interface with your bindings to type `env`:
 *
 *   declare module '@krate/runtime/server' {
 *     interface KrateCloudflareEnv { DB: D1Database; CACHE: KVNamespace }
 *   }
 */
// eslint-disable-next-line @typescript-eslint/no-empty-object-type
export interface KrateCloudflareEnv {}

export interface PlatformContext<Env = Record<string, unknown>> {
  target: PlatformTarget;
  /** The Worker `env` on cloudflare; an empty object elsewhere. */
  env: Env;
  ctx: PlatformExecutionContext;
  /** Cloudflare's `request.cf` properties (colo, country, ...), when present. */
  cf: Record<string, unknown> | undefined;
}

export interface CloudflareContext<Env = KrateCloudflareEnv & Record<string, any>> extends PlatformContext<Env> {
  target: 'cloudflare';
}

interface PlatformStore {
  getStore(): PlatformContext | undefined;
}

const PLATFORM_STORE = Symbol.for('krate.platform.store');

function current(): PlatformContext | undefined {
  const store = (globalThis as Record<symbol, unknown>)[PLATFORM_STORE] as PlatformStore | undefined;
  return store && typeof store.getStore === 'function' ? store.getStore() : undefined;
}

/**
 * The platform of the request being served. Outside a request handler (`krate
 * dev`, tests) it returns an empty environment and a `waitUntil` that just
 * detaches the promise, so code that only uses it opportunistically keeps working.
 */
export function getPlatform<Env = Record<string, unknown>>(): PlatformContext<Env> {
  const c = current();
  if (c) return c as PlatformContext<Env>;
  return {
    target: 'none',
    env: {} as Env,
    ctx: {
      waitUntil(work) {
        Promise.resolve(work).catch((err) => console.error(`[krate] waitUntil task failed: ${(err && (err as Error).stack) || err}`));
      },
    },
    cf: undefined,
  };
}

/**
 * The Cloudflare `{ env, ctx, cf }` of the request being served. Throws
 * outside a Worker request, so a missing binding is an error at the call
 * site rather than an `undefined` later.
 */
export function getCloudflareContext<Env = KrateCloudflareEnv & Record<string, any>>(): CloudflareContext<Env> {
  const c = current();
  if (!c || c.target !== 'cloudflare') {
    throw new Error(
      'getCloudflareContext() works only while a request is served by the cloudflare target (krate deploy --target cloudflare); ' +
        (c ? `this request is served by "${c.target}"` : 'there is no request in flight here (krate dev and krate serve run on Node and have no Worker bindings)') +
        '. Use getPlatform() for code that also has to run elsewhere.',
    );
  }
  return c as CloudflareContext<Env>;
}

/** Runs work after the response is sent, through the platform's waitUntil when there is one. */
export function platformWaitUntil(work: Promise<unknown>): boolean {
  const c = current();
  if (!c || c.target === 'none' || !c.ctx || typeof c.ctx.waitUntil !== 'function') return false;
  c.ctx.waitUntil(work);
  return true;
}
