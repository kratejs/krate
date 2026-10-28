import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createSignal } from './signal.js';
import { krateOn } from './events.js';
import {
  ACTION_ARGS_FIELD,
  ServerActionError,
  __krateAction,
  __krateActionState,
  __krateBeginAction,
  __krateBindFormAction,
  __krateFormStatusMount,
  __krateOptimistic,
  getFormStatus,
  installActionForms,
  isServerAction,
  requestFormReset,
  setActionTransitionRunner,
  type FormStatus,
} from './actions.js';

function deferred<T = void>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

const tick = () => new Promise((r) => setTimeout(r, 0));

function makeForm(html: string): HTMLFormElement {
  const host = document.createElement('div');
  host.innerHTML = html;
  document.body.appendChild(host);
  return host.querySelector('form') as HTMLFormElement;
}

function submit(form: HTMLFormElement, submitter?: HTMLElement): void {
  const ev = new Event('submit', { bubbles: true, cancelable: true }) as SubmitEvent;
  Object.defineProperty(ev, 'submitter', { value: submitter || null });
  form.dispatchEvent(ev);
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });
}

beforeEach(() => {
  document.body.innerHTML = '';
  installActionForms(document);
});

afterEach(() => {
  vi.unstubAllGlobals();
  setActionTransitionRunner(null);
});

