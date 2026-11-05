// Pluggable shared cache.
//
// By default every cache lives in the memory of one process. Setting
// `cache: { adapter: "redis", url }` in krate.config (or passing an adapter to
// createKrateHandler) moves the ISR page cache and the data cache (fetch with
// next options, unstable_cache) into a store every instance reads and writes, and
// makes revalidatePath / revalidateTag on one instance invalidate all of them.
//
// An adapter is a small key-value store with tag deletion and pub/sub. Write
// your own with the CacheAdapter interface below and point `cache.adapter` at a
// module that default-exports `(options) => CacheAdapter` (see the caching
// guide).
//
// Whatever the adapter does, a cache problem is never a request problem: every
// call is bounded by a timeout, failures are logged once, an unhealthy adapter
// is skipped for a while, and the page is rendered instead.

import { cacheRuntime, type SharedDataStore } from './cache-scope.js';

export interface CacheSetOptions {
  /** Seconds to keep the entry; omitted or Infinity keeps it until it is deleted. */
  ttlSeconds?: number;
  /** Tags the entry can be deleted by (deleteByTags). */
  tags?: string[];
}

/** The storage contract of a shared cache. Every method may reject; callers cope. */
export interface CacheAdapter {
  readonly name: string;
  /** The value stored under key, or null. */
  get(key: string): Promise<string | null>;
  set(key: string, value: string, options?: CacheSetOptions): Promise<void>;
  delete(key: string): Promise<void>;
  /** The keys stored with tag. */
  keysByTag(tag: string): Promise<string[]>;
  /** Deletes every key stored with one of tags; resolves with how many went. */
  deleteByTags(tags: string[]): Promise<number>;
  /** Sends message to every subscriber of channel, including this process. */
  publish(channel: string, message: string): Promise<void>;
  /**
   * Calls handler for each message published on channel. After a lost
   * connection that may have dropped messages, handler is called with "*":
   * treat it as "everything may be stale". Resolves with an unsubscribe function.
   */
  subscribe(channel: string, handler: (message: string) => void): Promise<() => void>;
  /** Releases connections. */
  close(): Promise<void>;
}

/** What the config and KRATE_CACHE carry. */
export interface CacheSpec {
  /** "memory" (default), "redis", or a module specifier / path exporting an adapter factory. */
  adapter?: string;
  url?: string;
  /** Key prefix, so several apps can share one store (default "krate:"). */
  prefix?: string;
  /** Milliseconds one cache call may take before it counts as a failure (default 250). */
  timeoutMs?: number;
  /** Milliseconds to keep ISR entries in the shared store (default 7 days). */
  ttlSeconds?: number;
  /** Adapter-specific options passed to a custom factory. */
  options?: Record<string, unknown>;
}

export const INVALIDATE_CHANNEL = 'invalidate';

/** The adapter tag a cache tag is stored under (kept apart from route index tags). */
export function tagName(tag: string): string {
  return 'tag:' + tag;
}

// A bounded, failure-tolerant wrapper

export type GuardResult<T> = { ok: true; value: T } | { ok: false };

/**
 * Runs adapter calls with a timeout and a circuit breaker. After a few
 * consecutive failures calls are skipped (reported as failed) for a cool-down,
 * so a dead cache costs a microsecond per request, not a timeout.
 */
export class CacheGuard {
  private failures = 0;
  private openUntil = 0;
  private lastLog = 0;

  constructor(
    readonly adapter: CacheAdapter,
    private readonly timeoutMs = 250,
    private readonly threshold = 3,
    private readonly coolDownMs = 5000,
  ) {}

  get healthy(): boolean {
    return Date.now() >= this.openUntil;
  }

  async call<T>(fn: (a: CacheAdapter) => Promise<T>): Promise<GuardResult<T>> {
    if (!this.healthy) return { ok: false };
    let timer: ReturnType<typeof setTimeout> | undefined;
    try {
      const value = await Promise.race([
        fn(this.adapter),
        new Promise<never>((_, reject) => {
          timer = setTimeout(() => reject(new Error(`cache ${this.adapter.name} did not answer within ${this.timeoutMs}ms`)), this.timeoutMs);
        }),
      ]);
      this.failures = 0;
      return { ok: true, value };
    } catch (err) {
      this.failures++;
      if (this.failures >= this.threshold) this.openUntil = Date.now() + this.coolDownMs;
      const now = Date.now();
      if (now - this.lastLog > 30000) {
        this.lastLog = now;
        console.warn(`[krate] shared cache (${this.adapter.name}) failed, serving without it: ${(err as Error)?.message || err}`);
      }
      return { ok: false };
    } finally {
      if (timer) clearTimeout(timer);
    }
  }
}

// Shared data cache (fetch with next options, unstable_cache)

const DATA_PREFIX = 'data:';

export class SharedDataCache implements SharedDataStore {
  constructor(private readonly guard: CacheGuard) {}

  async get(key: string): Promise<GuardResult<string | undefined>> {
    const r = await this.guard.call((a) => a.get(DATA_PREFIX + key));
    if (!r.ok) return r;
    return { ok: true, value: r.value === null ? undefined : r.value };
  }

  async set(key: string, json: string, ttlSeconds: number, tags: string[]): Promise<boolean> {
    const r = await this.guard.call((a) =>
      a.set(DATA_PREFIX + key, json, { ttlSeconds: ttlSeconds === Infinity ? undefined : ttlSeconds, tags: tags.map(tagName) }),
    );
    return r.ok;
  }

