/**
 * Stand-in for the `cloudflare:workers` module. Server bundles import
 * `cloudflare:workers` through this specifier; the cloudflare deploy bundle
 * replaces it with the real module, so on a Worker `env` is the genuine
 * binding object. Everywhere else (`krate dev`, `krate serve`) it reads the
 * request's platform and fails with a message naming the missing binding.
 */
import { getPlatform } from './platform.js';

export const env: Record<string, any> = new Proxy({} as Record<string, any>, {
  get(_t, key) {
    const platform = getPlatform();
    const bindings = platform.env as Record<string | symbol, unknown>;
    if (key in bindings || typeof key === 'symbol') return bindings[key];
    throw new Error(
      `cloudflare:workers env.${String(key)} is not available here: Worker bindings exist only on the cloudflare target ` +
        '(krate deploy --target cloudflare), not in krate dev or krate serve',
    );
  },
  has: (_t, key) => key in (getPlatform().env as object),
  ownKeys: () => Reflect.ownKeys(getPlatform().env as object),
  getOwnPropertyDescriptor: (_t, key) => {
    const d = Reflect.getOwnPropertyDescriptor(getPlatform().env as object, key);
    return d ? { ...d, configurable: true } : undefined;
  },
});

export function waitUntil(work: Promise<unknown>): void {
  getPlatform().ctx.waitUntil(work);
}
