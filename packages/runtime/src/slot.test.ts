import { describe, expect, it } from 'vitest';
import { createSlot, createSlottable, Slot, Slottable } from './slot.js';

describe('react-slot shim', () => {
  it('createSlot merges props and ref onto the single child', () => {
    const SlotImpl = createSlot('Dialog');
    const child = document.createElement('a');
    child.setAttribute('class', 'base');
    let refd: unknown = null;
    const node = SlotImpl({ className: 'extra', 'data-x': '1', ref: (n: unknown) => (refd = n), children: child });
    expect(node).toBe(child);
    expect(child.getAttribute('class')).toBe('base extra');
    expect(child.getAttribute('data-x')).toBe('1');
    expect(refd).toBe(child);

    const objRef = { current: null as unknown };
    SlotImpl({ ref: objRef, children: [child] });
    expect(objRef.current).toBe(child);
    expect((SlotImpl as { displayName?: string }).displayName).toBe('Dialog.Slot');
  });

  it('createSlottable returns a marker that renders its children', () => {
    const Marker = createSlottable('Dialog');
    expect(Marker({ children: 'x' })).toBe('x');
    expect(typeof Slot).toBe('function');
    expect(typeof Slottable).toBe('function');
  });
});
