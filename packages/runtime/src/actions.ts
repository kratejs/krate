/**
 * React 19 actions on Krate's server-action transport.
 *
 * This module owns the client half of server actions and the action hooks the
 * compiler lowers React source to:
 *
 * - `__krateAction(id)` builds the client proxy for a `"use server"` export.
 * - A document `submit` listener runs `<form action={fn}>` and
 *   `<button formAction={fn}>` actions (server proxies or client functions)
 *   inside an async action scope, drives `useFormStatus`, and resets the form
 *   after success. Without JavaScript, forms bound to server actions still POST
 *   to the action URL.
 * - `__krateActionState` backs `useActionState`, `__krateOptimistic` backs
 *   `useOptimistic`, and `__krateFormStatusMount` backs `useFormStatus`.
 *
 * Wire format (see guides/server-actions.md):
 *   request  JSON       {"args":[...]}                  every argument JSON
 *            multipart  form fields + "__krate_args"     last argument a FormData;
 *                       (JSON array of leading args)
 *   response {"ok":true,"result":R,"redirect":"/path"?}
 *            {"ok":false,"error":{"message":M,"digest":D}}
 */
import { createEffect, onCleanup } from './signal.js';

/** Field and query parameter carrying the JSON leading arguments of a form call. */
export const ACTION_ARGS_FIELD = '__krate_args';

type AnyFn = (...args: unknown[]) => unknown;

/** A client proxy for a server action. */
export interface ServerActionProxy {
  (...args: unknown[]): Promise<unknown>;
  readonly actionUrl: string;
  readonly $$krateAction: string;
  readonly $$krateBound: unknown[];
  bind(thisArg: unknown, ...args: unknown[]): ServerActionProxy;
}

/** Error raised on the client when a server action fails. */
export class ServerActionError extends Error {
  /** Opaque id logged next to the full error on the server. */
  digest?: string;
  status: number;
  constructor(message: string, status: number, digest?: string) {
    super(message);
    this.name = 'ServerActionError';
    this.status = status;
    if (digest) this.digest = digest;
  }
}

interface Envelope {
  ok?: boolean;
  result?: unknown;
  redirect?: string;
  error?: { message?: string; digest?: string } | string;
  details?: string;
}

function basePath(): string {
  const cfg = (globalThis as { __KRATE_CFG__?: { basePath?: string } }).__KRATE_CFG__;
  return cfg && cfg.basePath ? cfg.basePath : '';
}

/** True for a server action proxy created by `__krateAction`. */
export function isServerAction(value: unknown): value is ServerActionProxy {
  return typeof value === 'function' && typeof (value as Partial<ServerActionProxy>).$$krateAction === 'string';
}

function navigateTo(url: string): void {
  const nav = (globalThis as { navigate?: (u: string) => unknown }).navigate;
  if (typeof nav === 'function') nav(url);
  else if (typeof location !== 'undefined') location.assign(url);
}

function errorFrom(p: Envelope | null, status: number, statusText: string): ServerActionError {
  const e = p ? p.error : undefined;
  if (e && typeof e === 'object') {
    return new ServerActionError(e.message || 'Server action failed', status, e.digest);
  }
  const message = (p && (p.details || (typeof e === 'string' ? e : ''))) || statusText || 'Server action failed';
  return new ServerActionError(message, status);
}

/**
 * POSTs one action call. A trailing FormData argument is sent as multipart
 * with the leading arguments JSON-encoded in `__krate_args`; anything else is
 * sent as a JSON `{args}` body. Resolves with the action's return value and
 * follows a `redirect` from the response.
 */
