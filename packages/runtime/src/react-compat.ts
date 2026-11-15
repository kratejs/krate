// React 19 APIs that preact/compat lacks, for React-model code that runs on
// Preact: a "use client" module, an island, an npm React library. The
// compiler's island builds re-export these next to everything preact/compat
// provides, as `react` (and `useFormStatus` as `react-dom`).
//
// The module takes the bundle's own compat namespace and Preact `options`, so
// it never imports Preact and stays on the project's single Preact instance.

type AnyFn = (...args: any[]) => any;

export interface FormStatus {
  pending: boolean;
  data: FormData | null;
  method: string | null;
  action: unknown;
}

export interface ReactCompatExtras {
  useOptimistic: AnyFn;
  useActionState: AnyFn;
  useFormState: AnyFn;
  useFormStatus: () => FormStatus;
  requestFormReset: (form: HTMLFormElement) => void;
  use: AnyFn;
  cache: AnyFn;
  useEffectEvent: AnyFn;
  startTransition: AnyFn;
  useTransition: AnyFn;
}

const IDLE: FormStatus = Object.freeze({ pending: false, data: null, method: null, action: null }) as FormStatus;
const EMPTY: readonly unknown[] = Object.freeze([]);

function isThenable(v: any): v is PromiseLike<unknown> {
  return !!v && typeof v.then === 'function';
}

function reportError(err: unknown): void {
  const g = globalThis as { reportError?: (e: unknown) => void };
  if (typeof g.reportError === 'function') g.reportError(err);
  else console.error(err);
}

const SHARED = '__krateReactCompat';

/**
 * Builds the additions once per Preact instance: the `react` and `react-dom`
 * modules of a bundle (and of every island chunk) share one action scope and
 * one form-status context.
 */
export function createReactCompatExtras(c: any, options: any): ReactCompatExtras {
  if (options[SHARED]) return options[SHARED] as ReactCompatExtras;
  const extras = build(c, options);
  options[SHARED] = extras;
  return extras;
}

