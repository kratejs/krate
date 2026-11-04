/**
 * Imperative control of the zero-JS overlays (Dialog, AlertDialog, Sheet,
 * Popover, DropdownMenu, MenuBar, NavigationMenu, Accordion). They are CSS
 * scopes driven by hidden radios; these helpers flip the radios and read the
 * `krateopenchange` event the keyboard enhancer (inlined on pages that use an
 * overlay) dispatches, so they work from any client code. Give the overlay an
 * `id` prop and pass `'#that-id'`, or pass any element inside it.
 */

export interface OverlayChange {
  open: boolean;
  /** The `value` of the content that is open, "" when closed. */
  value: string;
}

export type OverlayTarget = string | Element | null | undefined;

function anchorOf(target: OverlayTarget): HTMLElement | null {
  if (typeof document === 'undefined' || !target) return null;
  const el = typeof target === 'string' ? document.querySelector(target) : target;
  if (!el) return null;
  return (el.closest('[data-kz]') || el.querySelector('[data-kz]')) as HTMLElement | null;
}

function ownRadios(anchor: HTMLElement): HTMLInputElement[] {
  return Array.from(anchor.querySelectorAll<HTMLInputElement>('input[type=radio]')).filter((r) => r.closest('[data-kz]') === anchor);
}

function sentinelOf(anchor: HTMLElement): HTMLInputElement | null {
  return ownRadios(anchor).find((r) => r.getAttribute('data-ka') === 'x') || null;
}

/** True when the overlay is currently open. */
export function isOverlayOpen(target: OverlayTarget): boolean {
  const a = anchorOf(target);
  const x = a && sentinelOf(a);
  return !!x && !x.checked;
}

/**
 * Opens the overlay: the content whose `value` matches, else the first content.
 * Returns false when the target is not an overlay or the value does not exist.
 */
export function openOverlay(target: OverlayTarget, value?: string): boolean {
  const a = anchorOf(target);
  const x = a && sentinelOf(a);
  if (!a || !x) return false;
  const radios = ownRadios(a).filter((r) => r !== x);
  const radio = value === undefined ? radios[0] : radios.find((r) => r.value === value);
  if (!radio) return false;
  if (!radio.checked) radio.click();
  return true;
}

/** Closes the overlay. Returns false when the target is not an overlay. */
export function closeOverlay(target: OverlayTarget): boolean {
  const a = anchorOf(target);
  const x = a && sentinelOf(a);
  if (!a || !x) return false;
  if (!x.checked) x.click();
  return true;
}

/** Sets the open state; `open === true` opens the first content. */
export function setOverlayOpen(target: OverlayTarget, open: boolean, value?: string): boolean {
  return open ? openOverlay(target, value) : closeOverlay(target);
}

/**
 * Subscribes to open/close changes (user-driven and programmatic). Returns an
 * unsubscribe function, or a no-op when the target is not an overlay.
 */
export function onOverlayChange(target: OverlayTarget, callback: (change: OverlayChange) => void): () => void {
  const a = anchorOf(target);
  if (!a) return () => {};
  const handler = (e: Event) => {
    if (e.target !== a) return;
    callback((e as CustomEvent<OverlayChange>).detail);
  };
  a.addEventListener('krateopenchange', handler);
  return () => a.removeEventListener('krateopenchange', handler);
}