describe('form actions', () => {
  it('runs a client function action with the form data, without navigating, and resets after success', async () => {
    const form = makeForm('<form><input name="title" value="initial"><button type="submit">Go</button></form>');
    const input = form.querySelector('input') as HTMLInputElement;
    input.value = 'typed';
    const gate = deferred();
    const seen: string[] = [];
    __krateBindFormAction(form, () => async (fd: FormData) => {
      seen.push(String(fd.get('title')));
      await gate.promise;
    });
    submit(form);
    expect(seen).toEqual(['typed']);
    expect(form.getAttribute('data-krate-action-pending')).toBe('1');
    expect(getFormStatus(form).pending).toBe(true);
    expect(input.value).toBe('typed');
    gate.resolve();
    await tick();
    expect(getFormStatus(form).pending).toBe(false);
    expect(form.hasAttribute('data-krate-action-pending')).toBe(false);
    expect(input.value).toBe('initial');
  });

  it('keeps fields when the action fails and reports the error', async () => {
    const form = makeForm('<form><input name="a" value="x"></form>');
    const input = form.querySelector('input') as HTMLInputElement;
    input.value = 'kept';
    const errors: unknown[] = [];
    form.addEventListener('krate:action-error', (e) => {
      errors.push((e as CustomEvent).detail.error);
      e.preventDefault();
    });
    __krateBindFormAction(form, () => async () => {
      throw new Error('boom');
    });
    submit(form);
    await tick();
    expect((errors[0] as Error).message).toBe('boom');
    expect(input.value).toBe('kept');
    expect(getFormStatus(form).pending).toBe(false);
  });

  it('drops the no-JS POST target once a function is bound', () => {
    const form = makeForm('<form action="/__krate/action/a1?__krate_args=%5B0%5D" method="post" data-krate-action><button formaction="/x" formmethod="post">b</button></form>');
    const button = form.querySelector('button') as HTMLButtonElement;
    __krateBindFormAction(form, () => () => {});
    __krateBindFormAction(button, () => () => {});
    expect(form.hasAttribute('action') || form.hasAttribute('method') || form.hasAttribute('data-krate-action')).toBe(false);
    expect(button.hasAttribute('formaction') || button.hasAttribute('formmethod')).toBe(false);
  });

  it('prefers a submit button formAction over the form action', async () => {
    const form = makeForm('<form><button type="submit" name="op" value="b">B</button></form>');
    const button = form.querySelector('button') as HTMLButtonElement;
    const calls: string[] = [];
    __krateBindFormAction(form, () => () => calls.push('form'));
    __krateBindFormAction(button, () => (fd: FormData) => calls.push('button:' + fd.get('op')));
    submit(form, button);
    await tick();
    expect(calls).toEqual(['button:b']);
  });

  it('leaves forms without an action and native formaction buttons alone', () => {
    const form = makeForm('<form action="/search"><button formaction="/other">x</button></form>');
    const ev = new Event('submit', { bubbles: true, cancelable: true });
    form.dispatchEvent(ev);
    expect(ev.defaultPrevented).toBe(false);
  });

  it('does not run the action when onSubmit prevented the default', async () => {
    const form = makeForm('<form></form>');
    const fn = vi.fn();
    __krateBindFormAction(form, () => fn);
    form.addEventListener('submit', (e) => e.preventDefault());
    submit(form);
    await tick();
    expect(fn).not.toHaveBeenCalled();
  });

  it('runs onSubmit through the React event system first; preventDefault cancels the action', async () => {
    const form = makeForm('<form><button>Go</button></form>');
    const order: string[] = [];
    let cancel = true;
    krateOn(form, 'onSubmit', (e) => {
      order.push('onSubmit');
      if (cancel) e.preventDefault();
    });
    __krateBindFormAction(form, () => () => order.push('action'));
    submit(form);
    await tick();
    expect(order).toEqual(['onSubmit']);
    cancel = false;
    submit(form);
    await tick();
    expect(order).toEqual(['onSubmit', 'onSubmit', 'action']);
  });

  it('restores controlled fields after the reset', async () => {
    const form = makeForm(
      '<form><input name="c" value="" data-kattr-value="k:1"><input name="u" value="d"></form>',
    );
    const [controlled, uncontrolled] = Array.from(form.querySelectorAll('input'));
    controlled.value = 'signal value';
    uncontrolled.value = 'typed';
    __krateBindFormAction(form, () => async () => {});
    submit(form);
    await tick();
    expect(controlled.value).toBe('signal value');
    expect(uncontrolled.value).toBe('d');
  });

  it('requestFormReset waits for the pending action', async () => {
    const form = makeForm('<form><input name="a" value="d"></form>');
    const input = form.querySelector('input') as HTMLInputElement;
    const gate = deferred();
    __krateBindFormAction(form, () => async () => {
      await gate.promise;
      throw new Error('fail');
    });
    form.addEventListener('krate:action-error', (e) => e.preventDefault());
    input.value = 'typed';
    submit(form);
    requestFormReset(form);
    expect(input.value).toBe('typed');
    gate.resolve();
    await tick();
    expect(input.value).toBe('d');
  });

  it('posts server action forms as multipart and does not reload', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ ok: true, result: { saved: true } }));
    vi.stubGlobal('fetch', fetchMock);
    const form = makeForm(
      '<form action="/__krate/action/a1" method="post" data-krate-action><input name="text" value="hi"></form>',
    );
    submit(form);
    await tick();
    await tick();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toContain('/__krate/action/a1');
    expect((init.headers as Record<string, string>)['x-krate-action']).toBe('fetch');
    expect(init.body).toBeInstanceOf(FormData);
    expect((init.body as FormData).get('text')).toBe('hi');
    expect(getFormStatus(form).pending).toBe(false);
  });
});

describe('useFormStatus', () => {
  it('is scoped to the parent form of the component', async () => {
    document.body.innerHTML =
      '<form id="a"><button id="ba">A</button></form><form id="b"><button id="bb">B</button></form>';
    const formA = document.getElementById('a') as HTMLFormElement;
    const statusA: FormStatus[] = [];
    const pendingB: boolean[] = [];
    __krateFormStatusMount(document.getElementById('ba'), { status: (s) => statusA.push(s) });
    __krateFormStatusMount(document.getElementById('bb'), { pending: (p) => pendingB.push(p) });
    const gate = deferred();
    const action = async () => {
      await gate.promise;
    };
    __krateBindFormAction(formA, () => action);
    submit(formA);
    const live = statusA[statusA.length - 1];
    expect(live.pending).toBe(true);
    expect(live.method).toBe('get');
    expect(live.action).toBe(action);
    expect(live.data).toBeInstanceOf(FormData);
    expect(pendingB).toEqual([false]);
    gate.resolve();
    await tick();
    expect(statusA[statusA.length - 1].pending).toBe(false);
    expect(pendingB).toEqual([false]);
  });

  it('does not report the form the component itself renders', () => {
    document.body.innerHTML = '<form id="self"></form>';
    const seen: boolean[] = [];
    __krateFormStatusMount(document.getElementById('self'), { pending: (p) => seen.push(p) });
    expect(seen).toEqual([]);
  });
});