export async function callServerAction(url: string, args: unknown[]): Promise<unknown> {
  const headers: Record<string, string> = {
    accept: 'application/json',
    'x-krate-action': 'fetch',
  };
  if (typeof location !== 'undefined') headers['x-krate-return'] = location.pathname + location.search;
  let body: BodyInit;
  const last = args[args.length - 1];
  if (typeof FormData !== 'undefined' && last instanceof FormData) {
    const fd = new FormData();
    last.forEach((value, key) => {
      if (key !== ACTION_ARGS_FIELD) fd.append(key, value);
    });
    const leading = args.slice(0, -1);
    if (leading.length > 0) fd.set(ACTION_ARGS_FIELD, JSON.stringify(leading));
    body = fd;
  } else {
    headers['content-type'] = 'application/json';
    body = JSON.stringify({ args });
  }
  const res = await fetch(url, { method: 'POST', credentials: 'same-origin', headers, body });
  let payload: Envelope | null = null;
  try {
    payload = (await res.json()) as Envelope;
  } catch {
    payload = null;
  }
  if (!res.ok || !payload || payload.ok === false) {
    throw errorFrom(payload, res.status, res.statusText);
  }
  if (typeof payload.redirect === 'string' && payload.redirect) navigateTo(payload.redirect);
  return payload.result;
}

function makeProxy(id: string, bound: unknown[]): ServerActionProxy {
  const url = basePath() + '/__krate/action/' + id;
  const action = function (...args: unknown[]): Promise<unknown> {
    return callServerAction(url, bound.concat(args));
  } as unknown as ServerActionProxy & { toString(): string };
  Object.defineProperty(action, 'actionUrl', { value: url });
  Object.defineProperty(action, '$$krateAction', { value: id });
  Object.defineProperty(action, '$$krateBound', { value: bound });
  // React server references support `.bind(null, ...args)` to pre-fill
  // arguments; the result is still a server action.
  Object.defineProperty(action, 'bind', {
    value: (_this: unknown, ...more: unknown[]) => makeProxy(id, bound.concat(more)),
  });
  action.toString = () => url;
  return action;
}

/** Client proxy for the server action with the given id. */
export function __krateAction(id: string): ServerActionProxy {
  return makeProxy(id, []);
}

// Action scope (async transitions)

let pendingActions = 0;
const settleListeners = new Set<() => void>();

/**
 * Marks the start of an action or async transition and returns its end
 * callback. Optimistic updates stay applied while any scope is open. Exposed so
 * `startTransition` implementations can join the same scope.
 */
export function __krateBeginAction(): () => void {
  pendingActions++;
  let ended = false;
  return () => {
    if (ended) return;
    ended = true;
    pendingActions--;
    if (pendingActions === 0) {
      for (const listener of Array.from(settleListeners)) listener();
    }
  };
}

/** True while an action or async transition is pending. */
export function isActionPending(): boolean {
  return pendingActions > 0;
}

/** Runs fn as an async transition: the action scope stays open until it settles. */
export type TransitionRunner = <T>(fn: () => T | Promise<T>) => Promise<T>;

const defaultRunner: TransitionRunner = <T,>(fn: () => T | Promise<T>): Promise<T> => {
  const end = __krateBeginAction();
  let result: T | Promise<T>;
  try {
    result = fn();
  } catch (err) {
    end();
    return Promise.reject(err);
  }
  return Promise.resolve(result).finally(end);
};

let runner: TransitionRunner = defaultRunner;

/**
 * Replaces the transition runner used for form actions and `useActionState`
 * dispatches, so a framework-wide async `startTransition` can own them. Passing
 * null restores the built-in runner.
 */
export function setActionTransitionRunner(next: TransitionRunner | null): void {
  runner = next || defaultRunner;
}

/** Runs fn inside the current action transition runner. */
export function runActionTransition<T>(fn: () => T | Promise<T>): Promise<T> {
  return runner(fn);
}

// Errors

/**
 * Reports an action error. An error boundary integration can install
 * `globalThis.__krateActionError(error, element)` and return true to claim the
 * error; otherwise a bubbling `krate:action-error` event is dispatched on the
 * element (or document) and the error is logged.
 */
