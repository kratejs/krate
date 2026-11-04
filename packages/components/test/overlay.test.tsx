import { describe, it, expect, beforeEach, vi } from 'vitest';
import { openOverlay, closeOverlay, isOverlayOpen, setOverlayOpen, onOverlayChange } from '../src/overlay/overlay';
import { toast } from '../src/toast/toast-api';

// A hand-built scope anchor shaped like the compiler output: a sentinel radio
// (data-ka="x") plus one radio per content value.
function mount(values: string[], extra = ''): HTMLElement {
  document.body.innerHTML =
    `<div id="host"><div data-kz="dialog" id="a">` +
    `<input type="radio" name="g" value="" data-ka="x" checked>` +
    values.map((v) => `<input type="radio" name="g" value="${v}">`).join('') +
    `<div class="krate-dialog" id="root"></div>${extra}</div></div>`;
  return document.getElementById('a') as HTMLElement;
}

describe('overlay control helpers', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
  });

  it('opens the first content, a named content, and closes', () => {
    const a = mount(['one', 'two']);
    expect(isOverlayOpen(a)).toBe(false);
    expect(openOverlay('#root')).toBe(true);
    expect(isOverlayOpen('#root')).toBe(true);
    expect((a.querySelector('input[value=one]') as HTMLInputElement).checked).toBe(true);
    expect(closeOverlay('#root')).toBe(true);
    expect(isOverlayOpen(a)).toBe(false);
    expect(openOverlay(a, 'two')).toBe(true);
    expect((a.querySelector('input[value=two]') as HTMLInputElement).checked).toBe(true);
    expect(setOverlayOpen(a, false)).toBe(true);
    expect(isOverlayOpen(a)).toBe(false);
  });

  it('reports a missing target or value instead of silently succeeding', () => {
    mount(['one']);
    expect(openOverlay('#nope')).toBe(false);
    expect(openOverlay('#root', 'missing')).toBe(false);
    expect(closeOverlay(null)).toBe(false);
  });

  it('only drives its own radios when overlays are nested', () => {
    const inner =
      `<div data-kz="popover" id="inner"><input type="radio" name="p" value="" data-ka="x" checked>` +
      `<input type="radio" name="p" value="in"></div>`;
    const a = mount(['out'], inner);
    openOverlay(a);
    expect((a.querySelector('input[value=out]') as HTMLInputElement).checked).toBe(true);
    expect(isOverlayOpen('#inner')).toBe(false);
  });

  it('delivers change events for the target anchor only and unsubscribes', () => {
    const a = mount(['one']);
    const cb = vi.fn();
    const off = onOverlayChange(a, cb);
    a.dispatchEvent(new CustomEvent('krateopenchange', { detail: { open: true, value: 'one' } }));
    expect(cb).toHaveBeenCalledWith({ open: true, value: 'one' });
    // Events bubbling from a nested overlay are ignored.
    const child = document.createElement('div');
    a.appendChild(child);
    child.dispatchEvent(new CustomEvent('krateopenchange', { bubbles: true, detail: { open: false, value: '' } }));
    expect(cb).toHaveBeenCalledTimes(1);
    off();
    a.dispatchEvent(new CustomEvent('krateopenchange', { detail: { open: false, value: '' } }));
    expect(cb).toHaveBeenCalledTimes(1);
  });
});

describe('toast()', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    vi.useFakeTimers();
  });

  it('renders into a polite live region with the Toast markup', () => {
    const t = toast({ title: 'Saved', description: 'Done', variant: 'success', duration: 0 });
    const vp = document.querySelector('.krate-toaster') as HTMLElement;
    expect(vp.getAttribute('aria-live')).toBe('polite');
    const el = document.getElementById(t.id) as HTMLElement;
    expect(el.className).toBe('krate-toast');
    expect(el.getAttribute('data-variant')).toBe('success');
    expect(el.getAttribute('role')).toBe('status');
    expect(el.querySelector('.krate-toast-title')?.textContent).toBe('Saved');
    expect(el.querySelector('.krate-toast-description')?.textContent).toBe('Done');
    expect((el.querySelector('.krate-toast-close') as HTMLElement).getAttribute('aria-label')).toBe('Close');
  });

  it('uses role=alert for destructive toasts and shares one viewport', () => {
    const a = toast({ title: 'A', variant: 'destructive', duration: 0 });
    toast('B');
    expect(document.getElementById(a.id)?.getAttribute('role')).toBe('alert');
    expect(document.querySelectorAll('.krate-toaster').length).toBe(1);
    expect(document.querySelectorAll('.krate-toast').length).toBe(2);
  });

  it('dismisses on the close button, on dismiss(), and after the duration', () => {
    const a = toast({ title: 'A', duration: 0 });
    (document.getElementById(a.id)!.querySelector('.krate-toast-close') as HTMLElement).click();
    expect(document.getElementById(a.id)).toBeNull();
    const b = toast({ title: 'B', duration: 0 });
    b.dismiss();
    expect(document.getElementById(b.id)).toBeNull();
    const c = toast({ title: 'C', duration: 1000 });
    expect(document.getElementById(c.id)).not.toBeNull();
    vi.advanceTimersByTime(1000);
    expect(document.getElementById(c.id)).toBeNull();
  });

  it('renders text as text, never HTML', () => {
    const t = toast({ title: '<img src=x onerror=alert(1)>', duration: 0 });
    const el = document.getElementById(t.id)!;
    expect(el.querySelector('img')).toBeNull();
    expect(el.querySelector('.krate-toast-title')?.textContent).toBe('<img src=x onerror=alert(1)>');
  });
});