describe('useActionState', () => {
  it('queues calls sequentially and threads the previous state', async () => {
    const [state, setState] = createSignal(0);
    const [pending, setPending] = createSignal(false);
    const gates = [deferred(), deferred(), deferred()];
    const log: string[] = [];
    let call = 0;
    const dispatch = __krateActionState<number>(
      async (prev, payload) => {
        const i = call++;
        log.push(`start ${i} prev=${prev} payload=${payload}`);
        await gates[i].promise;
        return prev + (payload as number);
      },
      state,
      setState,
      setPending,
    );
    dispatch(1);
    dispatch(10);
    dispatch(100);
    expect(pending()).toBe(true);
    await tick();
    expect(log).toEqual(['start 0 prev=0 payload=1']);
    gates[0].resolve();
    await tick();
    expect(state()).toBe(1);
    expect(log[1]).toBe('start 1 prev=1 payload=10');
    gates[1].resolve();
    await tick();
    gates[2].resolve();
    await tick();
    expect(state()).toBe(111);
    expect(pending()).toBe(false);
  });

  it('cancels queued calls when an action throws', async () => {
    const [state, setState] = createSignal('a');
    const [pending, setPending] = createSignal(false);
    const runs: string[] = [];
    const errors: unknown[] = [];
    const onErr = (e: Event) => {
      errors.push((e as CustomEvent).detail.error);
      e.preventDefault();
    };
    document.addEventListener('krate:action-error', onErr);
    const dispatch = __krateActionState<string>(
      async (prev, payload) => {
        runs.push(String(payload));
        if (payload === 'bad') throw new Error('nope');
        return prev + payload;
      },
      state,
      setState,
      setPending,
    );
    dispatch('bad');
    dispatch('next');
    await tick();
    await tick();
    document.removeEventListener('krate:action-error', onErr);
    expect(runs).toEqual(['bad']);
    expect(state()).toBe('a');
    expect(pending()).toBe(false);
    expect((errors[0] as Error).message).toBe('nope');
  });

  it('works as a form action and resets the form after success', async () => {
    const [state, setState] = createSignal<string[]>([]);
    const [, setPending] = createSignal(false);
    const form = makeForm('<form><input name="todo" value=""></form>');
    const input = form.querySelector('input') as HTMLInputElement;
    const dispatch = __krateActionState<string[]>(
      async (prev, fd) => [...prev, String((fd as FormData).get('todo'))],
      state,
      setState,
      setPending,
    );
    __krateBindFormAction(form, () => dispatch);
    input.value = 'one';
    submit(form);
    await tick();
    await tick();
    expect(state()).toEqual(['one']);
    expect(input.value).toBe('');
  });

  it('sends the previous state to a server action as the leading argument', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ ok: true, result: 5 }));
    vi.stubGlobal('fetch', fetchMock);
    const [state, setState] = createSignal(4);
    const [, setPending] = createSignal(false);
    const dispatch = __krateActionState<number>(
      __krateAction('a9') as unknown as (prev: number, p: unknown) => Promise<number>,
      state,
      setState,
      setPending,
    );
    const fd = new FormData();
    fd.set('n', '1');
    dispatch(fd);
    await tick();
    await tick();
    const init = (fetchMock.mock.calls[0] as unknown as [string, RequestInit])[1];
    const body = init.body as FormData;
    expect(JSON.parse(String(body.get(ACTION_ARGS_FIELD)))).toEqual([4]);
    expect(body.get('n')).toBe('1');
    expect(state()).toBe(5);
  });
});