export function reportActionError(error: unknown, el?: Element | null): void {
  const g = globalThis as { __krateActionError?: (e: unknown, el?: Element | null) => boolean | void };
  if (typeof g.__krateActionError === 'function') {
    try {
      if (g.__krateActionError(error, el) === true) return;
    } catch {
      // A failing boundary hook must not hide the original error.
    }
  }
  const target: EventTarget | null = el || (typeof document !== 'undefined' ? document : null);
  let handled = false;
  if (target && typeof CustomEvent !== 'undefined') {
    const ev = new CustomEvent('krate:action-error', { bubbles: true, cancelable: true, detail: { error } });
    handled = !target.dispatchEvent(ev);
  }
  if (!handled) console.error('krate server action:', error);
}

// Form status

/** The value `useFormStatus()` returns. */
export interface FormStatus {
  pending: boolean;
  data: FormData | null;
  method: string | null;
  action: unknown;
}

const IDLE: FormStatus = Object.freeze({ pending: false, data: null, method: null, action: null }) as FormStatus;

interface FormState {
  status: FormStatus;
  subs: Set<(s: FormStatus) => void>;
  running: number;
  resetRequested: boolean;
}

const formStates = new WeakMap<HTMLFormElement, FormState>();

function formState(form: HTMLFormElement): FormState {
  let st = formStates.get(form);
  if (!st) {
    st = { status: IDLE, subs: new Set(), running: 0, resetRequested: false };
    formStates.set(form, st);
  }
  return st;
}

function publish(st: FormState, status: FormStatus): void {
  st.status = status;
  for (const sub of Array.from(st.subs)) sub(status);
}

/** Setters a lowered `useFormStatus()` hands to the runtime. */
export interface FormStatusSetters {
  status?: (s: FormStatus) => void;
  pending?: (v: boolean) => void;
  data?: (v: FormData | null) => void;
  method?: (v: string | null) => void;
  action?: (v: unknown) => void;
}

/**
 * Subscribes a component's `useFormStatus()` to the nearest form that contains
 * its root element `el` (not `el` itself, matching React: a component reads the
 * status of a parent form). Returns the unsubscribe function, used as the
 * callback-ref cleanup when the element detaches.
 */
export function __krateFormStatusMount(el: Element | null, setters: FormStatusSetters): () => void {
  let form: HTMLFormElement | null = null;
  for (let n = el ? el.parentElement : null; n; n = n.parentElement) {
    if (n.tagName === 'FORM') {
      form = n as HTMLFormElement;
      break;
    }
  }
  if (!form) return () => {};
  const st = formState(form);
  const sub = (s: FormStatus): void => {
    if (setters.status) setters.status(s);
    if (setters.pending) setters.pending(s.pending);
    if (setters.data) setters.data(s.data);
    if (setters.method) setters.method(s.method);
    if (setters.action) setters.action(s.action);
  };
  st.subs.add(sub);
  sub(st.status);
  // The returned function is the callback-ref cleanup: the ref system calls
  // it when the element detaches. Registering it with onCleanup instead would
  // tie it to the ref binding's effect, which may re-run.
  return (): void => {
    st.subs.delete(sub);
  };
}

/**
 * Assigns an element to a ref value (callback or `{ current }` object). Used
 * when the compiler composes a component's own ref with the useFormStatus
 * mount ref.
 */
export function __krateAssignRef(ref: unknown, el: Element | null): void {
  if (typeof ref === 'function') (ref as (el: Element | null) => void)(el);
  else if (ref && typeof ref === 'object') (ref as { current: unknown }).current = el;
}

/** Current status of a form (idle unless an action submitted from it is pending). */
export function getFormStatus(form: HTMLFormElement): FormStatus {
  return formStates.get(form)?.status || IDLE;
}

// Form reset

function resetForm(form: HTMLFormElement): void {
  // Controlled fields keep their signal value: snapshot and restore them
  // around the native reset, which only affects uncontrolled fields.
  const controlled = Array.from(
    form.querySelectorAll<HTMLInputElement>('[data-kattr-value],[data-kattr-checked]'),
  ).map((el) => ({ el, value: el.value, checked: el.checked }));
  form.reset();
  for (const c of controlled) {
    if (c.el.hasAttribute('data-kattr-value')) c.el.value = c.value;
    if (c.el.hasAttribute('data-kattr-checked')) c.el.checked = c.checked;
  }
}

