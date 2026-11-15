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
