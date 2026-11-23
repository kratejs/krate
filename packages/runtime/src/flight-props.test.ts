import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { encodeProp, type FlightHooks } from './flight-props';

// The decoder ships as a string in the compiler; load the real one.
const goSrc = readFileSync(
  resolve(process.cwd(), '../compiler/internal/build/clientboundary.go'),
  'utf8',
);
const reviveSrc = goSrc.match(/const clientReviveJS = `([\s\S]*?)`\n/)![1];
const revive = new Function(`${reviveSrc}; return __krateRevive;`)() as (v: unknown, h: unknown, c: unknown, k: unknown) => any;

const hooks: FlightHooks = {
  isAction: () => undefined,
  isJSX: () => false,
  fail: (path, what) => new Error(`${path}: ${what}`),
};

function roundTrip(v: unknown): any {
  const wire = JSON.parse(JSON.stringify(encodeProp(v, 'p', hooks)));
  return revive(wire, null, null, null);
}

describe('flight props round trip', () => {
  it('keeps Date (valid and invalid), BigInt and the special numbers', () => {
    const out = roundTrip({ d: new Date(1700000000000), bad: new Date('x'), b: -5n, nan: NaN, neg: -0, i: Infinity, ni: -Infinity });
    expect(out.d).toBeInstanceOf(Date);
    expect(out.d.getTime()).toBe(1700000000000);
    expect(Number.isNaN(out.bad.getTime())).toBe(true);
    expect(out.b).toBe(-5n);
    expect(out.nan).toBeNaN();
    expect(Object.is(out.neg, -0)).toBe(true);
    expect([out.i, out.ni]).toEqual([Infinity, -Infinity]);
  });

  it('keeps nested Map and Set contents typed, and undefined distinct from null', () => {
    const out = roundTrip({ m: new Map<unknown, unknown>([['k', new Set([new Date(0), 2n])], [1, undefined]]), u: undefined, a: [undefined, null] });
    expect(out.m.get('k')).toBeInstanceOf(Set);
    expect([...out.m.get('k')][0]).toBeInstanceOf(Date);
    expect(out.m.has(1)).toBe(true);
    expect(out.m.get(1)).toBeUndefined();
    expect('u' in out && out.u === undefined).toBe(true);
    expect(out.a[0]).toBeUndefined();
    expect(out.a[1]).toBeNull();
  });

  it('keeps typed arrays, ArrayBuffer, DataView and registered symbols', () => {
    const f = new Float32Array([1.5, 2.5]);
    const out = roundTrip({ f, u: new Uint8Array([1, 2, 3]).subarray(1), buf: new Uint8Array([9, 8]).buffer, dv: new DataView(new Uint8Array([1, 2]).buffer), s: Symbol.for('x') });
    expect(out.f).toBeInstanceOf(Float32Array);
    expect([...out.f]).toEqual([1.5, 2.5]);
    expect([...out.u]).toEqual([2, 3]);
    expect(new Uint8Array(out.buf)[0]).toBe(9);
    expect(out.dv).toBeInstanceOf(DataView);
    expect(out.s).toBe(Symbol.for('x'));
  });

  it('escapes plain objects that look like tags or markers', () => {
    const user = { __krateT: 'D', v: 5, nested: { __krateAction: '/x' }, __krateChildren: 'hi' };
    const out = roundTrip({ user });
    expect(out.user).toEqual(user);
    expect(out.user).not.toBeInstanceOf(Date);
  });

  it('still rejects class instances, functions and unregistered symbols with a clear path', () => {
    class Thing {}
    expect(() => encodeProp({ a: new Thing() }, 'p', hooks)).toThrow('p.a: an instance of Thing');
    expect(() => encodeProp({ a: () => 1 }, 'p', hooks)).toThrow('p.a: a function');
    expect(() => encodeProp([Symbol('x')], 'p', hooks)).toThrow('p[0]: a symbol that is not registered');
    expect(() => encodeProp(new URL('http://x.test'), 'p', hooks)).toThrow('an instance of URL');
  });
});

