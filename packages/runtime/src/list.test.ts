import { describe, expect, it, vi } from 'vitest';
import { krateList } from './list.js';
import { h } from './dom.js';
import { createSignal, krateFlushSync } from './signal.js';

describe('krateList', () => {
  it('carries its arguments for custom renderers', () => {
    const [items] = createSignal([1, 2]);
    const key = (i: unknown) => i;
    const render = (i: unknown) => h('li', null, String(i));
    const list = krateList(items, key, render, 1);
    expect(list.__ka).toBe(1);
    expect(list.items).toBe(items);
    expect(list.items()).toEqual([1, 2]);
    expect(list.key).toBe(key);
    expect(list.render).toBe(render);
    expect(list.mode).toBe(1);
  });

  it('still mounts as DOM', () => {
    const [items, setItems] = createSignal(['a', 'b']);
    const ul = document.createElement('ul');
    ul.appendChild(krateList(items, (i) => i, (i) => h('li', null, String(i)), 0).mount());
    expect(ul.querySelectorAll('li')).toHaveLength(2);
    setItems(['a', 'b', 'c']);
    krateFlushSync();
    expect(ul.textContent).toBe('abc');
  });
});

describe('krateList object keys', () => {
  const mountList = (items: () => unknown[], key: (i: any) => unknown) => {
    const ul = document.createElement('ul');
    ul.appendChild(krateList(items as any, key, (i: any) => h('li', null, String(i.n ?? i)), 0).mount());
    return ul;
  };

  it('keys distinct DOM nodes apart without a duplicate-key warning', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const a = document.createElement('span');
    const b = document.createElement('span');
    const [items] = createSignal<unknown[]>([{ n: 'a', k: a }, { n: 'b', k: b }]);
    const ul = mountList(items, (i) => i.k);
    expect(ul.querySelectorAll('li')).toHaveLength(2);
    const msgs = spy.mock.calls.map((c) => String(c[0]));
    expect(msgs.some((m) => m.includes('same key'))).toBe(false);
    expect(msgs.filter((m) => m.includes('use a string or number') || m.includes('Use a string or number'))).toHaveLength(msgs.length);
    spy.mockRestore();
  });

  it('keeps the row of an object key across updates', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const a = { id: 1 };
    const b = { id: 2 };
    const ia = { n: 'a', k: a };
    const ib = { n: 'b', k: b };
    const [items, setItems] = createSignal<unknown[]>([ia, ib]);
    const ul = mountList(items, (i) => i.k);
    const rowB = ul.querySelectorAll('li')[1];
    setItems([ib, ia]);
    krateFlushSync();
    const lis = ul.querySelectorAll('li');
    expect(lis[0]).toBe(rowB);
    expect(ul.textContent).toBe('ba');
    spy.mockRestore();
  });

  it('leaves primitive keys as they were', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const [items, setItems] = createSignal<unknown[]>(['x', 'y']);
    const ul = mountList(items, (i) => i);
    const rowY = ul.querySelectorAll('li')[1];
    setItems(['y', 'x']);
    krateFlushSync();
    expect(ul.querySelectorAll('li')[0]).toBe(rowY);
    setItems(['y', 'y']);
    krateFlushSync();
    expect(spy.mock.calls.some((c) => String(c[0]).includes('same key'))).toBe(true);
    spy.mockRestore();
  });
});