describe('useOptimistic', () => {
  it('applies updates while an action is pending and reverts to the latest base', async () => {
    const [base, setBase] = createSignal<string[]>(['a']);
    const [value, setValue] = createSignal<string[]>(base());
    const add = __krateOptimistic<string[], string>(base, setValue, (s, n) => [...s, n]);
    const end = __krateBeginAction();
    add('b');
    add('c');
    expect(value()).toEqual(['a', 'b', 'c']);
    setBase(['a', 'x']);
    await tick();
    expect(value()).toEqual(['a', 'x', 'b', 'c']);
    end();
    expect(value()).toEqual(['a', 'x']);
  });

  it('drops an update made outside any action', async () => {
    const [base] = createSignal(1);
    const [value, setValue] = createSignal(base());
    const add = __krateOptimistic<number, number>(base, setValue);
    add(2);
    expect(value()).toBe(2);
    await tick();
    expect(value()).toBe(1);
  });

  it('keeps updates for a form action until it settles', async () => {
    const [base, setBase] = createSignal(0);
    const [value, setValue] = createSignal(base());
    const add = __krateOptimistic<number, number>(base, setValue, (s, n) => s + n);
    const gate = deferred();
    const form = makeForm('<form></form>');
    __krateBindFormAction(form, () => async () => {
      add(5);
      await gate.promise;
      setBase(1);
    });
    submit(form);
    await tick();
    expect(value()).toBe(5);
    gate.resolve();
    await tick();
    await tick();
    expect(value()).toBe(1);
  });

  it('uses a custom transition runner when one is installed', async () => {
    const runs: number[] = [];
    setActionTransitionRunner(async (fn) => {
      runs.push(1);
      const end = __krateBeginAction();
      try {
        return await fn();
      } finally {
        end();
      }
    });
    const form = makeForm('<form></form>');
    __krateBindFormAction(form, () => async () => {});
    submit(form);
    await tick();
    expect(runs).toEqual([1]);
  });
});

describe('server action proxies', () => {
  it('sends JSON args and returns the result', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ ok: true, result: { n: 2 } }));
    vi.stubGlobal('fetch', fetchMock);
    const save = __krateAction('a1');
    expect(isServerAction(save)).toBe(true);
    expect(save.actionUrl).toBe('/__krate/action/a1');
    await expect(save({ n: 1 })).resolves.toEqual({ n: 2 });
    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(JSON.parse(String(init.body))).toEqual({ args: [{ n: 1 }] });
    expect((init.headers as Record<string, string>)['content-type']).toBe('application/json');
  });

  it('keeps bound arguments ahead of the call arguments', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ ok: true, result: null }));
    vi.stubGlobal('fetch', fetchMock);
    const bound = __krateAction('a2').bind(null, 'id-7');
    expect(isServerAction(bound)).toBe(true);
    await bound('x');
    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(JSON.parse(String(init.body))).toEqual({ args: ['id-7', 'x'] });
  });

  it('rejects with the structured error envelope', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => jsonResponse({ ok: false, error: { message: 'An error occurred', digest: 'd1' } }, 500)),
    );
    const err = (await __krateAction('a3')().catch((e: unknown) => e)) as ServerActionError;
    expect(err).toBeInstanceOf(ServerActionError);
    expect(err.message).toBe('An error occurred');
    expect(err.digest).toBe('d1');
    expect(err.status).toBe(500);
  });

  it('follows a redirect from the response', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({ ok: true, result: null, redirect: '/next' })));
    const nav = vi.fn();
    vi.stubGlobal('navigate', nav);
    await __krateAction('a4')();
    expect(nav).toHaveBeenCalledWith('/next');
  });

  it('dispatches krate:revalidate when the action revalidated server caches', async () => {
    const revalidation = { paths: [{ path: '/posts', type: 'page' }], tags: ['posts'] };
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({ ok: true, result: 1, revalidated: true, revalidation })));
    const seen: unknown[] = [];
    const onRevalidate = (e: Event) => seen.push((e as CustomEvent).detail);
    window.addEventListener('krate:revalidate', onRevalidate);
    try {
      await expect(__krateAction('a5')()).resolves.toBe(1);
      expect(seen).toEqual([revalidation]);
    } finally {
      window.removeEventListener('krate:revalidate', onRevalidate);
    }
  });

  it('does not dispatch krate:revalidate for actions that revalidated nothing', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => jsonResponse({ ok: true, result: null })));
    const handler = vi.fn();
    window.addEventListener('krate:revalidate', handler);
    try {
      await __krateAction('a6')();
      expect(handler).not.toHaveBeenCalled();
    } finally {
      window.removeEventListener('krate:revalidate', handler);
    }
  });
});
