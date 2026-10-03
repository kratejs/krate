import './sheet.css';
import { createSignal, createEffect, onCleanup } from '@krate/runtime';

export interface SheetProps {
  children?: any;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  side?: 'left' | 'right' | 'top' | 'bottom';
}

// Sheet is a dialog-style panel anchored to a viewport edge (drawer). It reuses
// the delegated-click open/close contract of Dialog under its own data
// attributes so the two never interfere.
export function Sheet(props: SheetProps) {
  var [open, setOpen] = createSignal(props.open || false);
  var rootRef: HTMLElement | null = null;

  createEffect(function () {
    if (props.open !== undefined) {
      setOpen(props.open);
    }
  });

  function close() {
    setOpen(false);
    if (props.onOpenChange) props.onOpenChange(false);
  }

  function handleClick(e: MouseEvent) {
    var target = e.target as HTMLElement;
    var root = target.closest("[data-krate-sheet]") as HTMLElement;
    if (root) rootRef = root;
    if (target.closest("[data-krate-sheet-trigger]")) {
      setOpen(true);
      if (props.onOpenChange) props.onOpenChange(true);
      return;
    }
    if (target.closest("[data-krate-sheet-close]")) {
      close();
      return;
    }
    if (target.classList.contains("krate-sheet-overlay")) {
      close();
    }
  }

  function handleKey(e: KeyboardEvent) {
    if (e.key === "Escape" && open()) close();
  }

  createEffect(function () {
    document.addEventListener("click", handleClick);
    document.addEventListener("keydown", handleKey);
    return function () {
      document.removeEventListener("click", handleClick);
      document.removeEventListener("keydown", handleKey);
    };
  });

  return (
    <div class="krate-sheet" data-state={open() ? "open" : "closed"} data-krate-sheet="true">
      {props.children}
    </div>
  );
}

export function SheetTrigger(props: { children?: any }) {
  return (
    <button class="krate-sheet-trigger" type="button" data-krate-sheet-trigger="true">
      {props.children}
    </button>
  );
}

export function SheetContent(props: { children?: any; side?: 'left' | 'right' | 'top' | 'bottom' }) {
  var side = props.side || "right";
  return (
    <div class="krate-sheet-content-wrapper" data-krate-sheet-content-wrapper="true">
      <div class="krate-sheet-overlay"></div>
      <div class={"krate-sheet-content krate-sheet-" + side} role="dialog" aria-modal="true">
        {props.children}
      </div>
    </div>
  );
}

export function SheetTitle(props: { children?: any }) {
  return <div class="krate-sheet-title">{props.children}</div>;
}

export function SheetDescription(props: { children?: any }) {
  return <div class="krate-sheet-description">{props.children}</div>;
}

export function SheetClose(props: { children?: any }) {
  return (
    <button class="krate-sheet-close" type="button" data-krate-sheet-close="true" aria-label="Close">
      {props.children ? props.children : (
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M18 6 6 18" /><path d="m6 6 12 12" />
        </svg>
      )}
    </button>
  );
}
