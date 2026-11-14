import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
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
  setActionTransitionRunner,
  stripActionURL,
} from './actions.js';


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

  it('keeps only the sealed closure token of an inline action in the posted URL', async () => {
    const fetchMock = vi.fn(async () => jsonResponse({ ok: true, result: null }));
    vi.stubGlobal('fetch', fetchMock);
    const args = encodeURIComponent(JSON.stringify(['k1.AbC_-123']));
    const form = makeForm(
      `<form action="/__krate/action/a1?__krate_args=${args}&__krate_return=%2Fx" method="post" data-krate-action></form>`,
    );
    submit(form);
    await tick();
    await tick();
    const [url] = fetchMock.mock.calls[0] as unknown as [string];
    expect(url.replace(/^https?:\/\/[^/]+/, '')).toBe(`/__krate/action/a1?__krate_args=${args}`);
    expect(stripActionURL('/__krate/action/a2?__krate_args=%5B%7B%22n%22%3A1%7D%5D&__krate_return=%2Fx')).toBe('/__krate/action/a2');
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
});
