import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { initRouter, destroyRouter } from './router';

// The router claims method-less forms as GET navigations; a form that carries a
// client action (an island's `<form action={fn}>`) belongs to that action.
let fetchMock: ReturnType<typeof vi.fn>;

function submit(form: HTMLFormElement, submitter?: HTMLElement, first?: (e: Event) => void): SubmitEvent {
  const e = new Event('submit', { bubbles: true, cancelable: true }) as SubmitEvent;
  Object.defineProperty(e, 'submitter', { value: submitter ?? null });
  if (first) form.addEventListener('submit', first, { once: true });
  form.dispatchEvent(e);
  return e;
}

beforeEach(() => {
  destroyRouter();
  (globalThis as any).__KRATE_CFG__ = { viewTransitions: false };
  history.replaceState(null, '', '/');
  document.body.innerHTML = '<div id="root"><form id="f"><input name="a" value="1"><button id="b" type="submit">go</button></form></div>';
  fetchMock = vi.fn(async () => ({ ok: true, status: 200, url: location.href, headers: new Headers(), text: async () => '<html><body></body></html>' }));
  vi.stubGlobal('fetch', fetchMock);
  (globalThis as any).__krateHardNavigate = () => {};
  initRouter();
});

afterEach(() => {
  destroyRouter();
  vi.unstubAllGlobals();
  delete (globalThis as any).__krateHardNavigate;
  delete (globalThis as any).__KRATE_CFG__;
});

describe('router submit handling', () => {
  const form = () => document.getElementById('f') as HTMLFormElement;

  it('still turns a plain method-less form into a GET navigation', () => {
    expect(submit(form()).defaultPrevented).toBe(true);
  });

  it('leaves a form with an island action (__krateForm) alone', () => {
    (form() as any).__krateForm = { fn: () => {} };
    expect(submit(form()).defaultPrevented).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('leaves a form whose submit button has a client action alone', () => {
    const b = document.getElementById('b') as any;
    b.__krateFormAction = () => {};
    expect(submit(form(), b).defaultPrevented).toBe(false);
  });

  it('leaves a bound form action and a server-action form alone', () => {
    (form() as any).__krateFormAction = () => ({});
    expect(submit(form()).defaultPrevented).toBe(false);
    delete (form() as any).__krateFormAction;
    form().setAttribute('data-krate-action', '');
    expect(submit(form()).defaultPrevented).toBe(false);
  });

  it('does nothing when the island listener ran first and prevented the event', () => {
    (form() as any).__krateForm = { fn: () => {} };
    const e = submit(form(), undefined, (ev) => ev.preventDefault());
    expect(e.defaultPrevented).toBe(true);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
