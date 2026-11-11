// Host-side signal: read/write, lives in the Go or TS host.
export interface HostSignal<T> {
  get(): T;
  set(v: T): void;
  update(f: (prev: T) => T): void;
  subscribe(fn: (v: T) => void): () => void;
}

// UI view of a host signal: a callable getter, like Krate runtime signals, so
// `{Count()}` works in JSX. Read-only.
// R is the getter's type: the client's view is T | undefined until the first value.
export interface ReadonlyHostSignal<T, R = T> {
  (): R;
  subscribe(fn: (v: T) => void): () => void;
}

export interface CallOptions {
  signal?: AbortSignal;
  // Share one in-flight request between identical calls (use for reads).
  dedupe?: boolean;
}

export type Impl = Record<string, any>;
