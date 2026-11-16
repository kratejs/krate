import { describe, expect, it } from 'vitest';
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
    expect(ul.querySelectorAll('li').length).toBe(2);
    setItems(['a', 'b', 'c']);
    krateFlushSync();
    expect(ul.textContent).toBe('abc');
  });
});