/**
 * React DOM's `requestFormReset(form)`: resets the form's uncontrolled fields
 * once the action currently pending on it settles, or immediately when none is.
 */
export function requestFormReset(form: HTMLFormElement): void {
  const st = formStates.get(form);
  if (st && st.running > 0) {
    st.resetRequested = true;
    return;
  }
  resetForm(form);
}

// Form submission

type Getter = () => unknown;

interface ActionTarget {
  __krateFormAction?: Getter;
}

/** Binds a function action to a form or submit button (`action={fn}` / `formAction={fn}`). */
export function __krateBindFormAction(el: Element, get: Getter): void {
  (el as Element & ActionTarget).__krateFormAction = get;
}

interface RunnableAction {
  (payload: unknown): unknown;
  __krateRun?: (payload: unknown) => Promise<boolean>;
}

function boundAction(el: Element | null): unknown {
  if (!el) return null;
  const get = (el as Element & ActionTarget).__krateFormAction;
  if (typeof get !== 'function') return null;
  try {
    return get();
  } catch {
    return null;
  }
}

function resolveSubmission(form: HTMLFormElement, submitter: HTMLElement | null): { fn?: unknown; url?: string } | null {
  if (submitter && submitter !== (form as HTMLElement)) {
    const fn = boundAction(submitter);
    if (typeof fn === 'function') return { fn };
    if (submitter.hasAttribute('data-krate-action')) {
      return { url: (submitter as HTMLButtonElement).formAction || form.action };
    }
    // A plain native formaction wins over the form's action.
    if (submitter.hasAttribute('formaction')) return null;
  }
  const fn = boundAction(form);
  if (typeof fn === 'function') return { fn };
  if (form.hasAttribute('data-krate-action')) return { url: form.action };
  return null;
}

function formDataFor(form: HTMLFormElement, submitter: HTMLElement | null): FormData {
  let fd: FormData;
  try {
    fd = submitter ? new FormData(form, submitter) : new FormData(form);
  } catch {
    fd = new FormData(form);
    const s = submitter as HTMLButtonElement | null;
    if (s && s.name) fd.append(s.name, s.value);
  }
  return fd;
}

function stripActionURL(url: string): string {
  // The no-JS leading-args query is only for native posts.
  const i = url.indexOf('?');
  return i < 0 ? url : url.slice(0, i);
}

/**
 * Runs a form action: sets the form status, invokes the action with the form
 * data in an async transition, resets the form on success and reports errors.
 * Resolves true when the action succeeded.
 */
export async function submitFormAction(
  form: HTMLFormElement,
  action: unknown,
  data: FormData,
  submitter: HTMLElement | null = null,
  url?: string,
): Promise<boolean> {
  const st = formState(form);
  const method = (
    (submitter && submitter.getAttribute('formmethod')) ||
    form.getAttribute('method') ||
    'get'
  ).toLowerCase();
  st.running++;
  publish(st, { pending: true, data, method, action: action || url || null });
  form.setAttribute('data-krate-action-pending', '1');
  form.setAttribute('aria-busy', 'true');
  let ok = false;
  try {
    await runActionTransition(async () => {
      const run = action as RunnableAction | undefined;
      if (run && typeof run.__krateRun === 'function') {
        // useActionState dispatch: it queues and reports its own errors.
        ok = await run.__krateRun(data);
        return;
      }
      if (typeof run === 'function') await run(data);
      else if (url) await callServerAction(stripActionURL(url), [data]);
      ok = true;
    });
  } catch (err) {
    reportActionError(err, form);
  } finally {
    st.running--;
    if (st.running === 0) {
      form.removeAttribute('data-krate-action-pending');
      form.removeAttribute('aria-busy');
      publish(st, IDLE);
      if (ok || st.resetRequested) {
        st.resetRequested = false;
        if (ok && form.hasAttribute('data-krate-reload')) location.reload();
        else resetForm(form);
      }
    }
  }
  return ok;
}

