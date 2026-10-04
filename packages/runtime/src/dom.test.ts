import { describe, it, expect } from 'vitest';
import { h, insert } from './dom';

describe('h', () => {
  it('creates an element with attributes and text', () => {
    const el = h('div', { class: 'box', id: 'x' }, 'hello') as HTMLElement;
    expect(el.tagName).toBe('DIV');
    expect(el.getAttribute('class')).toBe('box');
    expect(el.id).toBe('x');
    expect(el.textContent).toBe('hello');
  });

  it('sets DOM properties for form controls', () => {
    const input = h('input', { value: 'abc' }) as HTMLInputElement;
    expect(input.value).toBe('abc');
  });

  it('nests child nodes', () => {
    const child = h('span', null, 'hi');
    const parent = h('div', null, child) as HTMLElement;
    expect(parent.firstChild).toBe(child);
  });
});

describe('insert', () => {
  it('replaces content between comment markers', () => {
    const parent = document.createElement('div');
    const start = document.createComment('k');
    const end = document.createComment('k');
    parent.append(start, document.createTextNode('old'), end);

    insert(parent, 'new', start);

    expect(parent.textContent).toBe('new');
    expect((parent.textContent as string).includes('old')).toBe(false);
  });
});
