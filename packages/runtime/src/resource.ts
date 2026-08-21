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

  const getSourceValue = (): S => {
    return typeof source === 'function' ? (source as () => S)() : source;
  };

  const fetch = async (): Promise<void> => {
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

  const resource: ResourceReturn<T> = Object.assign(data, {
    get loading() { return loading(); },
    get error() { return error(); },
    get state() { return state(); },
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