function onSubmit(e: Event): void {
  const form = e.target as HTMLFormElement | null;
  if (!form || form.tagName !== 'FORM' || e.defaultPrevented) return;
  const submitter = ((e as SubmitEvent).submitter as HTMLElement | null) || null;
  const target = resolveSubmission(form, submitter);
  if (!target) return;
  e.preventDefault();
  const data = formDataFor(form, submitter);
  void submitFormAction(form, target.fn, data, submitter, target.url);
}

/** Installs the document submit listener once per window. */
export function installActionForms(doc: Document | undefined = typeof document !== 'undefined' ? document : undefined): void {
  if (!doc) return;
  const w = doc.defaultView as (Window & { __krateActionBound?: boolean }) | null;
  if (!w || w.__krateActionBound) return;
  w.__krateActionBound = true;
  doc.addEventListener('submit', onSubmit, false);
}

// useActionState

interface QueuedAction {
  payload: unknown;
  end: () => void;
  done: (ok: boolean) => void;
}

/**
 * Backs `const [state, formAction, isPending] = useActionState(fn, initial)`.
 * The returned dispatch can be a form action, a button formAction, or called
 * directly. Calls run one at a time in dispatch order; each receives the state
 * the previous call produced. An error cancels the queued calls and is
 * reported to the nearest error boundary hook.
 */
export function __krateActionState<S>(
  action: (prev: S, payload: unknown) => S | Promise<S>,
  getState: () => S,
  setState: (next: S) => void,
  setPending: (pending: boolean) => void,
): RunnableAction {
  const queue: QueuedAction[] = [];
  let running = false;

  const drain = async (): Promise<void> => {
    running = true;
    while (queue.length > 0) {
      const item = queue.shift() as QueuedAction;
      let ok = false;
      try {
        const next = await runActionTransition(() => action(getState(), item.payload));
        setState(next as S);
        ok = true;
      } catch (err) {
        const cancelled = queue.splice(0);
        for (const c of cancelled) {
          c.end();
          c.done(false);
        }
        reportActionError(err);
      } finally {
        item.end();
        item.done(ok);
      }
    }
    running = false;
    setPending(false);
  };

  const run = (payload: unknown): Promise<boolean> =>
    new Promise<boolean>((done) => {
      queue.push({ payload, end: __krateBeginAction(), done });
      setPending(true);
      if (!running) void drain();
    });

  const dispatch = ((payload: unknown): void => {
    void run(payload);
  }) as RunnableAction;
  dispatch.__krateRun = run;
  return dispatch;
}

// useOptimistic

/**
 * Backs `const [value, addOptimistic] = useOptimistic(state, update?)`.
 * Optimistic updates are applied on top of the latest base state while an
 * action or async transition is pending and dropped when every pending action
 * has settled. Updates made outside any action are dropped on the next
 * microtask unless an action starts first.
 */
export function __krateOptimistic<T, A>(
  base: () => T,
  set: (value: T) => void,
  update?: (state: T, action: A) => T,
): (action: A) => void {
  let updates: A[] = [];
  const apply = (b: T): T => {
    let v = b;
    for (const a of updates) v = update ? update(v, a) : (a as unknown as T);
    return v;
  };
  createEffect(() => {
    set(apply(base()));
  });
  const revert = (): void => {
    if (updates.length === 0) return;
    updates = [];
    set(base());
  };
  settleListeners.add(revert);
  onCleanup(() => {
    settleListeners.delete(revert);
  });
  return (a: A): void => {
    updates.push(a);
    set(apply(base()));
    if (pendingActions === 0) {
      queueMicrotask(() => {
        if (pendingActions === 0) revert();
      });
    }
  };
}

if (typeof document !== 'undefined') installActionForms(document);
