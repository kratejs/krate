import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

// The keyboard and ARIA enhancer the compiler injects on pages that use CSS-signal
// widgets (tabs, dialogs, menus). The pointer path is pure CSS, so keyboard
// behaviour is otherwise only covered by the browser job (KRATE_BROWSER), which
// local runs skip. This drives the same script against markup shaped like the
// compiler output (see TestBuildOverlayARIAAndEnhancer in internal/build). There
// is no layout engine here, so visibility is simulated: an element is visible
// unless it, or an ancestor, has the `hidden` attribute.
const source = readFileSync(join(__dirname, '..', '..', 'compiler', 'internal', 'renderer', 'cssaria.js'), 'utf8');


const dialog = `
<div id="page"><a href="#" id="outside">outside link</a></div>
<div data-kz="dialog" id="dlg">
  <input type="radio" name="d" id="d-x" value="" aria-hidden="true" tabindex="-1" data-ka="x" checked>
  <input type="radio" name="d" id="d-d" value="d" aria-hidden="true" tabindex="-1">
  <label for="d-d" data-ka="t" role="button" aria-haspopup="dialog" aria-expanded="false" aria-controls="panel" id="opener">Open</label>
  <label for="d-x" data-ka="a" role="button" aria-haspopup="dialog" aria-expanded="true" aria-controls="panel" id="opener-open" hidden>Open</label>
  <div id="wrap" hidden>
    <div role="dialog" id="panel" data-ka="p" aria-modal="true" tabindex="-1">
      <h2 data-ka="title">Modal</h2><p data-ka="desc">Body</p>
      <button id="first">First</button>
      <button id="last">Last</button>
      <label for="d-x" data-ka="c" role="button" id="closer">Close</label>
    </div>
  </div>
</div>`;

// Listeners the script (and this harness) add to the document, removed after each
// test so scripts from earlier tests do not answer later events.
const added: Array<[string, EventListenerOrEventListenerObject, boolean | AddEventListenerOptions | undefined]> = [];

function install() {
  const add = document.addEventListener.bind(document);
  document.addEventListener = ((type: string, fn: EventListenerOrEventListenerObject, opts?: boolean | AddEventListenerOptions) => {
    added.push([type, fn, opts]);
    add(type, fn, opts);
  }) as typeof document.addEventListener;

  // No layout: everything is visible unless hidden.
  (Element.prototype as any).getClientRects = function () {
    for (let n: Element | null = this; n; n = n.parentElement) if (n.hasAttribute('hidden')) return [];
    return [{}];
  };
  // Activation: clicking a label or a radio checks it (and unchecks its group)
  // and fires change, as a browser does.
  const check = (input: HTMLInputElement) => {
    if (input.type !== 'radio' || input.checked) return;
    document.querySelectorAll<HTMLInputElement>(`input[type=radio][name="${input.name}"]`).forEach((r) => (r.checked = false));
    input.checked = true;
    input.dispatchEvent(new Event('change', { bubbles: true }));
  };
  document.addEventListener('click', (e) => {
    if (e.defaultPrevented) return;
    const t = e.target as Element;
    const label = t.closest?.('label[for]');
    if (label) check(document.getElementById(label.getAttribute('for')!) as HTMLInputElement);
    else if (t instanceof HTMLInputElement) check(t);
  });
  // eslint-disable-next-line no-eval
  (0, eval)(source);
  document.addEventListener = add;
}

const key = (el: Element, k: string, init: KeyboardEventInit = {}) => {
  const ev = new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true, ...init });
  el.dispatchEvent(ev);
  return ev;
};
const $ = (id: string) => document.getElementById(id) as HTMLElement;

beforeEach(() => {
  delete (window as any).__krateAria;
  document.documentElement.style.overflow = '';
});
afterEach(() => {
  for (const [type, fn, opts] of added.splice(0)) document.removeEventListener(type, fn, opts);
  document.body.innerHTML = '';
});

describe('modal dialog', () => {
  const open = () => {
    $('wrap').removeAttribute('hidden');
    $('opener').setAttribute('hidden', '');
    $('opener-open').removeAttribute('hidden');
    $('opener').focus();
    $('opener').click();
  };

  beforeEach(() => {
    document.body.innerHTML = dialog;
    install();
  });

  it('moves focus into the dialog, makes the rest of the page inert and locks scrolling when it opens', () => {
    open();
    expect(document.activeElement).toBe($('first'));
    expect($('page').hasAttribute('inert')).toBe(true);
    expect(document.documentElement.style.overflow).toBe('hidden');
    expect($('panel').hasAttribute('inert')).toBe(false);
  });

  it('keeps Tab and Shift+Tab inside the dialog', () => {
    open();
    const focusables = ['first', 'last'];
    $('closer').setAttribute('tabindex', '0');
    $('last').focus();
    // Closing control is the last tab stop in the dialog.
    $('closer').focus();
    expect(key($('closer'), 'Tab').defaultPrevented).toBe(true);
    expect(focusables).toContain(document.activeElement!.id);
    $('first').focus();
    expect(key($('first'), 'Tab', { shiftKey: true }).defaultPrevented).toBe(true);
    expect(document.activeElement).toBe($('closer'));
  });
});
