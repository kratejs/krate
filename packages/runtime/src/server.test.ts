import { describe, it, expect } from 'vitest';
import { jsx, renderToString } from './server';

describe('renderToString', () => {
  it('renders nested elements and escapes text', () => {
    const node = jsx('div', { class: 'x', children: [jsx('span', { children: 'a & b' })] });
    expect(renderToString(node)).toBe('<div class="x"><span>a &amp; b</span></div>');
  });

  it('renders void elements without a close tag', () => {
    expect(renderToString(jsx('img', { src: '/x.png', alt: 'y' }))).toBe('<img src="/x.png" alt="y">');
  });

  it('invokes function components', () => {
    const Comp = () => jsx('p', { children: 'hi' });
    expect(renderToString(jsx(Comp, {}))).toBe('<p>hi</p>');
  });

  it('drops event and internal props', () => {
    const node = jsx('button', { onClick: () => undefined, children: 'x' });
    expect(renderToString(node)).toBe('<button>x</button>');
  });

  it('omits null/false-valued attributes', () => {
    expect(renderToString(jsx('input', { disabled: false, value: null }))).toBe('<input>');
    expect(renderToString(jsx('input', { disabled: true }))).toBe('<input disabled>');
  });

  it('passes raw HTML through without escaping', () => {
    expect(renderToString({ __raw: '<b>hi</b>' } as any)).toBe('<b>hi</b>');
  });
});