describe('server actions passed as props', () => {
  const callSrc = goSrc.match(/const clientActionCallJS = `([\s\S]*?)`\n/)![1];
  const actionHooks: FlightHooks = {
    ...hooks,
    isAction: (v) => (typeof v === 'function' ? (v as any).actionUrl : undefined),
  };
  const makeAction = (bound: unknown[]) =>
    Object.assign(() => 0, { actionUrl: '/__krate/action/abc', $$krateAction: 'abc', $$krateBound: bound });

  function revived(bound: unknown[], call: (url: string, args: unknown[]) => unknown): any {
    const wire = JSON.parse(JSON.stringify(encodeProp({ a: makeAction(bound) }, 'p', actionHooks)));
    return revive(wire, null, call, null).a;
  }

  it('revives to a callable that posts to its URL; client bind adds client arguments', () => {
    const calls: unknown[][] = [];
    const fn = revived([], (url, args) => calls.push([url, args]));
    expect(fn.actionUrl).toBe('/__krate/action/abc');
    fn('prev', 'form');
    fn.bind(null, 'more')('x');
    expect(calls).toEqual([
      ['/__krate/action/abc', ['prev', 'form']],
      ['/__krate/action/abc', ['more', 'x']],
    ]);
  });

  it('sends arguments bound on the server sealed in the URL, never in the clear', () => {
    const sealed: unknown[][] = [];
    const h: FlightHooks = { ...actionHooks, sealBound: (id, bound, path) => (sealed.push([id, bound, path]), 'k1.TOKEN') };
    const wire = JSON.parse(JSON.stringify(encodeProp({ a: makeAction(['secret-id', { n: 1 }]) }, 'props', h)));
    expect(wire.a).toEqual({ __krateAction: '/__krate/action/abc?__krate_bound=k1.TOKEN' });
    expect(JSON.stringify(wire)).not.toContain('secret-id');
    expect(sealed).toEqual([['abc', ['secret-id', { n: 1 }], 'props.a']]);
  });

  it('refuses bound arguments when nothing can seal them', () => {
    expect(() => encodeProp({ a: makeAction(['x']) }, 'props', actionHooks)).toThrow('props.a: a server action with bound arguments');
  });

  it('does not repeat the sealed token of an inline action that is already in its URL', () => {
    const inline = Object.assign(() => 0, { actionUrl: '/__krate/action/abc?__krate_args=%5B%22k1.x%22%5D', $$krateAction: 'abc', $$krateBound: ['k1.x'] });
    const wire = JSON.parse(JSON.stringify(encodeProp({ a: inline }, 'p', actionHooks)));
    expect(wire.a).toEqual({ __krateAction: inline.actionUrl });
  });

  it('posts a FormData argument as multipart, leading arguments as JSON in __krate_args', async () => {
    const sent: any[] = [];
    const fetchMock = async (url: string, init: any) => {
      sent.push({ url, init });
      return { ok: true, status: 200, json: async () => ({ ok: true, result: 'r' }) };
    };
    const callAction = new Function('fetch', `${callSrc}; return __krateCallAction;`)(fetchMock) as (u: string, a: unknown[]) => Promise<unknown>;

    const fd = new FormData();
    fd.set('name', 'ada');
    expect(await callAction('/__krate/action/abc', ['tag', null, fd])).toBe('r');
    const body = sent[0].init.body as FormData;
    expect(body).toBeInstanceOf(FormData);
    expect(body.get('name')).toBe('ada');
    expect(JSON.parse(body.get('__krate_args') as string)).toEqual(['tag', null]);
    expect(sent[0].init.headers['content-type']).toBeUndefined();

    await callAction('/__krate/action/abc', [1, 2]);
    expect(sent[1].init.headers['content-type']).toBe('application/json');
    expect(JSON.parse(sent[1].init.body)).toEqual({ args: [1, 2] });
  });
});
