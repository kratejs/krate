import './alert-dialog.css';
import { createSignal, createEffect, onCleanup } from '@krate/runtime';

export interface AlertDialogProps {
  children?: any;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}

export function AlertDialog(props: AlertDialogProps) {
  var [open, setOpen] = createSignal(props.open || false);

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
    var trigger = (e.target as HTMLElement).closest("[data-krate-alert-dialog-trigger]");
    if (trigger) {
      setOpen(true);
      if (props.onOpenChange) {
        props.onOpenChange(true);
      }
      return;
    }
    var action = (e.target as HTMLElement).closest("[data-krate-alert-dialog-action]");
    if (action) {
      close();
      return;
    }
    var cancel = (e.target as HTMLElement).closest("[data-krate-alert-dialog-cancel]");
    if (cancel) {
      close();
      return;
    }
    var wrapper = (e.target as HTMLElement).closest("[data-krate-alert-dialog-content-wrapper]");
    if (wrapper && (e.target as HTMLElement).classList.contains("krate-alert-dialog-content-wrapper")) {
      close();
    }
  }

  createEffect(function () {
    if (open()) {
      document.addEventListener("click", handleClick);
    }
    return function () {
      document.removeEventListener("click", handleClick);
    };
  });

  return (
    <div class="krate-alert-dialog" data-state={open() ? "open" : "closed"}>
      {props.children}
    </div>
  );
}

export interface AlertDialogTriggerProps {
  children?: any;
}

export function AlertDialogTrigger(props: AlertDialogTriggerProps) {
  return (
    <button class="krate-alert-dialog-trigger" type="button" data-krate-alert-dialog-trigger="true">
      {props.children}
    </button>
  );
}

export interface AlertDialogPortalProps {
  children?: any;
}

export function AlertDialogPortal(props: AlertDialogPortalProps) {
  return (<div class="krate-alert-dialog-portal">{props.children}</div>);
}

export interface AlertDialogOverlayProps {
  children?: any;
}

export function AlertDialogOverlay(props: AlertDialogOverlayProps) {
  return (
    <div class="krate-alert-dialog-overlay">
      {props.children}
    </div>
  );
}

export interface AlertDialogContentProps {
  children?: any;
}

export function AlertDialogContent(props: AlertDialogContentProps) {
  return (
    <div class="krate-alert-dialog-content-wrapper" data-krate-alert-dialog-content-wrapper="true">
      <div class="krate-alert-dialog-overlay"></div>
      <div class="krate-alert-dialog-content" role="alertdialog">
        {props.children}
      </div>
    </div>
  );
}

export interface AlertDialogTitleProps {
  children?: any;
}

export function AlertDialogTitle(props: AlertDialogTitleProps) {
  return (
    <div class="krate-alert-dialog-title">{props.children}</div>
  );
}

export interface AlertDialogDescriptionProps {
  children?: any;
}

export function AlertDialogDescription(props: AlertDialogDescriptionProps) {
  return (
    <div class="krate-alert-dialog-description">{props.children}</div>
  );
}

export interface AlertDialogActionProps {
  children?: any;
  onClick?: () => void;
}

export function AlertDialogAction(props: AlertDialogActionProps) {
  return (
    <button class="krate-alert-dialog-action krate-alert-dialog-action-confirm" type="button" data-krate-alert-dialog-action="true" onClick={props.onClick}>
      {props.children}
    </button>
  );
}

export interface AlertDialogCancelProps {
  children?: any;
  onClick?: () => void;
}

export function AlertDialogCancel(props: AlertDialogCancelProps) {
  return (
    <button class="krate-alert-dialog-action krate-alert-dialog-action-cancel" type="button" data-krate-alert-dialog-cancel="true" onClick={props.onClick}>
      {props.children}
    </button>
  );
}
