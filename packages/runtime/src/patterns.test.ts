import { describe, it, expect } from 'vitest';
import { h, krateSpread } from './dom';
import { createContext, krateContextRows } from './context';
import { createSignal, krateDerived } from './signal';
import { krateParam } from './hook-args';
import { krateCondSwap } from './slots';
import { krateOn } from './events';

const tick = () => new Promise((r) => setTimeout(r, 0));
const SVG_NS = 'http://www.w3.org/2000/svg';

describe('SVG elements created by h()', () => {
  it('uses the SVG namespace for SVG-only tags and their children', () => {
    const svg = h('svg', { width: '10' }, h('g', null, h('circle', { r: '2' }))) as SVGElement;
    expect(svg.namespaceURI).toBe(SVG_NS);
    const circle = svg.querySelector('circle')!;
    expect(circle.namespaceURI).toBe(SVG_NS);
    expect(circle.parentElement!.localName).toBe('g');
  });

  it('re-creates an ambiguous tag appended to an SVG parent in the SVG namespace', () => {
    const svg = h('svg', null, h('a', { href: '#x' }, h('path', { d: 'M0 0' }))) as SVGElement;
    const a = svg.querySelector('a')!;
    expect(a.namespaceURI).toBe(SVG_NS);
    expect(a.getAttribute('href')).toBe('#x');
    expect(a.firstElementChild!.namespaceURI).toBe(SVG_NS);
  });

  it('keeps HTML inside a foreignObject', () => {
    const fo = h('svg', null, h('foreignObject', null, h('a', { href: '#y' }, 'x'))) as SVGElement;
    expect(fo.querySelector('a')!.namespaceURI).toBe('http://www.w3.org/1999/xhtml');
  });
});

describe('krateCondSwap with an SVG template', () => {
  it('unwraps the <svg> wrapper so the branch keeps the SVG namespace', () => {
    document.body.innerHTML =
      '<svg id="s"><!--k:c--><!--/k:c--></svg><template data-kc="c" data-kb="c" data-kns="svg"><svg><g id="grp"><path d="M0 0"></path></g></svg></template>';
    const start = document.querySelector('svg')!.firstChild as Comment;
    const tpl = document.querySelector('template') as HTMLTemplateElement;
    krateCondSwap(start, true, tpl);
    const g = document.getElementById('grp')!;
    expect(g.namespaceURI).toBe(SVG_NS);
    expect(g.parentElement!.id).toBe('s');
    expect(g.firstElementChild!.namespaceURI).toBe(SVG_NS);
  });
});

describe('krateSpread', () => {
  it('applies attributes and handlers and removes keys that disappear', async () => {
    const [on, setOn] = createSignal(true);
    const el = document.createElement('button');
    document.body.appendChild(el);
    let clicks = 0;
    krateSpread(el, () => (on() ? { title: 'x', className: 'a', onClick: () => clicks++ } : { className: 'b' }));
    expect(el.getAttribute('title')).toBe('x');
    expect(el.getAttribute('class')).toBe('a');
    el.dispatchEvent(new Event('click', { bubbles: true }));
    expect(clicks).toBe(1);
    setOn(false);
    await tick();
    expect(el.hasAttribute('title')).toBe(false);
    expect(el.getAttribute('class')).toBe('b');
    el.dispatchEvent(new Event('click', { bubbles: true }));
    expect(clicks).toBe(1);
  });
});

describe('krateContextRows', () => {
  it('shows live provider values to rows created under it', async () => {
    const Sel = createContext('');
    const [sel, setSel] = createSignal('a');
    const row = krateContextRows([[Sel, () => sel()]], () => Sel.useContextLive());
    const read = row();
    expect(read()).toBe('a');
    setSel('b');
    expect(read()).toBe('b');
    // Outside the rows the context has its default again.
    expect(Sel.useContext()).toBe('');
  });
});

describe('krateParam', () => {
  it('unwraps a derived getter argument and passes other values through', () => {
    const [n, setN] = createSignal(1);
    const arg = krateDerived(() => n() * 2);
    expect(krateParam(arg)).toBe(2);
    setN(2);
    expect(krateParam(arg)).toBe(4);
    expect(krateParam(5)).toBe(5);
    const fn = () => 1;
    expect(krateParam(fn)).toBe(fn);
  });
});

describe('krateOn unaffected by spread removal', () => {
  it('removes a handler with null', () => {
    const el = document.createElement('div');
    document.body.appendChild(el);
    let n = 0;
    krateOn(el, 'onClick', () => n++);
    el.dispatchEvent(new Event('click', { bubbles: true }));
    krateOn(el, 'onClick', null);
    el.dispatchEvent(new Event('click', { bubbles: true }));
    expect(n).toBe(1);
  });
});
