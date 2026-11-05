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

const tabs = `
<div data-kz="tabs">
  <input type="radio" class="krc-h" name="t" id="t-one" value="one" aria-hidden="true" tabindex="-1">
  <input type="radio" class="krc-h" name="t" id="t-two" value="two" aria-hidden="true" tabindex="-1" checked>
  <input type="radio" class="krc-h" name="t" id="t-three" value="three" aria-hidden="true" tabindex="-1">
  <div class="krate-tabs"><div role="tablist" aria-orientation="horizontal">
    <label for="t-one" data-ka="t" role="tab" id="tab-one">One</label>
    <label for="t-two" data-ka="t" role="tab" id="tab-two">Two</label>
    <label for="t-three" data-ka="t" role="tab" id="tab-three">Three</label>
  </div></div>
</div>`;

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

describe('tabs', () => {
  beforeEach(() => {
    document.body.innerHTML = tabs;
    install();
  });

  it('marks the checked tab selected and gives only it a tab stop', () => {
    expect($('tab-two').getAttribute('aria-selected')).toBe('true');
    expect($('tab-one').getAttribute('aria-selected')).toBe('false');
    expect([...document.querySelectorAll('label')].map((l) => l.getAttribute('tabindex'))).toEqual(['-1', '0', '-1']);
  });

  it('moves focus and selection with the arrow keys, wrapping at both ends', () => {
    $('tab-two').focus();
    expect(key($('tab-two'), 'ArrowRight').defaultPrevented).toBe(true);
    expect(document.activeElement).toBe($('tab-three'));
    expect(($('t-three') as HTMLInputElement).checked).toBe(true);
    expect($('tab-three').getAttribute('aria-selected')).toBe('true');
    expect($('tab-three').getAttribute('tabindex')).toBe('0');
    expect($('tab-two').getAttribute('tabindex')).toBe('-1');

    key($('tab-three'), 'ArrowRight');
    expect(document.activeElement).toBe($('tab-one'));
    key($('tab-one'), 'ArrowLeft');
    expect(document.activeElement).toBe($('tab-three'));
  });

  it('jumps to the first and last tab with Home and End', () => {
    $('tab-two').focus();
    key($('tab-two'), 'End');
    expect(document.activeElement).toBe($('tab-three'));
    key($('tab-three'), 'Home');
    expect(document.activeElement).toBe($('tab-one'));
    expect(($('t-one') as HTMLInputElement).checked).toBe(true);
  });

  it('ignores the vertical arrows on a horizontal tablist and other keys', () => {
    $('tab-two').focus();
    for (const k of ['ArrowDown', 'ArrowUp', 'a', 'Tab']) {
      const ev = key($('tab-two'), k);
      expect(ev.defaultPrevented, k).toBe(false);
    }
    expect(document.activeElement).toBe($('tab-two'));
  });

  it('activates the focused tab with Enter and Space', () => {
    $('tab-one').focus();
    expect(key($('tab-one'), 'Enter').defaultPrevented).toBe(true);
    expect(($('t-one') as HTMLInputElement).checked).toBe(true);
    $('tab-three').focus();
    key($('tab-three'), ' ');
    expect(($('t-three') as HTMLInputElement).checked).toBe(true);
  });

  it('flips left and right in a right-to-left tablist', () => {
    for (const l of document.querySelectorAll<HTMLElement>('label')) l.style.direction = 'rtl';
    $('tab-two').focus();
    key($('tab-two'), 'ArrowRight');
    expect(document.activeElement).toBe($('tab-one'));
    key($('tab-one'), 'ArrowLeft');
    expect(document.activeElement).toBe($('tab-two'));
  });

  it('does not touch a disabled tab\'s tab stop', () => {
    $('tab-three').setAttribute('aria-disabled', 'true');
    ($('t-two') as HTMLInputElement).dispatchEvent(new Event('change', { bubbles: true }));
    expect($('tab-three').getAttribute('tabindex')).toBe('-1');
  });
});

describe('modal dialog', () => {
  const open = () => {
    $('wrap').removeAttribute('hidden');
    $('opener').setAttribute('hidden', '');
    $('opener-open').removeAttribute('hidden');
    $('opener').focus();
    $('opener').click();
  };
  const closeVisuals = () => {
    $('wrap').setAttribute('hidden', '');
    $('opener').removeAttribute('hidden');
    $('opener-open').setAttribute('hidden', '');
  };

  beforeEach(() => {
    document.body.innerHTML = dialog;
    install();
  });

  it('links the dialog to its title and description', () => {
    expect($('panel').getAttribute('aria-labelledby')).toBe($('panel').querySelector('h2')!.id);
    expect($('panel').getAttribute('aria-describedby')).toBe($('panel').querySelector('p')!.id);
  });

  it('moves focus into the dialog, makes the rest of the page inert and locks scrolling when it opens', () => {
    open();
    expect(document.activeElement).toBe($('first'));
    expect($('page').hasAttribute('inert')).toBe(true);
    expect(document.documentElement.style.overflow).toBe('hidden');
    expect($('panel').hasAttribute('inert')).toBe(false);
  });

  it('closes on Escape from inside the dialog and restores the page', () => {
    open();
    expect(key($('first'), 'Escape').defaultPrevented).toBe(true);
    expect(($('d-x') as HTMLInputElement).checked).toBe(true);
    closeVisuals();
    ($('d-x') as HTMLInputElement).dispatchEvent(new Event('change', { bubbles: true }));
    expect($('page').hasAttribute('inert')).toBe(false);
    expect(document.documentElement.style.overflow).toBe('');
  });

  it('closes on Escape pressed anywhere while the modal is open', () => {
    open();
    key(document.body, 'Escape');
    expect(($('d-x') as HTMLInputElement).checked).toBe(true);
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

  it('pulls focus back when it escapes the open modal', () => {
    open();
    $('outside').focus();
    document.dispatchEvent(new FocusEvent('focusin', { bubbles: true }));
    // focusin on the outside link is redirected into the dialog
    $('outside').dispatchEvent(new FocusEvent('focusin', { bubbles: true }));
    expect($('panel').contains(document.activeElement)).toBe(true);
  });

  it('leaves Escape alone when no dialog is open', () => {
    expect(key(document.body, 'Escape').defaultPrevented).toBe(false);
    expect(($('d-x') as HTMLInputElement).checked).toBe(true);
  });
});
