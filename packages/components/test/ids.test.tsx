import { describe, it, expect } from 'vitest';
import { Command } from '../src/command/command';
import { Select } from '../src/select/select';

const items = [
  { value: 'a', label: 'A' },
  { value: 'b', label: 'B' },
];

function ids(root: Element): string[] {
  return Array.from(root.querySelectorAll('[id]')).map((e) => e.id);
}

describe('generated ids', () => {
  it('Command has a non-empty list id from the first render, unique per instance', () => {
    const one = Command({ items }) as HTMLElement;
    const two = Command({ items }) as HTMLElement;
    for (const el of [one, two]) {
      const list = el.querySelector('[role=listbox]') as HTMLElement;
      expect(list.id).not.toBe('');
      expect(el.querySelector('input')!.getAttribute('aria-controls')).toBe(list.id);
      for (const id of ids(el)) expect(id.startsWith('-')).toBe(false);
    }
    const all = [...ids(one), ...ids(two)];
    expect(new Set(all).size).toBe(all.length);
  });

  it('Command keeps an author-supplied id', () => {
    const el = Command({ items, id: 'mine' }) as HTMLElement;
    expect(el.querySelector('[role=listbox]')!.id).toBe('mine');
    expect(el.querySelector('[role=option]')!.id).toBe('mine-opt-0');
  });

  it('Select never renders an id or aria-controls that starts with a bare suffix', () => {
    const a = Select({ options: items }) as HTMLElement;
    const b = Select({ options: items }) as HTMLElement;
    for (const el of [a, b]) {
      for (const id of ids(el)) expect(id.startsWith('-')).toBe(false);
      for (const c of Array.from(el.querySelectorAll('[aria-controls]'))) {
        expect(c.getAttribute('aria-controls')!.startsWith('-')).toBe(false);
      }
    }
    const all = [...ids(a), ...ids(b)];
    expect(new Set(all).size).toBe(all.length);
  });
});
