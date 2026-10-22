import { afterEach, describe, expect, it } from 'vitest';
import { cookies, getActionContext, headers, redirect } from './action-context.js';
import { jsx, renderToString } from './server.js';

const KEY = Symbol.for('krate.action.store');

afterEach(() => {
  delete (globalThis as Record<symbol, unknown>)[KEY];
});

describe('server action context', () => {
  it('throws outside an action with an actionable message', () => {
    expect(getActionContext()).toBeUndefined();
    expect(() => headers()).toThrow(/only be called while a server action runs/);
  });

  it('reads headers and cookies from the sidecar store', () => {
    const jar = new Map([['sid', 'abc']]);
    const ctx = {
      actionId: 'a1',
      url: '/__krate/action/a1',
      method: 'POST',
      headers: new Headers({ 'x-test': 'yes' }),
      cookies: {
        get: (n: string) => jar.get(n),
        getAll: () => [],
        has: (n: string) => jar.has(n),
        set: () => {},
        delete: () => {},
      },
    };
    (globalThis as Record<symbol, unknown>)[KEY] = { getStore: () => ctx };
    expect(headers().get('x-test')).toBe('yes');
    expect(cookies().get('sid')).toBe('abc');
    expect(getActionContext()?.actionId).toBe('a1');
  });

  it('redirect() throws the marker the sidecar turns into a navigation', () => {
    let caught: unknown;
    try {
      redirect('/done');
    } catch (e) {
      caught = e;
    }
    expect((caught as { __krateRedirect: string }).__krateRedirect).toBe('/done');
  });

  it('does not render client function actions as attributes', () => {
    const html = renderToString(jsx('form', { action: async () => {}, children: 'x' }));
    expect(html).toBe('<form>x</form>');
  });
});