function build(c: any, options: any): ReactCompatExtras {
  const { useState, useRef, useEffect, useCallback, useContext, createContext, createElement } = c;

  // Action scope: optimistic updates stay applied while any transition or
  // form action is pending and are dropped when the last one settles.
  let open = 0;
  const settled = new Set<() => void>();
  const begin = (): (() => void) => {
    open++;
    let done = false;
    return () => {
      if (done) return;
      done = true;
      if (--open === 0) for (const fn of Array.from(settled)) fn();
    };
  };

  function startTransition(fn: () => unknown): void {
    const end = begin();
    let r: unknown;
    try {
      r = fn();
    } catch (err) {
      end();
      throw err;
    }
    if (isThenable(r)) r.then(end, (err) => { end(); reportError(err); });
    else end();
  }

  function useTransition(): [boolean, (fn: () => unknown) => void] {
    const [pending, setPending] = useState(false);
    const start = useCallback((fn: () => unknown) => {
      const end = begin();
      let r: unknown;
      try {
        r = fn();
      } catch (err) {
        end();
        throw err;
      }
      if (!isThenable(r)) {
        end();
        return;
      }
      setPending(true);
      const finish = () => { setPending(false); end(); };
      r.then(finish, (err) => { finish(); reportError(err); });
    }, []);
    return [pending, start];
  }

  function useOptimistic<T, A>(passthrough: T, reducer?: (state: T, action: A) => T): [T, (action: A) => void] {
    const [updates, setUpdates] = useState(EMPTY as readonly A[]);
    useEffect(() => {
      const revert = () => setUpdates(EMPTY as readonly A[]);
      settled.add(revert);
      return () => { settled.delete(revert); };
    }, []);
    let value = passthrough;
    for (const a of updates) value = reducer ? reducer(value, a) : (a as unknown as T);
    const add = useCallback((action: A) => {
      setUpdates((u: readonly A[]) => u.concat([action]));
      // An update made outside any transition is dropped right away.
      if (open === 0) {
        Promise.resolve().then(() => { if (open === 0) setUpdates(EMPTY as readonly A[]); });
      }
    }, []);
    return [value, add];
  }

  // Form actions: a function in `action` or `formAction` is not a URL. The
  // vnode hook keeps it off the element and a document submit listener runs it.
  interface FormBox {
    fn: AnyFn;
    set?: (s: FormStatus) => void;
  }
  type ActionEl = HTMLElement & { __krateForm?: FormBox; __krateFormAction?: AnyFn };
  const StatusCtx = createContext(IDLE);

  function FormScope(props: { box: FormBox; children?: unknown }) {
    const [status, setStatus] = useState(IDLE);
    props.box.set = setStatus;
    return createElement(StatusCtx.Provider, { value: status }, props.children);
  }

  const isPlainAction = (f: unknown): f is AnyFn =>
    typeof f === 'function' && typeof (f as { $$krateAction?: unknown }).$$krateAction !== 'string';

  function withRef(vnode: any, attach: (el: ActionEl) => void): void {
    const orig = vnode.ref;
    vnode.ref = (el: ActionEl | null) => {
      if (el) attach(el);
      if (typeof orig === 'function') orig(el);
      else if (orig && typeof orig === 'object') orig.current = el;
    };
  }

  function onSubmit(e: Event): void {
    const form = e.target as ActionEl | null;
    if (!form || form.tagName !== 'FORM' || e.defaultPrevented) return;
    const submitter = ((e as SubmitEvent).submitter as ActionEl | null) || null;
    const box = form.__krateForm;
    const fn = (submitter && submitter.__krateFormAction) || (box && box.fn);
    if (typeof fn !== 'function') return;
    e.preventDefault();
    let data: FormData;
    try {
      data = submitter ? new FormData(form as unknown as HTMLFormElement, submitter) : new FormData(form as unknown as HTMLFormElement);
    } catch {
      data = new FormData(form as unknown as HTMLFormElement);
    }
    const end = begin();
    if (box && box.set) box.set({ pending: true, data, method: 'post', action: fn });
    let ok = false;
    Promise.resolve()
      .then(() => fn(data))
      .then(() => { ok = true; }, reportError)
      .then(() => {
        if (box && box.set) box.set(IDLE);
        end();
        if (ok && form.isConnected) (form as unknown as HTMLFormElement).reset();
      });
  }

  {
    const prev = options.vnode;
    options.vnode = (vnode: any) => {
      const p = vnode.props;
      if (p && typeof vnode.type === 'string') {
        if (vnode.type === 'form' && isPlainAction(p.action)) {
          const box: FormBox = { fn: p.action };
          p.action = undefined;
          withRef(vnode, (el) => { el.__krateForm = box; });
          p.children = createElement(FormScope, { box }, p.children);
        } else if (isPlainAction(p.formAction)) {
          const fn = p.formAction as AnyFn;
          p.formAction = undefined;
          withRef(vnode, (el) => { el.__krateFormAction = fn; });
        }
      }
      if (prev) prev(vnode);
    };
    if (typeof document !== 'undefined') document.addEventListener('submit', onSubmit, false);
  }

  function useFormStatus(): FormStatus {
    return useContext(StatusCtx);
  }

  function requestFormReset(form: HTMLFormElement): void {
    form.reset();
  }

  function useActionState<S>(
    action: (prev: S, payload: unknown) => S | Promise<S>,
    initialState: S,
    _permalink?: string,
  ): [S, (payload: unknown) => Promise<void>, boolean] {
    const [state, setState] = useState(initialState);
    const [pending, setPending] = useState(false);
    const [failure, setFailure] = useState(null as null | { error: unknown });
    const live = useRef(null as any);
    if (!live.current) live.current = { state, action, queued: 0, chain: Promise.resolve(), cancelled: false };
    const l = live.current;
    l.action = action;
    l.state = state;
    const dispatch = useCallback((payload: unknown): Promise<void> => {
      l.queued++;
      setPending(true);
      const end = begin();
      const run = l.chain.then(async () => {
        if (l.cancelled) return;
        const next = await l.action(l.state, payload);
        l.state = next;
        setState(() => next);
      });
      l.chain = run.then(undefined, (error: unknown) => {
        // An error cancels the queued calls and surfaces at the nearest error boundary.
        l.cancelled = true;
        setFailure({ error });
      });
      return l.chain.then(() => {
        if (--l.queued === 0) {
          l.cancelled = false;
          setPending(false);
        }
        end();
      });
    }, []);
    if (failure) throw failure.error;
    return [state, dispatch, pending];
  }

  function use(usable: any): any {
    if (isThenable(usable)) {
      const p = usable as PromiseLike<unknown> & { status?: string; value?: unknown; reason?: unknown };
      if (p.status === 'fulfilled') return p.value;
      if (p.status === 'rejected') throw p.reason;
      if (p.status !== 'pending') {
        p.status = 'pending';
        p.then(
          (v) => { p.status = 'fulfilled'; p.value = v; },
          (e) => { p.status = 'rejected'; p.reason = e; },
        );
      }
      // preact/compat's Suspense catches a thrown promise and re-renders when it settles.
      throw p;
    }
    // Preact's createContext returns a function carrying Provider and Consumer.
    if (usable && (typeof usable === 'object' || typeof usable === 'function') && 'Provider' in usable) return useContext(usable);
    throw new Error('use() received something that is neither a promise nor a context.');
  }

  function cache<F extends AnyFn>(fn: F): F {
    const root = new Map<unknown, any>();
    const result = Symbol('result');
    return function (this: unknown, ...args: unknown[]) {
      let node = root;
      for (const a of args) {
        let next = node.get(a);
        if (!next) node.set(a, (next = new Map()));
        node = next;
      }
      if (!node.has(result)) {
        try {
          node.set(result, { value: fn.apply(this, args) });
        } catch (error) {
          node.set(result, { error, failed: true });
        }
      }
      const r = node.get(result);
      if (r.failed) throw r.error;
      return r.value;
    } as unknown as F;
  }

  function useEffectEvent<F extends AnyFn>(fn: F): F {
    const ref = useRef(fn);
    ref.current = fn;
    return useCallback((...args: unknown[]) => ref.current(...args), []) as unknown as F;
  }

  return {
    useOptimistic,
    useActionState,
    useFormState: useActionState,
    useFormStatus,
    requestFormReset,
    use,
    cache,
    useEffectEvent,
    startTransition,
    useTransition,
  };
}