  async deleteTags(tags: string[]): Promise<number> {
    const r = await this.guard.call((a) => a.deleteByTags(tags.map(tagName)));
    return r.ok ? r.value : 0;
  }
}

// In-process adapter (tests, and the reference for adapter authors)

/** A CacheAdapter over a Map. Two processes cannot share it; it exists for tests and as the reference. */
export class MemoryAdapter implements CacheAdapter {
  readonly name = 'memory';
  private values = new Map<string, { value: string; expires: number; tags: string[] }>();
  private byTag = new Map<string, Set<string>>();
  private subs = new Map<string, Set<(m: string) => void>>();

  async get(key: string): Promise<string | null> {
    const e = this.values.get(key);
    if (!e) return null;
    if (e.expires <= Date.now()) {
      await this.delete(key);
      return null;
    }
    return e.value;
  }

  async set(key: string, value: string, options: CacheSetOptions = {}): Promise<void> {
    await this.delete(key);
    const ttl = options.ttlSeconds;
    const tags = options.tags || [];
    this.values.set(key, { value, expires: ttl && ttl > 0 && ttl !== Infinity ? Date.now() + ttl * 1000 : Infinity, tags });
    for (const t of tags) {
      let keys = this.byTag.get(t);
      if (!keys) this.byTag.set(t, (keys = new Set()));
      keys.add(key);
    }
  }

  async delete(key: string): Promise<void> {
    const e = this.values.get(key);
    if (!e) return;
    this.values.delete(key);
    for (const t of e.tags) this.byTag.get(t)?.delete(key);
  }

  async keysByTag(tag: string): Promise<string[]> {
    return [...(this.byTag.get(tag) || [])];
  }

  async deleteByTags(tags: string[]): Promise<number> {
    let n = 0;
    for (const t of tags) {
      for (const k of [...(this.byTag.get(t) || [])]) {
        if (this.values.has(k)) n++;
        await this.delete(k);
      }
      this.byTag.delete(t);
    }
    return n;
  }

  async publish(channel: string, message: string): Promise<void> {
    for (const h of [...(this.subs.get(channel) || [])]) h(message);
  }

  async subscribe(channel: string, handler: (message: string) => void): Promise<() => void> {
    let set = this.subs.get(channel);
    if (!set) this.subs.set(channel, (set = new Set()));
    set.add(handler);
    return async () => void set!.delete(handler);
  }

  async close(): Promise<void> {
    this.values.clear();
    this.byTag.clear();
    this.subs.clear();
  }
}

// Loading and installing

type AdapterFactory = (options: Record<string, unknown> & { url?: string; prefix?: string; timeoutMs?: number }) => CacheAdapter | Promise<CacheAdapter>;

/** Resolves a spec to an adapter, or null for the default in-process caches. */
export async function loadCacheAdapter(spec: CacheSpec | undefined): Promise<CacheAdapter | null> {
  if (!spec || !spec.adapter || spec.adapter === 'memory') return null;
  const opts = { ...(spec.options || {}), url: spec.url, prefix: spec.prefix, timeoutMs: spec.timeoutMs };
  if (spec.adapter === 'redis') {
    const { RedisAdapter } = await import('./cache-redis.js');
    if (!spec.url) throw new Error('cache.adapter "redis" needs cache.url (redis://host:6379)');
    return new RedisAdapter(opts as { url: string; prefix?: string; timeoutMs?: number });
  }
  const mod = (await import(spec.adapter)) as { default?: unknown; createAdapter?: unknown };
  const factory = (mod.default ?? mod.createAdapter) as AdapterFactory | CacheAdapter | undefined;
  if (typeof factory === 'function') return factory(opts);
  if (factory && typeof (factory as CacheAdapter).get === 'function') return factory as CacheAdapter;
  throw new Error(`cache adapter module ${spec.adapter} must default-export a CacheAdapter or a (options) => CacheAdapter factory`);
}

/** Reads the cache spec the Go host passes in KRATE_CACHE. */
export function cacheSpecFromEnv(env: Record<string, string | undefined>): CacheSpec | undefined {
  const raw = env.KRATE_CACHE;
  if (!raw) return undefined;
  try {
    return JSON.parse(raw) as CacheSpec;
  } catch {
    console.warn('[krate] KRATE_CACHE is not valid JSON; using the in-process caches');
    return undefined;
  }
}

export interface InstalledCache {
  adapter: CacheAdapter;
  guard: CacheGuard;
  data: SharedDataCache;
}

/**
 * Wires an adapter into the process: the data cache reads and writes through it
 * (cacheRuntime().shared), and the returned stores replace the in-process ISR
 * cache. Pass the returned `isr` as the renderer's isrStore.
 */
export function installCacheAdapter(adapter: CacheAdapter, spec: CacheSpec = {}): InstalledCache {
  const guard = new CacheGuard(adapter, spec.timeoutMs && spec.timeoutMs > 0 ? spec.timeoutMs : 250);
  const data = new SharedDataCache(guard);
  cacheRuntime().shared = data;
  return { adapter, guard, data };
}

/**
 * The cache spec a handler bundle carries (data.config.cache): the adapter and
 * its tuning, with the URL left to the environment so no secret is bundled.
 */
export function cacheSpecFromConfig(cfg: (CacheSpec & { urlEnv?: string }) | undefined): CacheSpec | undefined {
  if (!cfg || !cfg.adapter || cfg.adapter === 'memory') return undefined;
  const env = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env || {};
  const url = cfg.url || (cfg.urlEnv ? env[cfg.urlEnv] : undefined) || env.KRATE_CACHE_URL;
  return { ...cfg, url };
}
