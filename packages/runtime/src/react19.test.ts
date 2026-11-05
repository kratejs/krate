import { describe, it, expect, beforeEach } from 'vitest';
import { createSignal, createEffect, disposeAll } from './signal';
import { createEffectEvent } from './effects';
import { krateWithIdScope, __krate_useId } from './use-id';
import { h, krateCustomProp } from './dom';
import { Activity, ViewTransition, useEffectEvent, createElement } from './react';

const tick = () => new Promise((r) => setTimeout(r, 0));

describe('createEffectEvent', () => {
  beforeEach(() => disposeAll());

  it('always runs the latest closure over live state', () => {
    const [n, setN] = createSignal(0);
    const seen: number[] = [];
    const onTick = createEffectEvent(() => void seen.push(n()));
    onTick();
    setN(5);
    onTick();
    expect(seen).toEqual([0, 5]);
  });

  it('does not subscribe the caller to what it reads', async () => {
    const [n, setN] = createSignal(0);
    let runs = 0;
    const onTick = createEffectEvent(() => n());
    createEffect(() => {
      runs++;
      onTick();
    });
    setN(1);
    await tick();
    expect(runs).toBe(1);
  });

  it('forwards arguments, this and the result', () => {
    const e = createEffectEvent(function (this: { k: number }, a: number, b: number) {
      return this.k + a + b;
    });
    expect(e.call({ k: 1 }, 2, 3)).toBe(6);
  });

  it('is exported by the compat runtime as useEffectEvent', () => {
    expect(useEffectEvent).toBe(createEffectEvent);
  });
});

describe('useId at runtime', () => {
  it('numbers ids per list row scope and falls back to a global counter', () => {
    const a = krateWithIdScope('L:$a', () => [__krate_useId(), __krate_useId()]);
    const b = krateWithIdScope('L:$b', () => [__krate_useId()]);
    expect(a).toEqual(['krate-L:$a-u0', 'krate-L:$a-u1']);
    expect(b).toEqual(['krate-L:$b-u0']);
    const x = __krate_useId();
    const y = __krate_useId();
    expect(x).not.toBe(y);
    expect(x.startsWith('krate_u')).toBe(true);
  });

  it('encodes characters that are not valid in an id', () => {
    const id = krateWithIdScope('L:$a b/ü', () => __krate_useId());
    expect(id).toBe('krate-L:$a%20b%2F%C3%BC-u0');
  });

  it('keeps the outer scope when the inner one has no prefix', () => {
    const ids = krateWithIdScope('L:$a', () => [__krate_useId(), krateWithIdScope('', () => __krate_useId())]);
    expect(ids).toEqual(['krate-L:$a-u0', 'krate-L:$a-u1']);
  });

  it('restores the scope after the callback throws', () => {
    expect(() =>
      krateWithIdScope('L:$a', () => {
        throw new Error('x');
      }),
    ).toThrow('x');
    expect(__krate_useId().startsWith('krate_u')).toBe(true);
  });
});

