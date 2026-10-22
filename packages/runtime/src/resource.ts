import { createSignal, createEffect, onCleanup } from './signal.js';

export interface ResourceReturn<T> {
  (): T | undefined;
  loading: boolean;
  error: unknown;
  state: 'unresolved' | 'loading' | 'ready' | 'error' | 'refreshing';
}

export interface ResourceActions<T> {
  mutate: (fn: (prev: T | undefined) => T) => void;
  refetch: () => void;
}

// Bridge to the optional Suspense module (shipped as a separate bundle). When
// present, a resource read while loading suspends the Suspense boundary of the
// component that created it.
interface SuspenseHook {
  current(): unknown;
  suspend(boundary: unknown, promise: PromiseLike<unknown>): void;
}

function suspenseHook(): SuspenseHook | undefined {
  return (globalThis as { __krateSuspenseHook?: SuspenseHook }).__krateSuspenseHook;
}

export function createResource<T, S = void>(
  source: (() => S) | S,
  fetcher: (source: S, opts?: { signal?: AbortSignal }) => Promise<T>
): [ResourceReturn<T>, ResourceActions<T>] {
  const [data, setData] = createSignal<T | undefined>(undefined);
  const [loading, setLoading] = createSignal(true);
  const [error, setError] = createSignal<unknown>(undefined);
  const [state, setState] = createSignal<ResourceReturn<T>['state']>('unresolved');
  const [version, setVersion] = createSignal(0);

  let abortController: AbortController | undefined;
  const owner = suspenseHook()?.current() ?? null;
  let inflight: Promise<void> | null = null;
  let notified: Promise<void> | null = null;

  const getSourceValue = (): S => {
    return typeof source === 'function' ? (source as () => S)() : source;
  };

  const fetch = (): Promise<void> => {
    const run = doFetch();
    inflight = run;
    run.then(() => {
      if (inflight === run) inflight = null;
    });
    return run;
  };

  const doFetch = async (): Promise<void> => {
    if (abortController) {
      abortController.abort();
    }
    abortController = new AbortController();

    // Fix 2.3: Capture local reference so concurrent fetches don't interfere
    // with the abort check after await
    const controller = abortController;

    const src = getSourceValue();
    setState(version() > 0 ? 'refreshing' : 'loading');
    setLoading(true);
    setError(undefined);

    try {
      const result = await fetcher(src, { signal: controller.signal });
      if (!controller.signal.aborted) {
        setData(() => result);
        setState('ready');
      }
    } catch (e: any) {
      if (e?.name !== 'AbortError' && !controller.signal.aborted) {
        setError(e);
        setState('error');
      }
    } finally {
      if (!controller.signal.aborted) {
        setLoading(false);
      }
    }
  };

  // Auto-fetch when source changes
  createEffect(() => {
    // Read source to track it for reactivity
    if (typeof source === 'function' && source.length === 0) {
      (source as () => S)();
    }
    // Read version to track refetches
    version();
    fetch();
    onCleanup(() => {
      if (abortController) {
        abortController.abort();
        abortController = undefined;
      }
    });
  });

  // Note: use Object.defineProperties, NOT Object.assign, for these getters.
  // Object.assign evaluates source getters via [[Set]] and copies the *value*
  // as a plain data property, snapshotting state/loading/error once at creation
  // so they never track reactive updates. defineProperties installs live accessors.
  const resource = function (): T | undefined {
    if (owner && inflight && notified !== inflight) {
      notified = inflight;
      suspenseHook()?.suspend(owner, inflight);
    }
    return data();
  } as ResourceReturn<T>;
  Object.defineProperties(resource, {
    loading: { get: () => loading() },
    error: { get: () => error() },
    state: { get: () => state() },
  });

  const actions: ResourceActions<T> = {
    mutate: (fn: (prev: T | undefined) => T) => {
      setData(fn);
    },
    refetch: () => {
      setVersion(v => v + 1);
    },
  };

  return [resource, actions];
}