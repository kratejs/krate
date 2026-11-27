import { describe, expect, it } from 'vitest';
import { createCSSChoice, createCSSFlags, createCSSToggle } from './index.js';

// tsc --noEmit -p . typechecks this file, so the calls below also pin the
// `.is()` / `.on()` / `.off()` helper types that the docs describe.
describe('CSS scope helper types', () => {
  it('CSSScope exposes is, on and off', () => {
    const tab = createCSSChoice('a', ['a', 'b']);
    const t = createCSSToggle(false);
    const isA: boolean = tab.is('a');
    const isOn: boolean = t.on();
    const isOff: boolean = t.off();
    // @ts-expect-error a choice of 'a' | 'b' rejects other values
    tab.is(3);
    expect([isA, isOn, isOff]).toEqual([true, false, true]);
    expect(tab.is('b')).toBe(false);
  });

  it('CSSFlagsScope exposes is', () => {
    const f = createCSSFlags(['bold', 'italic']);
    const bold: boolean = f.is('bold');
    // @ts-expect-error unknown flag
    f.is('underline');
    expect(bold).toBe(false);
  });

  it('helpers survive destructuring the tuple', () => {
    const scope = createCSSChoice('x', ['x', 'y']);
    const [get] = scope;
    expect(get()).toBe('x');
    expect(scope.is('x')).toBe(true);
  });
});