describe('custom element props', () => {
  it('sets objects, arrays and functions as properties', () => {
    const el = document.createElement('my-thing');
    const obj = { a: 1 };
    const fn = () => 1;
    expect(krateCustomProp(el, 'data', obj)).toBe(true);
    expect(krateCustomProp(el, 'list', [1])).toBe(true);
    expect(krateCustomProp(el, 'cb', fn)).toBe(true);
    const rec = el as unknown as Record<string, unknown>;
    expect(rec.data).toBe(obj);
    expect(rec.cb).toBe(fn);
    expect(el.hasAttribute('data')).toBe(false);
  });

  it('leaves primitives to attributes unless the element defines the property', () => {
    const el = document.createElement('my-thing');
    expect(krateCustomProp(el, 's', 'str')).toBe(false);
    class Defined extends HTMLElement {
      count = 0;
    }
    customElements.define('defined-thing', Defined);
    const d = document.createElement('defined-thing') as Defined;
    expect(krateCustomProp(d, 'count', 3)).toBe(true);
    expect(d.count).toBe(3);
  });

  it('ignores ordinary elements and dashed names', () => {
    expect(krateCustomProp(document.createElement('div'), 'data', {})).toBe(false);
    expect(krateCustomProp(document.createElement('my-thing'), 'data-x', {})).toBe(false);
  });

  it('re-applies an object once the element class is defined', async () => {
    const el = document.createElement('late-thing');
    document.body.appendChild(el);
    const obj = { v: 1 };
    krateCustomProp(el, 'data', obj);
    const seen: unknown[] = [];
    class Late extends HTMLElement {
      set data(v: unknown) {
        seen.push(v);
      }
      get data() {
        return seen[seen.length - 1];
      }
    }
    customElements.define('late-thing', Late);
    await customElements.whenDefined('late-thing');
    await tick();
    expect(seen).toContain(obj);
    el.remove();
  });

  it('h() routes props of a custom element through the property rules', () => {
    const obj = { a: 1 };
    const el = h('my-card', { data: obj, title: 'x', open: true }) as HTMLElement;
    expect((el as unknown as Record<string, unknown>).data).toBe(obj);
    expect(el.getAttribute('title')).toBe('x');
    expect(el.getAttribute('open')).toBe('');
    expect(el.hasAttribute('data')).toBe(false);
  });
});

describe('Activity and ViewTransition (unlowered code)', () => {
  beforeEach(() => disposeAll());

  it('Activity renders its children and hides them in hidden mode', async () => {
    const [mode, setMode] = createSignal<'visible' | 'hidden'>('visible');
    const root = Activity({
      get mode() {
        return mode();
      },
      children: createElement('p', null, 'kept'),
    }) as HTMLElement;
    document.body.appendChild(root);
    expect(root.querySelector('p')!.textContent).toBe('kept');
    expect(root.style.display).toBe('contents');
    setMode('hidden');
    await tick();
    expect(root.style.display).toBe('none');
    expect(root.querySelector('p')!.textContent).toBe('kept');
    root.remove();
  });

  it('ViewTransition names its first element child and keeps the rest', () => {
    const frag = ViewTransition({
      name: 'hero',
      default: 'slow',
      children: [createElement('h1', null, 'T'), createElement('p', null, 'rest')],
    }) as DocumentFragment;
    const h1 = frag.querySelector('h1')!;
    expect(h1.getAttribute('data-view-transition')).toBe('hero');
    expect(h1.getAttribute('data-view-transition-class')).toBe('slow');
    expect(frag.querySelector('p')!.hasAttribute('data-view-transition')).toBe(false);
    expect(frag.textContent).toBe('Trest');
  });
});

describe('resource hints', () => {
  it('adds each link once', async () => {
    const { krateHintPreload, krateHintPreconnect, krateHintPreinit, krateHintPrefetchDNS } = await import('./hints');
    krateHintPreload('/a.css', { as: 'style' });
    krateHintPreload('/a.css', { as: 'style' });
    krateHintPreconnect('https://example.com');
    krateHintPrefetchDNS('https://example.com');
    krateHintPreinit('/b.css', { as: 'style' });
    krateHintPreinit('/c.js', { as: 'script' });
    const q = (s: string) => document.head.querySelectorAll(s).length;
    expect(q('link[rel="preload"][href="/a.css"]')).toBe(1);
    expect(document.head.querySelector('link[rel="preload"]')!.getAttribute('as')).toBe('style');
    expect(q('link[rel="preconnect"][href="https://example.com"]')).toBe(1);
    expect(q('link[rel="dns-prefetch"][href="https://example.com"]')).toBe(1);
    expect(q('link[rel="stylesheet"][href="/b.css"]')).toBe(1);
    expect(q('script[src="/c.js"]')).toBe(1);
  });
});
