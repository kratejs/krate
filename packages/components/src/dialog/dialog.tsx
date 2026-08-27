import './dialog.css';
import { createSignal, createEffect, onCleanup } from '@krate/runtime';

export interface DialogProps {
  children?: any;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}

export function Dialog(props: DialogProps) {
  var [open, setOpen] = createSignal(props.open || false);
  var rootRef: HTMLElement | null = null;

  createEffect(function () {
    if (props.open !== undefined) {
      setOpen(props.open);
    }
  });

  function close() {
    setOpen(false);
    if (props.onOpenChange) {
      props.onOpenChange(false);
    }
  }

  function handleClick(e: MouseEvent) {
    var target = e.target as HTMLElement;
    var root = target.closest("[data-krate-dialog]") as HTMLElement;
    if (root) rootRef = root;

    var trigger = target.closest("[data-krate-dialog-trigger]");
    if (trigger) {
      setOpen(true);
      if (props.onOpenChange) {
        props.onOpenChange(true);
      }
      return;
    }
    var closeBtn = target.closest("[data-krate-dialog-close]");
    if (closeBtn) {
      close();
      return;
    }
    if (target.classList.contains("krate-dialog-overlay")) {
      close();
      return;
    }
  }

  function handleClickOutside(e: MouseEvent) {
    var r = rootRef;
    if (r && !r.contains(e.target as Node)) {
      close();
    }
  }

  createEffect(function () {
    document.addEventListener("click", handleClick);
    var outsideTimer: any = null;
    if (open()) {
      outsideTimer = setTimeout(function () {
        document.addEventListener("click", handleClickOutside);
      }, 0);
    }
    return function () {
      document.removeEventListener("click", handleClick);
      if (outsideTimer) clearTimeout(outsideTimer);
      document.removeEventListener("click", handleClickOutside);
    };
  });

  return (
    <div class="krate-dialog" data-state={open() ? "open" : "closed"} data-krate-dialog="true">
      {props.children}
    </div>
  );
}

export interface DialogTriggerProps {
  children?: any;
}

export function DialogTrigger(props: DialogTriggerProps) {
  return (
    <button class="krate-dialog-trigger" type="button" data-krate-dialog-trigger="true">
      {props.children}
    </button>
  );
}

export interface DialogPortalProps {
  children?: any;
}

export function DialogPortal(props: DialogPortalProps) {
  return <span class="krate-dialog-portal">{props.children}</span>;
}

export interface DialogOverlayProps {
}

export function DialogOverlay(props: DialogOverlayProps) {
  return (
    <div class="krate-dialog-overlay"></div>
  );
}

export interface DialogContentProps {
  children?: any;
}

export function DialogContent(props: DialogContentProps) {
  return (
    <div class="krate-dialog-content-wrapper" data-krate-dialog-content-wrapper="true">
      <div class="krate-dialog-overlay"></div>
      <div class="krate-dialog-content" role="dialog" aria-modal="true">
        {props.children}
      </div>
    </div>
  );
}

export interface DialogTitleProps {
  children?: any;
}

export function DialogTitle(props: DialogTitleProps) {
  return <div class="krate-dialog-title">{props.children}</div>;
}

export interface DialogDescriptionProps {
  children?: any;
}

export function DialogDescription(props: DialogDescriptionProps) {
  return <div class="krate-dialog-description">{props.children}</div>;
}

export interface DialogCloseProps {
  children?: any;
}

export function DialogClose(props: DialogCloseProps) {
  return (
    <button class="krate-dialog-close" type="button" data-krate-dialog-close="true">
      {props.children ? props.children : (
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M18 6 6 18"/><path d="m6 6 12 12"/>
        </svg>
      )}
    </button>
  );
}
