// Persistent state for Krate signals. `createSignal(value, { persist })` reads
// a stored value on creation and writes on every change. Persistence is
// SSR-safe (no storage access without a DOM) and supports cross-tab sync.
//
// Durable SSR state: when the page embeds `window.__KRATE_STATE__` (a JSON
// object keyed by persistence key), that server-chosen value takes precedence
// over browser storage on the first hydration, so the hydrated value matches
// what the server rendered.

/** Minimal storage surface used by the persistence layer. */
export interface StorageLike {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

/** Where a persisted signal is stored. */
export type PersistStore = 'local' | 'session' | 'memory' | StorageLike;

/** Options for a persisted signal. */
export interface PersistOptions<T> {
  /** Storage key. */
  key: string;
  /** Backing store (default 'local'). */
  store?: PersistStore;
  /** Serialize a value to a string (default JSON.stringify). */
  serialize?: (value: T) => string;
  /** Deserialize a string to a value (default JSON.parse). */
  deserialize?: (raw: string) => T;
  /** Synchronize across tabs/windows via the `storage` event (default true for 'local'). */
  sync?: boolean;
}

/** A persist option is either a bare key or a full options object. */
export type PersistSpec<T> = string | PersistOptions<T>;

/** Resolved persistence configuration. */
export interface ResolvedPersist<T> {
  key: string;
  store: PersistStore;
  serialize: (value: T) => string;
  deserialize: (raw: string) => T;
  sync: boolean;
}

function memoryStore(): StorageLike {
  const map = new Map<string, string>();
  return {
    getItem: (k) => (map.has(k) ? (map.get(k) as string) : null),
    setItem: (k, v) => {
      map.set(k, v);
    },
    removeItem: (k) => {
      map.delete(k);
    },
  };
}

// A shared in-memory fallback so persistence degrades gracefully when storage
// is unavailable (SSR, private mode, quota errors) instead of throwing.
const memoryFallback = memoryStore();

function hasWindow(): boolean {
  return typeof window !== 'undefined';
}

function pickStore(store: PersistStore | undefined): StorageLike {
  if (store && typeof store === 'object') return store;
  if (!hasWindow()) return memoryFallback;
  try {
    if (store === 'session') return window.sessionStorage;
    if (store === 'memory') return memoryFallback;
    return window.localStorage;
  } catch {
    return memoryFallback;
  }
}

/** Resolve a persist spec into concrete read/write configuration. */
export function resolvePersist<T>(spec: PersistSpec<T>): ResolvedPersist<T> {
  const opts: PersistOptions<T> = typeof spec === 'string' ? { key: spec } : spec;
  if (!opts.key) {
    throw new Error('[krate] createSignal persist requires a key');
  }
  const store = opts.store ?? 'local';
  return {
    key: opts.key,
    store,
    serialize: opts.serialize ?? ((v: T) => JSON.stringify(v)),
    deserialize: opts.deserialize ?? ((raw: string) => JSON.parse(raw) as T),
    sync: opts.sync ?? store === 'local',
  };
}

/** A read result distinguishes "absent" from a stored `undefined`/`null`. */
export interface Restored<T> {
  found: boolean;
  value?: T;
}

/** Read the durable SSR state embedded in the page for a key, if any. */
export function readDurableState<T>(key: string, deserialize: (raw: string) => T): Restored<T> {
  const g = globalThis as { __KRATE_STATE__?: Record<string, unknown> };
  const state = g.__KRATE_STATE__;
  if (!state || !Object.prototype.hasOwnProperty.call(state, key)) {
    return { found: false };
  }
  const raw = state[key];
  if (typeof raw === 'string') {
    try {
      return { found: true, value: deserialize(raw) };
    } catch {
      // The durable value may already be the plain value (e.g. "dark") rather
      // than its serialized form (e.g. "\"dark\""); use it verbatim.
      return { found: true, value: raw as unknown as T };
    }
  }
  return { found: true, value: raw as T };
}

/** Read a persisted value from a store, tolerating unavailable storage. */
export function readPersisted<T>(p: ResolvedPersist<T>): Restored<T> {
  const store = pickStore(p.store);
  let raw: string | null;
  try {
    raw = store.getItem(p.key);
  } catch {
    return { found: false };
  }
  if (raw === null || raw === undefined) {
    return { found: false };
  }
  try {
    return { found: true, value: p.deserialize(raw) };
  } catch {
    return { found: false };
  }
}

/** Persist a value, silently ignoring storage failures. */
export function writePersisted<T>(p: ResolvedPersist<T>, value: T): void {
  const store = pickStore(p.store);
  try {
    store.setItem(p.key, p.serialize(value));
  } catch {
    // Quota exceeded / storage disabled: keep the in-memory value only.
  }
}

/** Remove a persisted value. */
export function clearPersisted<T>(p: ResolvedPersist<T>): void {
  const store = pickStore(p.store);
  try {
    store.removeItem(p.key);
  } catch {
    // ignore
  }
}

// A package-scoped registry of cross-tab listeners, installed lazily so the
// runtime pays nothing when no signal opts into sync.
type SyncListener = (raw: string | null) => void;
const syncListeners = new Map<string, Set<SyncListener>>();
let storageListenerInstalled = false;

function installStorageListener(): void {
  if (storageListenerInstalled || !hasWindow()) return;
  storageListenerInstalled = true;
  window.addEventListener('storage', (e: StorageEvent) => {
    if (e.key === null) return;
    const listeners = syncListeners.get(e.key);
    if (!listeners) return;
    for (const fn of listeners) fn(e.newValue);
  });
}

/**
 * Subscribe to cross-tab changes for a persisted key. Returns an unsubscribe
 * function. The callback receives the raw new value (null when cleared).
 */
export function subscribePersist<T>(p: ResolvedPersist<T>, cb: (raw: string | null) => void): () => void {
  if (!p.sync) return () => {};
  const listeners = syncListeners.get(p.key) ?? new Set<SyncListener>();
  listeners.add(cb);
  syncListeners.set(p.key, listeners);
  installStorageListener();
  return () => {
    listeners.delete(cb);
    if (listeners.size === 0) syncListeners.delete(p.key);
  };
}
