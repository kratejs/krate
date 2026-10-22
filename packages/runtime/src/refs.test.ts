import { describe, it, expect, beforeEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { krateAttachRef, krateSetRef } from './refs';
import { krateBindRef, h, disposeNode, krateOn, krateControl } from './dom';
import { createSignal, createEffect, onCleanup, disposeAll } from './signal';
import { krateCondSwap, krateCondDetached, krateCondReset, krateSanitizeAttr } from './slots';
import { useImperativeHandle, createRef } from './react';

const tick = () => new Promise((r) => setTimeout(r, 0));

function bootstrap(): void {
  const here = dirname(fileURLToPath(import.meta.url));
  const src = readFileSync(resolve(here, '../../compiler/internal/renderer/newhydrate.go'), 'utf8');
  const start = src.indexOf('const HydrationBootstrapJS = `') + 'const HydrationBootstrapJS = `'.length;
  const end = src.indexOf('`\n', start);
  new Function(src.slice(start, end))();
}

describe('element refs', () => {
  beforeEach(() => {
    document.body.innerHTML = '<div id="root"><input id="i"></div>';
  });

  it('sets and clears object refs', () => {
    const el = document.getElementById('i')!;
    const ref = { current: null as unknown };
    const release = krateAttachRef(el, ref);
    expect(ref.current).toBe(el);
    release();
    expect(ref.current).toBe(null);
  });

  it('calls callback refs with the node, then null', () => {
    const el = document.getElementById('i')!;
    const calls: unknown[] = [];
    const release = krateAttachRef(el, (n) => {
      calls.push(n);
    });
    release();
    release();
    expect(calls).toEqual([el, null]);
  });

  it('calls a React 19 cleanup function instead of null', () => {
    const el = document.getElementById('i')!;
    const log: string[] = [];
    const release = krateAttachRef(el, () => {
      log.push('attach');
      return () => log.push('cleanup');
    });
    release();
    expect(log).toEqual(['attach', 'cleanup']);
  });

  it('re-binds when the callback identity changes', async () => {
    const el = document.getElementById('i')!;
    const log: string[] = [];
    const a = (n: unknown) => log.push('a ' + (n ? 'node' : 'null'));
    const b = (n: unknown) => log.push('b ' + (n ? 'node' : 'null'));
    const [which, setWhich] = createSignal<typeof a>(a);
    const dispose = krateBindRef(el, () => which());
    setWhich(() => b);
    await tick();
    dispose();
    expect(log).toEqual(['a node', 'a null', 'b node', 'b null']);
  });

  it('does not subscribe to signals read inside a callback ref', async () => {
    const el = document.getElementById('i')!;
    const [n, setN] = createSignal(0);
    let calls = 0;
    krateBindRef(el, () => () => {
      calls++;
      n();
    });
    setN(1);
    await tick();
    expect(calls).toBe(1);
  });

  it('assigns plain variable targets through the setter', () => {
    const el = document.getElementById('i')!;
    let target: unknown = undefined;
    const dispose = krateBindRef(el, () => undefined, (v) => {
      target = v;
    });
    expect(target).toBe(el);
    dispose();
    expect(target).toBe(null);
  });

  it('keeps the right element per list item and detaches removed items', async () => {
    const root = document.getElementById('root')!;
    const refs = new Map<number, unknown>();
    const [items, setItems] = createSignal([1, 2, 3]);
    const ul = document.createElement('ul');
    root.appendChild(ul);
    createEffect(() => {
      while (ul.firstChild) {
        disposeNode(ul.firstChild);
        ul.removeChild(ul.firstChild);
      }
      for (const i of items()) {
        ul.appendChild(h('li', { ref: (el: unknown) => (el ? refs.set(i, el) : refs.delete(i)) }, String(i)));
      }
    });
    expect([...refs.keys()]).toEqual([1, 2, 3]);
    expect((refs.get(2) as HTMLElement).textContent).toBe('2');
    setItems([1, 3]);
    await tick();
    expect([...refs.keys()].sort()).toEqual([1, 3]);
    expect((refs.get(3) as HTMLElement).textContent).toBe('3');
  });

  it('releases h() refs when the node is disposed', () => {
    const ref = { current: null as unknown };
    const el = h('div', { ref });
    expect(ref.current).toBe(el);
    disposeNode(el);
    expect(ref.current).toBe(null);
  });
});

describe('imperative handles', () => {
  it('attaches the handle and re-creates it when deps change', async () => {
    const ref = createRef<{ v: number }>();
    const [n, setN] = createSignal(1);
    let creates = 0;
    const [other, setOther] = createSignal(0);
    createEffect(() => {
      useImperativeHandle(
        ref,
        () => {
          creates++;
          other();
          return { v: n() };
        },
        () => [n()]
      );
    });
    expect(ref.current).toEqual({ v: 1 });
    setOther(1);
    await tick();
    expect(creates).toBe(1);
    setN(2);
    await tick();
    expect(ref.current).toEqual({ v: 2 });
    expect(creates).toBe(2);
  });

  it('gives callback refs the handle and cleans up', () => {
    const log: unknown[] = [];
    const undo = krateSetRef((v) => {
      log.push(v);
    }, { focus: 1 });
    undo();
    expect(log).toEqual([{ focus: 1 }, null]);
  });
});

describe('hydration bootstrap refs and events', () => {
  beforeEach(() => {
    const w = window as any;
    Object.assign(w, {
      createEffect,
      onCleanup,
      createSignal,
      krateCondSwap,
      krateCondDetached,
      krateCondReset,
      krateSanitizeAttr,
      krateBindRef,
      krateOn,
      krateControl,
    });
  });

  it('attaches refs before queued effects and detaches on teardown', () => {
    document.body.innerHTML = '<div id="root"><input data-k="k:i"></div>';
    bootstrap();
    const w = window as any;
    const ref = { current: null as unknown };
    const log: unknown[] = [];
    w.kbindRef('i', () => ref, (v: unknown) => (ref.current = v));
    createEffect(() => {
      log.push(ref.current);
    });
    expect(log[0]).toBe(document.querySelector('input'));
    disposeAll();
    expect(ref.current).toBe(null);
  });

  it('detaches refs of a hidden conditional branch and re-attaches when shown', async () => {
    document.body.innerHTML =
      '<div id="root"><p><!--k:c--><span data-k="k:s">on</span><!--/k:c--></p>' +
      '<template data-kc="c" data-kb="a"><i data-k="k:o">off</i></template></div>';
    bootstrap();
    const w = window as any;
    const log: string[] = [];
    const [on, setOn] = createSignal(true);
    w.kbindCond('c', () => on());
    w.kbindRef('s', () => (el: Element | null) => log.push('s ' + (el ? el.tagName : 'null')));
    w.kbindRef('o', () => (el: Element | null) => log.push('o ' + (el ? el.tagName : 'null')));
    expect(log).toEqual(['s SPAN']);
    setOn(false);
    await tick();
    expect(log).toEqual(['s SPAN', 's null', 'o I']);
    setOn(true);
    await tick();
    expect(log).toEqual(['s SPAN', 's null', 'o I', 'o null', 's SPAN']);
    disposeAll();
  });

  it('binds handlers and controlled values', () => {
    document.body.innerHTML = '<div id="root"><input data-k="k:i" value="a"></div>';
    bootstrap();
    const w = window as any;
    const log: string[] = [];
    w.kbindHandler('i', 'onChange', (e: any) => log.push(e.type + ' ' + e.target.value));
    w.kbindProp('i', 'value', () => 'a');
    const input = document.querySelector('input')!;
    input.value = 'ab';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    expect(log).toEqual(['change ab']);
    expect(input.value).toBe('a');
    disposeAll();
  });
});
