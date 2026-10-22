import { describe, it, expect, beforeEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { krateCondSwap, krateCondDetached, krateCondReset, krateSanitizeAttr, BLOCKED_URL } from './slots';
import { createSignal, createEffect } from './signal';
import { reconcileTrees } from './reconcile';

const tick = () => new Promise((r) => setTimeout(r, 0));

function startMarker(root: ParentNode, id: string): Comment {
  const w = document.createTreeWalker(root as Node, NodeFilter.SHOW_COMMENT);
  while (w.nextNode()) {
    if (w.currentNode.nodeValue === 'k:' + id) return w.currentNode as Comment;
  }
  throw new Error('marker not found: ' + id);
}

describe('krateCondSwap', () => {
  // happy-dom does not parse <tr> inside <template> per spec, so the
  // table-context case is covered by the Go emitter tests and the browser
  // conformance suite; the swap mechanics are identical for any element.
  it('swaps the active branch with the template branch', () => {
    document.body.innerHTML =
      '<ol><!--k:c1--><li>yes</li><!--/k:c1--></ol>' +
      '<template data-kc="c1" data-kb="a"><li>no</li></template>';
    const start = startMarker(document.body, 'c1');
    const tpl = document.querySelector('template[data-kc="c1"]') as HTMLTemplateElement;
    const tbody = document.querySelector('ol')!;
    const yesRow = tbody.querySelector('li')!;

    krateCondSwap(start, true, tpl);
    expect(tbody.textContent).toBe('yes');

    krateCondSwap(start, false, tpl);
    expect(tbody.querySelectorAll('li').length).toBe(1);
    expect(tbody.textContent).toBe('no');

    krateCondSwap(start, true, tpl);
    // The original (hydrated) node comes back, not a clone.
    expect(tbody.querySelector('li')).toBe(yesRow);
  });

  it('keeps bindings made on template content alive after the swap', async () => {
    document.body.innerHTML =
      '<ul><!--k:c2--><li>a</li><!--/k:c2--></ul><template data-kc="c2" data-kb="a"><li><button>b</button></li></template>';
    const tpl = document.querySelector('template')! as HTMLTemplateElement;
    const btn = tpl.content.querySelector('button')!;
    let clicks = 0;
    btn.addEventListener('click', () => clicks++);
    const [on, setOn] = createSignal(true);
    const start = startMarker(document.body, 'c2');
    createEffect(() => krateCondSwap(start, !!on(), tpl));
    expect(document.querySelector('ul')!.textContent).toBe('a');
    setOn(false);
    await tick();
    const live = document.querySelector('ul button') as HTMLButtonElement;
    expect(live).toBe(btn);
    live.click();
    expect(clicks).toBe(1);
  });

  it('starts from the consequent when the template holds it', () => {
    document.body.innerHTML = '<p><!--k:c3--><i>no</i><!--/k:c3--></p><template data-kc="c3" data-kb="c"><b>yes</b></template>';
    const start = startMarker(document.body, 'c3');
    const tpl = document.querySelector('template') as HTMLTemplateElement;
    krateCondSwap(start, false, tpl);
    expect(document.querySelector('p')!.innerHTML).toBe('<!--k:c3--><i>no</i><!--/k:c3-->');
    krateCondSwap(start, true, tpl);
    expect(document.querySelector('p')!.innerHTML).toBe('<!--k:c3--><b>yes</b><!--/k:c3-->');
  });

  it('treats a missing template as an empty branch', () => {
    document.body.innerHTML = '<div><!--k:c4--><b>x</b><!--/k:c4--></div>';
    const start = startMarker(document.body, 'c4');
    krateCondSwap(start, true, null);
    krateCondSwap(start, false, null);
    expect(document.querySelector('div')!.innerHTML).toBe('<!--k:c4--><!--/k:c4-->');
    krateCondSwap(start, true, null);
    expect(document.querySelector('div')!.textContent).toBe('x');
  });
});

describe('krateSanitizeAttr', () => {
  it('blocks javascript: URLs like React 19', () => {
    expect(krateSanitizeAttr('href', 'javascript:alert(1)')).toBe(BLOCKED_URL);
    expect(krateSanitizeAttr('src', ' \tJaVa\nScript:x')).toBe(BLOCKED_URL);
    expect(krateSanitizeAttr('xlink:href', 'javascript:x')).toBe(BLOCKED_URL);
    expect(krateSanitizeAttr('href', '/safe')).toBe('/safe');
    expect(krateSanitizeAttr('title', 'javascript:x')).toBe('javascript:x');
  });
});

describe('reconcile with conditional templates', () => {
  it('replaces a template whose content changed', () => {
    document.body.innerHTML = '<div id="root"><p>x</p><template data-kc="c1" data-kb="a"><b>old</b></template></div>';
    const root = document.getElementById('root')!;
    const next = document.createElement('div');
    next.id = 'root';
    next.innerHTML = '<p>x</p><template data-kc="c1" data-kb="a"><b>new</b></template>';
    reconcileTrees(root, next);
    const tpl = root.querySelector('template') as HTMLTemplateElement;
    expect(tpl.innerHTML).toBe('<b>new</b>');
  });
});

// The hydration bootstrap ships as a Go string constant appended to the
// runtime chunk; run the real source against compiler-shaped markup.
function loadBootstrap(): string {
  const here = dirname(fileURLToPath(import.meta.url));
  const src = readFileSync(resolve(here, '../../compiler/internal/renderer/newhydrate.go'), 'utf8');
  const start = src.indexOf('const HydrationBootstrapJS = `') + 'const HydrationBootstrapJS = `'.length;
  const end = src.indexOf('`\n', start);
  return src.slice(start, end);
}

describe('hydration bootstrap', () => {
  beforeEach(() => {
    const w = window as any;
    w.createEffect = createEffect;
    w.krateCondSwap = krateCondSwap;
    w.krateCondDetached = krateCondDetached;
    w.krateCondReset = krateCondReset;
    w.krateSanitizeAttr = krateSanitizeAttr;
  });

  it('binds handlers and text inside the inactive branch template, then toggles', async () => {
    document.body.innerHTML =
      '<div id="root"><ul><!--k:c--><li>on</li><!--/k:c--></ul>' +
      '<template data-kc="c" data-kb="a"><li><button data-k="k:btn">off</button><!--k:t-->0<!--/k:t--></li></template></div>';
    new Function(loadBootstrap())();
    const w = window as any;
    const [on, setOn] = createSignal(true);
    const [n, setN] = createSignal(0);
    let clicked = 0;
    w.kbindCond('c', () => on());
    w.kbindText('t', () => n());
    expect(w.findSlot('btn')).not.toBe(null);
    w.kbindEvent('btn', 'click', () => clicked++, false);
    expect(w.__krateErrs).toEqual([]);
    setN(5);
    setOn(false);
    await tick();
    const tbody = document.querySelector('ul')!;
    expect(tbody.textContent).toBe('off5');
    (tbody.querySelector('button') as HTMLButtonElement).click();
    expect(clicked).toBe(1);
    setOn(true);
    await tick();
    expect(tbody.textContent).toBe('on');
    setN(7);
    setOn(false);
    await tick();
    expect(tbody.textContent).toBe('off7');
  });

  it('finds nodes of a branch swapped out before its binding ran', async () => {
    document.body.innerHTML =
      '<div id="root"><p><!--k:c--><button data-k="k:b">ssr</button><!--/k:c--></p>' +
      '<template data-kc="c" data-kb="a"><i>client</i></template></div>';
    new Function(loadBootstrap())();
    const w = window as any;
    // The client state disagrees with the SSR state: the first run swaps.
    w.kbindCond('c', () => false);
    expect(document.querySelector('p')!.textContent).toBe('client');
    let clicks = 0;
    w.kbindEvent('b', 'click', () => clicks++, false);
    expect(w.findSlot('b')).not.toBe(null);
    w.findSlot('b').click();
    expect(clicks).toBe(1);
  });

  it('neutralizes javascript: URLs set by attribute bindings', async () => {
    document.body.innerHTML = '<div id="root"><a data-k="k:a">x</a></div>';
    new Function(loadBootstrap())();
    const [href, setHref] = createSignal('/ok');
    (window as any).kbindAttr('a', 'href', () => href());
    const a = document.querySelector('a')!;
    expect(a.getAttribute('href')).toBe('/ok');
    setHref('javascript:alert(1)');
    await tick();
    expect(a.getAttribute('href')).toBe(BLOCKED_URL);
  });
});
